package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ftpl binary and cached dependencies (atlas, sqlc)",
	Run:   runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolP("force", "y", false, "skip confirmation prompt")
}

func runUninstall(cmd *cobra.Command, args []string) {
	force, _ := cmd.Flags().GetBool("force")

	// 1. Find the running binary.
	exe, err := os.Executable()
	if err != nil {
		fatal("uninstall", fmt.Errorf("locate binary: %w", err))
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		fatal("uninstall", fmt.Errorf("resolve symlink: %w", err))
	}

	// 2. Find the cache directory (atlas, sqlc, update.json).
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = filepath.Join(os.TempDir(), "ftpl")
	} else {
		cacheDir = filepath.Join(cacheDir, "ftpl")
	}

	fmt.Println("This will remove:")
	fmt.Printf("  • Binary: %s\n", exe)
	if _, err := os.Stat(cacheDir); err == nil {
		fmt.Printf("  • Cache:  %s (atlas, sqlc, update check)\n", cacheDir)
	}
	fmt.Println()

	if !force && !confirmPrompt("Continue?", false) {
		fmt.Println("Cancelled.")
		return
	}

	// 3. Remove cache first (independent of binary).
	if _, err := os.Stat(cacheDir); err == nil {
		if err := os.RemoveAll(cacheDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove cache: %v\n", err)
		} else {
			fmt.Printf("✓ Removed cache: %s\n", cacheDir)
		}
	}

	// 4. Remove binary.
	if err := os.Remove(exe); err != nil {
		fatal("uninstall", fmt.Errorf("remove binary: %w", err))
	}
	fmt.Printf("✓ Removed binary: %s\n", exe)

	fmt.Println()
	fmt.Println("Done. You may also want to remove the PATH entry from your shell rc (~/.zshrc, ~/.bashrc, etc.).")
}
