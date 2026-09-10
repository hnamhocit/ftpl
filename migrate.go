package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration commands (atlas + sqlc)",
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Direct database commands (no migration history)",
}

var migrateDevCmd = &cobra.Command{
	Use:   "dev <name>",
	Short: "Daily workflow: diff + apply + sqlc generate",
	Args:  cobra.ExactArgs(1),
	Run:   runMigrateDev,
}

var migrateDiffCmd = &cobra.Command{
	Use:   "diff <name>",
	Short: "Generate migration file only (no apply)",
	Args:  cobra.ExactArgs(1),
	Run:   runMigrateDiff,
}

var migrateApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply pending migrations",
	Run:   runMigrateApply,
}

var migrateDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Production: apply pending migrations only (no diff, no generate)",
	Run:   runMigrateDeploy,
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback the last applied migration",
	Run:   runMigrateDown,
}

var migrateResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop schema, re-apply all migrations, regenerate structs",
	Run:   runMigrateReset,
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	Run:   runMigrateStatus,
}

var migrateLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint the latest migration for destructive changes",
	Run:   runMigrateLint,
}

var migrateValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate migration directory (atlas.sum integrity)",
	Run:   runMigrateValidate,
}

var dbPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push schema.sql straight to DB (dev-only, no migration file)",
	Run:   runDbPush,
}

var dbSeedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Run seed (seed/main.go via go run, or seed/seed.sql via psql)",
	Run:   runDbSeed,
}

func init() {
	migrateDownCmd.Flags().BoolP("force", "y", false, "skip confirmation")
	migrateResetCmd.Flags().BoolP("force", "y", false, "skip confirmation")
	dbPushCmd.Flags().BoolP("force", "y", false, "skip confirmation")

	migrateCmd.AddCommand(
		migrateDevCmd, migrateDiffCmd, migrateApplyCmd, migrateDeployCmd,
		migrateDownCmd, migrateResetCmd, migrateStatusCmd,
		migrateLintCmd, migrateValidateCmd,
	)
	dbCmd.AddCommand(dbPushCmd, dbSeedCmd)
}

// ---------- handlers ----------

// runMigrateDev = prisma migrate dev: diff -> apply -> generate.
func runMigrateDev(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()
	dbURL := requireDatabaseURL()

	devURL, cleanupDev, err := resolveDevURL(cmd)
	if err != nil {
		fatal("dev database", err)
	}
	defer cleanupDev()

	schema := getFlag(cmd, "schema", migrateCfg.Schema)
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "diff", args[0],
		"--dev-url", devURL, "--to", "file://"+schema, "--dir", "file://"+dir); err != nil {
		fatal("migrate diff", err)
	}
	fmt.Printf("✓ Created migration: %s\n", args[0])

	if err := runAtlas(atlas, "migrate", "apply", "--url", dbURL, "--dir", "file://"+dir); err != nil {
		fatal("migrate apply", err)
	}
	fmt.Println("✓ Applied migrations")

	runSqlcGenerate()
}

func runMigrateDiff(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()

	devURL, cleanupDev, err := resolveDevURL(cmd)
	if err != nil {
		fatal("dev database", err)
	}
	defer cleanupDev()

	schema := getFlag(cmd, "schema", migrateCfg.Schema)
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "diff", args[0],
		"--dev-url", devURL, "--to", "file://"+schema, "--dir", "file://"+dir); err != nil {
		fatal("migrate diff", err)
	}
	fmt.Printf("✓ Generated migration: %s\n", args[0])
	fmt.Println("  Review the file in", dir, "before applying")
}

func runMigrateApply(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()
	dbURL := requireDatabaseURL()
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "apply", "--url", dbURL, "--dir", "file://"+dir); err != nil {
		fatal("migrate apply", err)
	}
	fmt.Println("✓ Migrations applied")
}

// runMigrateDeploy: production applies only, DOES NOT generate structs
// (structs are generated in dev and committed to git).
func runMigrateDeploy(cmd *cobra.Command, args []string) {
	runMigrateApply(cmd, args)
	fmt.Println("  (deploy mode: struct generation skipped — structs come from git)")
}

func runMigrateDown(cmd *cobra.Command, args []string) {
	force, _ := cmd.Flags().GetBool("force")
	if !confirmPrompt("This will rollback the last applied migration. Continue?", force) {
		fmt.Println("Cancelled")
		return
	}

	atlas := mustAtlas()
	dbURL := requireDatabaseURL()

	devURL, cleanupDev, err := resolveDevURL(cmd)
	if err != nil {
		fatal("dev database", err)
	}
	defer cleanupDev()

	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "down", "1",
		"--url", dbURL, "--dev-url", devURL, "--dir", "file://"+dir); err != nil {
		fatal("migrate down", err)
	}
	fmt.Println("✓ Rolled back last migration")
}

// runMigrateReset: "atlas migrate clean" is removed from Atlas CLI,
// the correct command is "atlas schema clean" + --auto-approve.
func runMigrateReset(cmd *cobra.Command, args []string) {
	force, _ := cmd.Flags().GetBool("force")
	if !confirmPrompt("This will DROP the schema and re-apply ALL migrations. Continue?", force) {
		fmt.Println("Cancelled")
		return
	}

	atlas := mustAtlas()
	dbURL := requireDatabaseURL()
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "schema", "clean",
		"--url", dbURL, "--auto-approve"); err != nil {
		fatal("migrate clean", err)
	}
	fmt.Println("✓ Schema dropped")

	if err := runAtlas(atlas, "migrate", "apply", "--url", dbURL, "--dir", "file://"+dir); err != nil {
		fatal("migrate apply", err)
	}
	fmt.Println("✓ All migrations applied")

	runSqlcGenerate()
}

func runMigrateStatus(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()
	dbURL := requireDatabaseURL()
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "status", "--url", dbURL, "--dir", "file://"+dir); err != nil {
		fatal("migrate status", err)
	}
}

func runMigrateLint(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()

	devURL, cleanupDev, err := resolveDevURL(cmd)
	if err != nil {
		fatal("dev database", err)
	}
	defer cleanupDev()

	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "lint",
		"--dev-url", devURL, "--dir", "file://"+dir, "--latest", "1"); err != nil {
		fatal("migrate lint", err)
	}
	fmt.Println("✓ Lint passed")
}

func runMigrateValidate(cmd *cobra.Command, args []string) {
	atlas := mustAtlas()
	dir := getFlag(cmd, "dir", migrateCfg.Dir)

	if err := runAtlas(atlas, "migrate", "validate", "--dir", "file://"+dir); err != nil {
		fatal("migrate validate", err)
	}
	fmt.Println("✓ Migration directory is valid")
}

// runDbPush: schema apply asks for confirmation by default,
// we pass --auto-approve since we already confirmed.
func runDbPush(cmd *cobra.Command, args []string) {
	force, _ := cmd.Flags().GetBool("force")
	if !confirmPrompt("This will push schema WITHOUT creating migration files. Continue?", force) {
		fmt.Println("Cancelled")
		return
	}

	atlas := mustAtlas()
	dbURL := requireDatabaseURL()

	devURL, cleanupDev, err := resolveDevURL(cmd)
	if err != nil {
		fatal("dev database", err)
	}
	defer cleanupDev()

	schema := getFlag(cmd, "schema", migrateCfg.Schema)

	if err := runAtlas(atlas, "schema", "apply",
		"--url", dbURL, "--dev-url", devURL, "--to", "file://"+schema,
		"--auto-approve"); err != nil {
		fatal("db push", err)
	}
	fmt.Println("✓ Schema pushed")

	runSqlcGenerate()
}

// runDbSeed supports 2 conventions: seed/main.go (go run) or seed/seed.sql (psql).
func runDbSeed(cmd *cobra.Command, args []string) {
	dbURL := requireDatabaseURL()

	if _, err := os.Stat(filepath.Join("seed", "main.go")); err == nil {
		c := exec.Command("go", "run", "./seed")
		c.Env = append(os.Environ(), "DATABASE_URL="+dbURL)
		c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := c.Run(); err != nil {
			fatal("db seed", err)
		}
		fmt.Println("✓ Seed completed (go run ./seed)")
		return
	}

	if _, err := os.Stat(filepath.Join("seed", "seed.sql")); err == nil {
		psql, err := exec.LookPath("psql")
		if err != nil {
			fatal("db seed", fmt.Errorf("seed/seed.sql found but psql is not installed"))
		}
		c := exec.Command(psql, dbURL, "-v", "ON_ERROR_STOP=1", "-f", filepath.Join("seed", "seed.sql"))
		c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := c.Run(); err != nil {
			fatal("db seed", err)
		}
		fmt.Println("✓ Seed completed (psql)")
		return
	}

	fmt.Println("ftpl: no seed found (expected seed/main.go or seed/seed.sql)")
}
