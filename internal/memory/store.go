package memory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// Store owns the memory_entries + memory_fts tables.
type Store struct {
	tx     *db.Tx
	events events.Appender
	ids    *ids.Generator
	clock  clock.Clock
}

// NewStore binds a Store to the given transaction.
func NewStore(tx *db.Tx, a events.Appender, g *ids.Generator, c clock.Clock) *Store {
	return &Store{tx: tx, events: a, ids: g, clock: c}
}

// AppendInput is the caller-supplied data for a memory.append call.
// Entity fields are optional but XOR-enforced.
type AppendInput struct {
	OpID       string
	Kind       string
	Body       string
	EntityKind string   // optional; must be paired with EntityID
	EntityID   string   // optional; must be paired with EntityKind
	Tags       []string // optional; validated against tag format
}

// AppendResult is the successful response body.
type AppendResult struct {
	MemoryID   string   `json:"memory_id"`
	At         int64    `json:"at"`
	Kind       string   `json:"kind"`
	EntityKind string   `json:"entity_kind,omitempty"`
	EntityID   string   `json:"entity_id,omitempty"`
	Tags       []string `json:"tags"`
	OpID       string   `json:"op_id,omitempty"`
}

// Append validates the input, inserts the memory_entries row (AFTER INSERT
// trigger populates memory_fts), emits memory_appended, and records op_log
// for idempotent replay. Returns AppendResult.
func (s *Store) Append(in AppendInput) (AppendResult, error) {
	// 1. Validation.
	if err := ValidateKind(in.Kind); err != nil {
		return AppendResult{}, err
	}
	if strings.TrimSpace(in.Body) == "" {
		return AppendResult{}, cairnerr.New(cairnerr.CodeBadInput, "invalid_body",
			"body must not be empty")
	}
	if err := ValidateEntityPair(in.EntityKind, in.EntityID); err != nil {
		return AppendResult{}, err
	}
	if err := ValidateTags(in.Tags); err != nil {
		return AppendResult{}, err
	}

	// 2. op_log replay. If OpID already recorded, return cached result.
	if in.OpID != "" {
		if cached, ok, err := s.lookupOpLog(in.OpID); err != nil {
			return AppendResult{}, err
		} else if ok {
			return cached, nil
		}
	}

	// 3. Insert memory_entries row.
	memoryID := s.ids.ULID()
	at := s.clock.NowMilli()
	tagsJSON := TagsJSON(in.Tags)
	tagsText := TagsText(in.Tags)

	var ek, eid sql.NullString
	if in.EntityKind != "" {
		ek = sql.NullString{String: in.EntityKind, Valid: true}
		eid = sql.NullString{String: in.EntityID, Valid: true}
	}

	_, err := s.tx.Exec(
		`INSERT INTO memory_entries (id, at, kind, entity_kind, entity_id, body, tags_json, tags_text)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		memoryID, at, in.Kind, ek, eid, in.Body, tagsJSON, tagsText,
	)
	if err != nil {
		return AppendResult{}, fmt.Errorf("insert memory_entries: %w", err)
	}

	// 4. Emit event.
	if err := s.events.Append(s.tx, events.Record{
		Kind:       "memory_appended",
		EntityKind: "memory",
		EntityID:   memoryID,
		OpID:       in.OpID,
		Payload: map[string]any{
			"kind":        in.Kind,
			"entity_kind": in.EntityKind,
			"entity_id":   in.EntityID,
		},
	}); err != nil {
		return AppendResult{}, err
	}

	result := AppendResult{
		MemoryID:   memoryID,
		At:         at,
		Kind:       in.Kind,
		EntityKind: in.EntityKind,
		EntityID:   in.EntityID,
		Tags:       in.Tags,
		OpID:       in.OpID,
	}
	if result.Tags == nil {
		result.Tags = []string{}
	}

	// 5. Record op_log for future replay.
	if in.OpID != "" {
		if err := s.recordOpLog(in.OpID, "memory_append", at, result); err != nil {
			return AppendResult{}, err
		}
	}

	return result, nil
}

// lookupOpLog returns the cached result if this op_id has been seen before.
func (s *Store) lookupOpLog(opID string) (AppendResult, bool, error) {
	var resultJSON string
	err := s.tx.QueryRow(
		`SELECT result_json FROM op_log WHERE op_id = ? AND kind = 'memory_append'`,
		opID,
	).Scan(&resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return AppendResult{}, false, nil
	}
	if err != nil {
		return AppendResult{}, false, fmt.Errorf("lookup op_log: %w", err)
	}
	var cached AppendResult
	if err := json.Unmarshal([]byte(resultJSON), &cached); err != nil {
		return AppendResult{}, false, fmt.Errorf("unmarshal op_log result: %w", err)
	}
	return cached, true, nil
}

// recordOpLog writes the result for future replay.
func (s *Store) recordOpLog(opID, kind string, at int64, result AppendResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal op_log result: %w", err)
	}
	_, err = s.tx.Exec(
		`INSERT INTO op_log (op_id, kind, first_seen_at, result_json) VALUES (?, ?, ?, ?)`,
		opID, kind, at, string(resultJSON),
	)
	if err != nil {
		return fmt.Errorf("insert op_log: %w", err)
	}
	return nil
}

// Entry is the on-disk shape returned by List and Search.
type Entry struct {
	MemoryID   string   `json:"memory_id"`
	At         int64    `json:"at"`
	Kind       string   `json:"kind"`
	EntityKind string   `json:"entity_kind,omitempty"`
	EntityID   string   `json:"entity_id,omitempty"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags"`
}

// ListInput holds optional filters for Store.List.
type ListInput struct {
	Kind       string
	EntityKind string
	EntityID   string
	Since      *int64 // nil = no filter
	Limit      int    // default 10; 0 = unlimited
}

// ListResult is the envelope response for `cairn memory list`.
type ListResult struct {
	Entries       []Entry `json:"entries"`
	TotalMatching int64   `json:"total_matching"`
	Returned      int     `json:"returned"`
}

// List returns memory entries matching the optional filters, newest-first.
// Filters combine AND. Limit default is 10; 0 means unlimited.
func (s *Store) List(in ListInput) (ListResult, error) {
	// Validate entity pair (same rule as Append): both or neither.
	if err := ValidateEntityPair(in.EntityKind, in.EntityID); err != nil {
		return ListResult{}, err
	}
	if in.Kind != "" {
		if err := ValidateKind(in.Kind); err != nil {
			return ListResult{}, err
		}
	}

	where, args := buildListPredicate(in)

	// Count first (total matching, before LIMIT).
	var total int64
	countSQL := "SELECT COUNT(*) FROM memory_entries" + where
	if err := s.tx.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count memory_entries: %w", err)
	}

	limit := in.Limit
	if limit < 0 {
		return ListResult{}, cairnerr.New(cairnerr.CodeBadInput, "invalid_limit",
			"limit must be >= 0").WithDetails(map[string]any{"limit": limit})
	}
	querySQL := `SELECT id, at, kind, entity_kind, entity_id, body, tags_json
	             FROM memory_entries` + where + `
	             ORDER BY at DESC, id DESC`
	var queryArgs []any
	queryArgs = append(queryArgs, args...)
	if in.Limit > 0 || (in.Limit == 0 && !isExplicitUnlimited(in)) {
		effective := in.Limit
		if effective == 0 {
			effective = 10 // default when caller left Limit at zero-value
		}
		querySQL += " LIMIT ?"
		queryArgs = append(queryArgs, effective)
	}
	// Explicit unlimited: no LIMIT clause.

	rows, err := s.tx.Query(querySQL, queryArgs...)
	if err != nil {
		return ListResult{}, fmt.Errorf("query memory_entries: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var ek, eid sql.NullString
		var tagsJSON string
		if err := rows.Scan(&e.MemoryID, &e.At, &e.Kind, &ek, &eid, &e.Body, &tagsJSON); err != nil {
			return ListResult{}, err
		}
		if ek.Valid {
			e.EntityKind = ek.String
		}
		if eid.Valid {
			e.EntityID = eid.String
		}
		if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
			return ListResult{}, fmt.Errorf("unmarshal tags_json: %w", err)
		}
		if e.Tags == nil {
			e.Tags = []string{}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	if entries == nil {
		entries = []Entry{}
	}
	return ListResult{
		Entries:       entries,
		TotalMatching: total,
		Returned:      len(entries),
	}, nil
}

// buildListPredicate emits a WHERE clause + args vector for the shared filters.
func buildListPredicate(in ListInput) (string, []any) {
	var parts []string
	var args []any
	if in.Kind != "" {
		parts = append(parts, "kind = ?")
		args = append(args, in.Kind)
	}
	if in.EntityKind != "" {
		parts = append(parts, "entity_kind = ? AND entity_id = ?")
		args = append(args, in.EntityKind, in.EntityID)
	}
	if in.Since != nil {
		parts = append(parts, "at >= ?")
		args = append(args, *in.Since)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

// isExplicitUnlimited distinguishes caller-supplied 0 (from `--limit 0`) from
// the Go zero-value. We use a sentinel via a boolean field... but to keep
// ListInput shape simple, we treat Limit=0 as "default 10" ALWAYS, and require
// the CLI layer to translate `--limit 0` into a large number (e.g., math.MaxInt32)
// before calling List. That keeps the library API uncluttered.
//
// For callers invoking List directly from Go code: pass Limit=math.MaxInt32
// for unlimited; pass a positive int otherwise; Limit=0 means "library default".
func isExplicitUnlimited(_ ListInput) bool {
	// Always false — see doc comment above. The CLI translates --limit 0 to MaxInt.
	return false
}

// SearchInput holds the FTS5 MATCH query and optional filters.
type SearchInput struct {
	Query      string
	Kind       string
	EntityKind string
	EntityID   string
	Since      *int64
	Limit      int // 0 → library default 10; CLI translates --limit 0 to MaxInt
}

// SearchHit is one row returned by Search.
type SearchHit struct {
	Entry
	Relevance float64 `json:"relevance"`
}

// SearchResult is the envelope response for `cairn memory search`.
type SearchResult struct {
	Results       []SearchHit `json:"results"`
	TotalMatching int64       `json:"total_matching"`
	Returned      int         `json:"returned"`
}

// Search runs an FTS5 MATCH over memory_fts, joined back to memory_entries,
// with optional filters. Results are ordered by FTS5 rank ascending (best
// match first); the exposed Relevance field is -rank (higher = better).
func (s *Store) Search(in SearchInput) (SearchResult, error) {
	if strings.TrimSpace(in.Query) == "" {
		return SearchResult{}, cairnerr.New(cairnerr.CodeBadInput, "invalid_fts_query",
			"query must not be empty")
	}
	if err := ValidateEntityPair(in.EntityKind, in.EntityID); err != nil {
		return SearchResult{}, err
	}
	if in.Kind != "" {
		if err := ValidateKind(in.Kind); err != nil {
			return SearchResult{}, err
		}
	}

	// Build predicate (shared with list) plus the MATCH clause.
	args := []any{in.Query}
	var filters []string
	if in.Kind != "" {
		filters = append(filters, "me.kind = ?")
		args = append(args, in.Kind)
	}
	if in.EntityKind != "" {
		filters = append(filters, "me.entity_kind = ? AND me.entity_id = ?")
		args = append(args, in.EntityKind, in.EntityID)
	}
	if in.Since != nil {
		filters = append(filters, "me.at >= ?")
		args = append(args, *in.Since)
	}
	where := ""
	if len(filters) > 0 {
		where = " AND " + strings.Join(filters, " AND ")
	}

	// Count first.
	var total int64
	countSQL := `SELECT COUNT(*) FROM memory_entries me
	             JOIN memory_fts ON memory_fts.rowid = me.rowid
	             WHERE memory_fts MATCH ?` + where
	if err := s.tx.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return SearchResult{}, TranslateFTSError(err)
	}

	limit := in.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 0 {
		return SearchResult{}, cairnerr.New(cairnerr.CodeBadInput, "invalid_limit",
			"limit must be >= 0")
	}

	querySQL := `SELECT me.id, me.at, me.kind, me.entity_kind, me.entity_id,
	                    me.body, me.tags_json, (-memory_fts.rank) AS relevance
	             FROM memory_entries me
	             JOIN memory_fts ON memory_fts.rowid = me.rowid
	             WHERE memory_fts MATCH ?` + where + `
	             ORDER BY memory_fts.rank ASC
	             LIMIT ?`
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit)

	rows, err := s.tx.Query(querySQL, queryArgs...)
	if err != nil {
		return SearchResult{}, TranslateFTSError(err)
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		var ek, eid sql.NullString
		var tagsJSON string
		if err := rows.Scan(&h.MemoryID, &h.At, &h.Kind, &ek, &eid,
			&h.Body, &tagsJSON, &h.Relevance); err != nil {
			return SearchResult{}, err
		}
		if ek.Valid {
			h.EntityKind = ek.String
		}
		if eid.Valid {
			h.EntityID = eid.String
		}
		if err := json.Unmarshal([]byte(tagsJSON), &h.Tags); err != nil {
			return SearchResult{}, fmt.Errorf("unmarshal tags_json: %w", err)
		}
		if h.Tags == nil {
			h.Tags = []string{}
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, TranslateFTSError(err)
	}
	if hits == nil {
		hits = []SearchHit{}
	}
	return SearchResult{
		Results:       hits,
		TotalMatching: total,
		Returned:      len(hits),
	}, nil
}
