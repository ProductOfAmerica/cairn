package integration_test

import (
	"testing"
)

// TestReplay_OpIDReturnsCachedResult invokes `cairn task claim` twice with the
// same --op-id and verifies the second invocation returns the same claim_id
// (cached) and does NOT produce duplicate events.
func TestReplay_OpIDReturnsCachedResult(t *testing.T) {
	repo := mustDogfoodRepo(t)
	cairnHome := t.TempDir()
	_, _ = runCairn(t, repo, cairnHome, "init")
	_, _ = runCairn(t, repo, cairnHome, "task", "plan")

	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0P"
	env1, code1 := runCairn(t, repo, cairnHome,
		"--op-id", opID,
		"task", "claim", "TASK-001", "--agent", "a", "--ttl", "30m")
	if code1 != 0 {
		t.Fatalf("first claim: %+v", env1)
	}
	first := env1["data"].(map[string]any)["claim_id"].(string)

	env2, code2 := runCairn(t, repo, cairnHome,
		"--op-id", opID,
		"task", "claim", "TASK-001", "--agent", "a", "--ttl", "30m")
	if code2 != 0 {
		t.Fatalf("replay claim: %+v", env2)
	}
	second := env2["data"].(map[string]any)["claim_id"].(string)
	if first != second {
		t.Fatalf("replay should return cached claim_id: first=%s second=%s", first, second)
	}

	// Count claim_acquired events — must be exactly 1.
	env, _ := runCairn(t, repo, cairnHome, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	n := 0
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "claim_acquired" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("replay should NOT emit duplicate claim_acquired; got %d", n)
	}
}
