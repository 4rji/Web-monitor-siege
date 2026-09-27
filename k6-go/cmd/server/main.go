// Command server serves a web UI for running k6 load tests with the local k6
// binary.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"k6web/internal/k6bin"
	"k6web/internal/k6runner"
	"k6web/web"
)

// portAttempts is how many consecutive ports are tried when the requested one
// is already in use.
const portAttempts = 20

func main() {
	port := flag.Int("port", 0, "port to listen on (default 8080, or the port in ADDR); if busy, the next free port is used")
	flag.Parse()

	host, basePort := parseAddr(env("ADDR", ":8080"))
	if *port != 0 {
		basePort = *port
	}
	if basePort < 1 || basePort > 65535 {
		log.Fatalf("invalid port %d", basePort)
	}

	limits := k6runner.Limits{
		MaxVUs:      envInt("MAX_VUS", 200),
		MaxDuration: envDuration("MAX_DURATION", 10*time.Minute),
	}

	k6Bin, err := findK6()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("using k6 at %s", k6Bin)

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web.Files))
	mux.Handle("POST /api/run", &runHandler{k6Bin: k6Bin, limits: limits, busy: make(chan struct{}, 1)})

	ln, err := listen(host, basePort)
	if err != nil {
		log.Fatal(err)
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	if actual != basePort {
		log.Printf("port %d is in use, using %d instead", basePort, actual)
	}
	log.Printf("max VUs %d, max duration %s", limits.MaxVUs, limits.MaxDuration)
	for _, u := range urls(host, actual) {
		log.Printf("open %s", u)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(srv.Serve(ln))
}

// parseAddr splits an ADDR value like ":8080" or "127.0.0.1:9090".
func parseAddr(addr string) (string, int) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatalf("invalid ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		log.Fatalf("invalid port in ADDR %q", addr)
	}
	return host, port
}

// listen opens port, or the next free one if it is already in use.
func listen(host string, port int) (net.Listener, error) {
	var err error
	for p := port; p < port+portAttempts && p <= 65535; p++ {
		var ln net.Listener
		ln, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err == nil {
			return ln, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free port between %d and %d: %w", port, port+portAttempts-1, err)
}

// urls lists where the UI can be opened: localhost plus this machine's LAN
// addresses when listening on all interfaces.
func urls(host string, port int) []string {
	p := strconv.Itoa(port)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{"http://" + net.JoinHostPort(host, p)}
	}
	list := []string{"http://localhost:" + p}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			list = append(list, "http://"+net.JoinHostPort(ipn.IP.String(), p))
		}
	}
	return list
}

// event is one line of the NDJSON stream returned by /api/run.
type event struct {
	Type   string           `json:"type"` // "output" or "result"
	Text   string           `json:"text,omitempty"`
	Result *k6runner.Result `json:"result,omitempty"`
}

type runHandler struct {
	k6Bin  string
	limits k6runner.Limits
	busy   chan struct{} // allows one test at a time
}

func (h *runHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req k6runner.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	params, err := req.Validate(h.limits)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	select {
	case h.busy <- struct{}{}:
		defer func() { <-h.busy }()
	default:
		jsonError(w, http.StatusConflict, "another test is already running, try again when it finishes")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	sw := &streamWriter{enc: json.NewEncoder(w), rc: http.NewResponseController(w)}

	log.Printf("run start: %s vus=%d duration=%s", params.URL, params.VUs, params.Duration)
	res := k6runner.Run(r.Context(), h.k6Bin, params, sw)
	sw.flushRemainder()
	log.Printf("run end: %s success=%t exit=%d %s", params.URL, res.Success, res.ExitCode, res.Error)

	sw.send(event{Type: "result", Result: &res})
}

// streamWriter forwards k6 output to the client as NDJSON "output" events,
// one event per batch of complete lines so multi-byte characters are never
// split.
type streamWriter struct {
	enc *json.Encoder
	rc  *http.ResponseController
	buf []byte
}

func (s *streamWriter) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	if i := bytes.LastIndexByte(s.buf, '\n'); i >= 0 {
		s.send(event{Type: "output", Text: string(s.buf[:i+1])})
		s.buf = append(s.buf[:0], s.buf[i+1:]...)
	}
	// Never report errors: if the client disconnected, the request context
	// is cancelled and k6 is stopped through that path.
	return len(p), nil
}

func (s *streamWriter) flushRemainder() {
	if len(s.buf) > 0 {
		s.send(event{Type: "output", Text: string(s.buf)})
		s.buf = s.buf[:0]
	}
}

func (s *streamWriter) send(e event) {
	if err := s.enc.Encode(e); err == nil {
		s.rc.Flush()
	}
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// findK6 uses K6_BIN when set; otherwise k6 from PATH, downloading it if
// it is not installed.
func findK6() (string, error) {
	if bin := os.Getenv("K6_BIN"); bin != "" {
		p, err := exec.LookPath(bin)
		if err != nil {
			return "", fmt.Errorf("K6_BIN=%q not found: %w", bin, err)
		}
		return p, nil
	}
	return k6bin.Find()
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		log.Fatalf("%s must be a positive integer", key)
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Second {
		log.Fatalf("%s must be a duration of at least 1s (e.g. 10m)", key)
	}
	return d
}
