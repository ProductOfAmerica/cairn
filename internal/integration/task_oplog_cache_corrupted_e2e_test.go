package integration_test

import (
	"testing"
)

// TestE2E_OpLogCacheCorrupted drives the full cairn CLI end-to-end and asserts
// that an idempotent-replay path refuses with error.code="op_log_cache_corrupted"
// and exit code 4 (CodeSubstrate) when the cached op_log.result_json column has
// been silently corrupted in the state DB.
//
// This is a substrate-corruption path: cairn never writes invalid JSON into
// op_log.result_json itself, but if state.db is hand-edited or a future
// migration regresses, the replay branch in (*task.Store).Claim/Complete/
// Heartbeat must surface a stable, structured error rather than returning a
// zero-value Result as if the original mutation succeeded — which would
// silently break Invariant 6 (idempotent replay).
//
// The unit test in internal/task pins the wire shape; this test pins the CLI
// surface (envelope + exit code + error.details round-trip).
//
// Flow:
//  1. init → task plan → task claim TASK-001 with --op-id <ULID>.
//     The first claim records an op_log row keyed by that ULID.
//  2. UPDATE op_log SET result_json='{not json' WHERE op_id=<ULID>.
//  3. Re-run task claim with the SAME --op-id. The replay branch hits the
//     corrupted cache and must exit 4 with error.code=op_log_cache_corrupted.
//
// Reuses the op-id-replay fixture: it's the simplest spec the CLI accepts
// (one task, one gate, no deps) and is already the canonical scaffold for
// op-id replay scenarios in this suite. Sharing it avoids fixture sprawl
// for what is fundamentally the same setup.
func TestE2E_OpLogCacheCorrupted(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "op-id-replay")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// First claim — records the op_log row we will then corrupt.
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0Z"
	claimEnv := runCairnExit(t, repo, cairnHome, 0,
		"--op-id", opID,
		"task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	if _, ok := claimEnv["data"].(map[string]any)["claim_id"].(string); !ok {
		t.Fatalf("first claim: missing claim_id: %+v", claimEnv)
	}

	// Corrupt the cached result_json. The CLI never exposes a way to write
	// invalid JSON into op_log, so direct SQL is the minimum-privilege way to
	// reproduce the corruption case without coupling the test to any future
	// "unsafe import" command.
	mutateOneCell(t, repo, cairnHome, "op_log", "result_json",
		"{not json", "op_id", opID)

	// Second claim with the SAME op_id — must hit the replay branch, see the
	// corrupted result_json, and refuse with exit 4.
	env, code := runCairn(t, repo, cairnHome,
		"--op-id", opID,
		"task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	if code != 4 {
		t.Fatalf("replay claim: expected exit 4 (CodeSubstrate), got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "op_log_cache_corrupted")

	// Details.op_id and details.kind must round-trip through the envelope so
	// callers can pinpoint which row was corrupted under which mutation kind.
	e, _ := env["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	if got, _ := details["op_id"].(string); got != opID {
		t.Fatalf("error.details.op_id=%q want %q (env=%+v)", got, opID, env)
	}
	if got, _ := details["kind"].(string); got != "task.claim" {
		t.Fatalf("error.details.kind=%q want task.claim (env=%+v)", got, env)
	}
}
