// Package k6runner validates load-test settings and runs them with the local
// k6 binary.
package k6runner

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

//go:embed script.js
var script []byte

// gracePeriod is added to the test duration for the overall timeout: k6 keeps
// running after `duration` to let iterations finish (gracefulStop, 30s by
// default) and to print the summary.
const gracePeriod = 60 * time.Second

// exitThresholdsFailed is the k6 exit code when the test ran but at least one
// threshold was crossed.
const exitThresholdsFailed = 99

// Request is the JSON body accepted by the API.
type Request struct {
	URL            string  `json:"url"`
	VUs            int     `json:"vus"`
	Duration       string  `json:"duration"`
	P95Ms          int     `json:"p95_ms"`
	MaxFailureRate float64 `json:"max_failure_rate"`
}

// Limits caps what a single test may request.
type Limits struct {
	MaxVUs      int
	MaxDuration time.Duration
}

// Params is a validated, normalized Request.
type Params struct {
	URL            string
	VUs            int
	Duration       time.Duration
	P95Ms          int
	MaxFailureRate float64
}

// Result describes how a run ended.
type Result struct {
	// Success is true only when k6 ran and every threshold passed.
	Success bool `json:"success"`
	// ThresholdsFailed is true when k6 ran fine but the target did not meet
	// the p95 or failure-rate limits.
	ThresholdsFailed bool   `json:"thresholds_failed"`
	ExitCode         int    `json:"exit_code"`
	Error            string `json:"error"`
}

// Validate checks r against l and returns normalized parameters.
func (r Request) Validate(l Limits) (Params, error) {
	var p Params

	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		return p, errors.New("url is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return p, fmt.Errorf("invalid url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return p, errors.New("url must use http or https")
	}
	if u.Host == "" {
		return p, errors.New("url must include a host")
	}
	p.URL = u.String()

	if r.VUs < 1 || r.VUs > l.MaxVUs {
		return p, fmt.Errorf("vus must be between 1 and %d", l.MaxVUs)
	}
	p.VUs = r.VUs

	d, err := parseDuration(r.Duration)
	if err != nil {
		return p, err
	}
	if d < time.Second || d > l.MaxDuration {
		return p, fmt.Errorf("duration must be between 1s and %s", l.MaxDuration)
	}
	p.Duration = d

	if r.P95Ms < 1 || r.P95Ms > 600000 {
		return p, errors.New("p95_ms must be between 1 and 600000")
	}
	p.P95Ms = r.P95Ms

	if !(r.MaxFailureRate > 0 && r.MaxFailureRate <= 1) {
		return p, errors.New("max_failure_rate must be greater than 0 and at most 1")
	}
	p.MaxFailureRate = r.MaxFailureRate

	return p, nil
}

// parseDuration accepts Go durations ("30s", "2m", "1m30s") or a bare number
// of seconds, truncated to whole seconds.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("invalid duration, use values like 30s, 2m or 1m30s")
	}
	return d.Truncate(time.Second), nil
}

// Run executes the test with k6Bin and writes k6's combined stdout/stderr to
// out. It blocks until k6 exits, ctx is cancelled, or the timeout is hit.
func Run(ctx context.Context, k6Bin string, p Params, out io.Writer) Result {
	scriptPath, err := writeScript()
	if err != nil {
		return Result{ExitCode: -1, Error: err.Error()}
	}
	defer os.Remove(scriptPath)

	ctx, cancel := context.WithTimeout(ctx, p.Duration+gracePeriod)
	defer cancel()

	cmd := exec.CommandContext(ctx, k6Bin, "run", "--no-color",
		"-e", "TARGET_URL="+p.URL,
		"-e", "VUS="+strconv.Itoa(p.VUs),
		"-e", fmt.Sprintf("DURATION=%ds", int(p.Duration.Seconds())),
		"-e", "P95_MS="+strconv.Itoa(p.P95Ms),
		"-e", "MAX_FAILURE_RATE="+strconv.FormatFloat(p.MaxFailureRate, 'f', -1, 64),
		scriptPath,
	)
	cmd.Stdout = out
	cmd.Stderr = out
	// Stop k6 with SIGINT so it still prints its summary; kill it if it
	// does not exit shortly after.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second

	err = cmd.Run()
	res := Result{ExitCode: cmd.ProcessState.ExitCode()}

	switch {
	case err == nil:
		res.Success = true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Error = "test exceeded its time limit and was stopped"
	case errors.Is(ctx.Err(), context.Canceled):
		res.Error = "test was cancelled"
	case res.ExitCode == exitThresholdsFailed:
		res.ThresholdsFailed = true
	case res.ExitCode > 0:
		res.Error = fmt.Sprintf("k6 exited with code %d", res.ExitCode)
	default:
		res.Error = err.Error()
	}
	return res
}

func writeScript() (string, error) {
	f, err := os.CreateTemp("", "k6-web-*.js")
	if err != nil {
		return "", fmt.Errorf("create temp script: %w", err)
	}
	if _, err := f.Write(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("write temp script: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("close temp script: %w", err)
	}
	return f.Name(), nil
}
