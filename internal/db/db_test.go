package db_test

import (
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

func TestOpen_CreatesFileAndSetsPragmas(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	var mode string
	if err := h.SQL().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q want wal", mode)
	}

	var fk int
	if err := h.SQL().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys=%d want 1", fk)
	}
}

func TestOpen_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h1, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := h1.Close(); err != nil {
		t.Fatal(err)
	}
	h2, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h2.Close(); err != nil {
			t.Errorf("h2 close: %v", err)
		}
	})
}
