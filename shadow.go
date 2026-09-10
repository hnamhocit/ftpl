package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"github.com/spf13/cobra"
)

// defaultDevURL: fallback when shadow DB creation fails (e.g. missing CREATE DATABASE privilege).
const defaultDevURL = "docker://postgres/16/dev"

func resolveDevURL(cmd *cobra.Command) (string, func(), error) {
	noop := func() {}

	if cmd.Flags().Changed("dev-url") {
		v, _ := cmd.Flags().GetString("dev-url")
		return v, noop, nil
	}
	if v := os.Getenv("DEV_URL"); v != "" {
		return v, noop, nil
	}

	dbURL := requireDatabaseURL()
	shadow, cleanup, err := shadowDB(dbURL)
	if err != nil {
		if verbose {
			fmt.Printf("[debug] shadow db unavailable (%v), fallback %s\n", err, defaultDevURL)
		}
		return defaultDevURL, noop, nil
	}
	return shadow, cleanup, nil
}

// shadowDB creates a one-time database (Prisma shadow database model):
// CREATE -> caller uses it as dev-url -> cleanup() drops it right after.
// Random name: prevents conflicts, needs no convention, no existence check.
func shadowDB(targetURL string) (string, func(), error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return "", func() {}, err
	}

	switch u.Scheme {
	case "postgres", "postgresql":
		return shadowPostgres(targetURL, u)
	case "mysql", "mariadb":
		return shadowMySQL(targetURL, u)
	case "sqlite", "sqlite3", "file":
		return shadowSQLite(targetURL)
	default:
		return "", func() {}, fmt.Errorf("unsupported scheme %q (want postgres|mysql|sqlite)", u.Scheme)
	}
}

// ---------- postgres ----------

func shadowPostgres(targetURL string, u *url.URL) (string, func(), error) {
	noop := func() {}

	admin, err := sql.Open("postgres", targetURL)
	if err != nil {
		return "", noop, err
	}

	name := fmt.Sprintf("ftpl_shadow_%08x", rand.Uint32())
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		return "", noop, err
	}
	if verbose {
		fmt.Printf("[debug] shadow db (postgres) created: %s\n", name)
	}

	su := *u
	su.Path = "/" + name

	cleanup := func() {
		// WITH (FORCE) kicks out any remaining atlas connections (pg13+)
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`)
		admin.Close()
		if verbose {
			fmt.Printf("[debug] shadow db dropped: %s\n", name)
		}
	}
	return su.String(), cleanup, nil
}

// ---------- mysql / mariadb ----------

func shadowMySQL(targetURL string, u *url.URL) (string, func(), error) {
	noop := func() {}

	admin, err := sql.Open("mysql", targetURL)
	if err != nil {
		return "", noop, err
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		return "", noop, err
	}

	name := fmt.Sprintf("ftpl_shadow_%08x", rand.Uint32())
	if _, err := admin.Exec("CREATE DATABASE `" + name + "`"); err != nil {
		admin.Close()
		return "", noop, err
	}
	if verbose {
		fmt.Printf("[debug] shadow db (mysql) created: %s\n", name)
	}

	// Clone URL, change dbname (MySQL URL format: user:pass@tcp(host:port)/dbname?params)
	su := *u
	su.Path = "/" + name
	// MySQL needs multiStatements=true for Atlas to run multiple statements in a batch
	q := su.Query()
	q.Set("multiStatements", "true")
	q.Set("parseTime", "true")
	su.RawQuery = q.Encode()

	cleanup := func() {
		// Close all DB connections before dropping (MySQL has no FORCE like pg)
		_, _ = admin.Exec(fmt.Sprintf("SET FOREIGN_KEY_CHECKS=0"))
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		admin.Close()
		if verbose {
			fmt.Printf("[debug] shadow db dropped: %s\n", name)
		}
	}
	return su.String(), cleanup, nil
}

// ---------- sqlite ----------

func shadowSQLite(targetURL string) (string, func(), error) {
	noop := func() {}

	// SQLite URL: sqlite://path/to/file.db or file:path/to/file.db
	u, err := url.Parse(targetURL)
	if err != nil {
		return "", noop, err
	}

	// Create shadow DB next to main file (same dir) to avoid different mount points
	original := strings.TrimPrefix(u.Path, "/")
	if original == "" || original == ":memory:" {
		// In-memory: create temporary shadow DB in temp dir
		original = filepath.Join(os.TempDir(), "ftpl_main.db")
	}
	dir := filepath.Dir(original)
	name := fmt.Sprintf("ftpl_shadow_%08x.db", rand.Uint32())
	shadowPath := filepath.Join(dir, name)

	// "CREATE" = connect to trigger file creation (SQLite creates on connect)
	db, err := sql.Open("sqlite", shadowPath)
	if err != nil {
		return "", noop, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return "", noop, err
	}
	db.Close() // Atlas will connect on its own

	if verbose {
		fmt.Printf("[debug] shadow db (sqlite) created: %s\n", shadowPath)
	}

	shadowURL := "sqlite://" + shadowPath

	cleanup := func() {
		_ = os.Remove(shadowPath)
		if verbose {
			fmt.Printf("[debug] shadow db dropped: %s\n", shadowPath)
		}
	}
	return shadowURL, cleanup, nil
}
