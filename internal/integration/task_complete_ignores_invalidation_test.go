package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// TestTaskCompleteIgnoresInvalidation locks down design spec §5.10 row 3:
// `cairn task complete` is deliberately UNAWARE of `evidence.invalidated_at`.
//
// Binary staleness for task completion is defined by verdict.Store.IsFreshPass
// as:  gate_def_hash match + status='pass'  — nothing else. An invalidated
// evidence row is a signal that (a) a producer must re-run before further
// bindings, surfaced to `verdict report` (row 1), and (b) informational on
// `verdict latest` / `verdict history` (row 2); it does NOT retroactively
// block a task whose verdict was recorded while the evidence was still
// healthy. Rows 1+2 are covered by TestEvidenceInvalidationE2E.
//
// Flow:
//  1. Seed repo + one REQ/gate AC-001 + one TASK-001.
//  2. Drive plan → claim → evidence put → verdict report(pass), but do NOT
//     complete yet.
//  3. Simulate a prior reconcile flipping the evidence row by writing
//     invalidated_at directly via SQL (the CLI never exposes this flip
//     outside rule 3).
//  4. `task complete <claim_id>` — MUST succeed (exit 0) and transition
//     TASK-001 to `done`.
//  5. Confirm via `task list` (never direct SQL reads — the test checks the
//     user-visible surface).
func TestTaskCompleteIgnoresInvalidation(t *testing.T) {
	repo := mustReconcileRepo(t)
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "spec", "validate")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// ---- Claim + evidence + verdict, but do NOT complete yet. ----
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

	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)

	// ---- Simulate a prior reconcile: flip the evidence row invalidated. ----
	// The CLI never exposes a direct invalidate-evidence command; rule 3 of
	// reconcile writes `invalidated_at` when a blob is missing/mismatched.
	// Direct SQL here is the minimum-privilege way to reproduce that state
	// without coupling the test to rule 3's probe semantics.
	dbPath := resolveStateDBPath(t, repo, cairnHome)
	invalidateEvidence(t, dbPath, sha)

	// ---- Complete must still succeed (exit 0). ----
	completeEnv := runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)
	expectEnvelopeKind(t, completeEnv, "task.complete")

	// ---- Assert TASK-001 is now `done` via the task-list surface. ----
	if got := taskStatus(t, repo, cairnHome, "TASK-001"); got != "done" {
		t.Fatalf("post-complete TASK-001 status = %q, want done", got)
	}

	// Belt-and-braces: the open list must NOT contain TASK-001.
	openEnv := runCairnExit(t, repo, cairnHome, 0, "task", "list", "--status", "open")
	openTasks, _ := openEnv["data"].(map[string]any)["tasks"].([]any)
	if containsTaskID(openTasks, "TASK-001") {
		t.Fatalf("TASK-001 still in open list after complete: %+v", openTasks)
	}

	// And the done list MUST contain it.
	doneEnv := runCairnExit(t, repo, cairnHome, 0, "task", "list", "--status", "done")
	doneTasks, _ := doneEnv["data"].(map[string]any)["tasks"].([]any)
	if !containsTaskID(doneTasks, "TASK-001") {
		t.Fatalf("done list does not include TASK-001: %+v", doneTasks)
	}
}

// invalidateEvidence writes `invalidated_at` on the evidence row with the
// given sha. Uses the same db package the CLI uses so the connection string,
// pragmas, and migration state stay identical — no driver drift with modernc's
// SQLite wrapper.
func invalidateEvidence(t *testing.T, dbPath, sha string) {
	t.Helper()
	h, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db %q: %v", dbPath, err)
	}
	defer h.Close()
	res, err := h.SQL().Exec(
		`UPDATE evidence SET invalidated_at = ? WHERE sha256 = ?`,
		time.Now().UnixMilli(), sha,
	)
	if err != nil {
		t.Fatalf("UPDATE evidence: %v", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	if n != 1 {
		t.Fatalf("UPDATE evidence affected %d rows, want 1 (sha=%s)", n, sha)
	}
}
