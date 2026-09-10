package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/schollz/progressbar/v3"
)

func ensureSqlc() (string, error) {
	// 1. Check PATH.
	if p, err := exec.LookPath("sqlc"); err == nil {
		if verbose {
			fmt.Printf("[debug] using sqlc from PATH: %s\n", p)
		}
		return p, nil
	}

	// 2. Cache.
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sqlcDir := filepath.Join(cacheDir, "ftpl", "sqlc")
	sqlcPath := filepath.Join(sqlcDir, "sqlc")
	if runtime.GOOS == "windows" {
		sqlcPath += ".exe"
	}

	if _, err := os.Stat(sqlcPath); err == nil {
		if verbose {
			fmt.Printf("[debug] using cached sqlc: %s\n", sqlcPath)
		}
		return sqlcPath, nil
	}

	// 3. Download latest.
	tag, err := sqlcLatestTag()
	if err != nil {
		return "", err
	}

	resp, err := httpGetFirstOK(sqlcReleaseURLs(tag))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := os.MkdirAll(sqlcDir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(sqlcDir, "sqlc-*.archive")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { tmp.Close(); os.Remove(tmpPath) }()

	// Progress bar when downloading archive (tar.gz / zip).
	bar := progressbar.DefaultBytes(resp.ContentLength, "Downloading sqlc "+tag)
	if _, err := io.Copy(io.MultiWriter(tmp, bar), resp.Body); err != nil {
		return "", err
	}
	bar.Finish()
	fmt.Println()

	tmp.Close()

	if err := extractSqlc(tmpPath, sqlcPath); err != nil {
		return "", err
	}
	if err := os.Chmod(sqlcPath, 0o755); err != nil {
		return "", err
	}

	fmt.Printf("✓ sqlc %s cached at %s\n", tag, sqlcPath)
	return sqlcPath, nil
}

func sqlcLatestTag() (string, error) {
	resp, err := httpClient.Get("https://api.github.com/repos/sqlc-dev/sqlc/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query sqlc latest release: status %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no latest sqlc release found")
	}
	return rel.TagName, nil
}

func sqlcReleaseURL(tag string) string {
	ver := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("https://github.com/sqlc-dev/sqlc/releases/download/%s/sqlc_%s_%s_%s.%s",
		tag, ver, runtime.GOOS, runtime.GOARCH, ext)
}

func sqlcReleaseURLs(tag string) []string {
	return []string{sqlcReleaseURL(tag)}
}

func extractSqlc(archivePath, destPath string) error {
	if runtime.GOOS == "windows" {
		return extractZip(archivePath, destPath)
	}
	return extractTarGz(archivePath, destPath)
}

func extractTarGz(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "sqlc" {
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			return err
		}
	}
	return fmt.Errorf("sqlc binary not found in archive")
}

func extractZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == "sqlc.exe" {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				rc.Close()
				return err
			}
			_, err = io.Copy(out, rc)
			rc.Close()
			out.Close()
			return err
		}
	}
	return fmt.Errorf("sqlc.exe not found in archive")
}
