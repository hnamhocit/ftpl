package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// templateModule is the module path of the upstream template.
// Every cloned .go file imports packages under this path; after renaming the
// module we MUST rewrite them, otherwise Go resolves them as an EXTERNAL
// dependency and silently downloads the public template repo (Frankenstein
// build: local files + remote packages).
const templateModule = "github.com/hnamhocit/fiber-template"

var newCmd = &cobra.Command{
	Use:   "new <project-name>",
	Short: "Create a new Fiber project from the production template",
	Args:  cobra.ExactArgs(1),
	Run:   runNew,
}

type newProjectConfig struct {
	Name     string
	Module   string // e.g. github.com/username/project
	Database string // postgres | mysql | sqlite
	Redis    bool
	Minio    bool
}

func runNew(cmd *cobra.Command, args []string) {
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if name == "" {
		fatal("new", fmt.Errorf("project name cannot be empty"))
	}
	if _, err := os.Stat(name); err == nil {
		fatal("new", fmt.Errorf("directory %q already exists", name))
	}

	cfg := newProjectConfig{Name: name}
	cfg.Database = askDatabase()
	cfg.Redis = askBool("Enable Redis (caching + shared rate limiting)?", true)
	cfg.Minio = askBool("Enable MinIO (object storage)?", false)
	cfg.Module = askModulePath(name)

	fmt.Println()
	fmt.Println("Creating project:")
	fmt.Printf("  • Name:     %s\n", cfg.Name)
	fmt.Printf("  • Module:   %s\n", cfg.Module)
	fmt.Printf("  • Database: %s\n", cfg.Database)
	fmt.Printf("  • Redis:    %v\n", cfg.Redis)
	fmt.Printf("  • MinIO:    %v\n", cfg.Minio)
	fmt.Println()

	if err := cloneAndCustomize(cfg); err != nil {
		fatal("new", err)
	}

	fmt.Println()
	fmt.Println("✓ Project created at ./" + cfg.Name)

	// A child process cannot change the parent shell's cwd, so "cd" is
	// impossible from here; the closest real help is resolving deps in place.
	if askBool("Download Go dependencies now?", true) {
		installDependencies(cfg.Name)
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", cfg.Name)
	if cfg.Database != "sqlite" {
		fmt.Println("  docker compose up -d    # start infra")
	}
	fmt.Println("  ftpl run dev            # start dev server")
	fmt.Println()
	fmt.Println("For production, update .env with your credentials.")
}

// ---------- prompts ----------

func askDatabase() string {
	var db string
	err := huh.NewSelect[string]().
		Title("Choose database").
		Options(
			huh.NewOption("PostgreSQL (recommended)", "postgres"),
			huh.NewOption("MySQL", "mysql"),
			huh.NewOption("SQLite (embedded, no docker needed)", "sqlite"),
		).
		Value(&db).
		Run()
	if err != nil {
		canceled()
	}
	return db
}

func askModulePath(projectName string) string {
	username := "yourname"
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		u := strings.ToLower(strings.TrimSpace(string(out)))
		u = strings.ReplaceAll(u, " ", "-")
		if u != "" {
			username = u
		}
	}
	def := fmt.Sprintf("github.com/%s/%s", username, projectName)

	path := def
	err := huh.NewInput().
		Title("Module path").
		Description("Go module path for the new project").
		Value(&path).
		Run()
	if err != nil {
		canceled()
	}
	if strings.TrimSpace(path) == "" {
		path = def
	}
	return strings.TrimSpace(path)
}

// ---------- clone + customize ----------

func cloneAndCustomize(cfg newProjectConfig) error {
	fmt.Println("→ Cloning template...")
	if err := streamCmd("", "git", "clone", "--depth", "1", "--quiet", templateModule+".git", cfg.Name); err != nil {
		return fmt.Errorf("clone template: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(cfg.Name, ".git")); err != nil {
		return fmt.Errorf("remove .git: %w", err)
	}

	// ORDER MATTERS:
	// 1. scrub first — its matchers expect the OLD import paths
	// 2. then rewrite old module path -> new module path everywhere
	// 3. then rename the module line in go.mod
	if err := removeUnusedPackages(cfg); err != nil {
		return err
	}
	if err := rewriteModulePaths(cfg); err != nil {
		return err
	}
	if err := updateGoMod(cfg); err != nil {
		return err
	}
	if err := updateDotEnv(cfg); err != nil {
		return err
	}
	if err := updateDockerCompose(cfg); err != nil {
		return err
	}
	updateReadmeTitle(cfg)
	initGitRepo(cfg.Name)

	return nil
}

// ---------- pruning: delete packages + scrub references ----------

// removeUnusedPackages deletes entire package folders for disabled features
// and scrubs every reference to them. No dead code ships with the project.
func removeUnusedPackages(cfg newProjectConfig) error {
	fmt.Println("→ Pruning unused packages...")

	if !cfg.Redis {
		if err := os.RemoveAll(filepath.Join(cfg.Name, "internal", "cache")); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(cfg.Name, "internal", "server", "redisstorage.go"))
		for _, f := range scrubTargets() {
			if err := scrubFile(f, dropRedis, transformDepsLine); err != nil {
				return err
			}
		}
	}

	if !cfg.Minio {
		if err := os.RemoveAll(filepath.Join(cfg.Name, "internal", "storage")); err != nil {
			return err
		}
		for _, f := range scrubTargets() {
			if err := scrubFile(f, dropMinio, transformDepsLine); err != nil {
				return err
			}
		}
	}

	return nil
}

func scrubTargets() []string {
	return []string{
		filepath.Join("internal", "bootstrap", "infra.go"),
		filepath.Join("internal", "bootstrap", "health.go"),
		filepath.Join("internal", "server", "deps.go"),
		filepath.Join("internal", "server", "server.go"),
		filepath.Join("cmd", "server", "main.go"),
	}
}

func dropRedis(line string) bool {
	return strings.Contains(line, "/internal/cache") ||
		strings.Contains(line, ".Redis") ||
		strings.Contains(line, "Redis ") ||
		strings.Contains(line, "RedisEnabled") ||
		strings.Contains(line, `Name: "redis"`)
}

func dropMinio(line string) bool {
	return strings.Contains(line, "/internal/storage") ||
		strings.Contains(line, ".Minio") ||
		strings.Contains(line, "Minio ") ||
		strings.Contains(line, "MinioEnabled") ||
		strings.Contains(line, `Name: "minio"`)
}

// transformDepsLine removes Redis/Minio fields from the one-line Deps
// literal in main.go instead of dropping the whole line:
//
//	deps := server.Deps{Cfg: cfg, DB: infra.DB, Redis: infra.Redis, Minio: infra.Minio}
//	-> deps := server.Deps{Cfg: cfg, DB: infra.DB}
func transformDepsLine(line string) string {
	if !strings.Contains(line, "server.Deps{") {
		return line
	}
	for _, pat := range []string{
		"Redis: infra.Redis, ", "Redis: infra.Redis,", "Redis: infra.Redis",
		"Minio: infra.Minio, ", "Minio: infra.Minio,", "Minio: infra.Minio",
	} {
		line = strings.ReplaceAll(line, pat, "")
	}
	return line
}

// scrubFile rewrites path: lines matched by drop are removed — and when a
// dropped line opens a block (`if x {`), the WHOLE block is removed so no
// orphan closing brace survives. Deps-literal lines go through transform.
func scrubFile(path string, drop func(string) bool, transform func(string) string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.Contains(line, "server.Deps{") {
			out = append(out, transform(line))
			continue
		}

		if drop(line) {
			if strings.HasSuffix(strings.TrimSpace(line), "{") {
				depth := 1
				for i+1 < len(lines) && depth > 0 {
					i++
					depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
				}
			}
			continue
		}

		out = append(out, line)
	}

	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0644)
}

// ---------- module path rewrite ----------

// rewriteModulePaths replaces the template module path with the new project
// module path in every .go file. Without this, Go downloads the public
// template as an external dependency (see templateModule comment).
func rewriteModulePaths(cfg newProjectConfig) error {
	fmt.Println("→ Rewriting import paths...")
	return filepath.WalkDir(cfg.Name, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if !strings.Contains(string(data), templateModule) {
			return nil
		}
		updated := strings.ReplaceAll(string(data), templateModule, cfg.Module)
		return os.WriteFile(p, []byte(updated), 0644)
	})
}

// ---------- go.mod ----------

func updateGoMod(cfg newProjectConfig) error {
	fmt.Println("→ Updating go.mod...")
	path := filepath.Join(cfg.Name, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "module ") {
			lines[i] = "module " + cfg.Module
			break
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// ---------- .env ----------

func updateDotEnv(cfg newProjectConfig) error {
	fmt.Println("→ Writing .env...")
	src := filepath.Join(cfg.Name, ".env.example")
	dst := filepath.Join(cfg.Name, ".env")

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	skipSection := ""

	for _, line := range lines {
		if strings.HasPrefix(line, "# ===== Redis =====") && !cfg.Redis {
			skipSection = "redis"
		}
		if strings.HasPrefix(line, "# ===== MinIO =====") && !cfg.Minio {
			skipSection = "minio"
		}
		if strings.HasPrefix(line, "# ===== App =====") || strings.HasPrefix(line, "# ===== DB =====") {
			skipSection = ""
		}
		if skipSection != "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "APP_NAME="):
			out = append(out, "APP_NAME="+cfg.Name)
		case strings.HasPrefix(line, "DB_NAME="):
			out = append(out, "DB_NAME="+cfg.Name)
		case strings.HasPrefix(line, "DATABASE_URL="):
			out = append(out, "DATABASE_URL="+dbURLFor(cfg.Database))
		default:
			out = append(out, line)
		}
	}

	if err := os.WriteFile(dst, []byte(strings.Join(out, "\n")+"\n"), 0644); err != nil {
		return err
	}
	return os.Remove(src)
}

func dbURLFor(driver string) string {
	switch driver {
	case "mysql":
		return "mysql://${DB_USERNAME}:${DB_PASSWORD}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}?parseTime=true"
	case "sqlite":
		return "file:./data/${DB_NAME}.db?cache=shared&mode=rwc"
	default:
		return "postgres://${DB_USERNAME}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable"
	}
}

// ---------- docker-compose ----------

func updateDockerCompose(cfg newProjectConfig) error {
	fmt.Println("→ Updating docker-compose.yaml...")
	path := filepath.Join(cfg.Name, "docker-compose.yaml")

	if cfg.Database == "sqlite" {
		return os.Remove(path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "postgres:" && cfg.Database != "postgres" {
			skipping = true
			continue
		}
		if trimmed == "redis:" && !cfg.Redis {
			skipping = true
			continue
		}
		if trimmed == "minio:" && !cfg.Minio {
			skipping = true
			continue
		}
		if skipping && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}

	out = pruneVolumes(out, cfg)
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0644)
}

func pruneVolumes(lines []string, cfg newProjectConfig) []string {
	var out []string
	inVolumes := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "volumes:" {
			inVolumes = true
			out = append(out, line)
			continue
		}
		if inVolumes && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			inVolumes = false
		}
		if inVolumes {
			n := strings.TrimSuffix(trimmed, ":")
			if n == "postgres_data" && cfg.Database != "postgres" {
				continue
			}
			if n == "redis_data" && !cfg.Redis {
				continue
			}
			if n == "minio_data" && !cfg.Minio {
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// ---------- readme + git ----------

func updateReadmeTitle(cfg newProjectConfig) {
	path := filepath.Join(cfg.Name, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			lines[i] = "# " + cfg.Name
			break
		}
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func initGitRepo(dir string) {
	fmt.Println("→ Initializing git repository...")
	_ = streamCmd(dir, "git", "init", "-q")
	_ = streamCmd(dir, "git", "add", ".")
	_ = streamCmd(dir, "git", "commit", "-q", "-m", "Initial commit from ftpl new")
}

// ---------- dependency install ----------

// installDependencies resolves modules in place. A child process cannot cd
// the parent shell, so this is the closest real automation possible.
func installDependencies(dir string) {
	fmt.Println("→ Resolving Go dependencies...")

	// tidy FIRST: it fixes requires after pruning + module rewrite
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Stdout, tidy.Stderr = os.Stdout, os.Stderr
	if err := tidy.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: go mod tidy failed: %v\n", err)
	}

	// SAFETY NET: the project must never depend on its own template.
	// If this trips, the import rewrite silently failed somewhere.
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err == nil && strings.Contains(string(data), templateModule) {
		fmt.Fprintln(os.Stderr, "error: go.mod still requires the template module — import rewrite failed")
		os.Exit(1)
	}

	dl := exec.Command("go", "mod", "download")
	dl.Dir = dir
	dl.Stdout, dl.Stderr = os.Stdout, os.Stderr
	if err := dl.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: go mod download failed: %v\n", err)
		return
	}
	fmt.Println("✓ Dependencies installed")
}
