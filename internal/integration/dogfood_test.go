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
