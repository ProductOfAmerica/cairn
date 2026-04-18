package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReconcileE2E exercises the Ship 2 dogfood loop end-to-end via the built
// binary. Each subtest has its own repo + CAIRN_HOME so state never leaks.
//
// Scenarios (PLAN.md §"Ship 2 dogfood" steps 2, 3+4, 5):
//   - rule2_drift_flip: done task flips to stale when the underlying gate's
//     producer config changes.
//   - memory_append_search: `memory append` + `memory search` round-trip and
//     a `memory_appended` event reaches the event log.
//   - rule1_expired_lease: a 1ms TTL claim is released by reconcile after the
//     wall clock expires, and the task reverts to open.
//
// Note on drift detection (subtest 1): gate_def_hash is computed over
// {id, kind, producer.kind, producer.config} — NOT over scope_in. The task
// description mentions `scope_in`, but actually triggering a hash change
// requires mutating the producer config (mirrors TestShip1Dogfood_SpecEditFlipsStale).
func TestReconcileE2E(t *testing.T) {
	t.Run("rule2_drift_flip", func(t *testing.T) {
		repo := mustReconcileRepo(t)
		cairnHome := t.TempDir()

		runCairnExit(t, repo, cairnHome, 0, "init")
		runCairnExit(t, repo, cairnHome, 0, "spec", "validate")
		runCairnExit(t, repo, cairnHome, 0, "task", "plan")

		// Claim + "run" the gate (echo ok) + bind a pass verdict + complete.
		claimEnv := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
			"--agent", "a", "--ttl", "30m")
		data := claimEnv["data"].(map[string]any)
		claimID, _ := data["claim_id"].(string)
		runID, _ := data["run_id"].(string)
		if claimID == "" || runID == "" {
			t.Fatalf("claim: missing claim_id/run_id: %+v", data)
		}

		// Gate is `command: [echo, ok]`. We don't need to actually execute it
		// to bind a verdict — we just need a file with the expected output
		// bytes so evidence.put has something to hash.
		outPath := filepath.Join(repo, "ok.txt")
		if err := os.WriteFile(outPath, []byte("ok\n"), 0o644); err != nil {
			t.Fatalf("write ok.txt: %v", err)
		}
		runCairnExit(t, repo, cairnHome, 0, "evidence", "put", outPath)
		runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
			"--gate", "AC-001", "--run", runID, "--status", "pass",
			"--evidence", outPath,
			"--producer-hash", strings.Repeat("a", 64),
			"--inputs-hash", strings.Repeat("b", 64))
		runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

		// Sanity: the task is currently `done`.
		if got := taskStatus(t, repo, cairnHome, "TASK-001"); got != "done" {
			t.Fatalf("pre-edit task status = %q, want done", got)
		}

		// Edit the gate's producer config so gate_def_hash changes. Adding a
		// scope_in entry would not be enough (it doesn't feed into gate_def_hash
		// — see internal/intent/hash.go).
		writeReq001(t, repo, "dogfood (edited)", "changed")
		runCairnExit(t, repo, cairnHome, 0, "task", "plan")

		// Reconcile should flip exactly one task stale.
		recEnv := runCairnExit(t, repo, cairnHome, 0, "reconcile")
		stats := recEnv["data"].(map[string]any)["stats"].(map[string]any)
		if got, _ := stats["rule_2_tasks_flipped_stale"].(float64); int(got) != 1 {
			t.Fatalf("rule_2_tasks_flipped_stale=%v, want 1; stats=%+v", got, stats)
		}

		// `task list --status stale` should include TASK-001.
		listEnv := runCairnExit(t, repo, cairnHome, 0, "task", "list", "--status", "stale")
		tasks, _ := listEnv["data"].(map[string]any)["tasks"].([]any)
		if !containsTaskID(tasks, "TASK-001") {
			t.Fatalf("stale list does not include TASK-001: %+v", tasks)
		}
	})

	t.Run("memory_append_search", func(t *testing.T) {
		repo := mustEmptyRepo(t)
		cairnHome := t.TempDir()

		runCairnExit(t, repo, cairnHome, 0, "init")

		// Append a decision.
		appEnv := runCairnExit(t, repo, cairnHome, 0, "memory", "append",
			"--kind", "decision",
			"--body", "chose to hash evidence before binding",
		)
		expectEnvelopeKind(t, appEnv, "memory.append")

		// Search should find exactly one match for "evidence".
		searchEnv := runCairnExit(t, repo, cairnHome, 0, "memory", "search", "evidence")
		expectEnvelopeKind(t, searchEnv, "memory.search")
		sdata := searchEnv["data"].(map[string]any)
		if got, _ := sdata["total_matching"].(float64); int(got) != 1 {
			t.Fatalf("total_matching=%v, want 1; data=%+v", got, sdata)
		}

		// The event log should include `memory_appended`.
		evEnv := runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
		evs, _ := evEnv["data"].(map[string]any)["events"].([]any)
		if !hasEventKind(evs, "memory_appended") {
			t.Fatalf("memory_appended not in events; got kinds=%v", distinctKinds(evs))
		}
	})

	t.Run("rule1_expired_lease", func(t *testing.T) {
		repo := mustReconcileRepo(t)
		cairnHome := t.TempDir()

		runCairnExit(t, repo, cairnHome, 0, "init")
		runCairnExit(t, repo, cairnHome, 0, "task", "plan")

		// --ttl 1ms is the shortest meaningful TTL time.ParseDuration accepts.
		// Subprocess shares the wall clock with reconcile, so the sleep below
		// is enough for expires_at to be in the past.
		runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
			"--agent", "a", "--ttl", "1ms")
		time.Sleep(50 * time.Millisecond)

		recEnv := runCairnExit(t, repo, cairnHome, 0, "reconcile")
		stats := recEnv["data"].(map[string]any)["stats"].(map[string]any)
		if got, _ := stats["rule_1_claims_released"].(float64); int(got) != 1 {
			t.Fatalf("rule_1_claims_released=%v, want 1; stats=%+v", got, stats)
		}
		if got, _ := stats["rule_1_tasks_reverted"].(float64); int(got) != 1 {
			t.Fatalf("rule_1_tasks_reverted=%v, want 1; stats=%+v", got, stats)
		}

		// Task should be back to `open`.
		listEnv := runCairnExit(t, repo, cairnHome, 0, "task", "list", "--status", "open")
		tasks, _ := listEnv["data"].(map[string]any)["tasks"].([]any)
		if !containsTaskID(tasks, "TASK-001") {
			t.Fatalf("open list does not include TASK-001: %+v", tasks)
		}
	})
}

// mustReconcileRepo creates a temp git repo seeded with REQ-001 + TASK-001 in
// the shape the three subtests need. Mirrors mustDogfoodRepo but kept separate
// so the base gate definition is owned by this test file (swapping out the
// fixture won't silently break rule-2 drift detection).
func mustReconcileRepo(t *testing.T) string {
	t.Helper()
	d := mustEmptyRepo(t)
	if err := os.MkdirAll(filepath.Join(d, "specs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d, "specs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReq001(t, d, "dogfood", "ok")
	if err := os.WriteFile(filepath.Join(d, "specs", "tasks", "TASK-001.yaml"),
		[]byte(`id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

// writeReq001 writes specs/requirements/REQ-001.yaml with one gate AC-001
// whose executable producer echoes `echoArg`. Changing echoArg changes
// gate_def_hash.
func writeReq001(t *testing.T, repo, why, echoArg string) {
	t.Helper()
	body := `id: REQ-001
title: demo
why: ` + why + `
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ` + echoArg + `]
        pass_on_exit_code: 0
`
	if err := os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// taskStatus reads the status of a single task via `cairn task list`.
func taskStatus(t *testing.T, repo, cairnHome, id string) string {
	t.Helper()
	env := runCairnExit(t, repo, cairnHome, 0, "task", "list")
	tasks, _ := env["data"].(map[string]any)["tasks"].([]any)
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if got, _ := row["id"].(string); got == id {
			s, _ := row["status"].(string)
			return s
		}
	}
	return ""
}

func containsTaskID(tasks []any, id string) bool {
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if got, _ := row["id"].(string); got == id {
			return true
		}
	}
	return false
}

func hasEventKind(evs []any, want string) bool {
	for _, raw := range evs {
		e, _ := raw.(map[string]any)
		if got, _ := e["Kind"].(string); got == want {
			return true
		}
	}
	return false
}

func distinctKinds(evs []any) []string {
	seen := map[string]bool{}
	for _, raw := range evs {
		e, _ := raw.(map[string]any)
		if k, _ := e["Kind"].(string); k != "" {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
