package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_SpecEditStale(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "spec-edit-stale")

	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	runID := env["data"].(map[string]any)["run_id"].(string)
	claimID := env["data"].(map[string]any)["claim_id"].(string)

	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); !fresh {
		t.Fatal("expected fresh=true before spec edit")
	}

	// REQ-001-edited.yaml is stored at the repo root (outside specs/) so the
	// initial plan doesn't see it as a duplicate.
	data, err := os.ReadFile(filepath.Join(repo, "REQ-001-edited.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); fresh {
		t.Fatalf("expected fresh=false after gate edit, got env=%+v", env)
	}
}

func TestE2E_DepBlocking(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "dep-blocking")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Attempt to claim TASK-MAIN while TASK-DEP is still open. Should get
	// exit 2 with error.code="dep_not_done" and details listing TASK-DEP.
	env, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-MAIN",
		"--agent", "e2e", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "dep_not_done")

	// Details should mention TASK-DEP with status=open.
	e, _ := env["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	blocking, _ := details["blocking"].([]any)
	if len(blocking) != 1 {
		t.Fatalf("expected 1 blocking dep, got %d", len(blocking))
	}
	b := blocking[0].(map[string]any)
	if b["id"] != "TASK-DEP" {
		t.Errorf("blocking[0].id=%v, want TASK-DEP", b["id"])
	}
	if b["status"] != "open" {
		t.Errorf("blocking[0].status=%v, want open", b["status"])
	}

	// Complete TASK-DEP (claim → evidence → verdict → complete).
	env = runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-DEP",
		"--agent", "e2e", "--ttl", "30m")
	depRun := env["data"].(map[string]any)["run_id"].(string)
	depClaim := env["data"].(map[string]any)["claim_id"].(string)

	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", depRun, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", depClaim)

	// Now TASK-MAIN should claim cleanly.
	runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-MAIN",
		"--agent", "e2e", "--ttl", "30m")
}

func TestE2E_LeaseExpiry(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "lease-expiry")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Claim with a very short TTL (1s).
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "agent-A", "--ttl", "1s")
	firstClaim := env["data"].(map[string]any)["claim_id"].(string)
	_ = firstClaim

	// Immediately attempting a second claim must fail — lease still live.
	_, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "agent-B", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2 on contested claim, got %d", code)
	}

	// Wait for the lease to expire. Add a margin for OS timer resolution.
	time.Sleep(1500 * time.Millisecond)

	// Second claim should now succeed — inline rule-1 cleanup releases the
	// expired claim and reverts the task to open before the new CAS.
	env = runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "agent-B", "--ttl", "30m")
	secondClaim := env["data"].(map[string]any)["claim_id"].(string)
	if secondClaim == "" {
		t.Fatal("second claim did not return a claim_id")
	}

	// Events should show the expired-lease cleanup: claim_released (expired)
	// and a task_status_changed entry with reason=lease_expired.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	sawExpired := false
	sawLeaseExpiredTransition := false
	for _, raw := range evs {
		e := raw.(map[string]any)
		kind, _ := e["Kind"].(string)
		payloadRaw := e["Payload"]
		var payloadStr string
		switch p := payloadRaw.(type) {
		case string:
			payloadStr = p
		case []byte:
			payloadStr = string(p)
		}
		if kind == "claim_released" && strings.Contains(payloadStr, `"reason":"expired"`) {
			sawExpired = true
		}
		if kind == "task_status_changed" && strings.Contains(payloadStr, `"reason":"lease_expired"`) {
			sawLeaseExpiredTransition = true
		}
	}
	if !sawExpired {
		t.Error("expected claim_released{reason:expired} event, not found")
	}
	if !sawLeaseExpiredTransition {
		t.Error("expected task_status_changed{reason:lease_expired} event, not found")
	}
}

func TestE2E_OpIDReplay(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "op-id-replay")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0Q"
	args := []string{"--op-id", opID, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m"}

	// First subprocess invocation.
	env := runCairnExit(t, repo, cairnHome, 0, args...)
	firstClaim := env["data"].(map[string]any)["claim_id"].(string)
	firstRun := env["data"].(map[string]any)["run_id"].(string)

	// Second subprocess invocation with the SAME op_id — must return the
	// cached result verbatim.
	env = runCairnExit(t, repo, cairnHome, 0, args...)
	secondClaim := env["data"].(map[string]any)["claim_id"].(string)
	secondRun := env["data"].(map[string]any)["run_id"].(string)
	if firstClaim != secondClaim {
		t.Fatalf("claim_id differs on replay: first=%s second=%s", firstClaim, secondClaim)
	}
	if firstRun != secondRun {
		t.Fatalf("run_id differs on replay: first=%s second=%s", firstRun, secondRun)
	}

	// Event-log must show exactly one claim_acquired — no duplicate side-effect.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	count := 0
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "claim_acquired" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 claim_acquired event, got %d", count)
	}

	// Third invocation with a DIFFERENT op_id + same task should now fail
	// with CodeConflict because the task is already claimed (not because of
	// op_id mismatch).
	env, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2 on already-claimed, got %d", code)
	}
	expectErrorKind(t, env, "task_not_claimable")

	// Fourth invocation reusing the first op_id but with a DIFFERENT command
	// (task.heartbeat) must error with op_id_kind_mismatch. claim_id must be
	// the one returned in step 1.
	env, code = runCairn(t, repo, cairnHome, "--op-id", opID,
		"task", "heartbeat", firstClaim)
	if code != 2 {
		t.Fatalf("expected exit 2 on op_id_kind_mismatch, got %d env=%+v", code, env)
	}
	expectErrorKind(t, env, "op_id_kind_mismatch")
}

func TestE2E_EvidenceHashMismatch(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "evidence-hash-mismatch")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Put legitimate evidence.
	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	sha := env["data"].(map[string]any)["sha256"].(string)

	// Corrupt the blob on disk directly. Find the blob under
	// <cairnHome>/<repoId>/blobs/<sha[:2]>/<sha> and overwrite.
	// We don't know the repoId, so walk cairnHome.
	var blobPath string
	err := filepath.WalkDir(cairnHome, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == sha {
			blobPath = path
		}
		return nil
	})
	if err != nil || blobPath == "" {
		t.Fatalf("could not locate blob for sha=%s under %s: %v", sha, cairnHome, err)
	}
	if err := os.WriteFile(blobPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("could not tamper blob: %v", err)
	}

	// Evidence verify should now fail with CodeSubstrate + error.code
	// "hash_mismatch". Exit 4.
	env, code := runCairn(t, repo, cairnHome, "evidence", "verify", sha)
	if code != 4 {
		t.Fatalf("expected exit 4, got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "hash_mismatch")

	// An evidence_invalidated event must now be present.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	sawInvalidated := false
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "evidence_invalidated" {
			sawInvalidated = true
		}
	}
	if !sawInvalidated {
		t.Error("expected evidence_invalidated event after tamper, not found")
	}
}
