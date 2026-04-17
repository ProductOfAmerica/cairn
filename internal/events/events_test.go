package events_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

func TestAppend_VisibleAfterCommit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	clk := clock.NewFake(1_000)
	appender := events.NewAppender(clk)

	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		return appender.Append(tx, events.Record{
			Kind:       "task_planned",
			EntityKind: "task",
			EntityID:   "TASK-001",
			Payload:    map[string]any{"hello": "world"},
			OpID:       "01HNBXBT9J6MGK3Z5R7WVXTM0P",
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	ev, err := events.Since(h.SQL(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 1 {
		t.Fatalf("got %d events, want 1", len(ev))
	}
	if ev[0].Kind != "task_planned" {
		t.Errorf("kind=%s", ev[0].Kind)
	}
	if ev[0].At != 1_000 {
		t.Errorf("at=%d want 1000", ev[0].At)
	}
	var pl map[string]any
	if err := json.Unmarshal(ev[0].Payload, &pl); err != nil {
		t.Fatal(err)
	}
	if pl["hello"] != "world" {
		t.Errorf("payload roundtrip failed: %+v", pl)
	}
	if !ev[0].OpID.Valid || ev[0].OpID.String != "01HNBXBT9J6MGK3Z5R7WVXTM0P" {
		t.Errorf("op_id roundtrip failed: %+v", ev[0].OpID)
	}
}

func TestAppend_RollbackDiscards(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	appender := events.NewAppender(clock.NewFake(1))
	sentinel := errForceRollback
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		if err := appender.Append(tx, events.Record{
			Kind: "task_planned", EntityKind: "task", EntityID: "X",
			Payload: map[string]any{}, OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0P",
		}); err != nil {
			t.Fatal(err)
		}
		return sentinel
	})
	ev, err := events.Since(h.SQL(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Fatalf("rollback should discard events, got %d", len(ev))
	}
}

var errForceRollback = &rollbackSentinel{}

type rollbackSentinel struct{}

func (*rollbackSentinel) Error() string { return "force rollback" }

func TestKinds_GroupCounts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	clk := clock.NewFake(1)
	appender := events.NewAppender(clk)
	// Emit two task_planned + one claim_acquired across separate txns.
	for i := 0; i < 2; i++ {
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			return appender.Append(tx, events.Record{
				Kind: "task_planned", EntityKind: "task", EntityID: "T",
				Payload: map[string]any{},
			})
		})
	}
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		return appender.Append(tx, events.Record{
			Kind: "claim_acquired", EntityKind: "claim", EntityID: "C",
			Payload: map[string]any{},
		})
	})

	kinds, err := events.Kinds(h.SQL(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if kinds["task_planned"] != 2 {
		t.Errorf("task_planned count = %d, want 2", kinds["task_planned"])
	}
	if kinds["claim_acquired"] != 1 {
		t.Errorf("claim_acquired count = %d, want 1", kinds["claim_acquired"])
	}
}
