package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// TestCrossCheckPutShas_MatchReturnsNil exercises the happy path of Put's
// pass-1/pass-2 sha cross-check: when the bytes hashed in pass 1 match the
// bytes written in pass 2, the helper returns nil and Put proceeds to the
// evidence-row insert.
func TestCrossCheckPutShas_MatchReturnsNil(t *testing.T) {
	t.Parallel()
	data := []byte("identical-content")
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	if err := crossCheckPutShas("/tmp/file", sha, sum, int64(len(data)), int64(len(data))); err != nil {
		t.Fatalf("expected nil for matching shas, got: %v", err)
	}
}

// TestCrossCheckPutShas_MismatchReturnsSourceMutated proves the TOCTOU
// detection wired into Put: when pass 2's observed sha differs from pass
// 1's, the helper returns a CodeSubstrate "source_mutated" error whose
// Details map carries both shas and both byte counts so operators can
// diagnose the race.
//
// Reproducing the real race in Put end-to-end requires a deterministic
// mid-write mutation harness that does not exist (and is not portable
// across Linux/macOS/Windows file-system semantics) — so the helper
// boundary is the deterministic test seam. See the TODO in Put.
func TestCrossCheckPutShas_MismatchReturnsSourceMutated(t *testing.T) {
	t.Parallel()

	pass1Data := []byte("original-content-aaaaaaaaaaaaaaaaa")
	pass2Data := []byte("mutated-content-bbbbbbbbbbbb")

	pass1Sum := sha256.Sum256(pass1Data)
	pass2Sum := sha256.Sum256(pass2Data)
	pass1Sha := hex.EncodeToString(pass1Sum[:])
	pass2Sha := hex.EncodeToString(pass2Sum[:])

	err := crossCheckPutShas("/tmp/race", pass1Sha, pass2Sum,
		int64(len(pass1Data)), int64(len(pass2Data)))
	if err == nil {
		t.Fatal("expected source_mutated error, got nil")
	}

	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("expected *cairnerr.Err, got %T: %v", err, err)
	}
	if ce.Code != cairnerr.CodeSubstrate {
		t.Errorf("Code = %q, want %q", ce.Code, cairnerr.CodeSubstrate)
	}
	if ce.Kind != "source_mutated" {
		t.Errorf("Kind = %q, want source_mutated", ce.Kind)
	}

	// Details map must carry both shas + both byte counts so operators
	// can tell which pass disagreed and by how much.
	if got, _ := ce.Details["path"].(string); got != "/tmp/race" {
		t.Errorf("Details[path] = %v, want /tmp/race", ce.Details["path"])
	}
	if got, _ := ce.Details["pass1_sha"].(string); got != pass1Sha {
		t.Errorf("Details[pass1_sha] = %v, want %s", ce.Details["pass1_sha"], pass1Sha)
	}
	if got, _ := ce.Details["pass2_sha"].(string); got != pass2Sha {
		t.Errorf("Details[pass2_sha] = %v, want %s", ce.Details["pass2_sha"], pass2Sha)
	}
	if got, _ := ce.Details["pass1_bytes"].(int64); got != int64(len(pass1Data)) {
		t.Errorf("Details[pass1_bytes] = %v, want %d", ce.Details["pass1_bytes"], len(pass1Data))
	}
	if got, _ := ce.Details["pass2_bytes"].(int64); got != int64(len(pass2Data)) {
		t.Errorf("Details[pass2_bytes] = %v, want %d", ce.Details["pass2_bytes"], len(pass2Data))
	}
}
