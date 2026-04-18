package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvidenceInvalidationE2E locks down design spec §5.10 rows 1 and 2: the
// three-surface contract for evidence invalidation.
//
//  1. `verdict report` (new bindings) BLOCKS on invalidated evidence with
//     error kind "evidence_invalidated" and exit 1 (CodeValidation).
//  2. `verdict latest` and `verdict history` are informational — they still
//     return the verdict row but surface `evidence_invalidated: true` so
//     callers can react.
//
// Row 3 ("task complete" ignores invalidation — binary staleness unchanged)
// is covered separately by Task 17.6.
//
// The test drives the full loop end-to-end via the built binary: plan, claim,
// run, evidence put, verdict report, task complete, blob deletion, reconcile
// --evidence-sample-full, and then asserts the three surfaces.
func TestEvidenceInvalidationE2E(t *testing.T) {
	repo := mustReconcileRepo(t)
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "spec", "validate")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// -------------------------------------------------------------------
	// Step 2: claim + run + evidence put.
	// -------------------------------------------------------------------
	claimEnv := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	claimData := claimEnv["data"].(map[string]any)
	claimID, _ := claimData["claim_id"].(string)
	runID, _ := claimData["run_id"].(string)
	if claimID == "" || runID == "" {
		t.Fatalf("claim: missing claim_id/run_id: %+v", claimData)
	}

	outPath := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(outPath, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write ok.txt: %v", err)
	}
	putEnv := runCairnExit(t, repo, cairnHome, 0, "evidence", "put", outPath)
	sha, _ := putEnv["data"].(map[string]any)["sha256"].(string)
	if len(sha) != 64 {
		t.Fatalf("evidence put: sha256 missing/short: %+v", putEnv)
	}

	// -------------------------------------------------------------------
	// Step 3: bind a verdict. This is the row the three surfaces will
	// later report against.
	// -------------------------------------------------------------------
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)

	// -------------------------------------------------------------------
	// Step 4: complete TASK-001 so we have history. Not strictly required
	// for this test's asserts (Task 17.6 covers row 3 separately) but
	// matches the realistic lifecycle and makes the state well-formed.
	// -------------------------------------------------------------------
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

	// -------------------------------------------------------------------
	// Step 5: look up the blob URI via `evidence get` (not SQL) and delete
	// the blob on disk. Reconcile rule 3 will detect missing-on-disk.
	// -------------------------------------------------------------------
	getEnv := runCairnExit(t, repo, cairnHome, 0, "evidence", "get", sha)
	getData, _ := getEnv["data"].(map[string]any)
	blobURI, _ := getData["uri"].(string)
	if blobURI == "" {
		t.Fatalf("evidence get: uri missing: %+v", getEnv)
	}
	if err := os.Remove(blobURI); err != nil {
		t.Fatalf("remove blob %q: %v", blobURI, err)
	}

	// -------------------------------------------------------------------
	// Step 6: reconcile with full sample — exactly 1 evidence row flips.
	// -------------------------------------------------------------------
	recEnv := runCairnExit(t, repo, cairnHome, 0, "reconcile", "--evidence-sample-full")
	stats := recEnv["data"].(map[string]any)["stats"].(map[string]any)
	if got, _ := stats["rule_3_evidence_invalidated"].(float64); int(got) != 1 {
		t.Fatalf("rule_3_evidence_invalidated=%v, want 1; stats=%+v", got, stats)
	}

	// -------------------------------------------------------------------
	// Step 7 — Surface 1: `verdict report` against the invalidated sha
	// must BLOCK with exit 1 and error.code=evidence_invalidated.
	//
	// We need a fresh run to bind against (Report requires the run not to
	// have ended). Add a second task file, re-plan, claim it, then try to
	// bind. The verdict store's Report calls evidence.Verify() early, which
	// short-circuits on invalidated_at BEFORE any blob read — so blob
	// absence on disk doesn't matter for this assertion.
	// -------------------------------------------------------------------
	if err := os.WriteFile(filepath.Join(repo, "specs", "tasks", "TASK-002.yaml"),
		[]byte(`id: TASK-002
implements: [REQ-001]
required_gates: [AC-001]
`), 0o644); err != nil {
		t.Fatalf("write TASK-002: %v", err)
	}
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	claim2Env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-002",
		"--agent", "e2e", "--ttl", "30m")
	claim2Data := claim2Env["data"].(map[string]any)
	run2ID, _ := claim2Data["run_id"].(string)
	if run2ID == "" {
		t.Fatalf("claim TASK-002: missing run_id: %+v", claim2Data)
	}

	// `evidence put` will restore the blob on disk (dedupe by sha), but the
	// evidence row's invalidated_at is still set — Report's Verify must
	// reject before hashing. We re-put to cover the realistic case where
	// an agent re-runs the producer and tries to re-bind.
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", outPath)

	reportEnv, reportCode := runCairn(t, repo, cairnHome, "verdict", "report",
		"--gate", "AC-001", "--run", run2ID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	if reportCode != 1 {
		t.Fatalf("verdict report on invalidated evidence: expected exit 1, got %d; env=%+v",
			reportCode, reportEnv)
	}
	expectErrorKind(t, reportEnv, "evidence_invalidated")

	// -------------------------------------------------------------------
	// Step 8 — Surface 2: `verdict latest` and `verdict history` stay
	// informational and surface evidence_invalidated=true on the stored
	// verdict row. No exit-code change.
	// -------------------------------------------------------------------
	latestEnv := runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	latestData, _ := latestEnv["data"].(map[string]any)
	vRaw, _ := latestData["verdict"].(map[string]any)
	if vRaw == nil {
		t.Fatalf("verdict latest: verdict missing: %+v", latestEnv)
	}
	if inv, _ := vRaw["evidence_invalidated"].(bool); !inv {
		t.Fatalf("verdict latest: evidence_invalidated=%v, want true; verdict=%+v", inv, vRaw)
	}

	histEnv := runCairnExit(t, repo, cairnHome, 0, "verdict", "history", "AC-001")
	histData, _ := histEnv["data"].(map[string]any)
	verdicts, _ := histData["verdicts"].([]any)
	if len(verdicts) == 0 {
		t.Fatalf("verdict history: expected >=1 row, got %+v", histData)
	}
	for i, raw := range verdicts {
		row, _ := raw.(map[string]any)
		if inv, _ := row["evidence_invalidated"].(bool); !inv {
			t.Fatalf("verdict history[%d]: evidence_invalidated=%v, want true; row=%+v",
				i, inv, row)
		}
	}
}
