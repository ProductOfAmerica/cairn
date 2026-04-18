package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestOrchestrator_EmitsStartedEnded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(1000)
	blobRoot := t.TempDir()

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	result, err := orch.Run(context.Background(), reconcile.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReconcileID == "" {
		t.Error("empty reconcile_id")
	}
	if result.DryRun {
		t.Error("expected dry_run=false")
	}

	// Events: started + ended pair always present on real runs.
	var kinds []string
	rows, _ := h.SQL().Query(`SELECT kind FROM events ORDER BY id`)
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	if len(kinds) < 2 {
		t.Fatalf("expected >=2 events; got %v", kinds)
	}
	if kinds[0] != "reconcile_started" || kinds[len(kinds)-1] != "reconcile_ended" {
		t.Errorf("bracket events not surrounding: %v", kinds)
	}
}

func TestOrchestrator_Rule4DependsOnRule1Ordering(t *testing.T) {
	// Scenario: one task, one claim with expires_at just past now; one
	// in-progress run against it. A single `Run` must:
	//   1. Release the claim (rule 1 sets released_at = now).
	//   2. NOT orphan the run (rule 4 sees released_at + 10min > now, skips).
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)
	blobRoot := t.TempDir()

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','claimed',0,0)`)
		// Expired a microsecond ago.
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,9999999,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at)
		                 VALUES ('RUN-1','T-1','CL-1',0)`)
		return nil
	})

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	if _, err := orch.Run(context.Background(), reconcile.Opts{}); err != nil {
		t.Fatal(err)
	}

	var outcome *string
	_ = h.SQL().QueryRow(`SELECT outcome FROM runs WHERE id='RUN-1'`).Scan(&outcome)
	if outcome != nil {
		t.Errorf("run must NOT be orphaned within grace; outcome=%v", *outcome)
	}
	var released int64
	_ = h.SQL().QueryRow(`SELECT released_at FROM claims WHERE id='CL-1'`).Scan(&released)
	if released != 10_000_000 {
		t.Errorf("claim not released by rule 1; released_at=%d", released)
	}
}

func TestOrchestrator_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	r1, _ := orch.Run(context.Background(), reconcile.Opts{})
	clk.Set(20000)
	r2, _ := orch.Run(context.Background(), reconcile.Opts{})

	// Second run has different reconcile_id (accurate record of two invocations)
	// but stats show zero mutations.
	if r1.ReconcileID == r2.ReconcileID {
		t.Error("reconcile_ids should differ between invocations")
	}
	totalSecond := r2.Stats.Rule1ClaimsReleased + r2.Stats.Rule1TasksReverted +
		r2.Stats.Rule2TasksFlippedStale + r2.Stats.Rule3EvidenceInvalid +
		r2.Stats.Rule4RunsOrphaned
	if totalSecond != 0 {
		t.Errorf("second run not idempotent; stats: %+v", r2.Stats)
	}
}
