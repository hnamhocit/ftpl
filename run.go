package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/swaggo/swag/gen"
)

// runCmd groups project lifecycle commands.
// These commands run in the USER's project root (go.mod + cmd/server),
// not inside the ftpl repository.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Project lifecycle: dev (live-reload server), build (release binary)",
}

var runDevCmd = &cobra.Command{
	Use:   "dev",
	Short: "Live-reload dev server: regenerates docs and restarts on every save",
	Long: "All-in-one development mode (air-style, bundled):\n" +
		"  - regenerates swagger docs in-process (swag as a library) on .go changes\n" +
		"  - rebuilds and restarts the server automatically\n" +
		"  - no manual build step ever; Ctrl-C to stop",
	Run: runDev,
}

var runBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Regenerate docs and build the release binary into bin/",
	Run:   runBuild,
}

func init() {
	runCmd.AddCommand(runDevCmd, runBuildCmd)
}

// skipWatchDirs are never watched.
// "docs" MUST be skipped: swag rewrites docs/docs.go on every regeneration,
// and watching it would create an infinite loop (write -> event -> regen -> write...).
var skipWatchDirs = map[string]bool{
	"docs": true, ".git": true, ".ftpl": true, "bin": true,
	"node_modules": true, ".idea": true, ".vscode": true, "tmp": true,
}

// requireTemplateProject fails early when run from the wrong directory.
func requireTemplateProject() {
	if _, err := os.Stat("go.mod"); err != nil {
		fatal("run", fmt.Errorf("go.mod not found — run this command from the root of a Fiber template project"))
	}
	if _, err := os.Stat(filepath.Join("cmd", "server", "main.go")); err != nil {
		fatal("run", fmt.Errorf("cmd/server/main.go not found — this is not a Fiber template project"))
	}
}

// regenDocs invokes swag as a library, so template users never install the swag CLI.
func regenDocs() error {
	return gen.New().Build(&gen.Config{
		SearchDir:       "./",
		MainAPIFile:     "./cmd/server/main.go",
		OutputDir:       "./docs",
		OutputTypes:     []string{"go", "json"},
		ParseDependency: true,
	})
}

// devBinaryPath is the throwaway binary used by dev mode.
// It lives in .ftpl/ (gitignored) and is removed on exit.
func devBinaryPath() string {
	p := filepath.Join(".ftpl", "server-dev")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	return p
}

// supervisor owns the running server process.
// The binary is executed directly (never via `go run`) so Kill() leaves no
// orphaned child processes behind.
type supervisor struct{ cmd *exec.Cmd }

func (s *supervisor) start(bin string) error {
	c := exec.Command(bin)
	c.Env = os.Environ()
	c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := c.Start(); err != nil {
		return err
	}
	s.cmd = c
	return nil
}

func (s *supervisor) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	s.cmd = nil
}

// ---------- ftpl run dev ----------

func runDev(cmd *cobra.Command, args []string) {
	requireTemplateProject()

	ctx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSig()

	bin := devBinaryPath()
	defer os.Remove(bin) // leave no trace behind
	var sup supervisor

	// 1. Initial docs generation.
	fmt.Println("[docs] generating...")
	if err := regenDocs(); err != nil {
		fatal("docs", err)
	}

	// 2. Initial compile + start. Users never run a build command in dev mode.
	fmt.Println("[build] compiling server...")
	if err := compileTo(bin); err != nil {
		fatal("build", err)
	}
	if err := sup.start(bin); err != nil {
		fatal("server", err)
	}
	fmt.Println("[dev] watching — save any .go file to reload; Ctrl-C to stop")

	// 3. Filesystem watcher.
	w, err := fsnotify.NewWatcher()
	if err != nil {
		fatal("watcher", err)
	}
	defer w.Close()

	root, _ := os.Getwd()
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skipWatchDirs[d.Name()] {
			return filepath.SkipDir
		}
		return w.Add(p)
	})

	// Reload channel + debounce: saving 10 files in a burst triggers one reload.
	reload := make(chan struct{}, 1)
	notify := func() {
		select {
		case reload <- struct{}{}:
		default:
		}
	}
	go func() {
		for range reload {
			time.Sleep(400 * time.Millisecond)
			select {
			case <-reload: // drain events that arrived during the window
			default:
			}
			doReload(&sup, bin)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			sup.stop()
			fmt.Println("\n[dev] stopped")
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) {
				continue
			}
			if !strings.HasSuffix(ev.Name, ".go") {
				continue
			}
			notify()
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// doReload regenerates docs, rebuilds the binary and restarts the server.
// If the build fails, the PREVIOUS server keeps running (air-like behavior),
// so a typo never kills the dev session.
func doReload(sup *supervisor, bin string) {
	fmt.Println("[docs] regenerating...")
	if err := regenDocs(); err != nil {
		fmt.Fprintln(os.Stderr, "[docs] error:", err)
	}

	fmt.Println("[build] recompiling...")
	if err := compileTo(bin); err != nil {
		fmt.Fprintln(os.Stderr, "[build] failed — keeping previous server running")
		return
	}

	sup.stop()
	if err := sup.start(bin); err != nil {
		fmt.Fprintln(os.Stderr, "[server] restart failed:", err)
		return
	}
	fmt.Println("[dev] server restarted")
}

// compileTo builds ./cmd/server into bin.
func compileTo(bin string) error {
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	c := exec.Command("go", "build", "-o", bin, "./cmd/server")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

// ---------- ftpl run build ----------

// runBuild is the ONLY command that produces a deployable artifact.
func runBuild(cmd *cobra.Command, args []string) {
	requireTemplateProject()

	fmt.Println("[docs] generating...")
	if err := regenDocs(); err != nil {
		fatal("docs", err)
	}

	out := filepath.Join("bin", "server")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	c := exec.Command("go", "build", "-o", out, "./cmd/server")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		fatal("build", err)
	}
	fmt.Printf("✓ Built %s\n", out)
}
