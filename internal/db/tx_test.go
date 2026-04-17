package db_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

func TestWithTx_Commit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
             VALUES ('REQ-1', 'p', 'h', 0, 0)`,
		)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := h.SQL().QueryRow("SELECT count(*) FROM requirements").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("insert not visible post-commit: n=%d", n)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	sentinel := errors.New("boom")
	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(
			`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
             VALUES ('REQ-1', 'p', 'h', 0, 0)`,
		)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	var n int
	if err := h.SQL().QueryRow("SELECT count(*) FROM requirements").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rollback failed: n=%d", n)
	}
}

func TestWithTx_ConcurrentWritersSerialized(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	var wg sync.WaitGroup
	const N = 20
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
				_, err := tx.Exec(
					`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
                     VALUES (?, 'p', 'h', 0, 0)`,
					fmt.Sprintf("REQ-%d", i),
				)
				return err
			})
		}(i)
	}
	wg.Wait()
	var n int
	if err := h.SQL().QueryRow("SELECT count(*) FROM requirements").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != N {
		t.Fatalf("lost writes under concurrency: got %d want %d", n, N)
	}
}
