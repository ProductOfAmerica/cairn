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

	// Claim with a very short TTL (2s) — widened from 1s to absorb Windows
	// timer resolution (~15.6 ms) and process-start overhead.
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "agent-A", "--ttl", "2s")
	firstClaim := env["data"].(map[string]any)["claim_id"].(string)
	_ = firstClaim

	// Immediately attempting a second claim must fail — lease still live.
	_, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "agent-B", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2 on contested claim, got %d", code)
	}

	// Wait for the lease to expire. 3 s gives a full second of margin beyond
	// the 2 s TTL to absorb OS timer jitter and process-start latency.
	time.Sleep(3 * time.Second)

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
		// Payload is unmarshalled as map[string]any by the JSON decoder.
		payload, _ := e["Payload"].(map[string]any)
		reason, _ := payload["reason"].(string)
		if kind == "claim_released" && reason == "expired" {
			sawExpired = true
		}
		if kind == "task_status_changed" && reason == "lease_expired" {
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

	// Fourth invocation with the SAME op_id but DIFFERENT --agent and --ttl
	// must STILL return the cached result from the first call. The op_log
	// is keyed on (op_id, kind) — argument changes are ignored on replay.
	env = runCairnExit(t, repo, cairnHome, 0,
		"--op-id", opID,
		"task", "claim", "TASK-001",
		"--agent", "DIFFERENT-AGENT", "--ttl", "999m",
	)
	thirdClaim := env["data"].(map[string]any)["claim_id"].(string)
	thirdRun := env["data"].(map[string]any)["run_id"].(string)
	if thirdClaim != firstClaim {
		t.Fatalf("replay with different args returned new claim_id: first=%s third=%s", firstClaim, thirdClaim)
	}
	if thirdRun != firstRun {
		t.Fatalf("replay with different args returned new run_id: first=%s third=%s", firstRun, thirdRun)
	}

	// Fifth invocation: still exactly 1 claim_acquired event (no dedupe hole).
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs = env["data"].(map[string]any)["events"].([]any)
	count = 0
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "claim_acquired" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 claim_acquired event after different-args replay, got %d", count)
	}

	// Fifth invocation reusing the first op_id but with a DIFFERENT command
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

	// Claim a task so we have a run to bind a verdict against later.
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	runID := env["data"].(map[string]any)["run_id"].(string)

	// Put legitimate evidence.
	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	env = runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	sha := env["data"].(map[string]any)["sha256"].(string)

	// Locate the blob on disk.
	var blobPath string
	if err := filepath.WalkDir(cairnHome, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) == sha {
			blobPath = path
		}
		return nil
	}); err != nil || blobPath == "" {
		t.Fatalf("could not locate blob for sha=%s: %v", sha, err)
	}

	// Tamper the blob.
	if err := os.WriteFile(blobPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("could not tamper blob: %v", err)
	}

	// Path 1: `cairn evidence verify` should fail with exit 4 + kind="evidence_hash_mismatch".
	env, code := runCairn(t, repo, cairnHome, "evidence", "verify", sha)
	if code != 4 {
		t.Fatalf("evidence verify: expected exit 4, got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "evidence_hash_mismatch")

	// Path 2: `cairn verdict report` must ALSO refuse to bind. The verdict
	// code path re-verifies the evidence as a safety gate (spec §5). On
	// tampered evidence the command exits 4 with a hash-mismatch signal.
	// The evidence file on disk (from put) still has its original content;
	// the CLI re-reads + stores the blob for --evidence path. If the blob
	// is already stored under that sha, report calls evidence.Verify which
	// re-reads from the blob store (tampered) and rejects.
	env, code = runCairn(t, repo, cairnHome, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	if code != 4 {
		t.Fatalf("verdict report on tampered blob: expected exit 4, got %d; env=%+v", code, env)
	}
	// error.code may be "hash_mismatch", "evidence_hash_mismatch", or
	// "blob_collision" depending on which gate fires first. All three
	// indicate the tampered blob was detected and the verdict was refused.
	e, _ := env["error"].(map[string]any)
	kind, _ := e["code"].(string)
	if kind != "hash_mismatch" && kind != "evidence_hash_mismatch" && kind != "blob_collision" {
		t.Fatalf("verdict report: expected error.code hash_mismatch, evidence_hash_mismatch, or blob_collision, got %q (env=%+v)", kind, env)
	}
}
