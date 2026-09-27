// Package k6bin locates the k6 binary. When k6 is not installed it downloads a
// pinned official release into the user cache directory, so nothing has to be
// installed by hand.
package k6bin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const version = "v2.3.0"

// checksums are the SHA-256 sums published in k6-v2.3.0-checksums.txt.
var checksums = map[string]string{
	"k6-v2.3.0-linux-amd64.tar.gz": "39c3117b6af817592dcd0ce4242105c0a7af10948c2a425306f0be8f7a8a8ab1",
	"k6-v2.3.0-linux-arm64.tar.gz": "5ca3433e8201da72a284aaa241a1bb5fb47f4abb4e384d39410ddd8062f49b90",
	"k6-v2.3.0-macos-amd64.zip":    "c83bb16f54f0676afa4ea235fd757f3892d307dbd1bd201450de83af31986654",
	"k6-v2.3.0-macos-arm64.zip":    "b2417a3038edc5fe81dc178a889237724b595c5c9cfed875822008e46e862c7d",
	"k6-v2.3.0-windows-amd64.zip":  "112276d495e5741c968e2bc09ea6196099c1275bd6db9ee0875d173c7148ce43",
}

// Find returns the path of a usable k6 binary: k6 from PATH if installed,
// otherwise a cached copy, downloaded on first use.
func Find() (string, error) {
	if p, err := exec.LookPath("k6"); err == nil {
		return p, nil
	}

	archive, err := archiveName()
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache directory: %w", err)
	}
	dir := filepath.Join(cache, "k6-web", version)
	bin := filepath.Join(dir, exeName())
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}

	log.Printf("k6 not found in PATH, downloading %s to %s", archive, dir)
	if err := download(archive, dir, bin); err != nil {
		return "", fmt.Errorf("download k6: %w", err)
	}
	return bin, nil
}

func archiveName() (string, error) {
	osName, ext := runtime.GOOS, ".tar.gz"
	switch runtime.GOOS {
	case "darwin":
		osName, ext = "macos", ".zip"
	case "windows":
		ext = ".zip"
	}
	name := fmt.Sprintf("k6-%s-%s-%s%s", version, osName, runtime.GOARCH, ext)
	if _, ok := checksums[name]; !ok {
		return "", fmt.Errorf("no k6 download for %s/%s, install k6 manually or set K6_BIN", runtime.GOOS, runtime.GOARCH)
	}
	return name, nil
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "k6.exe"
	}
	return "k6"
}

func download(archive, dir, bin string) error {
	url := fmt.Sprintf("https://github.com/grafana/k6/releases/download/%s/%s", version, archive)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return err
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != checksums[archive] {
		return fmt.Errorf("checksum mismatch for %s: got %s", archive, got)
	}

	exe, err := extract(archive, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Write to a temp name and rename, so a half-written file is never used.
	tmp := bin + ".tmp"
	if err := os.WriteFile(tmp, exe, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, bin)
}

// extract returns the k6 executable from a release archive.
func extract(archive string, data []byte) ([]byte, error) {
	want := exeName()

	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("k6 executable not found in archive")
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("k6 executable not found in archive")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == want {
			return io.ReadAll(tr)
		}
	}
}
