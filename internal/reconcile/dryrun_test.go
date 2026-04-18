package reconcile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestDryRun_EmitsZeroEventsAndNoWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)
	blobRoot := t.TempDir()

	// Seed one expired claim so rule 1 would mutate.
	seedLeaseFixture(t, h, 5000)

	// Seed one evidence row with a deleted blob so rule 3 would mutate.
	shas := seedEvidence(t, h, blobRoot, 1)
	var uri string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri)
	_ = os.Remove(uri)

	var eventsBefore int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsBefore)
	var taskStatusBefore string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&taskStatusBefore)

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	dr, err := orch.DryRun(context.Background(), reconcile.Opts{EvidenceSampleFull: true})
	if err != nil {
		t.Fatal(err)
	}

	// Zero writes: events count unchanged, task status unchanged.
	var eventsAfter int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsAfter)
	if eventsAfter != eventsBefore {
		t.Errorf("events changed %d -> %d in dry-run", eventsBefore, eventsAfter)
	}
	var statusAfter string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&statusAfter)
	if statusAfter != taskStatusBefore {
		t.Errorf("task status changed %q -> %q in dry-run", taskStatusBefore, statusAfter)
	}

	// DryRunResult contains the would-be mutations.
	totalWouldMutate := 0
	for _, r := range dr.Rules {
		totalWouldMutate += len(r.Mutations)
	}
	if totalWouldMutate < 2 {
		t.Errorf("expected >=2 would-mutate entries (rule 1 release + revert + rule 3 invalidate); got %d",
			totalWouldMutate)
	}
}
