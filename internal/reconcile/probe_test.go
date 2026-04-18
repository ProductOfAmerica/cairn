package reconcile_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// seedCounter guarantees unique blob content across successive seedEvidence
// calls in the same test — using only `i` would collide when a test seeds two
// batches back-to-back, since evidence dedupes on sha256.
var seedCounter int

// seedEvidence inserts N blobs and returns their sha256s in insertion order.
func seedEvidence(t *testing.T, h *db.DB, blobRoot string, n int) []string {
	t.Helper()
	clk := clock.NewFake(100)
	var shas []string
	for i := 0; i < n; i++ {
		src := filepath.Join(t.TempDir(), "src.txt")
		_ = os.WriteFile(src, []byte(string(rune('a'+seedCounter))), 0o644)
		seedCounter++
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot, clk)
			r, err := store.Put("", src, "")
			if err != nil {
				return err
			}
			shas = append(shas, r.SHA256)
			return nil
		})
	}
	return shas
}

func TestProbe_DetectsMissingAndMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 5)

	// Break blob[0]: delete file. Break blob[1]: mutate content.
	var uri0, uri1 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[1]).Scan(&uri1)
	_ = os.Remove(uri0)
	_ = os.WriteFile(uri1, []byte("tampered"), 0o644)

	// --evidence-sample-full mode: scan everything.
	candidates, err := reconcile.RunEvidenceProbe(context.Background(), h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(candidates), candidates)
	}

	byReason := map[string]int{}
	for _, c := range candidates {
		byReason[c.Reason]++
	}
	if byReason["missing"] != 1 || byReason["hash_mismatch"] != 1 {
		t.Errorf("reasons = %+v", byReason)
	}
}

func TestProbe_SampleRespectsCapAndPct(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	blobRoot := t.TempDir()

	seedEvidence(t, h, blobRoot, 500)

	// Default 5% / cap 100 → min(100, ceil(500*0.05)) = 25.
	sampled, err := reconcile.SampleSize(h, reconcile.ProbeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 25 {
		t.Errorf("default sample = %d, want 25 (5%% of 500)", sampled)
	}

	// With 2000 rows: min(100, ceil(2000*0.05)) = 100 (cap).
	seedEvidence(t, h, blobRoot, 1500) // now 2000 total
	sampled, err = reconcile.SampleSize(h, reconcile.ProbeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 100 {
		t.Errorf("capped sample = %d, want 100", sampled)
	}

	// Full mode returns total count.
	sampled, err = reconcile.SampleSize(h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 2000 {
		t.Errorf("full sample = %d, want 2000", sampled)
	}
}

// unused helper kept to silence unused-import warnings in some fixtures.
var _ = sha256.Sum256
var _ = hex.EncodeToString
