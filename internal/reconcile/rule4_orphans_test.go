package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// seedOrphanFixture inserts one task, one claim (released_at set), one run
// in-progress. Caller controls released_at.
func seedOrphanFixture(t *testing.T, h *db.DB, releasedAt int64) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','open',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at,
		                                     released_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,1,?,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`, releasedAt)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at)
		                 VALUES ('RUN-1','T-1','CL-1',0)`)
		return nil
	})
}

func TestRule4_OrphansAfterGrace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000) // 10,000,000ms

	// Released 11 min ago (11*60*1000 = 660,000ms prior = 9,340,000).
	seedOrphanFixture(t, h, 9_340_000)

	var result reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule4OrphanExpiredRuns(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		result = r
		return nil
	})
	if result.Stats.Rule4RunsOrphaned != 1 {
		t.Errorf("orphaned = %d, want 1", result.Stats.Rule4RunsOrphaned)
	}
	var outcome string
	_ = h.SQL().QueryRow(`SELECT outcome FROM runs WHERE id='RUN-1'`).Scan(&outcome)
	if outcome != "orphaned" {
		t.Errorf("outcome = %q, want orphaned", outcome)
	}
}

func TestRule4_WithinGraceIsSkipped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)

	// Released 5 min ago (within 10min grace).
	seedOrphanFixture(t, h, 9_700_000)

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule4OrphanExpiredRuns(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Stats.Rule4RunsOrphaned != 0 {
			t.Errorf("orphaned = %d, want 0", r.Stats.Rule4RunsOrphaned)
		}
		return nil
	})
}
