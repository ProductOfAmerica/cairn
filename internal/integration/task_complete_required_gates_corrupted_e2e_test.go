package integration_test

import (
	"testing"
)

// TestE2E_RequiredGatesCorrupted drives the full cairn CLI end-to-end and
// asserts that `task complete` refuses with error.code="required_gates_corrupted"
// and exit code 4 (CodeSubstrate) when a task's required_gates_json column has
// been silently corrupted in the state DB.
//
// This is a substrate-corruption path: cairn never writes invalid JSON itself,
// but if state.db is hand-edited or a future migration regresses, the load step
// in (*task.Store).Complete must surface a stable, structured error rather than
// crashing or silently degrading. The unit test in internal/task pins the wire
// shape; this test pins the CLI surface (envelope + exit code).
//
// Flow:
//  1. init → task plan → task claim TASK-001 (state is well-formed).
//  2. Open state.db directly and `UPDATE tasks SET required_gates_json='{not valid json'`.
//  3. `task complete <claim_id>` — must exit 4 with error.code="required_gates_corrupted".
func TestE2E_RequiredGatesCorrupted(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "required-gates-corrupted")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Claim the task — needed so `task complete` has a live claim to look up.
	// Required-gates-corrupted fires during complete, before verdict checks,
	// so we don't need to put evidence or report a verdict.
	claimEnv := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	claimID, _ := claimEnv["data"].(map[string]any)["claim_id"].(string)
	if claimID == "" {
		t.Fatalf("claim: missing claim_id: %+v", claimEnv)
	}

	// Corrupt the required_gates_json column directly. The CLI never exposes
	// a way to write invalid JSON here, so direct SQL is the minimum-privilege
	// way to reproduce the corruption case without coupling the test to any
	// future "unsafe import" command.
	mutateOneCell(t, repo, cairnHome, "tasks", "required_gates_json",
		"{not valid json", "id", "TASK-001")

	// `task complete` must fail with exit 4 + error.code=required_gates_corrupted.
	env, code := runCairn(t, repo, cairnHome, "task", "complete", claimID)
	if code != 4 {
		t.Fatalf("task complete: expected exit 4 (CodeSubstrate), got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "required_gates_corrupted")

	// Details.task_id must round-trip through the envelope so callers can
	// pinpoint which row was corrupted.
	e, _ := env["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	if got, _ := details["task_id"].(string); got != "TASK-001" {
		t.Fatalf("error.details.task_id=%q want TASK-001 (env=%+v)", got, env)
	}
}

