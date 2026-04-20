package integration_test

import (
	"testing"
)

// TestE2E_DependsOnCorrupted drives the full cairn CLI end-to-end and asserts
// that `task claim` refuses with error.code="depends_on_corrupted" and exit
// code 4 (CodeSubstrate) when a task's depends_on_json column has been
// silently corrupted in the state DB.
//
// This is a substrate-corruption path: cairn never writes invalid JSON itself,
// but if state.db is hand-edited or a future migration regresses, the dep
// check inside (*task.Store).Claim must surface a stable, structured error
// rather than silently treating the deps list as empty (which would let the
// claim succeed with unfinished dependencies). The unit test in internal/task
// pins the wire shape; this test pins the CLI surface (envelope + exit code).
//
// Flow:
//  1. init → task plan (state is well-formed).
//  2. Open state.db directly and `UPDATE tasks SET depends_on_json='{garbage'`.
//  3. `task claim TASK-001` — must exit 4 with error.code="depends_on_corrupted".
func TestE2E_DependsOnCorrupted(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "depends-on-corrupted")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Corrupt the depends_on_json column directly. The CLI never exposes a
	// way to write invalid JSON here, so direct SQL is the minimum-privilege
	// way to reproduce the corruption case without coupling the test to any
	// future "unsafe import" command.
	mutateOneCell(t, repo, cairnHome, "tasks", "depends_on_json",
		"{garbage", "id", "TASK-001")

	// `task claim` must fail with exit 4 + error.code=depends_on_corrupted.
	env, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	if code != 4 {
		t.Fatalf("task claim: expected exit 4 (CodeSubstrate), got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "depends_on_corrupted")

	// Details.task_id must round-trip through the envelope so callers can
	// pinpoint which row was corrupted.
	e, _ := env["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	if got, _ := details["task_id"].(string); got != "TASK-001" {
		t.Fatalf("error.details.task_id=%q want TASK-001 (env=%+v)", got, env)
	}
}

