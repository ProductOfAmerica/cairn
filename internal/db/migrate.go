package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// migrate applies any unapplied migrations in schema/ in filename-sorted order.
// Files are named NNN_<description>.sql; the NNN prefix is the version.
// Each migration runs inside its own transaction; schema_migrations records
// the applied version.
func migrate(d *sql.DB) error {
	// Ensure the tracking table exists before detecting "applied" state.
	// 001_init.sql also creates this table (idempotently via IF NOT EXISTS),
	// but we need to read it BEFORE 001 runs to decide whether 001 has been
	// applied on an already-migrated DB.
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := schemaFS.ReadDir("schema")
	if err != nil {
		return fmt.Errorf("read embedded schema: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			return fmt.Errorf("parse version from %q: %w", name, err)
		}
		var applied int
		if err := d.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=?",
			version).Scan(&applied); err != nil {
			return fmt.Errorf("check v%d applied: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		body, err := schemaFS.ReadFile("schema/" + name)
		if err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
		if err := applyOne(d, version, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func parseVersion(fname string) (int, error) {
	cut := strings.IndexByte(fname, '_')
	if cut <= 0 {
		return 0, fmt.Errorf("no version prefix")
	}
	return strconv.Atoi(fname[:cut])
}

func applyOne(d *sql.DB, version int, body string) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("begin for v%d: %w", version, err)
	}
	if _, err := tx.Exec(body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply v%d: %w", version, err)
	}
	if _, err := tx.Exec(
		"INSERT OR REPLACE INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		version, time.Now().UnixMilli(),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record v%d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit v%d: %w", version, err)
	}
	return nil
}
