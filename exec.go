package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func runAtlas(atlasPath string, args ...string) error {
	if verbose {
		fmt.Printf("[debug] $ %s %s\n", atlasPath, strings.Join(args, " "))
	}
	cmd := exec.Command(atlasPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runSqlc(sqlcPath string, args ...string) error {
	if verbose {
		fmt.Printf("[debug] $ %s %s\n", sqlcPath, strings.Join(args, " "))
	}
	cmd := exec.Command(sqlcPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// mustAtlas là shortcut cho pattern ensureAtlas + help + fatal.
func mustAtlas() string {
	atlas, err := ensureAtlas()
	if err != nil {
		atlasMissingHelp()
		fatal("ensure atlas", err)
	}
	return atlas
}

// runSqlcGenerate dùng chung cho dev/reset/push.
func runSqlcGenerate() {
	sqlcPath, err := ensureSqlc()
	if err != nil {
		fatal("ensure sqlc", err)
	}
	if err := runSqlc(sqlcPath, "generate"); err != nil {
		fatal("sqlc generate", err)
	}
	fmt.Println("✓ Generated structs (sqlc)")
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "ftpl: %s: %v\n", msg, err)
	os.Exit(1)
}

func atlasMissingHelp() {
	fmt.Fprintln(os.Stderr, "\nftpl: failed to auto-install atlas")
	fmt.Fprintln(os.Stderr, "Install manually:")
	switch runtime.GOOS {
	case "darwin":
		fmt.Fprintln(os.Stderr, "  brew install ariga/tap/atlas")
	case "linux":
		fmt.Fprintln(os.Stderr, "  curl -sSf https://atlasgo.sh | sh")
	default:
		fmt.Fprintln(os.Stderr, "  See https://atlasgo.io/installation")
	}
}
