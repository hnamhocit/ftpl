package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

const minAtlasVersion = "0.30.0"

var httpClient = &http.Client{Timeout: 2 * time.Minute}

var atlasBaseURLs = []string{
	"https://atlasbinaries.com/atlas",
	"https://release.ariga.io/atlas",
}

func ensureAtlas() (string, error) {
	// 1. Atlas cài sẵn trên máy: kiểm tra min version.
	if p, err := exec.LookPath("atlas"); err == nil {
		v, verr := binaryVersion(p)
		if verr != nil {
			return "", fmt.Errorf("cannot read atlas version: %w", verr)
		}
		if !versionAtLeast(v, minAtlasVersion) {
			return "", fmt.Errorf("atlas %s is too old (ftpl needs >= %s)\n  update: https://atlasgo.io/installation", v, minAtlasVersion)
		}
		if verbose {
			fmt.Printf("[debug] using atlas %s from PATH\n", v)
		}
		return p, nil
	}

	// 2. Cache.
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	atlasDir := filepath.Join(cacheDir, "ftpl", "atlas")
	atlasPath := filepath.Join(atlasDir, "atlas")
	if runtime.GOOS == "windows" {
		atlasPath += ".exe"
	}

	if _, err := os.Stat(atlasPath); err == nil {
		v, verr := binaryVersion(atlasPath)
		if verr != nil || !versionAtLeast(v, minAtlasVersion) {
			if verbose {
				fmt.Printf("[debug] cached atlas invalid (ver=%q err=%v), redownloading\n", v, verr)
			}
			os.Remove(atlasPath)
		} else {
			if verbose {
				fmt.Printf("[debug] using cached atlas %s\n", v)
			}
			return atlasPath, nil
		}
	}

	// 3. Download latest — có progress bar.
	if err := os.MkdirAll(atlasDir, 0o755); err != nil {
		return "", err
	}

	resp, err := httpGetFirstOK(atlasReleaseURLs())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(atlasDir, "atlas-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { tmp.Close(); os.Remove(tmpPath) }()

	// Progress bar: dùng Content-Length header để biết tổng size.
	// Nếu server không gửi header (ContentLength == -1), bar sẽ hiện bytes downloaded thôi (không %).
	bar := progressbar.DefaultBytes(resp.ContentLength, "Downloading atlas")
	if _, err := io.Copy(io.MultiWriter(tmp, bar), resp.Body); err != nil {
		return "", err
	}
	bar.Finish()
	fmt.Println() // xuống dòng sau khi bar hoàn thành

	tmp.Close()
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, atlasPath); err != nil {
		return "", err
	}

	if v, err := binaryVersion(atlasPath); err == nil {
		fmt.Printf("✓ Atlas %s cached at %s\n", v, atlasPath)
	}
	return atlasPath, nil
}

func atlasReleaseURLs() []string {
	out := make([]string, 0, len(atlasBaseURLs))
	for _, base := range atlasBaseURLs {
		out = append(out, fmt.Sprintf("%s/atlas-%s-%s-latest", base, runtime.GOOS, runtime.GOARCH))
	}
	return out
}

func httpGetFirstOK(urls []string) (*http.Response, error) {
	var lastErr error
	for _, u := range urls {
		resp, err := httpClient.Get(u)
		if err != nil {
			lastErr = err
			if verbose {
				fmt.Printf("[debug] GET %s failed: %v\n", u, err)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("status %d from %s", resp.StatusCode, u)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("all atlas download URLs failed; last error: %w", lastErr)
}

// ---------- version helpers ----------

func binaryVersion(path string) (string, error) {
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	if v := parseVersion(string(out)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("cannot parse version from: %s", strings.TrimSpace(string(out)))
}

func parseVersion(s string) string {
	for _, tok := range strings.Fields(s) {
		tok = strings.TrimPrefix(tok, "v")
		if v, ok := takeSemver(tok); ok {
			return v
		}
	}
	return ""
}

func takeSemver(s string) (string, bool) {
	end, dots := 0, 0
	for end < len(s) {
		c := s[end]
		if c >= '0' && c <= '9' {
			end++
			continue
		}
		if c == '.' && dots < 2 {
			dots++
			end++
			continue
		}
		break
	}
	if dots != 2 || end == 0 {
		return "", false
	}
	return s[:end], true
}

func versionAtLeast(have, want string) bool {
	h, w := splitVersion(have), splitVersion(want)
	for i := 0; i < 3; i++ {
		if h[i] != w[i] {
			return h[i] > w[i]
		}
	}
	return true
}

func splitVersion(v string) [3]int {
	var out [3]int
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}
