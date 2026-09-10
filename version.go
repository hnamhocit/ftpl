package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Build metadata, injected via -ldflags by install.sh / self-update.
// A plain `go build` leaves these at dev defaults.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// repoURL is the canonical source; FTPL_REPO overrides it (same env as install.sh).
func repoURL() string {
	if v := os.Getenv("FTPL_REPO"); v != "" {
		return v
	}
	return "https://github.com/hnamhocit/ftpl"
}

// ---------- commands ----------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version, then offer an update if a newer tag exists",
	Run:   runVersion,
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update ftpl to the latest tagged release",
	Run:   runUpdate,
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("ftpl %s (commit %s, built %s, go %s)\n",
		version, commit, date, runtime.Version())

	latest, err := latestTagBounded(8 * time.Second)
	if err != nil {
		if verbose {
			fmt.Printf("[debug] update check skipped: %v\n", err)
		}
		return // offline / no git: version still printed, check silently skipped
	}
	if latest == "" {
		fmt.Println("  (no release tags found in the repo yet)")
		return
	}

	// Dev builds track main, which can be AHEAD of the latest tag:
	// never offer an "update" that is actually a downgrade.
	if version == "dev" {
		fmt.Printf("  local dev build; latest release tag: %s (run `ftpl update` to install it)\n", latest)
		return
	}

	if !isNewer(latest, version) {
		fmt.Printf("✓ up to date (latest tag: %s)\n", latest)
		return
	}

	fmt.Printf("↑ newer version available: %s (you are on %s)\n", latest, version)
	if confirmPrompt("Update now?", false) {
		if err := selfUpdate(latest); err != nil {
			fatal("update", err)
		}
	} else {
		fmt.Println("Skipped. Run `ftpl update` whenever ready.")
	}
}

func runUpdate(cmd *cobra.Command, args []string) {
	latest, err := latestTagBounded(8 * time.Second)
	if err != nil {
		fatal("update check", err)
	}
	if latest == "" {
		fatal("update", fmt.Errorf("no release tags found in %s", repoURL()))
	}
	if version != "dev" && !isNewer(latest, version) {
		fmt.Printf("✓ already on %s (latest tag: %s)\n", version, latest)
		return
	}
	if err := selfUpdate(latest); err != nil {
		fatal("update", err)
	}
}

// ---------- automatic per-session update check ----------

// updateState persists the per-session update-check result.
// "Session" = one shell process (its PID): the first ftpl command of a
// terminal checks for a new release; later commands in the same terminal
// reuse the cached result. New terminal = new session = check again.
type updateState struct {
	SessionPID int       `json:"session_pid"`
	CheckedAt  time.Time `json:"checked_at"`
	Latest     string    `json:"latest"`
	Notified   bool      `json:"notified"`
}

func updateStatePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ftpl", "update.json")
	}
	return filepath.Join(dir, "ftpl", "update.json")
}

func loadUpdateState() updateState {
	var s updateState
	b, err := os.ReadFile(updateStatePath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func saveUpdateState(s updateState) {
	p := updateStatePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := json.Marshal(s)
	_ = os.WriteFile(p, b, 0o644)
}

// checkUpdateOncePerSession runs the release check on the first ftpl command
// of each shell session and prints a one-line notice when a newer tag exists.
// Hooked into PersistentPreRun so EVERY command gets this for free.
func checkUpdateOncePerSession(cmd *cobra.Command) {
	// Opt-out for scripts/CI paranoia, and the commands that already handle it.
	if os.Getenv("FTPL_NO_UPDATE_CHECK") != "" {
		return
	}
	if cmd.Name() == "version" || cmd.Name() == "update" {
		return
	}
	if !stdoutIsTTY() {
		return // piped output / CI: never phone home, never add latency
	}
	if version == "dev" {
		return // dev builds track main; latest tag could be a downgrade
	}

	ppid := os.Getppid()
	st := loadUpdateState()

	if st.SessionPID != ppid {
		// First command of a new session: check now (bounded, ~0.5s).
		latest, err := latestTagBounded(4 * time.Second)
		st = updateState{SessionPID: ppid, CheckedAt: time.Now()}
		if err == nil {
			st.Latest = latest
		}
		// On error Latest stays "": silent this session, retried next session.
		saveUpdateState(st)
	}

	if st.Latest != "" && isNewer(st.Latest, version) && !st.Notified {
		fmt.Printf("↑ ftpl %s available (you are on %s) — run `ftpl update`\n\n",
			st.Latest, version)
		st.Notified = true
		saveUpdateState(st)
	}
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// ---------- remote tag lookup ----------

// latestTagBounded lists remote tags via git ls-remote (no API rate limits)
// and returns the highest vX.Y.Z, within the given timeout.
func latestTagBounded(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "ls-remote", "--tags", repoURL()).Output()
	if err != nil {
		return "", err
	}
	best := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := strings.TrimPrefix(fields[1], "refs/tags/")
		if strings.HasSuffix(ref, "^{}") { // annotated-tag peel duplicate
			continue
		}
		if !strings.HasPrefix(ref, "v") {
			continue
		}
		if isNewer(ref, best) {
			best = ref
		}
	}
	return best, nil
}

// isNewer reports whether tag a is strictly newer than tag b ("" = none yet).
func isNewer(a, b string) bool {
	if b == "" {
		return a != ""
	}
	av, bv := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

// parseSemver extracts the numeric core of "v1.2.3[-rc1]".
func parseSemver(tag string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		digits := ""
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		n, _ := strconv.Atoi(digits)
		out[i] = n
	}
	return out
}

// ---------- self update ----------

// selfUpdate clones the tagged source, builds it with the same ldflags
// recipe as install.sh, and swaps the running binary in place.
func selfUpdate(tag string) error {
	tmp, err := os.MkdirTemp("", "ftpl-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "src")
	fmt.Printf("→ cloning %s @ %s\n", repoURL(), tag)
	if err := streamCmd("", "git", "clone", "--depth", "1", "--branch", tag, repoURL(), src); err != nil {
		return err
	}

	sha := shortSHA(src)
	newBin := filepath.Join(tmp, "ftpl-new")
	fmt.Println("→ building...")
	if err := streamCmd(src, "go", "build",
		"-ldflags", fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.date=%s",
			tag, sha, buildDate()),
		"-o", newBin, "."); err != nil {
		return err
	}

	if err := replaceBinary(newBin); err != nil {
		return err
	}
	fmt.Printf("✓ updated to %s — re-run your command\n", tag)
	return nil
}

// replaceBinary swaps the running executable. Renaming a running binary is
// safe on Unix AND Windows (the old process keeps its open handle/inode).
// When /tmp and install dir are on different mounts (EXDEV), fall back to
// copy + delete — the user sees no difference.
func replaceBinary(newBin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("move current binary aside: %w", err)
	}

	// Try fast rename first (same filesystem).
	if err := os.Rename(newBin, exe); err != nil {
		// Cross-device link (e.g. /tmp -> /home): copy + delete.
		if isCrossDevice(err) {
			if cerr := copyFile(newBin, exe); cerr != nil {
				_ = os.Rename(old, exe) // roll back
				return fmt.Errorf("copy new binary: %w", cerr)
			}
		} else {
			_ = os.Rename(old, exe) // roll back
			return fmt.Errorf("install new binary: %w", err)
		}
	}

	_ = os.Chmod(exe, 0o755)
	_ = os.Remove(old) // fails on Windows while process lives; cleaned next update
	_ = os.Remove(newBin)
	return nil
}

// isCrossDevice detects EXDEV (cross-device link) errors from rename(2).
func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	// Linux/Darwin: "invalid cross-device link" or syscall.EXDEV
	return strings.Contains(err.Error(), "cross-device link") ||
		strings.Contains(err.Error(), "EXDEV")
}

// copyFile copies src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func streamCmd(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func shortSHA(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func buildDate() string { return time.Now().UTC().Format(time.RFC3339) }
