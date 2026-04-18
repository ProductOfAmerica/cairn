package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

func TestMigrate_CreatesAllTables(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	want := []string{
		"requirements", "gates", "tasks", "claims", "runs",
		"evidence", "verdicts", "events", "op_log", "schema_migrations",
	}
	for _, tbl := range want {
		var n int
		err := h.SQL().QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("missing table %q", tbl)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h1, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var first int
	if err := h1.SQL().QueryRow("SELECT count(*) FROM schema_migrations").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := h1.Close(); err != nil {
		t.Fatal(err)
	}
	h2, err := db.Open(p) // migrations should be no-op second time
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h2.Close() })

	var second int
	if err := h2.SQL().QueryRow("SELECT count(*) FROM schema_migrations").Scan(&second); err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("migration count changed on re-open: first=%d second=%d", first, second)
	}
}

func TestMigrate_Ship2Objects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	h, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Objects that must exist after migration 002.
	expected := []struct {
		kind string // "table"|"index"|"trigger"
		name string
	}{
		{"table", "memory_entries"},
		{"table", "memory_fts"}, // virtual table registers as "table"
		{"index", "idx_memory_at"},
		{"index", "idx_memory_kind"},
		{"index", "idx_memory_entity"},
		{"trigger", "memory_fts_ai"},
		{"trigger", "memory_no_delete"},
		{"trigger", "memory_no_update"},
		{"trigger", "evidence_only_invalidated_at_updatable"},
		{"trigger", "evidence_no_delete"},
	}
	for _, obj := range expected {
		var got string
		err := h.SQL().QueryRow(
			`SELECT name FROM sqlite_master WHERE type = ? AND name = ?`,
			obj.kind, obj.name,
		).Scan(&got)
		if err != nil {
			t.Errorf("%s %q missing after migration: %v", obj.kind, obj.name, err)
		}
	}

	// evidence.invalidated_at column must exist and be nullable.
	rows, err := h.SQL().Query(`PRAGMA table_info(evidence)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "invalidated_at" {
			found = true
			if typ != "INTEGER" {
				t.Errorf("invalidated_at type = %q, want INTEGER", typ)
			}
			if notnull != 0 {
				t.Errorf("invalidated_at NOT NULL = %d, want 0 (nullable)", notnull)
			}
		}
	}
	if !found {
		t.Error("evidence.invalidated_at column not found")
	}

	// schema_migrations shows version 2.
	var maxVersion int
	if err := h.SQL().QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion < 2 {
		t.Errorf("schema_migrations max version = %d, want >= 2", maxVersion)
	}
}
