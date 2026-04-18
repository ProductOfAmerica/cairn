package reconcile_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// hash64 produces a 64-char hex string from a seed character; convenience
// for fixtures where the actual value doesn't matter, only equality.
func hash64(c byte) string {
	return strings.Repeat(string(c), 64)
}

// seedStalenessFixture inserts one requirement with one gate, one task,
// one claim, one run, and one verdict (status=pass). Caller can optionally
// drift the gate_def_hash on the gates row to simulate spec edits.
func seedStalenessFixture(t *testing.T, h *db.DB, gateHash, verdictGateHash string) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO gates (id, requirement_id, kind, definition_json,
		                  gate_def_hash, producer_kind, producer_config)
		                  VALUES ('AC-1','REQ-1','test','{}',?,'executable','{}')`, gateHash)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','["AC-1"]','done',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,9999999999999,'01HNBXBT9J6MGK3Z5R7WVXTM0A')`)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at, ended_at, outcome)
		                 VALUES ('RUN-1','T-1','CL-1',0,1,'done')`)
		_, _ = tx.Exec(`INSERT INTO verdicts (id, run_id, gate_id, status, score_json,
		                   producer_hash, gate_def_hash, inputs_hash,
		                   evidence_id, bound_at, sequence)
		                 VALUES ('V-1','RUN-1','AC-1','pass',NULL,?,?,?,NULL,1,1)`,
			hash64('p'), verdictGateHash, hash64('i'))
		return nil
	})
}

func TestRule2_FlipsStaleOnGateDrift(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Current gate_def_hash = 'a'*64; verdict was bound against 'b'*64.
	seedStalenessFixture(t, h, hash64('a'), hash64('b'))

	var result reconcile.RuleResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule2TasksFlippedStale != 1 {
		t.Errorf("flipped = %d, want 1", result.Stats.Rule2TasksFlippedStale)
	}
	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "stale" {
		t.Errorf("task status = %q, want stale", status)
	}
}

func TestRule2_LeavesFreshDone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Current gate and verdict hashes match.
	seedStalenessFixture(t, h, hash64('a'), hash64('a'))

	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			return err
		}
		if r.Stats.Rule2TasksFlippedStale != 0 {
			t.Errorf("flipped = %d, want 0", r.Stats.Rule2TasksFlippedStale)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRule2_LatestVerdictPrecedence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Seed: gate hash='a'. Verdict 1 (earlier) is fresh+pass. Verdict 2
	// (later) has drifted hash. Rule 2 must see the latest and mark stale.
	seedStalenessFixture(t, h, hash64('a'), hash64('a'))
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := tx.Exec(`INSERT INTO verdicts (id, run_id, gate_id, status, score_json,
		                      producer_hash, gate_def_hash, inputs_hash,
		                      evidence_id, bound_at, sequence)
		                    VALUES ('V-2','RUN-1','AC-1','pass',NULL,?,?,?,NULL,2,2)`,
			hash64('p'), hash64('b'), hash64('i'))
		return err
	})

	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			return err
		}
		if r.Stats.Rule2TasksFlippedStale != 1 {
			t.Errorf("latest-verdict precedence failed: flipped=%d, want 1",
				r.Stats.Rule2TasksFlippedStale)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Reference verdict package to keep imports stable.
var _ = verdict.LatestResult{}
