package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:embed templates
var templateFS embed.FS

type ResourceData struct {
	Name       string
	NameSingle string
	TableName  string
	ModulePath string
	CRUD       bool
	Tests      bool
}

type WireData struct {
	ModulePath string
	Features   []string
}

func runGenerate(cmd *cobra.Command, args []string) {
	name := args[0]

	noTests, _ := cmd.Flags().GetBool("no-tests")
	noCRUD, _ := cmd.Flags().GetBool("no-crud")
	yes, _ := cmd.Flags().GetBool("yes")
	resType, _ := cmd.Flags().GetString("type")

	typ := resType
	if !cmd.Flags().Changed("type") && !yes {
		typ = askType()
	}
	if typ != "restful" {
		fmt.Printf("ftpl: %s transport is coming soon; only restful is available now\n", typ)
		os.Exit(1)
	}

	crud := !noCRUD
	if !cmd.Flags().Changed("no-crud") && !yes {
		crud = askBool("Generate basic CRUD methods?", true)
	}

	tests := !noTests
	if !cmd.Flags().Changed("no-tests") && !yes {
		tests = askBool("Generate test files?", true)
	}

	generateResource(ResourceData{
		Name:       strings.ToLower(name),
		NameSingle: strings.TrimSuffix(strings.ToLower(name), "s"),
		TableName:  strings.ToLower(name),
		ModulePath: detectModulePath(),
		CRUD:       crud,
		Tests:      tests,
	})
}

func generateResource(data ResourceData) {
	featureDir := filepath.Join("internal", "features", data.Name)
	if err := os.MkdirAll(featureDir, 0755); err != nil {
		fatal("creating directory", err)
	}

	files := map[string]string{
		"feature.go":    "feature.tmpl",
		"handler.go":    "handler.tmpl",
		"service.go":    "service.tmpl",
		"repository.go": "repository.tmpl",
		"dto.go":        "dto.tmpl",
	}
	if data.Tests {
		files["handler_test.go"] = "handler_test.tmpl"
		files["service_test.go"] = "service_test.tmpl"
		files["repository_test.go"] = "repository_test.tmpl"
	}

	for filename, tmplName := range files {
		if err := generateFile(featureDir, filename, tmplName, data); err != nil {
			fatal("generating "+filename, err)
		}
	}

	if err := regenerateWireFile(); err != nil {
		fatal("regenerating wire file", err)
	}

	mode := "skeleton"
	if data.CRUD {
		mode = "crud"
	}
	withTests := "no tests"
	if data.Tests {
		withTests = "tests"
	}
	fmt.Printf("✓ Generated feature: %s (restful, %s, %s)\n", data.Name, mode, withTests)
	fmt.Printf("  Location: %s\n", featureDir)
}

func deleteResource(name string) {
	name = strings.ToLower(name)
	featureDir := filepath.Join("internal", "features", name)
	if _, err := os.Stat(featureDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ftpl: feature %q does not exist\n", name)
		os.Exit(1)
	}
	if err := os.RemoveAll(featureDir); err != nil {
		fatal("deleting feature", err)
	}
	if err := regenerateWireFile(); err != nil {
		fatal("regenerating wire file", err)
	}
	fmt.Printf("✓ Deleted feature: %s\n", name)
}

func generateFile(dir, filename, tmplName string, data ResourceData) error {
	content, err := templateFS.ReadFile("templates/" + tmplName)
	if err != nil {
		return err
	}

	tmpl, err := template.New(filename).Parse(string(content))
	if err != nil {
		return err
	}

	file, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

// regenerateWireFile scans internal/features and rewrites cmd/server/features_gen.go.
func regenerateWireFile() error {
	var names []string
	entries, err := os.ReadDir(filepath.Join("internal", "features"))
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)

	content, err := templateFS.ReadFile("templates/wire.tmpl")
	if err != nil {
		return err
	}
	tmpl, err := template.New("wire").Parse(string(content))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join("cmd", "server"), 0755); err != nil {
		return err
	}

	file, err := os.Create(filepath.Join("cmd", "server", "features_gen.go"))
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, WireData{ModulePath: detectModulePath(), Features: names})
}

// detectModulePath reads the module line from go.mod in the current directory.
func detectModulePath() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "github.com/hnamhocit/fiber-template"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module ")
		}
	}
	return "github.com/hnamhocit/fiber-template"
}

// ── prompts (huh) ──

func askBool(q string, def bool) bool {
	v := def
	err := huh.NewConfirm().
		Title(q).
		Affirmative("Yes").
		Negative("No").
		Value(&v).
		Run()
	if err != nil {
		canceled()
	}
	return v
}

func askType() string {
	v := "restful"
	err := huh.NewSelect[string]().
		Title("Select transport").
		Options(
			huh.NewOption("restful", "restful"),
			huh.NewOption("graphql (coming soon)", "graphql"),
			huh.NewOption("websocket (coming soon)", "websocket"),
			huh.NewOption("tcp socket (coming soon)", "socket"),
		).
		Value(&v).
		Run()
	if err != nil {
		canceled()
	}
	return v
}

func canceled() {
	fmt.Println("ftpl: canceled")
	os.Exit(1)
}

// ── did-you-mean cho flags ──

func flagErrorFunc(cmd *cobra.Command, err error) error {
	const prefix = "unknown flag: "
	msg := err.Error()
	if i := strings.Index(msg, prefix); i >= 0 {
		name := strings.TrimSpace(msg[i+len(prefix):])
		var known []string
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			known = append(known, "--"+f.Name)
			if f.Shorthand != "" {
				known = append(known, "-"+f.Shorthand)
			}
		})
		if s := suggest(name, known); s != "" {
			return fmt.Errorf("unknown flag %q\nDid you mean %s?", name, s)
		}
	}
	return err
}

func suggest(input string, candidates []string) string {
	best, bestDist := "", -1
	for _, c := range candidates {
		d := levenshtein(input, c)
		if d > 3 {
			continue
		}
		if bestDist == -1 || d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if curr[j-1]+1 < m {
				m = curr[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
