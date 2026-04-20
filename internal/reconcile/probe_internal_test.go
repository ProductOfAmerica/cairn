package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCheckBlob_RespectsContextCancellation guards M13: the io.Copy hash loop
// inside checkBlob must consult ctx.Done() per Read so that hashing a multi-GB
// blob can be interrupted (e.g. Ctrl-C during `cairn reconcile`) without
// waiting for the entire file to drain.
//
// We pre-cancel the context, then call checkBlob; the very first Read should
// fail with context.Canceled, surfaced through io.Copy's wrapped error.
func TestCheckBlob_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blob := filepath.Join(dir, "blob.bin")

	// Real bytes (not sparse) so io.Copy actually iterates if cancellation
	// is somehow ignored — the test would still hash and return ok=true,
	// failing the err==nil assertion below. 4 MiB is plenty.
	data := bytes.Repeat([]byte{'X'}, 4*1024*1024)
	if err := os.WriteFile(blob, data, 0o600); err != nil {
		t.Fatal(err)
	}

	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling — every Read should fail immediately

	_, ok, err := checkBlob(ctx, blob, expectedHex)
	if err == nil {
		t.Fatalf("expected canceled error, got nil (ok=%v)", ok)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled in error chain, got %v", err)
	}
}
