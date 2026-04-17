package task_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/task"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestOpLog_HitReturnsCached(t *testing.T) {
	h := openDB(t)
	clk := clock.NewFake(1)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0A"

	// First record: no hit.
	var firstResult struct{ V int }
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		cached, hit, err := store.CheckOpLog(opID, "task.claim")
		if err != nil {
			t.Fatal(err)
		}
		if hit {
			t.Fatal("expected miss on first call")
		}
		_ = cached
		// Write our sentinel.
		payload, _ := json.Marshal(struct{ V int }{V: 42})
		firstResult.V = 42
		return store.RecordOpLog(opID, "task.claim", payload)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second call with same opID: must hit.
	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		cached, hit, err := store.CheckOpLog(opID, "task.claim")
		if err != nil {
			t.Fatal(err)
		}
		if !hit {
			t.Fatal("expected hit on replay")
		}
		var got struct{ V int }
		_ = json.Unmarshal(cached, &got)
		if got.V != 42 {
			t.Fatalf("cached result mismatch: %+v", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpLog_KindMismatchIsConflict(t *testing.T) {
	h := openDB(t)
	clk := clock.NewFake(1)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0B"
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		return store.RecordOpLog(opID, "task.claim", []byte(`{}`))
	})
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, _, err := store.CheckOpLog(opID, "task.heartbeat")
		return err
	})
	if err == nil {
		t.Fatal("kind mismatch should error")
	}
}

func TestList_FilterByStatus(t *testing.T) {
	h := openDB(t)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                 depends_on_json, required_gates_json, status, created_at, updated_at)
		                 VALUES ('T-A','REQ-1','p','h','[]','[]','open',0,0),
		                        ('T-B','REQ-1','p','h','[]','[]','done',0,0)`)
		return nil
	})

	clk := clock.NewFake(1)
	var openOnly []task.TaskRow
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		list, err := store.List("open")
		openOnly = list
		return err
	})
	if len(openOnly) != 1 || openOnly[0].ID != "T-A" {
		t.Fatalf("unexpected list: %+v", openOnly)
	}
}
