package db_test

import (
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
	if err := h1.Close(); err != nil {
		t.Fatal(err)
	}
	h2, err := db.Open(p) // migrations should be no-op second time
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h2.Close() })

	var n int
	if err := h2.SQL().QueryRow("SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one migration row, got %d", n)
	}
}
