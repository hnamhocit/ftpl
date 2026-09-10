package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type migrateConfig struct {
	Schema string
	Dir    string
}

var migrateCfg migrateConfig

func defaultMigrateConfig() migrateConfig {
	return migrateConfig{
		Schema: "schema.sql",
		Dir:    "migrations",
	}
}

// loadMigrateConfig: env vars override defaults.
// Production (APP_ENV=production) DOES NOT load .env — env vars are the only source.
func loadMigrateConfig() migrateConfig {
	cfg := defaultMigrateConfig()

	if !isProduction() {
		// godotenv does not override existing env vars, and expands ${VAR} in .env.
		_ = godotenv.Load()
	}
	if v := os.Getenv("DB_SCHEMA"); v != "" {
		cfg.Schema = v
	}
	if v := os.Getenv("MIGRATIONS_DIR"); v != "" {
		cfg.Dir = v
	}
	return cfg
}

// isProduction: checks for APP_ENV=prod|production. Default is dev.
func isProduction() bool {
	v := strings.ToLower(os.Getenv("APP_ENV"))
	return v == "prod" || v == "production"
}

// loadDatabaseURL: env overrides, .env is just a fallback for dev.
func loadDatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	if isProduction() {
		return "" // prod does not check .env
	}
	_ = godotenv.Load()
	return os.Getenv("DATABASE_URL")
}

func requireDatabaseURL() string {
	v := loadDatabaseURL()
	if v == "" {
		if isProduction() {
			fatal("config", fmt.Errorf("DATABASE_URL not set in environment (production mode, .env is ignored)"))
		}
		fatal("config", fmt.Errorf("DATABASE_URL not found in environment or .env"))
	}
	return v
}

// getFlag returns the explicit flag if set, otherwise from config.
func getFlag(cmd *cobra.Command, name, fallback string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	return fallback
}

// confirmPrompt asks y/N, skipped when force = true.
// Non-TTY (EOF) -> empty input -> false: safe default.
func confirmPrompt(msg string, force bool) bool {
	if force {
		return true
	}
	fmt.Printf("⚠ %s [y/N] ", msg)
	var input string
	_, _ = fmt.Scanln(&input)
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}
