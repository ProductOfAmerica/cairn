package reconcile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestRule3_InvalidatesMissingBlob(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	// Seed two evidence rows.
	shas := seedEvidence(t, h, blobRoot, 2)

	// Delete blob[0] on disk.
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = os.Remove(uri0)

	// Probe outside tx.
	candidates, err := reconcile.RunEvidenceProbe(context.Background(), h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	var result reconcile.RuleResult
	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule3EvidenceInvalid != 1 {
		t.Errorf("invalidated = %d, want 1", result.Stats.Rule3EvidenceInvalid)
	}
	var inv int64
	_ = h.SQL().QueryRow(`SELECT invalidated_at FROM evidence WHERE sha256=?`, shas[0]).Scan(&inv)
	if inv != 10000 {
		t.Errorf("invalidated_at = %d, want 10000", inv)
	}
}

func TestRule3_ReStatSkipsRecoveredBlob(t *testing.T) {
	// Scenario: probe says "missing", but before the tx commits, another
	// process `evidence put` recreates the blob at the exact sha256.
	// Re-stat inside the tx must observe presence + matching hash and skip.
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 1)
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)

	// Simulate stale probe: craft a candidate manually (file still present).
	candidates := []reconcile.EvidenceCandidate{{
		EvidenceID: "E-bogus", // id is not used in the re-stat
		Sha256:     shas[0],
		URI:        uri0,
		Reason:     "missing", // probe's stale conclusion
	}}

	// Look up real evidence_id so the mutation can touch the row.
	var realID string
	_ = h.SQL().QueryRow(`SELECT id FROM evidence WHERE sha256=?`, shas[0]).Scan(&realID)
	candidates[0].EvidenceID = realID

	var result reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		if err != nil {
			t.Fatal(err)
		}
		result = r
		return nil
	})
	if result.Stats.Rule3EvidenceInvalid != 0 {
		t.Errorf("re-stat should have skipped; got invalidated=%d",
			result.Stats.Rule3EvidenceInvalid)
	}
	var inv *int64
	_ = h.SQL().QueryRow(`SELECT invalidated_at FROM evidence WHERE sha256=?`, shas[0]).Scan(&inv)
	if inv != nil {
		t.Errorf("evidence should NOT be marked invalidated; got %v", *inv)
	}
}

func TestRule3_IsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 1)
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = os.Remove(uri0)

	var realID string
	_ = h.SQL().QueryRow(`SELECT id FROM evidence WHERE sha256=?`, shas[0]).Scan(&realID)
	candidates := []reconcile.EvidenceCandidate{{
		EvidenceID: realID,
		Sha256:     shas[0],
		URI:        uri0,
		Reason:     "missing",
	}}

	// First application invalidates.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		return nil
	})
	// Second application must be a no-op (row already invalidated).
	var second reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-2", candidates)
		if err != nil {
			t.Fatal(err)
		}
		second = r
		return nil
	})
	if second.Stats.Rule3EvidenceInvalid != 0 {
		t.Errorf("second run should be no-op, got invalidated=%d", second.Stats.Rule3EvidenceInvalid)
	}
}
