package db_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestWithTx_ContentionAbsorbedByBusyTimeout(t *testing.T) {
	// Two DB handles against the same file simulate cross-connection
	// contention. Goroutine A holds an open write txn past a sync point.
	// Goroutine B attempts its own write txn and initially blocks at
	// BEGIN IMMEDIATE. When A releases, B's busy_timeout=5000 absorbs the
	// wait and the write commits. Without busy_timeout, B would fail
	// immediately with BUSY.
	p := filepath.Join(t.TempDir(), "state.db")
	hA, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hA.Close() })
	hB, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hB.Close() })

	release := make(chan struct{})
	done := make(chan error, 1)

	// A: acquire write lock via an INSERT, then block on `release`.
	go func() {
		_ = hA.WithTx(context.Background(), func(tx *db.Tx) error {
			_, _ = tx.Exec(
				`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
                 VALUES ('A', 'p', 'h', 0, 0)`,
			)
			<-release
			return nil
		})
	}()

	// Give A time to open its txn + write.
	time.Sleep(50 * time.Millisecond)

	// B: attempt a concurrent write. Expect it to succeed (busy_timeout spins).
	go func() {
		done <- hB.WithTx(context.Background(), func(tx *db.Tx) error {
			_, err := tx.Exec(
				`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
                 VALUES ('B', 'p', 'h', 0, 0)`,
			)
			return err
		})
	}()

	// Release A after a short delay — well under busy_timeout=5000ms.
	time.AfterFunc(150*time.Millisecond, func() { close(release) })

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("B should succeed after A releases: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("B never completed within 3s")
	}

	// Both rows should be persisted.
	var n int
	if err := hA.SQL().QueryRow("SELECT count(*) FROM requirements").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("both writes should have committed: got %d rows", n)
	}
}
