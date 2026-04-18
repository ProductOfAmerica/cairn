package memory_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestAppend_HappyPath(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(1000)

	var result memory.AppendResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Append(memory.AppendInput{
			OpID:       "01HNBXBT9J6MGK3Z5R7WVXTM001",
			Kind:       "decision",
			Body:       "chose hash evidence before binding",
			EntityKind: "task",
			EntityID:   "TASK-017",
			Tags:       []string{"evidence", "binding"},
		})
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MemoryID == "" {
		t.Fatal("empty memory_id")
	}
	if result.At != 1000 {
		t.Errorf("at = %d, want 1000", result.At)
	}
	if result.Kind != "decision" || result.EntityKind != "task" || result.EntityID != "TASK-017" {
		t.Errorf("bad result: %+v", result)
	}

	// Verify row landed + fts row populated.
	var body, tagsText string
	if err := h.SQL().QueryRow(
		`SELECT body, tags_text FROM memory_entries WHERE id=?`, result.MemoryID,
	).Scan(&body, &tagsText); err != nil {
		t.Fatal(err)
	}
	if body != "chose hash evidence before binding" {
		t.Errorf("body = %q", body)
	}
	if tagsText != "evidence binding" {
		t.Errorf("tags_text = %q", tagsText)
	}

	// FTS5 MATCH works.
	var hit int
	if err := h.SQL().QueryRow(
		`SELECT COUNT(*) FROM memory_fts WHERE memory_fts MATCH 'evidence'`,
	).Scan(&hit); err != nil {
		t.Fatal(err)
	}
	if hit != 1 {
		t.Errorf("fts hit = %d, want 1", hit)
	}

	// memory_appended event emitted.
	var kind string
	if err := h.SQL().QueryRow(
		`SELECT kind FROM events WHERE entity_id=?`, result.MemoryID,
	).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "memory_appended" {
		t.Errorf("event kind = %q, want memory_appended", kind)
	}
}

func TestAppend_EntityOmitted(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(1000)

	var result memory.AppendResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Append(memory.AppendInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM002",
			Kind: "rationale",
			Body: "no entity attached",
		})
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EntityKind != "" || result.EntityID != "" {
		t.Errorf("entity fields leaked: %+v", result)
	}
}

func TestAppend_OpLogReplay(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(1000)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM003"

	var first, second memory.AppendResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		first, _ = store.Append(memory.AppendInput{OpID: opID, Kind: "decision", Body: "A"})
		return nil
	})

	// Advance clock so we'd detect re-execution.
	clk.Set(2000)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		second, _ = store.Append(memory.AppendInput{OpID: opID, Kind: "decision", Body: "A"})
		return nil
	})

	if first.MemoryID != second.MemoryID || first.At != second.At {
		t.Errorf("replay mismatch: first=%+v second=%+v", first, second)
	}

	// Only one memory_entries row exists.
	var count int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM memory_entries`).Scan(&count)
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestAppend_ValidationErrors(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(1000)

	cases := []struct {
		name string
		in   memory.AppendInput
	}{
		{"bad kind", memory.AppendInput{Kind: "unknown", Body: "x"}},
		{"empty body", memory.AppendInput{Kind: "decision", Body: ""}},
		{"entity kind without id", memory.AppendInput{Kind: "decision", Body: "x", EntityKind: "task"}},
		{"entity id without kind", memory.AppendInput{Kind: "decision", Body: "x", EntityID: "T-1"}},
		{"invalid entity kind", memory.AppendInput{Kind: "decision", Body: "x", EntityKind: "Task", EntityID: "T-1"}},
		{"invalid tag", memory.AppendInput{Kind: "decision", Body: "x", Tags: []string{"foo-bar"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := h.WithTx(context.Background(), func(tx *db.Tx) error {
				store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
				_, e := store.Append(c.in)
				return e
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestList_NewestFirst_DefaultLimit10(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)

	// Seed 15 entries across two kinds.
	for i := 0; i < 15; i++ {
		clk.Set(int64(i + 1))
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			kind := "decision"
			if i%2 == 0 {
				kind = "outcome"
			}
			_, err := store.Append(memory.AppendInput{
				Kind: kind,
				Body: fmt.Sprintf("entry %d", i),
			})
			return err
		})
	}

	clk.Set(100)
	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.List(memory.ListInput{})
		res = r
		return err
	})

	if res.Returned != 10 {
		t.Errorf("returned = %d, want 10", res.Returned)
	}
	if res.TotalMatching != 15 {
		t.Errorf("total_matching = %d, want 15", res.TotalMatching)
	}
	// Newest first: entries[0].at should be 15 (last seeded).
	if res.Entries[0].At != 15 {
		t.Errorf("entries[0].at = %d, want 15", res.Entries[0].At)
	}
	// Sort direction must be strictly descending across the full returned slice.
	for i := 1; i < len(res.Entries); i++ {
		if res.Entries[i-1].At <= res.Entries[i].At {
			t.Errorf("not descending at idx %d: %d → %d",
				i, res.Entries[i-1].At, res.Entries[i].At)
		}
	}
	// With seeded values 1..15 and limit 10, entries[9].At should be 6.
	if len(res.Entries) == 10 && res.Entries[9].At != 6 {
		t.Errorf("entries[9].at = %d, want 6", res.Entries[9].At)
	}
}

func TestList_LimitZeroUnlimited(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	for i := 0; i < 25; i++ {
		clk.Set(int64(i + 1))
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(memory.AppendInput{Kind: "decision", Body: "x"})
			return err
		})
	}
	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, _ := store.List(memory.ListInput{Limit: math.MaxInt32})
		res = r
		return nil
	})
	if res.Returned != 25 || res.TotalMatching != 25 {
		t.Errorf("unlimited: returned=%d total=%d, want 25/25", res.Returned, res.TotalMatching)
	}
}

func TestList_FilterByKind(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	for i, kind := range []string{"decision", "decision", "outcome", "failure"} {
		clk.Set(int64(i + 1))
		k := kind
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(memory.AppendInput{Kind: k, Body: "x"})
			return err
		})
	}
	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, _ := store.List(memory.ListInput{Kind: "decision"})
		res = r
		return nil
	})
	if res.TotalMatching != 2 {
		t.Errorf("kind filter: total = %d, want 2", res.TotalMatching)
	}
}

func TestList_FilterBySince(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	for i := 0; i < 5; i++ {
		clk.Set(int64((i + 1) * 100))
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(memory.AppendInput{Kind: "decision", Body: "x"})
			return err
		})
	}
	// Since = 300 → entries at 300, 400, 500 only (>= 300).
	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		since := int64(300)
		r, _ := store.List(memory.ListInput{Since: &since})
		res = r
		return nil
	})
	if res.TotalMatching != 3 {
		t.Errorf("since filter: total = %d, want 3", res.TotalMatching)
	}
}

func TestList_FilterByEntity(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	seeds := []memory.AppendInput{
		{Kind: "decision", Body: "a", EntityKind: "task", EntityID: "T-1"},
		{Kind: "decision", Body: "b", EntityKind: "task", EntityID: "T-2"},
		{Kind: "decision", Body: "c"},
	}
	for i, in := range seeds {
		clk.Set(int64(i + 1))
		cp := in
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(cp)
			return err
		})
	}
	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, _ := store.List(memory.ListInput{EntityKind: "task", EntityID: "T-1"})
		res = r
		return nil
	})
	if res.TotalMatching != 1 || res.Entries[0].Body != "a" {
		t.Errorf("entity filter: %+v", res)
	}
}

func TestList_FilterCombined(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)

	// Seed 6 entries: vary kind, entity, and timestamp so a compound
	// filter (kind + entity + since) leaves exactly one match.
	type seed struct {
		at   int64
		in   memory.AppendInput
	}
	seeds := []seed{
		{100, memory.AppendInput{Kind: "decision", Body: "d1", EntityKind: "task", EntityID: "T-1"}},
		{200, memory.AppendInput{Kind: "decision", Body: "d2", EntityKind: "task", EntityID: "T-1"}},
		{300, memory.AppendInput{Kind: "decision", Body: "d3", EntityKind: "task", EntityID: "T-1"}},
		{400, memory.AppendInput{Kind: "outcome", Body: "o1", EntityKind: "task", EntityID: "T-1"}},
		{500, memory.AppendInput{Kind: "decision", Body: "d5", EntityKind: "task", EntityID: "T-2"}},
		{600, memory.AppendInput{Kind: "decision", Body: "d6"}},
	}
	for _, s := range seeds {
		clk.Set(s.at)
		cp := s.in
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(cp)
			return err
		})
	}

	var res memory.ListResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		since := int64(300)
		r, _ := store.List(memory.ListInput{
			Kind:       "decision",
			EntityKind: "task",
			EntityID:   "T-1",
			Since:      &since,
		})
		res = r
		return nil
	})

	if res.TotalMatching != 1 {
		t.Fatalf("combined filter: total = %d, want 1: %+v", res.TotalMatching, res.Entries)
	}
	if res.Entries[0].Body != "d3" {
		t.Errorf("combined filter matched wrong row: %+v", res.Entries[0])
	}
}

func TestSearch_MatchesBody(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	for i, body := range []string{
		"evidence binding decision",
		"reconcile sweep orphan",
		"stale verdict",
	} {
		clk.Set(int64(i + 1))
		b := body
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(memory.AppendInput{Kind: "decision", Body: b})
			return err
		})
	}

	var res memory.SearchResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Search(memory.SearchInput{Query: "evidence"})
		res = r
		return err
	})

	if res.TotalMatching != 1 {
		t.Errorf("total = %d, want 1", res.TotalMatching)
	}
	if len(res.Results) != 1 || res.Results[0].Body != "evidence binding decision" {
		t.Errorf("bad body in result: %+v", res.Results)
	}
	if res.Results[0].Relevance <= 0 {
		t.Errorf("relevance should be positive (higher=better); got %f", res.Results[0].Relevance)
	}
}

func TestSearch_TagMatch(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	entries := []memory.AppendInput{
		{Kind: "decision", Body: "x", Tags: []string{"evidence"}},
		{Kind: "decision", Body: "y", Tags: []string{"reconcile"}},
	}
	for i, e := range entries {
		clk.Set(int64(i + 1))
		cp := e
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(cp)
			return err
		})
	}

	var res memory.SearchResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Search(memory.SearchInput{Query: "tags:evidence"})
		res = r
		return err
	})
	if res.TotalMatching != 1 || res.Results[0].Body != "x" {
		t.Errorf("tags:evidence result: %+v", res)
	}
}

func TestSearch_RelevanceHigherIsBetter(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	entries := []string{
		"orphan",
		"orphan orphan orphan",
		"nothing here",
	}
	for i, b := range entries {
		clk.Set(int64(i + 1))
		body := b
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
			_, err := store.Append(memory.AppendInput{Kind: "decision", Body: body})
			return err
		})
	}

	var res memory.SearchResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Search(memory.SearchInput{Query: "orphan"})
		res = r
		return err
	})
	if len(res.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(res.Results))
	}
	if res.Results[0].Body != "orphan orphan orphan" {
		t.Errorf("best match should come first, got %+v", res.Results)
	}
	if res.Results[0].Relevance < res.Results[1].Relevance {
		t.Errorf("first relevance %f < second %f (higher=better)",
			res.Results[0].Relevance, res.Results[1].Relevance)
	}
}

func TestSearch_MalformedQueryTranslates(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)
	var err error
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err = store.Search(memory.SearchInput{Query: "AND AND"})
		return nil
	})
	if err == nil {
		t.Fatal("expected error for malformed FTS query")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) || ce.Kind != "invalid_fts_query" {
		t.Fatalf("expected invalid_fts_query, got %v", err)
	}
}
