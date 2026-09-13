package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new <project-name>",
	Short: "Create a new Fiber project from the production template",
	Args:  cobra.ExactArgs(1),
	Run:   runNew,
}

type newProjectConfig struct {
	Name        string
	Module      string // e.g. github.com/username/project
	Database    string // postgres | mysql | sqlite
	Redis       bool
	Minio       bool
	InstallDeps bool
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

	// Ask to cd + install deps
	cfg.InstallDeps = askBool("Download Go dependencies now?", true)
	if cfg.InstallDeps {
		installDependencies(cfg.Name)
	}

	// Print next steps
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", cfg.Name)
	if cfg.Database != "sqlite" {
		fmt.Println("  docker compose up -d    # start infra")
	}
	fmt.Println("  ftpl migrate init       # (if fresh DB)")
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
	// Try to guess GitHub username from git config
	username := ""
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		// Normalize: lowercase, replace spaces with dashes
		username = strings.ToLower(strings.TrimSpace(string(out)))
		username = strings.ReplaceAll(username, " ", "-")
	}
	if username == "" {
		username = "yourname"
	}
	def := fmt.Sprintf("github.com/%s/%s", username, projectName)

	var path string
	err := huh.NewInput().
		Title("Module path").
		Value(&path).
		Placeholder(def).
		Run()
	if err != nil {
		canceled()
	}
	if path == "" {
		path = def
	}
	return path
}

// ---------- clone + customize ----------

func cloneAndCustomize(cfg newProjectConfig) error {
	repo := "https://github.com/hnamhocit/fiber-template.git"

	fmt.Println("→ Cloning template...")
	if err := streamCmd("", "git", "clone", "--depth", "1", "--quiet", repo, cfg.Name); err != nil {
		return fmt.Errorf("clone template: %w", err)
	}

	// Fresh git history
	if err := os.RemoveAll(filepath.Join(cfg.Name, ".git")); err != nil {
		return fmt.Errorf("remove .git: %w", err)
	}

	// 1. Remove unused packages entirely
	if err := removeUnusedPackages(cfg); err != nil {
		return err
	}

	// 2. Update module path
	if err := updateGoMod(cfg); err != nil {
		return err
	}

	// 3. Generate .env from example
	if err := updateDotEnv(cfg); err != nil {
		return err
	}

	// 4. Trim docker-compose.yaml
	if err := updateDockerCompose(cfg); err != nil {
		return err
	}

	// 5. Update README title
	updateReadmeTitle(cfg)

	// 6. Init fresh git repo
	initGitRepo(cfg.Name)

	return nil
}

// removeUnusedPackages deletes entire package folders and scrubs their
// references from files that import them. This is the key difference
// from just commenting out env vars — no unused code ships with the project.
func removeUnusedPackages(cfg newProjectConfig) error {
	fmt.Println("→ Pruning unused packages...")

	if !cfg.Redis {
		// Delete cache package
		if err := os.RemoveAll(filepath.Join(cfg.Name, "internal", "cache")); err != nil {
			return err
		}
		// Delete redis storage file in server
		_ = os.Remove(filepath.Join(cfg.Name, "internal", "server", "redisstorage.go"))

		// Scrub imports + references
		if err := scrubRedis(filepath.Join(cfg.Name, "internal", "bootstrap", "infra.go")); err != nil {
			return err
		}
		if err := scrubRedis(filepath.Join(cfg.Name, "internal", "bootstrap", "health.go")); err != nil {
			return err
		}
		if err := scrubRedis(filepath.Join(cfg.Name, "internal", "server", "deps.go")); err != nil {
			return err
		}
		if err := scrubRedis(filepath.Join(cfg.Name, "internal", "server", "server.go")); err != nil {
			return err
		}
		if err := scrubRedis(filepath.Join(cfg.Name, "cmd", "server", "main.go")); err != nil {
			return err
		}
	}

	if !cfg.Minio {
		if err := os.RemoveAll(filepath.Join(cfg.Name, "internal", "storage")); err != nil {
			return err
		}
		if err := scrubMinio(filepath.Join(cfg.Name, "internal", "bootstrap", "infra.go")); err != nil {
			return err
		}
		if err := scrubMinio(filepath.Join(cfg.Name, "internal", "bootstrap", "health.go")); err != nil {
			return err
		}
		if err := scrubMinio(filepath.Join(cfg.Name, "internal", "server", "deps.go")); err != nil {
			return err
		}
		if err := scrubMinio(filepath.Join(cfg.Name, "cmd", "server", "main.go")); err != nil {
			return err
		}
	}

	return nil
}

// ---------- scrubbers: line-by-line, no regex ----------

// scrubRedis removes lines referencing Redis/cache from a file.
// Skips empty files gracefully.
func scrubRedis(path string) error {
	return scrubFile(path, func(line string) bool {
		return strings.Contains(line, `"github.com/hnamhocit/fiber-template/internal/cache"`) ||
			strings.Contains(line, "cache.") ||
			strings.Contains(line, "Redis ") || // field declaration like `Redis *cache.Client`
			strings.Contains(line, "Redis:") || // struct literal `Redis: infra.Redis`
			strings.Contains(line, ".Redis") || // access `deps.Redis`, `infra.Redis`
			strings.Contains(line, "RedisEnabled") ||
			strings.Contains(line, "deps.Redis") ||
			strings.Contains(line, "infra.Redis") ||
			strings.Contains(line, `Name: "redis"`)
	})
}

func scrubMinio(path string) error {
	return scrubFile(path, func(line string) bool {
		return strings.Contains(line, `"github.com/hnamhocit/fiber-template/internal/storage"`) ||
			strings.Contains(line, "storage.") ||
			strings.Contains(line, "Minio ") ||
			strings.Contains(line, "Minio:") ||
			strings.Contains(line, ".Minio") ||
			strings.Contains(line, "MinioEnabled") ||
			strings.Contains(line, "deps.Minio") ||
			strings.Contains(line, "infra.Minio") ||
			strings.Contains(line, `Name: "minio"`)
	})
}

// scrubFile reads path, drops lines matched by shouldDrop, rewrites.
// Also removes empty `if` blocks that become `if <cond> { }` after scrubbing.
func scrubFile(path string, shouldDrop func(string) bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var kept []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if shouldDrop(line) {
			continue
		}
		kept = append(kept, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Collapse empty if/else blocks that were just pruned.
	kept = collapseEmptyBlocks(kept)

	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

// collapseEmptyBlocks removes patterns like:
//
//	if foo {
//	}
//
// (with only whitespace between braces)
func collapseEmptyBlocks(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		// Look for `if <cond> {` and check next non-empty line
		if strings.Contains(line, " if ") || strings.HasPrefix(strings.TrimSpace(line), "if ") {
			if strings.HasSuffix(strings.TrimSpace(line), "{") {
				// Scan ahead: if only whitespace until matching `}`, drop both lines
				j := i + 1
				for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
					j++
				}
				if j < len(lines) && strings.TrimSpace(lines[j]) == "}" {
					// Drop this line and skip ahead past the closing brace
					i = j
					continue
				}
			}
		}
		out = append(out, line)
	}
	return out
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
		// Section markers: skip entire section if feature disabled
		if strings.HasPrefix(line, "# ===== Redis =====") {
			if !cfg.Redis {
				skipSection = "redis"
			}
		}
		if strings.HasPrefix(line, "# ===== MinIO =====") {
			if !cfg.Minio {
				skipSection = "minio"
			}
		}
		if strings.HasPrefix(line, "# ===== App =====") || strings.HasPrefix(line, "# ===== DB =====") {
			skipSection = ""
		}
		if skipSection != "" {
			continue
		}

		// Substitute dynamic values
		switch {
		case strings.HasPrefix(line, "APP_NAME="):
			out = append(out, "APP_NAME="+cfg.Name)
		case strings.HasPrefix(line, "DATABASE_URL="):
			out = append(out, "DATABASE_URL="+dbURLFor(cfg.Database))
		case strings.HasPrefix(line, "DB_NAME="):
			out = append(out, "DB_NAME="+cfg.Name)
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

	// SQLite: no compose needed
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

		// Start skipping a service block
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

		// End skipping: new top-level key (no leading whitespace, not a comment)
		if skipping && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}

	// Remove unused volumes (only keep those whose service survived)
	out = pruneVolumes(out, cfg)

	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0644)
}

func pruneVolumes(lines []string, cfg newProjectConfig) []string {
	volName := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.Index(s, ":"); i > 0 {
			s = s[:i]
		}
		return strings.TrimSuffix(s, ":")
	}

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
			n := volName(line)
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

// ---------- README + git ----------

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

// ---------- install dependencies ----------

func installDependencies(dir string) {
	fmt.Println("→ Downloading Go dependencies (this may take a moment)...")
	c := exec.Command("go", "mod", "download")
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: go mod download failed: %v\n", err)
		fmt.Println("  Run `go mod download` manually inside the project.")
		return
	}
	// Tidy up: drop unused deps from packages we pruned
	c2 := exec.Command("go", "mod", "tidy")
	c2.Dir = dir
	c2.Stdout = os.Stdout
	c2.Stderr = os.Stderr
	_ = c2.Run()
	fmt.Println("✓ Dependencies installed")
}
