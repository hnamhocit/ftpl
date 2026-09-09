package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type migrateConfig struct {
	DevURL string
	Schema string
	Dir    string
}

var migrateCfg migrateConfig

func defaultMigrateConfig() migrateConfig {
	return migrateConfig{
		DevURL: "docker://postgres/16/dev",
		Schema: "schema.sql",
		Dir:    "migrations",
	}
}

func loadMigrateConfig() migrateConfig {
	cfg := defaultMigrateConfig()

	if !isProduction() {
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

// isProduction: chỉ nhận APP_ENV=prod|production. Mặc định = dev.
func isProduction() bool {
	v := strings.ToLower(os.Getenv("APP_ENV"))
	return v == "prod" || v == "production"
}

// loadDatabaseURL: env thắng (prod chuẩn), .env chỉ là fallback cho dev.
func loadDatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	if isProduction() {
		return "" // prod không mò .env
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

func getFlag(cmd *cobra.Command, name, fallback string) string {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	return fallback
}

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
