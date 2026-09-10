package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check ftpl toolchain (go, git, atlas, sqlc)",
	Run: func(cmd *cobra.Command, args []string) {
		runDoctor()
	},
}

type check struct {
	Name string
	OK   bool
	Msg  string
	Help string
}

func runDoctor() {
	checks := []check{
		checkGo(),
		checkGit(),
		checkAtlas(),
		checkSqlc(),
	}

	pass := 0
	for _, c := range checks {
		if c.OK {
			fmt.Printf("  [ok]   %-6s %s\n", c.Name, c.Msg)
			pass++
		} else {
			fmt.Printf("  [fail] %-6s %s\n", c.Name, c.Msg)
			if c.Help != "" {
				fmt.Printf("         -> %s\n", c.Help)
			}
		}
	}

	fmt.Printf("\n  preset  dev_url=auto (shadow db per run, fallback %s)\n          schema=%s  dir=%s\n",
		defaultDevURL, migrateCfg.Schema, migrateCfg.Dir)

	fmt.Printf("\n%d/%d checks passed\n", pass, len(checks))
	if pass < len(checks) {
		os.Exit(1)
	}
}

func checkGo() check {
	if _, err := exec.LookPath("go"); err != nil {
		return check{"go", false, "not found", "https://go.dev/doc/install"}
	}
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return check{"go", false, "go found but broken: " + err.Error(), "https://go.dev/doc/install"}
	}
	return check{"go", true, strings.TrimSpace(string(out)), ""}
}

func checkGit() check {
	if _, err := exec.LookPath("git"); err != nil {
		return check{"git", false, "not found", "https://git-scm.com/downloads"}
	}
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return check{"git", false, "git found but broken: " + err.Error(), "https://git-scm.com/downloads"}
	}
	return check{"git", true, strings.TrimSpace(string(out)), ""}
}

func checkAtlas() check {
	p, err := ensureAtlas()
	if err != nil {
		return check{"atlas", false, "auto-install failed: " + err.Error(),
			"manual: curl -sSf https://atlasgo.sh | sh"}
	}
	v, _ := binaryVersion(p)
	return check{"atlas", true, v, ""}
}

func checkSqlc() check {
	p, err := ensureSqlc()
	if err != nil {
		return check{"sqlc", false, "auto-install failed: " + err.Error(),
			"manual: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest"}
	}
	out, err := exec.Command(p, "version").Output()
	if err != nil {
		return check{"sqlc", true, p, ""}
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return check{"sqlc", true, first, ""}
}
