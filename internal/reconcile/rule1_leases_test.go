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

// seedLeaseFixture inserts one requirement, one task (status=claimed), and
// one claim with expires_at < now. Returns (task_id, claim_id).
func seedLeaseFixture(t *testing.T, h *db.DB, claimExpiresAt int64) (string, string) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','claimed',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,?,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`, claimExpiresAt)
		return nil
	})
	return "T-1", "CL-1"
}

func TestRule1_ReleasesExpiredAndRevertsTask(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	seedLeaseFixture(t, h, 5000) // expires in the past

	var result reconcile.RuleResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule1ReleaseExpiredLeases(tx, events.NewAppender(clk), clk, "RC-1")
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule1ClaimsReleased != 1 {
		t.Errorf("released = %d, want 1", result.Stats.Rule1ClaimsReleased)
	}
	if result.Stats.Rule1TasksReverted != 1 {
		t.Errorf("reverted = %d, want 1", result.Stats.Rule1TasksReverted)
	}

	// Side effects on DB.
	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "open" {
		t.Errorf("task status = %q, want open", status)
	}
	var released int64
	_ = h.SQL().QueryRow(`SELECT released_at FROM claims WHERE id='CL-1'`).Scan(&released)
	if released != 10000 {
		t.Errorf("released_at = %d, want 10000", released)
	}

	// Events emitted.
	var n int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events
	                     WHERE kind IN ('claim_released','task_status_changed','reconcile_rule_applied')`).Scan(&n)
	if n < 3 {
		t.Errorf("events emitted = %d, want >=3", n)
	}
}

func TestRule1_LiveClaimIsIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	seedLeaseFixture(t, h, 99999999) // not expired

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := reconcile.Rule1ReleaseExpiredLeases(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		return nil
	})

	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "claimed" {
		t.Errorf("task should still be claimed, got %q", status)
	}
}
