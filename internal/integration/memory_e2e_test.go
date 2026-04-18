package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestMemoryE2E exercises the `cairn memory` subcommands end-to-end via the
// built binary. Each scenario runs as a subtest but all share the same repo +
// CAIRN_HOME so ordering is intentional: seeding happens in "append_seed" and
// later subtests rely on that state. Scenarios that must isolate state (the
// op-id replay) create their own repo/cairnHome inline.
func TestMemoryE2E(t *testing.T) {
	repo := mustEmptyRepo(t)
	cairnHome := t.TempDir()

	// init sets up the state-db under CAIRN_HOME.
	runCairnExit(t, repo, cairnHome, 0, "init")

	// Seed counts tracked across subtests.
	var seededOutcomes int

	// -----------------------------------------------------------------
	// Scenario 1: single append round-trips the expected envelope shape.
	// -----------------------------------------------------------------
	t.Run("append_first", func(t *testing.T) {
		env := runCairnExit(t, repo, cairnHome, 0, "memory", "append",
			"--kind", "decision",
			"--body", "first",
			"--tags", "foo,bar",
		)
		expectEnvelopeKind(t, env, "memory.append")
		data, ok := env["data"].(map[string]any)
		if !ok {
			t.Fatalf("append: data missing or wrong type: %+v", env)
		}
		if mid, _ := data["memory_id"].(string); mid == "" {
			t.Fatalf("append: memory_id empty: %+v", data)
		}
		if at, _ := data["at"].(float64); at <= 0 {
			t.Fatalf("append: at must be > 0, got %v", data["at"])
		}
		tagsRaw, _ := data["tags"].([]any)
		if len(tagsRaw) != 2 {
			t.Fatalf("append: want 2 tags, got %+v", tagsRaw)
		}
		if tagsRaw[0] != "foo" || tagsRaw[1] != "bar" {
			t.Fatalf("append: tags order/content wrong: %+v", tagsRaw)
		}
	})

	// -----------------------------------------------------------------
	// Seed 15 more entries across all four kinds so subsequent list/search
	// scenarios have real data to filter over. Total rows after this: 16.
	// -----------------------------------------------------------------
	t.Run("append_seed", func(t *testing.T) {
		kinds := []string{"decision", "rationale", "outcome", "failure"}
		for i := 0; i < 15; i++ {
			k := kinds[i%len(kinds)]
			if k == "outcome" {
				seededOutcomes++
			}
			body := fmt.Sprintf("entry_%d body text", i)
			runCairnExit(t, repo, cairnHome, 0, "memory", "append",
				"--kind", k,
				"--body", body,
			)
		}
	})

	// -----------------------------------------------------------------
	// Scenario 2: default list pages to 10 but reports total_matching=16.
	// -----------------------------------------------------------------
	t.Run("list_default_pagination", func(t *testing.T) {
		env := runCairnExit(t, repo, cairnHome, 0, "memory", "list")
		expectEnvelopeKind(t, env, "memory.list")
		data := env["data"].(map[string]any)
		if got, _ := data["returned"].(float64); got != 10 {
			t.Fatalf("returned=%v want 10", data["returned"])
		}
		if got, _ := data["total_matching"].(float64); got != 16 {
			t.Fatalf("total_matching=%v want 16", data["total_matching"])
		}
	})

	// -----------------------------------------------------------------
	// Scenario 3: --limit 0 means unlimited, so returned == total == 16.
	// -----------------------------------------------------------------
	t.Run("list_unlimited", func(t *testing.T) {
		env := runCairnExit(t, repo, cairnHome, 0, "memory", "list", "--limit", "0")
		data := env["data"].(map[string]any)
		if got, _ := data["returned"].(float64); got != 16 {
			t.Fatalf("returned=%v want 16", data["returned"])
		}
		if got, _ := data["total_matching"].(float64); got != 16 {
			t.Fatalf("total_matching=%v want 16", data["total_matching"])
		}
	})

	// -----------------------------------------------------------------
	// Scenario 4: kind filter narrows total_matching to the seeded count
	// for that kind.
	// -----------------------------------------------------------------
	t.Run("list_kind_filter", func(t *testing.T) {
		env := runCairnExit(t, repo, cairnHome, 0, "memory", "list",
			"--kind", "outcome", "--limit", "0")
		data := env["data"].(map[string]any)
		if got, _ := data["total_matching"].(float64); int(got) != seededOutcomes {
			t.Fatalf("total_matching=%v want %d (seeded outcomes)", got, seededOutcomes)
		}
	})

	// -----------------------------------------------------------------
	// Scenario 5: FTS search respects --limit and returns positive relevance.
	// -----------------------------------------------------------------
	t.Run("search_limits_and_relevance", func(t *testing.T) {
		env := runCairnExit(t, repo, cairnHome, 0, "memory", "search",
			"first", "--limit", "3")
		expectEnvelopeKind(t, env, "memory.search")
		data := env["data"].(map[string]any)
		returned, _ := data["returned"].(float64)
		if returned < 1 || returned > 3 {
			t.Fatalf("returned=%v want 1..3", returned)
		}
		results, _ := data["results"].([]any)
		if len(results) != int(returned) {
			t.Fatalf("results length %d != returned %v", len(results), returned)
		}
		for i, h := range results {
			hit := h.(map[string]any)
			rel, _ := hit["relevance"].(float64)
			if rel <= 0 {
				t.Fatalf("hit[%d] relevance=%v, want > 0", i, rel)
			}
		}
	})

	// -----------------------------------------------------------------
	// Scenario 6: FTS5 syntax error surfaces as invalid_fts_query with a
	// SANITIZED message — the envelope text must NOT leak sqlite/fts5/near
	// markers from the raw driver error.
	// -----------------------------------------------------------------
	t.Run("search_fts_error_sanitized", func(t *testing.T) {
		env, code := runCairn(t, repo, cairnHome, "memory", "search", "AND AND")
		if code != 1 {
			t.Fatalf("want exit 1, got %d; env=%+v", code, env)
		}
		expectErrorKind(t, env, "invalid_fts_query")
		// Re-marshal the full envelope and grep the bytes so details/cause
		// leakage is caught even if Message is clean.
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		text := strings.ToLower(string(raw))
		for _, banned := range []string{"sqlite", "fts5:", `near "`} {
			if strings.Contains(text, strings.ToLower(banned)) {
				t.Fatalf("envelope leaks %q: %s", banned, raw)
			}
		}
	})

	// -----------------------------------------------------------------
	// Scenario 8: entity_kind without entity_id must fail with the specific
	// XOR error code. Runs before op-id replay so we don't contaminate that
	// subtest's isolated state expectations. (Just uses the shared repo.)
	// -----------------------------------------------------------------
	t.Run("entity_xor_mismatch", func(t *testing.T) {
		env, code := runCairn(t, repo, cairnHome, "memory", "append",
			"--kind", "decision",
			"--body", "x",
			"--entity-kind", "task",
		)
		if code != 1 {
			t.Fatalf("want exit 1, got %d; env=%+v", code, env)
		}
		expectErrorKind(t, env, "entity_kind_id_mismatch")
	})

	// -----------------------------------------------------------------
	// Scenario 7: op-id replay returns the cached memory_id AND inserts
	// exactly one row. Runs in an isolated repo+cairnHome so we can snapshot
	// the row count cleanly before and after.
	// -----------------------------------------------------------------
	t.Run("op_id_replay_idempotent", func(t *testing.T) {
		repo2 := mustEmptyRepo(t)
		home2 := t.TempDir()
		runCairnExit(t, repo2, home2, 0, "init")

		// Baseline: empty store.
		env := runCairnExit(t, repo2, home2, 0, "memory", "list", "--limit", "0")
		if got, _ := env["data"].(map[string]any)["total_matching"].(float64); got != 0 {
			t.Fatalf("baseline total=%v, want 0", got)
		}

		const opID = "01HNBXBT9J6MGK3Z5R7WVXTM01"
		env1 := runCairnExit(t, repo2, home2, 0,
			"--op-id", opID,
			"memory", "append",
			"--kind", "decision",
			"--body", "replay-body",
		)
		first, _ := env1["data"].(map[string]any)["memory_id"].(string)
		if first == "" {
			t.Fatalf("first append: no memory_id: %+v", env1)
		}

		env2 := runCairnExit(t, repo2, home2, 0,
			"--op-id", opID,
			"memory", "append",
			"--kind", "decision",
			"--body", "replay-body",
		)
		second, _ := env2["data"].(map[string]any)["memory_id"].(string)
		if second != first {
			t.Fatalf("replay should return cached memory_id: first=%s second=%s", first, second)
		}

		// Verify exactly ONE row — the replay must not double-insert.
		env = runCairnExit(t, repo2, home2, 0, "memory", "list", "--limit", "0")
		data := env["data"].(map[string]any)
		if got, _ := data["total_matching"].(float64); got != 1 {
			t.Fatalf("after replay total_matching=%v, want 1", got)
		}
		if got, _ := data["returned"].(float64); got != 1 {
			t.Fatalf("after replay returned=%v, want 1", got)
		}
	})
}
