package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCairn invokes the cairn binary with the given args, in cwd `dir`,
// with CAIRN_HOME pointing at a scratch state-root. Returns stdout + exit code.
func runCairn(t *testing.T, dir, cairnHome string, args ...string) (map[string]any, int) {
	t.Helper()
	cmd := exec.Command(cairnBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	exitCode := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("cairn %v: %v\nstderr: %s", args, err, errb.String())
	}
	stripped := bytes.TrimSpace(out.Bytes())
	if len(stripped) == 0 {
		return nil, exitCode
	}
	var env map[string]any
	if err := json.Unmarshal(stripped, &env); err != nil {
		t.Fatalf("cairn %v: invalid JSON: %s\n(err=%v, stderr=%s)", args, out.String(), err, errb.String())
	}
	return env, exitCode
}

// mustDogfoodRepo creates a throwaway git repo + writes the Ship 1 dogfood spec.
func mustDogfoodRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run := func(args ...string) {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = d
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "commit", "--allow-empty", "-q", "-m", "bootstrap")

	_ = os.MkdirAll(filepath.Join(d, "specs", "requirements"), 0o755)
	_ = os.MkdirAll(filepath.Join(d, "specs", "tasks"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "specs", "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: demo
why: dogfood
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
`), 0o644)
	_ = os.WriteFile(filepath.Join(d, "specs", "tasks", "TASK-001.yaml"),
		[]byte(`id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
`), 0o644)
	return d
}

func TestShip1DogfoodEventCoverage(t *testing.T) {
	repo := mustDogfoodRepo(t)
	cairnHome := t.TempDir()

	// init.
	_, code := runCairn(t, repo, cairnHome, "init")
	if code != 0 {
		t.Fatal("init failed")
	}

	// spec validate.
	env, code := runCairn(t, repo, cairnHome, "spec", "validate")
	if code != 0 {
		t.Fatalf("validate: env=%+v", env)
	}

	// task plan.
	_, code = runCairn(t, repo, cairnHome, "task", "plan")
	if code != 0 {
		t.Fatal("task plan failed")
	}

	// task claim.
	env, code = runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "dogfood", "--ttl", "30m")
	if code != 0 {
		t.Fatalf("claim failed: %+v", env)
	}
	data := env["data"].(map[string]any)
	claimID := data["claim_id"].(string)
	runID := data["run_id"].(string)

	// "Run" the gate: produce an output file.
	outPath := filepath.Join(repo, "ok.txt")
	_ = os.WriteFile(outPath, []byte("ok"), 0o644)

	// evidence put.
	env, code = runCairn(t, repo, cairnHome, "evidence", "put", outPath)
	if code != 0 {
		t.Fatalf("evidence put: %+v", env)
	}

	// verdict report (re-puts evidence internally; that's safe).
	env, code = runCairn(t, repo, cairnHome, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--inputs-hash", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if code != 0 {
		t.Fatalf("verdict report: %+v", env)
	}

	// task complete.
	env, code = runCairn(t, repo, cairnHome, "task", "complete", claimID)
	if code != 0 {
		t.Fatalf("complete: %+v", env)
	}

	// events since 0 — extract distinct kinds.
	env, code = runCairn(t, repo, cairnHome, "events", "since", "0", "--limit", "500")
	if code != 0 {
		t.Fatalf("events since: %+v", env)
	}
	evs := env["data"].(map[string]any)["events"].([]any)
	kinds := map[string]bool{}
	for _, raw := range evs {
		e := raw.(map[string]any)
		kinds[e["Kind"].(string)] = true
	}
	expected := []string{
		"task_planned", "spec_materialized",
		"claim_acquired", "run_started",
		"task_status_changed",
		"evidence_stored",
		"verdict_bound",
		"run_ended", "claim_released",
	}
	for _, want := range expected {
		if !kinds[want] {
			t.Errorf("missing event kind: %s (emitted set: %v)", want, kindNames(kinds))
		}
	}
}

func kindNames(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestShip1Dogfood_SpecEditFlipsStale verifies spec-edit → re-plan →
// verdict-latest-fresh=false.
func TestShip1Dogfood_SpecEditFlipsStale(t *testing.T) {
	repo := mustDogfoodRepo(t)
	cairnHome := t.TempDir()

	_, _ = runCairn(t, repo, cairnHome, "init")
	_, _ = runCairn(t, repo, cairnHome, "task", "plan")
	env, _ := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "a", "--ttl", "30m")
	runID := env["data"].(map[string]any)["run_id"].(string)

	outPath := filepath.Join(repo, "ok.txt")
	_ = os.WriteFile(outPath, []byte("ok"), 0o644)

	_, _ = runCairn(t, repo, cairnHome, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64))

	// Sanity: latest = fresh.
	env, _ = runCairn(t, repo, cairnHome, "verdict", "latest", "AC-001")
	if !env["data"].(map[string]any)["fresh"].(bool) {
		t.Fatal("expected fresh=true before spec edit")
	}

	// Edit the gate's config to change gate_def_hash.
	_ = os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: demo
why: dogfood (edited)
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, changed]
        pass_on_exit_code: 0
`), 0o644)
	_, _ = runCairn(t, repo, cairnHome, "task", "plan")

	env, _ = runCairn(t, repo, cairnHome, "verdict", "latest", "AC-001")
	if env["data"].(map[string]any)["fresh"].(bool) {
		t.Fatal("expected fresh=false after gate config edit")
	}
}

// TestShip2DogfoodEventCoverage locks down the event-log completeness
// contract for Ship 2 kinds. It runs the full Ship 1 dogfood flow, appends
// a memory entry, deletes an evidence blob on disk, then runs
// `reconcile --evidence-sample-full`. After that sequence the event log
// must contain:
//
//	Ship 1 kinds (regression guard): task_planned, spec_materialized,
//	  claim_acquired, run_started, task_status_changed, evidence_stored,
//	  verdict_bound, run_ended, claim_released
//
//	Ship 2 kinds (new coverage): memory_appended, reconcile_started,
//	  reconcile_ended, reconcile_rule_applied, evidence_invalidated
//
// Ship 1's own test (TestShip1DogfoodEventCoverage) stays untouched — it
// still runs independently as the pure Ship 1 regression guard.
func TestShip2DogfoodEventCoverage(t *testing.T) {
	repo := mustDogfoodRepo(t)
	cairnHome := t.TempDir()

	// -------------------------------------------------------------------
	// Phase 1 — full Ship 1 dogfood flow. Mirrors the body of
	// TestShip1DogfoodEventCoverage. Kept inline (not extracted) because
	// test scaffolding reads better when each test owns its own setup.
	// -------------------------------------------------------------------
	if _, code := runCairn(t, repo, cairnHome, "init"); code != 0 {
		t.Fatal("init failed")
	}
	if env, code := runCairn(t, repo, cairnHome, "spec", "validate"); code != 0 {
		t.Fatalf("validate: env=%+v", env)
	}
	if _, code := runCairn(t, repo, cairnHome, "task", "plan"); code != 0 {
		t.Fatal("task plan failed")
	}

	claimEnv, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "dogfood", "--ttl", "30m")
	if code != 0 {
		t.Fatalf("claim failed: %+v", claimEnv)
	}
	claimData := claimEnv["data"].(map[string]any)
	claimID := claimData["claim_id"].(string)
	runID := claimData["run_id"].(string)

	outPath := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(outPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write ok.txt: %v", err)
	}

	putEnv, code := runCairn(t, repo, cairnHome, "evidence", "put", outPath)
	if code != 0 {
		t.Fatalf("evidence put: %+v", putEnv)
	}
	sha, _ := putEnv["data"].(map[string]any)["sha256"].(string)
	if len(sha) != 64 {
		t.Fatalf("evidence put: sha256 missing/short: %+v", putEnv)
	}

	if env, code := runCairn(t, repo, cairnHome, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64)); code != 0 {
		t.Fatalf("verdict report: %+v", env)
	}
	if env, code := runCairn(t, repo, cairnHome, "task", "complete", claimID); code != 0 {
		t.Fatalf("complete: %+v", env)
	}

	// -------------------------------------------------------------------
	// Phase 2 — memory append. Emits `memory_appended`.
	// -------------------------------------------------------------------
	if env, code := runCairn(t, repo, cairnHome, "memory", "append",
		"--kind", "decision", "--body", "smoke"); code != 0 {
		t.Fatalf("memory append: %+v", env)
	}

	// -------------------------------------------------------------------
	// Phase 3 — delete the evidence blob on disk so reconcile rule 3 fires.
	// Look up the blob URI via `evidence get` (never read sqlite directly).
	// -------------------------------------------------------------------
	getEnv, code := runCairn(t, repo, cairnHome, "evidence", "get", sha)
	if code != 0 {
		t.Fatalf("evidence get: %+v", getEnv)
	}
	blobURI, _ := getEnv["data"].(map[string]any)["uri"].(string)
	if blobURI == "" {
		t.Fatalf("evidence get: uri missing: %+v", getEnv)
	}
	if err := os.Remove(blobURI); err != nil {
		t.Fatalf("remove blob %q: %v", blobURI, err)
	}

	// -------------------------------------------------------------------
	// Phase 4 — reconcile with full evidence sample. This run emits:
	//   reconcile_started, evidence_invalidated, reconcile_rule_applied,
	//   reconcile_ended.
	//
	// Asserting rule 3 actually flipped a row guarantees the rule-applied
	// and evidence_invalidated events are real, not false-negatives from
	// an empty sample.
	// -------------------------------------------------------------------
	recEnv, code := runCairn(t, repo, cairnHome, "reconcile", "--evidence-sample-full")
	if code != 0 {
		t.Fatalf("reconcile: %+v", recEnv)
	}
	stats := recEnv["data"].(map[string]any)["stats"].(map[string]any)
	if got, _ := stats["rule_3_evidence_invalidated"].(float64); int(got) != 1 {
		t.Fatalf("rule_3_evidence_invalidated=%v, want 1; stats=%+v", got, stats)
	}

	// -------------------------------------------------------------------
	// Phase 5 — fetch events and assert every expected kind is present.
	// Set-membership, order-independent.
	// -------------------------------------------------------------------
	evEnv, code := runCairn(t, repo, cairnHome, "events", "since", "0", "--limit", "500")
	if code != 0 {
		t.Fatalf("events since: %+v", evEnv)
	}
	evs := evEnv["data"].(map[string]any)["events"].([]any)
	kinds := map[string]bool{}
	for _, raw := range evs {
		e := raw.(map[string]any)
		kinds[e["Kind"].(string)] = true
	}

	expected := []string{
		// Ship 1 — regression guard. If any drops out, the Ship 1 flow
		// stopped producing it and we want to know in this test too.
		"task_planned", "spec_materialized",
		"claim_acquired", "run_started",
		"task_status_changed",
		"evidence_stored",
		"verdict_bound",
		"run_ended", "claim_released",
		// Ship 2 — the kinds this test was added to cover.
		"memory_appended",
		"reconcile_started",
		"reconcile_rule_applied",
		"evidence_invalidated",
		"reconcile_ended",
	}
	for _, want := range expected {
		if !kinds[want] {
			t.Errorf("missing event kind: %s (emitted set: %v)", want, kindNames(kinds))
		}
	}
}
