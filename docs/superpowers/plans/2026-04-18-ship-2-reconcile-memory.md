# Ship 2 — Reconcile + Memory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Ship 2 of the cairn CLI — a `memory` domain (append + FTS5 search + list) and a `reconcile` command with five rules — on top of the Ship 1 core substrate. Produces drift detection, searchable cross-session decision memory, and on-demand reconciliation verified end-to-end by the Ship 1 event-log-completeness CI test (extended with five new event kinds).

**Architecture:** Two new `internal/` packages (`memory`, `reconcile`) following the Ship 1 store pattern (each Store wraps `*db.Tx`; cross-domain calls share the caller's tx). Reconcile uses a hybrid transaction: filesystem I/O (evidence probe) runs outside any tx; all state mutations run inside one `BEGIN IMMEDIATE`. Memory is append-only, FTS5-indexed, with schema-level triggers (`RAISE(ABORT, ...)`) blocking UPDATE/DELETE. Migration 002 also closes a Ship 1 gap by adding restricted-UPDATE + DELETE triggers to `evidence`. CLI commands are thin cobra `RunE` wrappers over library functions, reusing Ship 1's envelope + exit-code machinery.

**Tech Stack:**
- Go 1.25 (matrix CI: Linux/Windows/macOS × Go 1.25.x)
- `modernc.org/sqlite` (pure-Go SQLite + FTS5, already a dep)
- `github.com/spf13/cobra`
- `github.com/oklog/ulid/v2`
- `github.com/stretchr/testify`

**Source of truth for every design decision:** `docs/superpowers/specs/2026-04-18-ship-2-reconcile-memory-design.md`. This plan implements exactly that spec. Q1–Q10 locks, §5.10 three-surface table, and the re-stat defense in §5.5 are non-negotiable.

---

## File Structure

```
internal/
  db/
    schema/
      002_ship2.sql                # NEW — memory tables + FTS5 + evidence.invalidated_at + evidence triggers

  memory/
    store.go                       # NEW — Store; Append, Search, List
    validate.go                    # NEW — kind enum, entity_kind enum, tag regex, helpers
    fts_error.go                   # NEW — TranslateFTSError: SQLite → cairnerr.Err
    memory_test.go                 # NEW — unit tests
    fts_test.go                    # NEW — FTS5 + error-translation tests

  reconcile/
    reconcile.go                   # NEW — Orchestrator.Run (phase markers), Result type
    probe.go                       # NEW — runEvidenceProbe (OUTSIDE tx)
    rule1_leases.go                # NEW — releaseExpiredLeases
    rule2_staleness.go             # NEW — flipStaleTasks (Go loop over tasks × gates)
    rule3_evidence.go              # NEW — applyEvidenceInvalidations (mutation half + re-stat)
    rule4_orphans.go               # NEW — orphanExpiredRuns
    rule5_authoring.go             # NEW — collectAuthoringErrors (read-only)
    dryrun.go                      # NEW — pure-read dry-run simulator
    reconcile_test.go              # NEW — per-rule unit tests

  evidence/
    store.go                       # MODIFY — Verify checks invalidated_at; new GetWithInvalidation

  verdict/
    store.go                       # MODIFY — Latest/History JOIN evidence to surface evidence_invalidated

  cli/
    memory_append.go               # NEW — cobra RunE glue
    memory_search.go               # NEW
    memory_list.go                 # NEW
    reconcile.go                   # NEW

cmd/cairn/
  memory.go                        # NEW — subcommand group registration
  reconcile.go                     # NEW — top-level reconcile command

internal/integration/
  memory_e2e_test.go               # NEW — append → list → search with filters
  reconcile_e2e_test.go            # NEW — Ship 2 dogfood steps 2 + 5
  evidence_invalidation_e2e_test.go # NEW — put → delete on disk → reconcile → verify-block surface
  task_complete_ignores_invalidation_test.go # NEW — verify §5.10 row 3
  reconcile_concurrent_test.go     # NEW — 5 goroutines + 2 subprocesses
  reconcile_dryrun_parity_test.go  # NEW — snapshot/restore protocol

.github/workflows/
  ci.yml                           # MODIFY — extend event-log completeness assertion
```

**Pre-existing Ship 1 files touched:** `internal/evidence/store.go` (Verify + new helper), `internal/verdict/store.go` (JOIN for invalidation surface). All other Ship 1 packages are read-only consumers.

**Packages NOT touched:** `internal/ids`, `internal/clock`, `internal/repoid`, `internal/cairnerr`, `internal/intent`, `internal/db` (schema dir only), `internal/events`. Ship 1 interfaces are stable.

**Go module name** (from Ship 1): `github.com/ProductOfAmerica/cairn`.

---

## Conventions (carried forward from Ship 1)

- **Store pattern.** `type Store struct { tx *db.Tx; ... }`. Constructor takes all deps. Methods do NOT take `context.Context`; they operate on `s.tx` directly.
- **Events in same tx.** Every mutation calls `s.events.Append(s.tx, events.Record{...})` before commit.
- **Clock injection.** Every mutation uses `s.clock.NowMilli()` — never `time.Now()`. Tests use `clock.NewFake(ms)`.
- **Errors.** `cairnerr.New(code, kind, msg).WithDetails(...)`. Codes: `CodeBadInput`/`CodeValidation` → exit 1, `CodeConflict` → 2, `CodeNotFound` → 3, `CodeSubstrate` → 4.
- **IDs.** ULIDs via `s.ids.ULID()`. `op_id` caller-supplied; if empty, the CLI layer generates and records it.
- **JSON envelope.** CLI wraps every command response through `cli.Envelope`; `--op-id` is a global flag.
- **JSON tags.** Every struct exposed via response JSON has explicit `json:"..."` tags (Ship 1 lesson).
- **TEXT → json.RawMessage/[]byte scans.** Keep the `string` intermediate (Ship 1 lesson: `modernc-sqlite-text-scan.md`).
- **Dep discipline.** No new `go get`. FTS5 ships with `modernc.org/sqlite`. If this changes during implementation, land the dep in the commit that first imports (Ship 1 lesson: `go-deps-inline.md`).
- **Commits.** One task = one to several commits. Conventional-commit-ish prefix (`feat`, `fix`, `test`, `refactor`, `chore`, `plan`, `docs`). No Claude attribution in commit messages (per repo convention).

---

## Phase 0: Migration 002 (memory tables + evidence invalidation + triggers)

### Task 0.1: Write migration 002 SQL file

**Files:**
- Create: `internal/db/schema/002_ship2.sql`

- [ ] **Step 1: Write the failing migration test (test file already exists)**

The Ship 1 test `internal/db/migrate_test.go` (function `TestMigrate_AppliesAll`) opens a fresh DB, applies all migrations in `internal/db/schema/`, then inspects `sqlite_master`. It already iterates over every `.sql` file in that directory. To drive Task 0.1 via TDD, append a new assertion to that test that will fail until migration 002 creates the expected objects.

Open `internal/db/migrate_test.go` and find `TestMigrate_AppliesAll`. Append this new test function at the end of the file:

```go
func TestMigrate_Ship2Objects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	h, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Objects that must exist after migration 002.
	expected := []struct {
		kind string // "table"|"index"|"trigger"
		name string
	}{
		{"table", "memory_entries"},
		{"table", "memory_fts"}, // virtual table registers as "table"
		{"index", "idx_memory_at"},
		{"index", "idx_memory_kind"},
		{"index", "idx_memory_entity"},
		{"trigger", "memory_fts_ai"},
		{"trigger", "memory_no_delete"},
		{"trigger", "memory_no_update"},
		{"trigger", "evidence_only_invalidated_at_updatable"},
		{"trigger", "evidence_no_delete"},
	}
	for _, obj := range expected {
		var got string
		err := h.SQL().QueryRow(
			`SELECT name FROM sqlite_master WHERE type = ? AND name = ?`,
			obj.kind, obj.name,
		).Scan(&got)
		if err != nil {
			t.Errorf("%s %q missing after migration: %v", obj.kind, obj.name, err)
		}
	}

	// evidence.invalidated_at column must exist and be nullable.
	rows, err := h.SQL().Query(`PRAGMA table_info(evidence)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "invalidated_at" {
			found = true
			if typ != "INTEGER" {
				t.Errorf("invalidated_at type = %q, want INTEGER", typ)
			}
			if notnull != 0 {
				t.Errorf("invalidated_at NOT NULL = %d, want 0 (nullable)", notnull)
			}
		}
	}
	if !found {
		t.Error("evidence.invalidated_at column not found")
	}

	// schema_migrations shows version 2.
	var maxVersion int
	if err := h.SQL().QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion < 2 {
		t.Errorf("schema_migrations max version = %d, want >= 2", maxVersion)
	}
}
```

Ensure the `database/sql` import is present in the test file; if missing, add it.

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/db/... -run TestMigrate_Ship2Objects -v
```
Expected: FAIL — migration 002 does not exist yet, so none of the tables/indexes/triggers are created.

- [ ] **Step 3: Write migration 002**

Create `internal/db/schema/002_ship2.sql` with this exact content:

```sql
-- cairn Ship 2 schema.
-- Adds memory (append-only with FTS5) + evidence invalidation plumbing.
-- Amends Ship 1 gap: makes evidence rows append-only at the schema level,
-- matching the discipline already applied to verdicts.

-- Part A: memory

CREATE TABLE memory_entries (
    id          TEXT PRIMARY KEY,
    at          INTEGER NOT NULL,
    kind        TEXT NOT NULL,                         -- decision|rationale|outcome|failure (CLI-validated)
    entity_kind TEXT,                                  -- CLI-validated enum when non-null
    entity_id   TEXT,                                  -- free text; no FK
    body        TEXT NOT NULL,
    tags_json   TEXT NOT NULL DEFAULT '[]',            -- structured output
    tags_text   TEXT NOT NULL DEFAULT '',              -- space-joined; FTS5-indexed
    CHECK ((entity_kind IS NULL) = (entity_id IS NULL))
);

CREATE INDEX idx_memory_at     ON memory_entries(at DESC);
CREATE INDEX idx_memory_kind   ON memory_entries(kind, at DESC);
CREATE INDEX idx_memory_entity ON memory_entries(entity_kind, entity_id);

CREATE VIRTUAL TABLE memory_fts USING fts5(body, tags);

CREATE TRIGGER memory_fts_ai AFTER INSERT ON memory_entries BEGIN
    INSERT INTO memory_fts(rowid, body, tags)
    VALUES (new.rowid, new.body, new.tags_text);
END;

CREATE TRIGGER memory_no_delete BEFORE DELETE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

CREATE TRIGGER memory_no_update BEFORE UPDATE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

-- Part B: evidence invalidation signal + schema-level append-only enforcement.

ALTER TABLE evidence ADD COLUMN invalidated_at INTEGER;

CREATE TRIGGER evidence_only_invalidated_at_updatable
BEFORE UPDATE ON evidence
FOR EACH ROW
WHEN new.id           IS NOT old.id
  OR new.sha256       IS NOT old.sha256
  OR new.uri          IS NOT old.uri
  OR new.bytes        IS NOT old.bytes
  OR new.content_type IS NOT old.content_type
  OR new.created_at   IS NOT old.created_at
BEGIN
    SELECT RAISE(ABORT, 'evidence is append-only except invalidated_at');
END;

CREATE TRIGGER evidence_no_delete BEFORE DELETE ON evidence BEGIN
    SELECT RAISE(ABORT, 'evidence rows cannot be deleted');
END;
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/db/... -run TestMigrate_Ship2Objects -v
```
Expected: PASS. Also run the full migrate test file to confirm no regression:
```bash
go test ./internal/db/... -v
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/schema/002_ship2.sql internal/db/migrate_test.go
git commit -m "feat(db): migration 002 — memory tables + evidence invalidation + triggers"
```

---

### Task 0.2: Test evidence append-only triggers fire correctly

**Files:**
- Modify: `internal/evidence/evidence_test.go`

- [ ] **Step 1: Write two failing tests**

Append these test functions at the end of `internal/evidence/evidence_test.go`:

```go
func TestEvidenceUpdateRestricted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Seed one evidence row via direct SQL (Store not under test here).
	_, err = h.SQL().Exec(
		`INSERT INTO evidence (id, sha256, uri, bytes, content_type, created_at)
		 VALUES ('E-1',
		         '0000000000000000000000000000000000000000000000000000000000000001',
		         '/tmp/x', 1, 'text/plain', 100)`,
	)
	if err != nil {
		t.Fatal(err)
	}

	// UPDATE that mutates sha256 must fail with RAISE.
	_, err = h.SQL().Exec(
		`UPDATE evidence SET sha256 =
		   '0000000000000000000000000000000000000000000000000000000000000002'
		 WHERE id = 'E-1'`,
	)
	if err == nil {
		t.Fatal("expected UPDATE to fail, got nil")
	}
	if !strings.Contains(err.Error(), "evidence is append-only except invalidated_at") {
		t.Fatalf("unexpected error: %v", err)
	}

	// UPDATE of invalidated_at only must succeed.
	_, err = h.SQL().Exec(
		`UPDATE evidence SET invalidated_at = 123 WHERE id = 'E-1'`,
	)
	if err != nil {
		t.Fatalf("UPDATE invalidated_at should succeed: %v", err)
	}
}

func TestEvidenceDeleteBlocked(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	_, err = h.SQL().Exec(
		`INSERT INTO evidence (id, sha256, uri, bytes, content_type, created_at)
		 VALUES ('E-1',
		         '0000000000000000000000000000000000000000000000000000000000000001',
		         '/tmp/x', 1, 'text/plain', 100)`,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = h.SQL().Exec(`DELETE FROM evidence WHERE id = 'E-1'`)
	if err == nil {
		t.Fatal("expected DELETE to fail, got nil")
	}
	if !strings.Contains(err.Error(), "evidence rows cannot be deleted") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

Make sure the imports at the top of the test file include `"path/filepath"`, `"strings"`, and `"github.com/ProductOfAmerica/cairn/internal/db"`.

- [ ] **Step 2: Run tests to verify pass**

Run:
```bash
go test ./internal/evidence/... -run 'TestEvidenceUpdateRestricted|TestEvidenceDeleteBlocked' -v
```
Expected: PASS. (Task 0.1 already added the triggers; these tests verify they fire at runtime, not just that they exist in sqlite_master.)

- [ ] **Step 3: Commit**

```bash
git add internal/evidence/evidence_test.go
git commit -m "test(evidence): triggers block UPDATE of immutable columns + any DELETE"
```

---

## Phase 1: Memory validation helpers (pure functions)

### Task 1.1: `internal/memory/validate.go` + tests

**Files:**
- Create: `internal/memory/validate.go`
- Create: `internal/memory/validate_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/memory/validate_test.go`:

```go
package memory_test

import (
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func TestValidateKind(t *testing.T) {
	valid := []string{"decision", "rationale", "outcome", "failure"}
	for _, k := range valid {
		if err := memory.ValidateKind(k); err != nil {
			t.Errorf("ValidateKind(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{"", "DECISION", "unknown", " decision", "decision "}
	for _, k := range invalid {
		if err := memory.ValidateKind(k); err == nil {
			t.Errorf("ValidateKind(%q) = nil, want error", k)
		}
	}
}

func TestValidateEntityKind(t *testing.T) {
	valid := []string{"requirement", "task", "gate", "verdict", "run", "claim", "evidence", "memory"}
	for _, k := range valid {
		if err := memory.ValidateEntityKind(k); err != nil {
			t.Errorf("ValidateEntityKind(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{"", "Task", "unknown"}
	for _, k := range invalid {
		if err := memory.ValidateEntityKind(k); err == nil {
			t.Errorf("ValidateEntityKind(%q) = nil, want error", k)
		}
	}
}

func TestValidateEntityPair(t *testing.T) {
	// Both empty → ok (no entity).
	if err := memory.ValidateEntityPair("", ""); err != nil {
		t.Errorf("empty pair: %v", err)
	}
	// Both present → ok if kind valid.
	if err := memory.ValidateEntityPair("task", "TASK-001"); err != nil {
		t.Errorf("valid pair: %v", err)
	}
	// Kind but no id → error.
	if err := memory.ValidateEntityPair("task", ""); err == nil {
		t.Error("kind-without-id should fail")
	}
	// Id but no kind → error.
	if err := memory.ValidateEntityPair("", "TASK-001"); err == nil {
		t.Error("id-without-kind should fail")
	}
	// Invalid kind → error.
	if err := memory.ValidateEntityPair("Task", "TASK-001"); err == nil {
		t.Error("invalid kind should fail")
	}
	// Whitespace-only id → error.
	if err := memory.ValidateEntityPair("task", "   "); err == nil {
		t.Error("whitespace id should fail")
	}
}

func TestValidateTags(t *testing.T) {
	// Happy path.
	if err := memory.ValidateTags([]string{"foo", "bar_baz", "A1"}); err != nil {
		t.Errorf("happy: %v", err)
	}
	// Empty slice → ok.
	if err := memory.ValidateTags(nil); err != nil {
		t.Errorf("nil slice: %v", err)
	}
	if err := memory.ValidateTags([]string{}); err != nil {
		t.Errorf("empty slice: %v", err)
	}

	// Bad cases.
	bad := [][]string{
		{""},                  // empty tag
		{"foo-bar"},           // hyphen
		{"foo.bar"},           // dot
		{"foo bar"},           // space
		{"foo!"},              // symbol
		{strings.Repeat("a", 65)}, // too long (65)
	}
	for _, tags := range bad {
		if err := memory.ValidateTags(tags); err == nil {
			t.Errorf("ValidateTags(%v) = nil, want error", tags)
		}
	}

	// Too many (>20).
	many := make([]string, 21)
	for i := range many {
		many[i] = "t"
	}
	if err := memory.ValidateTags(many); err == nil {
		t.Error("21 tags should fail")
	}
}

func TestTagsText(t *testing.T) {
	got := memory.TagsText([]string{"foo", "bar"})
	if got != "foo bar" {
		t.Errorf("TagsText = %q, want %q", got, "foo bar")
	}
	if memory.TagsText(nil) != "" {
		t.Error("nil → empty")
	}
}

func TestTagsJSON(t *testing.T) {
	got := memory.TagsJSON([]string{"foo", "bar"})
	if got != `["foo","bar"]` {
		t.Errorf("TagsJSON = %q", got)
	}
	if memory.TagsJSON(nil) != `[]` {
		t.Error("nil → []")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: FAIL — package `memory` doesn't compile; nothing defined yet.

- [ ] **Step 3: Write validate.go**

Create `internal/memory/validate.go`:

```go
// Package memory owns the memory_entries + memory_fts tables. Entries are
// append-only (enforced by schema triggers); search is FTS5-ranked.
package memory

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// Locked enums. See design spec §2 (Q2, Q3).
var validKinds = map[string]bool{
	"decision":  true,
	"rationale": true,
	"outcome":   true,
	"failure":   true,
}

var validEntityKinds = map[string]bool{
	"requirement": true,
	"task":        true,
	"gate":        true,
	"verdict":     true,
	"run":         true,
	"claim":       true,
	"evidence":    true,
	"memory":      true,
}

// Tag contract (design spec §3): ASCII [a-zA-Z0-9_], 1..64 chars, max 20 per entry.
var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

const (
	maxTagLen   = 64
	maxTagCount = 20
)

// ValidateKind returns nil iff k is one of the four locked memory kinds.
func ValidateKind(k string) error {
	if !validKinds[k] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_kind",
			"kind must be one of decision|rationale|outcome|failure").
			WithDetails(map[string]any{"got": k})
	}
	return nil
}

// ValidateEntityKind returns nil iff k is one of the allowed entity kinds.
// Pass empty string only when no entity is attached; callers that accept
// optional entities should use ValidateEntityPair instead.
func ValidateEntityKind(k string) error {
	if !validEntityKinds[k] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_entity_kind",
			"entity_kind must be one of requirement|task|gate|verdict|run|claim|evidence|memory").
			WithDetails(map[string]any{"got": k})
	}
	return nil
}

// ValidateEntityPair enforces the XOR invariant: both empty, or both present.
// When present, the kind must be valid and id must be non-whitespace.
func ValidateEntityPair(kind, id string) error {
	kindEmpty := kind == ""
	idEmpty := strings.TrimSpace(id) == ""
	if kindEmpty != idEmpty {
		return cairnerr.New(cairnerr.CodeBadInput, "entity_kind_id_mismatch",
			"entity_kind and entity_id must both be set or both omitted").
			WithDetails(map[string]any{"entity_kind": kind, "entity_id": id})
	}
	if kindEmpty {
		return nil
	}
	if !validEntityKinds[kind] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_entity_kind",
			"entity_kind must be one of requirement|task|gate|verdict|run|claim|evidence|memory").
			WithDetails(map[string]any{"got": kind})
	}
	return nil
}

// ValidateTags enforces the tag format contract. Empty/nil is valid.
func ValidateTags(tags []string) error {
	if len(tags) > maxTagCount {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
			"too many tags").
			WithDetails(map[string]any{"count": len(tags), "max": maxTagCount})
	}
	for _, t := range tags {
		if len(t) == 0 {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag must not be empty")
		}
		if len(t) > maxTagLen {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag too long").
				WithDetails(map[string]any{"tag": t, "max": maxTagLen})
		}
		if !tagPattern.MatchString(t) {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag must match [a-zA-Z0-9_]+").
				WithDetails(map[string]any{"tag": t})
		}
	}
	return nil
}

// TagsText returns the FTS5-indexed form: space-joined tokens.
func TagsText(tags []string) string {
	return strings.Join(tags, " ")
}

// TagsJSON returns the canonical structured form stored in tags_json.
// Always emits a JSON array; nil → "[]".
func TagsJSON(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, _ := json.Marshal(tags)
	return string(b)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/validate.go internal/memory/validate_test.go
git commit -m "feat(memory): validation helpers for kind, entity pair, tags"
```

---

## Phase 2: Memory FTS error translator

### Task 2.1: `internal/memory/fts_error.go` + tests

**Files:**
- Create: `internal/memory/fts_error.go`
- Create: `internal/memory/fts_error_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/memory/fts_error_test.go`:

```go
package memory_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func TestTranslateFTSError_WrapsSQLiteSyntax(t *testing.T) {
	raw := errors.New(`fts5: syntax error near "AND AND"`)
	err := memory.TranslateFTSError(raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("not cairnerr.Err: %T", err)
	}
	if ce.Kind != "invalid_fts_query" {
		t.Errorf("kind = %q, want invalid_fts_query", ce.Kind)
	}
	if ce.Code != cairnerr.CodeBadInput {
		t.Errorf("code = %q, want bad_input", ce.Code)
	}

	// Envelope-visible message must be sanitized.
	for _, leak := range []string{"sqlite", "fts5:", "near \""} {
		if strings.Contains(strings.ToLower(ce.Message), strings.ToLower(leak)) {
			t.Errorf("message leaks %q: %s", leak, ce.Message)
		}
	}
	// Raw underlying error is preserved via Unwrap for debug/trace.
	if !errors.Is(errors.Unwrap(ce), raw) {
		t.Error("raw SQLite error not preserved via Unwrap")
	}
}

func TestTranslateFTSError_NilPassthrough(t *testing.T) {
	if got := memory.TranslateFTSError(nil); got != nil {
		t.Errorf("nil → %v, want nil", got)
	}
}

func TestTranslateFTSError_UnrecognizedWraps(t *testing.T) {
	// Unexpected errors (not FTS syntax) still get wrapped but without
	// leaking raw text; caller can Unwrap for diagnostics.
	raw := errors.New("disk is on fire")
	err := memory.TranslateFTSError(raw)
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("not cairnerr.Err: %T", err)
	}
	if ce.Kind != "invalid_fts_query" {
		t.Errorf("kind = %q", ce.Kind)
	}
	if strings.Contains(ce.Message, "disk") {
		t.Errorf("leaked raw message: %s", ce.Message)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:
```bash
go test ./internal/memory/... -run TestTranslateFTSError -v
```
Expected: FAIL — `memory.TranslateFTSError` does not exist.

- [ ] **Step 3: Write fts_error.go**

Create `internal/memory/fts_error.go`:

```go
package memory

import (
	"regexp"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// ftsSyntaxPattern extracts the position marker out of `fts5: syntax error near "..."`
// if the driver emits one. Not all FTS5 errors include it; callers tolerate miss.
var ftsNearPattern = regexp.MustCompile(`near\s+"([^"]*)"`)

// TranslateFTSError wraps a SQLite/FTS5 error as a cairnerr.Err with a
// sanitized message. Returns nil if err is nil. The raw error is preserved
// as Cause for Unwrap but NOT serialized to the envelope.
//
// Per design spec §4.6: the envelope message must not contain "sqlite",
// "fts5:", or `near "` substrings.
func TranslateFTSError(err error) error {
	if err == nil {
		return nil
	}
	msg := "query syntax invalid; see FTS5 query syntax docs"
	details := map[string]any{}

	raw := err.Error()
	if m := ftsNearPattern.FindStringSubmatch(raw); len(m) == 2 {
		// Record the offending fragment as a detail for debugging. Kept as
		// a structured field; the envelope's top-level Message stays clean.
		details["near"] = m[1]
	}

	// Heuristic: differentiate "syntax" from other classes. Kept minimal.
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "syntax"):
		msg = "query syntax invalid"
	case strings.Contains(low, "no such column"):
		msg = "unknown FTS5 column in query"
	}

	return cairnerr.New(cairnerr.CodeBadInput, "invalid_fts_query", msg).
		WithDetails(details).
		WithCause(err)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/fts_error.go internal/memory/fts_error_test.go
git commit -m "feat(memory): FTS5 error translator sanitizes SQLite leak"
```

---

## Phase 3: Memory Store — Append

### Task 3.1: Store skeleton + Append happy path

**Files:**
- Create: `internal/memory/store.go`
- Create: `internal/memory/memory_test.go`

- [ ] **Step 1: Write failing test for Append happy path**

Create `internal/memory/memory_test.go`:

```go
package memory_test

import (
	"context"
	"path/filepath"
	"testing"

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
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: FAIL — `memory.NewStore`, `memory.AppendInput`, `memory.AppendResult`, `Append` don't exist.

- [ ] **Step 3: Write store.go skeleton + Append**

Create `internal/memory/store.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: all tests PASS (including validation-error cases and op_log replay).

- [ ] **Step 5: Commit**

```bash
git add internal/memory/store.go internal/memory/memory_test.go
git commit -m "feat(memory): Store.Append + op_log replay + event emission"
```

---

## Phase 4: Memory Store — List

### Task 4.1: `Store.List` with filters + envelope response

**Files:**
- Modify: `internal/memory/store.go`
- Modify: `internal/memory/memory_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/memory/memory_test.go`:

```go
func TestList_NewestFirst_DefaultLimit10(t *testing.T) {
	h := openTestDB(t)
	clk := clock.NewFake(0)

	// Seed 15 entries across two kinds, two entity-kinds.
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
		r, _ := store.List(memory.ListInput{Limit: 0})
		res = r
		return nil
	})
	if res.Returned != 25 || res.TotalMatching != 25 {
		t.Errorf("limit=0: returned=%d total=%d, want 25/25", res.Returned, res.TotalMatching)
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
```

Add `"fmt"` to the test file's imports if not already present.

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/memory/... -run TestList -v
```
Expected: FAIL — `memory.ListInput`, `memory.ListResult`, `Store.List` don't exist.

- [ ] **Step 3: Write List implementation**

Append to `internal/memory/store.go`:

```go
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
	if limit == 0 && (in.Limit != 0 || total == 0) {
		// explicit unlimited, or no rows at all
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
```

The `isExplicitUnlimited` helper exists to document the contract between library and CLI: **the library treats `Limit=0` as "use the default 10"**, and the CLI layer (Phase 6) translates `--limit 0` into a large number (e.g. `math.MaxInt32`) before calling List. This keeps the Go API simple.

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/memory/... -run TestList -v
```
Expected: all PASS.

Note the `TestList_LimitZeroUnlimited` test: it passes `ListInput{Limit: 0}` expecting unlimited behavior. Given the contract above, **fix the test** to pass `Limit: math.MaxInt32` instead — that's the library-level representation of "unlimited." Update the test and re-run.

Update the test:
```go
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
```
Add `"math"` to the test imports.

Re-run:
```bash
go test ./internal/memory/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/store.go internal/memory/memory_test.go
git commit -m "feat(memory): Store.List with filters + envelope response"
```

---

## Phase 5: Memory Store — Search

### Task 5.1: `Store.Search` FTS5 MATCH + relevance + filters

**Files:**
- Modify: `internal/memory/store.go`
- Modify: `internal/memory/memory_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/memory/memory_test.go`:

```go
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
```

Add `"errors"` and `"github.com/ProductOfAmerica/cairn/internal/cairnerr"` to imports if not present.

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/memory/... -run TestSearch -v
```
Expected: FAIL — `SearchInput`, `SearchResult`, `Search` don't exist.

- [ ] **Step 3: Write Search implementation**

Append to `internal/memory/store.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/memory/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/store.go internal/memory/memory_test.go
git commit -m "feat(memory): Store.Search via FTS5 with relevance + error translation"
```

---

## Phase 6: Memory CLI

### Task 6.1: `cmd/cairn/memory.go` + three `internal/cli/memory_*.go` handlers

**Files:**
- Create: `internal/cli/memory_append.go`
- Create: `internal/cli/memory_search.go`
- Create: `internal/cli/memory_list.go`
- Create: `cmd/cairn/memory.go`
- Modify: `cmd/cairn/main.go` — register the `memory` subcommand group

- [ ] **Step 1: Study existing CLI patterns**

Before writing, read:
- `internal/cli/run.go` — envelope helper pattern
- `internal/cli/flags.go` — how global flags are registered
- `cmd/cairn/task.go` — a fully-wired example of a subcommand group with multiple actions

These set the conventions for `RunE` signature, envelope use, and flag wiring. Follow them.

- [ ] **Step 2: Write CLI handler functions**

Create `internal/cli/memory_append.go`:

```go
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

// MemoryAppendInput mirrors the CLI flags.
type MemoryAppendInput struct {
	Kind       string
	Body       string
	EntityKind string
	EntityID   string
	TagsCSV    string
	OpID       string
}

// MemoryAppend runs `cairn memory append`. It opens a WithTx on h, constructs
// a memory.Store, and calls Append.
func MemoryAppend(ctx context.Context, h *db.DB, clk clock.Clock, gen *ids.Generator, in MemoryAppendInput) (memory.AppendResult, error) {
	tags, err := parseTagsCSV(in.TagsCSV)
	if err != nil {
		return memory.AppendResult{}, err
	}
	opID := in.OpID
	if opID == "" {
		opID = gen.ULID()
	}
	var result memory.AppendResult
	err = h.WithTx(ctx, func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), gen, clk)
		r, err := store.Append(memory.AppendInput{
			OpID:       opID,
			Kind:       in.Kind,
			Body:       in.Body,
			EntityKind: in.EntityKind,
			EntityID:   in.EntityID,
			Tags:       tags,
		})
		result = r
		return err
	})
	return result, err
}

// parseTagsCSV splits on commas, trimming whitespace, ignoring empties.
func parseTagsCSV(csv string) ([]string, error) {
	if csv == "" {
		return nil, nil
	}
	raw := strings.Split(csv, ",")
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// EnvelopeSuccess wraps the result — placeholder; real envelope plumbing
// is in cli/envelope.go. The tests drive through the cobra RunE so the
// envelope shape is covered end-to-end.
var _ = fmt.Sprintf // keep imports stable when envelope usage is factored out
```

Create `internal/cli/memory_search.go`:

```go
package cli

import (
	"context"
	"math"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

// MemorySearchInput mirrors the CLI flags.
type MemorySearchInput struct {
	Query      string
	Kind       string
	EntityKind string
	EntityID   string
	Since      int64 // 0 = no filter
	SinceSet   bool  // distinguish 0-explicit from omitted
	Limit      int   // 0 = --limit 0 = unlimited (CLI-level convention)
}

// MemorySearch runs `cairn memory search`.
func MemorySearch(ctx context.Context, h *db.DB, clk clock.Clock, gen *ids.Generator, in MemorySearchInput) (memory.SearchResult, error) {
	limit := in.Limit
	if limit == 0 {
		// --limit 0 at the CLI = unlimited; translate for the library.
		limit = math.MaxInt32
	}
	var since *int64
	if in.SinceSet {
		since = &in.Since
	}
	var result memory.SearchResult
	err := h.WithTx(ctx, func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), gen, clk)
		r, err := store.Search(memory.SearchInput{
			Query:      in.Query,
			Kind:       in.Kind,
			EntityKind: in.EntityKind,
			EntityID:   in.EntityID,
			Since:      since,
			Limit:      limit,
		})
		result = r
		return err
	})
	return result, err
}
```

Create `internal/cli/memory_list.go`:

```go
package cli

import (
	"context"
	"math"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

// MemoryListInput mirrors the CLI flags.
type MemoryListInput struct {
	Kind       string
	EntityKind string
	EntityID   string
	Since      int64
	SinceSet   bool
	Limit      int
}

// MemoryList runs `cairn memory list`.
func MemoryList(ctx context.Context, h *db.DB, clk clock.Clock, gen *ids.Generator, in MemoryListInput) (memory.ListResult, error) {
	limit := in.Limit
	if limit == 0 {
		limit = math.MaxInt32
	}
	var since *int64
	if in.SinceSet {
		since = &in.Since
	}
	var result memory.ListResult
	err := h.WithTx(ctx, func(tx *db.Tx) error {
		store := memory.NewStore(tx, events.NewAppender(clk), gen, clk)
		r, err := store.List(memory.ListInput{
			Kind:       in.Kind,
			EntityKind: in.EntityKind,
			EntityID:   in.EntityID,
			Since:      since,
			Limit:      limit,
		})
		result = r
		return err
	})
	return result, err
}
```

- [ ] **Step 3: Write cobra command wiring**

Create `cmd/cairn/memory.go`:

```go
package main

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

func newMemoryCmd(app *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Append, search, and list cross-session memory entries.",
	}
	cmd.AddCommand(newMemoryAppendCmd(app))
	cmd.AddCommand(newMemorySearchCmd(app))
	cmd.AddCommand(newMemoryListCmd(app))
	return cmd
}

func newMemoryAppendCmd(app *appCtx) *cobra.Command {
	var in cli.MemoryAppendInput
	cmd := &cobra.Command{
		Use:   "append",
		Short: "Append a memory entry.",
		RunE: func(cmd *cobra.Command, args []string) error {
			in.OpID = app.opID
			r, err := cli.MemoryAppend(cmd.Context(), app.db, app.clock, app.ids, in)
			return app.emit(r, err)
		},
	}
	cmd.Flags().StringVar(&in.Kind, "kind", "", "decision|rationale|outcome|failure (required)")
	cmd.Flags().StringVar(&in.Body, "body", "", "entry body text (required)")
	cmd.Flags().StringVar(&in.EntityKind, "entity-kind", "",
		"entity kind (requirement|task|gate|verdict|run|claim|evidence|memory); must be paired with --entity-id")
	cmd.Flags().StringVar(&in.EntityID, "entity-id", "", "entity id; must be paired with --entity-kind")
	cmd.Flags().StringVar(&in.TagsCSV, "tags", "",
		"comma-separated tags; each tag matches [a-zA-Z0-9_]+, ≤64 chars, ≤20 tags")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newMemorySearchCmd(app *appCtx) *cobra.Command {
	var in cli.MemorySearchInput
	var sinceRaw string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "FTS5-ranked search over memory body + tags.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Query = args[0]
			if sinceRaw != "" {
				n, err := strconv.ParseInt(sinceRaw, 10, 64)
				if err != nil {
					return invalidSinceErr(sinceRaw)
				}
				in.Since = n
				in.SinceSet = true
			}
			r, err := cli.MemorySearch(cmd.Context(), app.db, app.clock, app.ids, in)
			return app.emit(r, err)
		},
	}
	cmd.Flags().StringVar(&in.Kind, "kind", "", "optional kind filter")
	cmd.Flags().StringVar(&in.EntityKind, "entity-kind", "", "optional entity kind filter")
	cmd.Flags().StringVar(&in.EntityID, "entity-id", "", "optional entity id filter")
	cmd.Flags().StringVar(&sinceRaw, "since", "", "integer ms since epoch")
	cmd.Flags().IntVar(&in.Limit, "limit", 10, "max results; 0 = unlimited")
	return cmd
}

func newMemoryListCmd(app *appCtx) *cobra.Command {
	var in cli.MemoryListInput
	var sinceRaw string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List memory entries, newest first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sinceRaw != "" {
				n, err := strconv.ParseInt(sinceRaw, 10, 64)
				if err != nil {
					return invalidSinceErr(sinceRaw)
				}
				in.Since = n
				in.SinceSet = true
			}
			r, err := cli.MemoryList(cmd.Context(), app.db, app.clock, app.ids, in)
			return app.emit(r, err)
		},
	}
	cmd.Flags().StringVar(&in.Kind, "kind", "", "optional kind filter")
	cmd.Flags().StringVar(&in.EntityKind, "entity-kind", "", "optional entity kind filter")
	cmd.Flags().StringVar(&in.EntityID, "entity-id", "", "optional entity id filter")
	cmd.Flags().StringVar(&sinceRaw, "since", "", "integer ms since epoch")
	cmd.Flags().IntVar(&in.Limit, "limit", 10, "max entries; 0 = unlimited")
	return cmd
}
```

Add a small helper for the since-parse error. Append to `internal/cli/memory_list.go`:

```go
import (
	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

func invalidSinceErr(raw string) error {
	return cairnerr.New(cairnerr.CodeBadInput, "invalid_since",
		"--since must be integer ms since epoch").
		WithDetails(map[string]any{"got": raw})
}
```

(If the import is duplicated with package-level imports, consolidate.)

- [ ] **Step 3a: Register the memory subcommand group in root**

Modify `cmd/cairn/main.go` to add `rootCmd.AddCommand(newMemoryCmd(app))` alongside existing registrations. Find the block that calls `newTaskCmd`, `newVerdictCmd`, etc., and add the memory registration there.

- [ ] **Step 4: Smoke-test via go build**

Run:
```bash
go build ./...
```
Expected: compiles.

Run:
```bash
go test ./internal/cli/... ./cmd/... -v
```
Expected: PASS (plus CLI tests for existing commands — no regression).

- [ ] **Step 5: Integration smoke**

Build the binary and exercise the commands by hand against a temp state-root:

```bash
go build -o bin/cairn ./cmd/cairn
export CAIRN_HOME=$(mktemp -d)
./bin/cairn init --repo-root .
./bin/cairn memory append --kind decision --body "chose FTS5 over grep" --tags fts5,search
./bin/cairn memory list
./bin/cairn memory search "FTS5"
```
Expected: JSON responses with the envelope shape, append event recorded.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/memory_append.go internal/cli/memory_search.go internal/cli/memory_list.go cmd/cairn/memory.go cmd/cairn/main.go
git commit -m "feat(cli): memory append|search|list subcommands"
```

---

## Phase 7: Evidence + Verdict invalidation surface

Per design spec §5.10 three-surface table:
- `cairn verdict report` must block when bound evidence has `invalidated_at IS NOT NULL`.
- `cairn verdict latest/history` must surface `evidence_invalidated: bool` in the response.
- `cairn task complete` must NOT consider `evidence_invalidated` (binary staleness unchanged).

Ship 2 extends Ship 1's `Verify` and the `Verdict` struct. No task.Complete changes are needed — it reads through `verdict.Store.IsFreshPass`, which only checks `gate_def_hash` + `status=pass`. Adding `evidence_invalidated` to the Verdict JSON changes output but not the freshness logic.

### Task 7.1: `Verify` rejects already-invalidated evidence

**Files:**
- Modify: `internal/evidence/store.go`
- Modify: `internal/evidence/evidence_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/evidence/evidence_test.go`:

```go
func TestVerify_RejectsInvalidatedRow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Seed one valid blob + evidence row, then mark invalidated.
	blobRoot := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	clk := clock.NewFake(100)
	var sha string
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot, clk)
		r, err := store.Put("01HNBXBT9J6MGK3Z5R7WVXTM0A", src, "")
		if err != nil {
			t.Fatal(err)
		}
		sha = r.SHA256
		return nil
	})

	// Mark row invalidated directly (trigger permits this single-column UPDATE).
	if _, err := h.SQL().Exec(
		`UPDATE evidence SET invalidated_at = 200 WHERE sha256 = ?`, sha,
	); err != nil {
		t.Fatal(err)
	}

	// Verify should now fail with evidence_invalidated (not hash_mismatch).
	var verr error
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot, clk)
		verr = store.Verify(sha)
		return nil
	})
	if verr == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *cairnerr.Err
	if !errors.As(verr, &ce) {
		t.Fatalf("not cairnerr.Err: %T", verr)
	}
	if ce.Kind != "evidence_invalidated" {
		t.Errorf("kind = %q, want evidence_invalidated", ce.Kind)
	}
	if ce.Code != cairnerr.CodeValidation {
		t.Errorf("code = %q, want validation", ce.Code)
	}
}
```

Add imports as needed: `"errors"`, `"os"`, `"context"`, `"github.com/ProductOfAmerica/cairn/internal/cairnerr"`, `"github.com/ProductOfAmerica/cairn/internal/events"`, `"github.com/ProductOfAmerica/cairn/internal/ids"`, `"github.com/ProductOfAmerica/cairn/internal/clock"`, `"github.com/ProductOfAmerica/cairn/internal/evidence"`.

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/evidence/... -run TestVerify_RejectsInvalidatedRow -v
```
Expected: FAIL — current `Verify` does not check `invalidated_at`.

- [ ] **Step 3: Extend Verify**

Modify `internal/evidence/store.go`'s `Verify` method. Before the existing SELECT that reads `id, uri`, change it to also read `invalidated_at`:

```go
func (s *Store) Verify(sha string) error {
	var evidenceID, uri string
	var invalidatedAt sql.NullInt64
	if err := s.tx.QueryRow(
		`SELECT id, uri, invalidated_at FROM evidence WHERE sha256 = ?`, sha,
	).Scan(&evidenceID, &uri, &invalidatedAt); err != nil {
		if err == sql.ErrNoRows {
			return cairnerr.New(cairnerr.CodeNotFound, "not_stored",
				fmt.Sprintf("no evidence row for sha256 %s", sha))
		}
		return fmt.Errorf("query evidence for verify: %w", err)
	}

	if invalidatedAt.Valid {
		return cairnerr.New(cairnerr.CodeValidation, "evidence_invalidated",
			"evidence was invalidated by a prior reconcile").
			WithDetails(map[string]any{
				"evidence_id":    evidenceID,
				"invalidated_at": invalidatedAt.Int64,
			})
	}

	// ... rest unchanged: os.ReadFile + rehash + compare ...
```

The rest of `Verify` stays as-is. The early-return on `invalidatedAt.Valid` ensures callers get `evidence_invalidated` (CodeValidation → exit 1) before the hash-recompute runs.

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/evidence/... -v
```
Expected: all evidence tests PASS (including the new one). Also run `verdict` tests to confirm no regression:

```bash
go test ./internal/verdict/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/evidence/store.go internal/evidence/evidence_test.go
git commit -m "feat(evidence): Verify rejects rows with invalidated_at set"
```

---

### Task 7.2: Verdict `Latest`/`History` expose `evidence_invalidated`

**Files:**
- Modify: `internal/verdict/store.go`
- Modify: `internal/verdict/verdict_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/verdict/verdict_test.go`:

```go
func TestLatest_SurfacesEvidenceInvalidation(t *testing.T) {
	h, runID, gateID, evSha, _, clk := seed(t)

	// Report a passing verdict bound to evSha.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		_, err := store.Report(verdict.ReportInput{
			OpID:         "01HNBXBT9J6MGK3Z5R7WVXTM0R",
			GateID:       gateID,
			RunID:        runID,
			Status:       "pass",
			Sha256:       evSha,
			ProducerHash: strings.Repeat("a", 64),
			InputsHash:   strings.Repeat("b", 64),
		})
		return err
	})

	// Invalidate the evidence row.
	if _, err := h.SQL().Exec(
		`UPDATE evidence SET invalidated_at = 999 WHERE sha256 = ?`, evSha,
	); err != nil {
		t.Fatal(err)
	}

	// Latest should surface evidence_invalidated=true.
	var r verdict.LatestResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		r, _ = store.Latest(gateID)
		return nil
	})
	if r.Verdict == nil {
		t.Fatal("no verdict returned")
	}
	if !r.Verdict.EvidenceInvalidated {
		t.Error("EvidenceInvalidated = false, want true")
	}

	// History must surface the same flag per row.
	var hist []verdict.VerdictWithFresh
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		hist, _ = store.History(gateID, 10)
		return nil
	})
	if len(hist) != 1 || !hist[0].EvidenceInvalidated {
		t.Errorf("history invalidation flag missing: %+v", hist)
	}
}
```

Add `"strings"` to the test file's imports if not already there.

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/verdict/... -run TestLatest_SurfacesEvidenceInvalidation -v
```
Expected: FAIL — `EvidenceInvalidated` field does not exist.

- [ ] **Step 3: Extend Verdict struct + queries**

Modify `internal/verdict/store.go`:

1. Add the field to the `Verdict` struct:
```go
type Verdict struct {
	ID                  string `json:"verdict_id"`
	RunID               string `json:"run_id"`
	GateID              string `json:"gate_id"`
	Status              string `json:"status"`
	ScoreJSON           string `json:"score_json,omitempty"`
	ProducerHash        string `json:"producer_hash"`
	GateDefHash         string `json:"gate_def_hash"`
	InputsHash          string `json:"inputs_hash"`
	EvidenceID          string `json:"evidence_id,omitempty"`
	EvidenceInvalidated bool   `json:"evidence_invalidated"`
	BoundAt             int64  `json:"bound_at"`
	Sequence            int64  `json:"sequence"`
}
```

2. Update the `Latest` SQL to LEFT JOIN `evidence` and scan `invalidated_at IS NOT NULL`:
```go
var invalidated sql.NullInt64
err = s.tx.QueryRow(
	`SELECT v.id, v.run_id, v.gate_id, v.status, v.score_json, v.producer_hash,
	        v.gate_def_hash, v.inputs_hash, v.evidence_id, v.bound_at, v.sequence,
	        e.invalidated_at
	 FROM verdicts v LEFT JOIN evidence e ON e.id = v.evidence_id
	 WHERE v.gate_id=?
	 ORDER BY v.bound_at DESC, v.sequence DESC LIMIT 1`,
	gateID,
).Scan(&v.ID, &v.RunID, &v.GateID, &v.Status, &score, &v.ProducerHash,
	&v.GateDefHash, &v.InputsHash, &evID, &v.BoundAt, &v.Sequence, &invalidated)
// ...
v.EvidenceInvalidated = invalidated.Valid
```

3. Same treatment for `History` — update its SELECT to LEFT JOIN evidence, add `invalidated_at` column, scan into a `sql.NullInt64`, set `EvidenceInvalidated` per row.

4. Note: `IsFreshPass` logic is unchanged. Invalidation is NOT a staleness flip (Q7 lock).

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/verdict/... -v
```
Expected: all PASS.

Additionally confirm task.Complete is unaffected:
```bash
go test ./internal/task/... -v
```
Expected: PASS (no regression).

- [ ] **Step 5: Commit**

```bash
git add internal/verdict/store.go internal/verdict/verdict_test.go
git commit -m "feat(verdict): Latest/History surface evidence_invalidated flag"
```

---

## Phase 8: Reconcile probe (outside tx)

### Task 8.1: `internal/reconcile/probe.go`

**Files:**
- Create: `internal/reconcile/probe.go`
- Create: `internal/reconcile/probe_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/probe_test.go`:

```go
package reconcile_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// seedEvidence inserts N blobs and returns their sha256s in insertion order.
func seedEvidence(t *testing.T, h *db.DB, blobRoot string, n int) []string {
	t.Helper()
	clk := clock.NewFake(100)
	var shas []string
	for i := 0; i < n; i++ {
		src := filepath.Join(t.TempDir(), "src.txt")
		_ = os.WriteFile(src, []byte(string(rune('a'+i))), 0o644)
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot, clk)
			r, err := store.Put("", src, "")
			if err != nil {
				return err
			}
			shas = append(shas, r.SHA256)
			return nil
		})
	}
	return shas
}

func TestProbe_DetectsMissingAndMismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 5)

	// Break blob[0]: delete file. Break blob[1]: mutate content.
	var uri0, uri1 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[1]).Scan(&uri1)
	_ = os.Remove(uri0)
	_ = os.WriteFile(uri1, []byte("tampered"), 0o644)

	// --evidence-sample-full mode: scan everything.
	candidates, err := reconcile.RunEvidenceProbe(context.Background(), h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(candidates), candidates)
	}

	byReason := map[string]int{}
	for _, c := range candidates {
		byReason[c.Reason]++
	}
	if byReason["missing"] != 1 || byReason["hash_mismatch"] != 1 {
		t.Errorf("reasons = %+v", byReason)
	}
}

func TestProbe_SampleRespectsCapAndPct(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	blobRoot := t.TempDir()

	seedEvidence(t, h, blobRoot, 500)

	// Default 5% / cap 100 → min(100, ceil(500*0.05)) = 25.
	sampled, err := reconcile.SampleSize(h, reconcile.ProbeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 25 {
		t.Errorf("default sample = %d, want 25 (5%% of 500)", sampled)
	}

	// With 2000 rows: min(100, ceil(2000*0.05)) = 100 (cap).
	seedEvidence(t, h, blobRoot, 1500) // now 2000 total
	sampled, err = reconcile.SampleSize(h, reconcile.ProbeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 100 {
		t.Errorf("capped sample = %d, want 100", sampled)
	}

	// Full mode returns total count.
	sampled, err = reconcile.SampleSize(h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if sampled != 2000 {
		t.Errorf("full sample = %d, want 2000", sampled)
	}
}

// unused helper kept to silence unused-import warnings in some fixtures.
var _ = sha256.Sum256
var _ = hex.EncodeToString
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Write probe.go**

Create `internal/reconcile/probe.go`:

```go
// Package reconcile owns `cairn reconcile`. See design spec §5 for the
// contract: probe phase runs OUTSIDE any tx (filesystem I/O only), mutation
// phase runs inside one BEGIN IMMEDIATE. Do not merge the two.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// EvidenceCandidate is a probe finding: one evidence row whose blob is
// missing or whose on-disk content no longer matches evidence.sha256.
type EvidenceCandidate struct {
	EvidenceID string
	Sha256     string
	URI        string
	Reason     string // "missing" | "hash_mismatch"
}

// ProbeOpts controls probe behavior.
type ProbeOpts struct {
	Full       bool    // --evidence-sample-full
	SamplePct  float64 // default 0.05
	SampleCap  int     // default 100
}

func (o *ProbeOpts) pct() float64 {
	if o.SamplePct > 0 {
		return o.SamplePct
	}
	return 0.05
}

func (o *ProbeOpts) cap() int {
	if o.SampleCap > 0 {
		return o.SampleCap
	}
	return 100
}

// SampleSize computes how many rows the probe will scan for the given opts.
// Full → total row count. Otherwise min(cap, ceil(total * pct)).
// Exposed for tests and for populating the reconcile_ended stats payload.
func SampleSize(h *db.DB, opts ProbeOpts) (int, error) {
	var total int
	if err := h.SQL().QueryRow(`SELECT COUNT(*) FROM evidence`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count evidence: %w", err)
	}
	if opts.Full {
		return total, nil
	}
	n := int(math.Ceil(float64(total) * opts.pct()))
	if n > opts.cap() {
		n = opts.cap()
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// RunEvidenceProbe scans evidence rows outside any tx and returns candidates
// for invalidation. Reads evidence rows (read-only SQL) and hashes blob files
// on disk. Does not touch tx-held state.
//
// Candidates ONLY. The mutation phase re-stats each candidate inside the tx
// before writing (see rule 3 implementation for the re-stat defense).
func RunEvidenceProbe(ctx context.Context, h *db.DB, opts ProbeOpts) ([]EvidenceCandidate, error) {
	limit, err := SampleSize(h, opts)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		return nil, nil
	}

	var query string
	if opts.Full {
		query = `SELECT id, sha256, uri FROM evidence WHERE invalidated_at IS NULL`
	} else {
		query = `SELECT id, sha256, uri FROM evidence
		         WHERE invalidated_at IS NULL
		         ORDER BY RANDOM() LIMIT ?`
	}

	var rows interface {
		Next() bool
		Scan(dest ...any) error
		Close() error
		Err() error
	}
	if opts.Full {
		r, err := h.SQL().QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("sample query: %w", err)
		}
		rows = r
	} else {
		r, err := h.SQL().QueryContext(ctx, query, limit)
		if err != nil {
			return nil, fmt.Errorf("sample query: %w", err)
		}
		rows = r
	}
	defer rows.Close()

	var out []EvidenceCandidate
	for rows.Next() {
		var c EvidenceCandidate
		if err := rows.Scan(&c.EvidenceID, &c.Sha256, &c.URI); err != nil {
			return nil, err
		}
		reason, ok := checkBlob(c.URI, c.Sha256)
		if ok {
			continue
		}
		c.Reason = reason
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// checkBlob returns ("", true) if the file at uri is present and its sha256
// matches expected. Otherwise returns (reason, false) where reason is
// "missing" or "hash_mismatch".
func checkBlob(uri, expected string) (string, bool) {
	f, err := os.Open(uri)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "missing", false
		}
		return "missing", false // treat any open error as missing
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "missing", false
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got == expected {
		return "", true
	}
	return "hash_mismatch", false
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/probe.go internal/reconcile/probe_test.go
git commit -m "feat(reconcile): evidence probe (OUTSIDE tx) + sample sizing"
```

---

## Phase 9: Shared reconcile types

### Task 9.1: Common types for rule results + mutation tuples

**Files:**
- Create: `internal/reconcile/types.go`

This task stands alone so later rule tasks can import these types cleanly. No tests — pure type declarations.

- [ ] **Step 1: Write types.go**

Create `internal/reconcile/types.go`:

```go
package reconcile

// Mutation is one concrete state change a rule would apply. Used by both
// the real mutation path (to emit reconcile_rule_applied payloads) and the
// dry-run simulator (for parity testing).
type Mutation struct {
	Rule     int    `json:"rule"`
	EntityID string `json:"entity_id"`
	Action   string `json:"action"` // e.g. "release", "flip_stale", "invalidate", "orphan"
	Reason   string `json:"reason"` // e.g. "expired", "spec_drift", "missing", "grace_expired"
}

// RuleResult captures what one mutating rule did during a real run.
type RuleResult struct {
	Rule          int        `json:"rule"`
	Mutations     []Mutation `json:"mutations"`
	Stats         Stats      `json:"-"` // flattened into reconcile stats
}

// Stats is the per-run summary surfaced in the reconcile_ended event payload
// and the JSON response. All counters default to 0.
type Stats struct {
	Rule1ClaimsReleased     int   `json:"rule_1_claims_released"`
	Rule1TasksReverted      int   `json:"rule_1_tasks_reverted"`
	Rule2TasksFlippedStale  int   `json:"rule_2_tasks_flipped_stale"`
	Rule2LatencyMs          int64 `json:"rule_2_latency_ms"`
	Rule3EvidenceInvalid    int   `json:"rule_3_evidence_invalidated"`
	Rule3Sampled            int   `json:"rule_3_sampled"`
	Rule3OfTotal            int   `json:"rule_3_of_total"`
	Rule3Mode               string `json:"rule_3_mode"` // "sample" | "full"
	Rule4RunsOrphaned       int   `json:"rule_4_runs_orphaned"`
	Rule5AuthoringErrors    int   `json:"rule_5_authoring_errors"`
}

// AuthoringError is a finding from rule 5 (read-only).
type AuthoringError struct {
	TaskID         string `json:"task_id"`
	MissingGateID  string `json:"missing_gate_id"`
}

// Result is the top-level response for a real reconcile run.
type Result struct {
	ReconcileID     string           `json:"reconcile_id"`
	DryRun          bool             `json:"dry_run"`
	Stats           Stats            `json:"stats"`
	AuthoringErrors []AuthoringError `json:"authoring_errors"`
}

// DryRunResult is the response shape for `cairn reconcile --dry-run`. Does
// NOT include reconcile_id (Q9: dry-run didn't happen; no event references an id).
type DryRunResult struct {
	DryRun bool              `json:"dry_run"`
	Rules  []DryRunRule      `json:"rules"`
}

// DryRunRule is a per-rule preview. For mutating rules 1..4, Mutations holds
// the would-be mutations. For rule 5 (read-only), AuthoringErrors holds the
// findings; Mutations stays empty.
type DryRunRule struct {
	Rule            int              `json:"rule"`
	Mutations       []Mutation       `json:"would_mutate,omitempty"`
	AuthoringErrors []AuthoringError `json:"authoring_errors,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./internal/reconcile/...
```
Expected: compiles clean.

- [ ] **Step 3: Commit**

```bash
git add internal/reconcile/types.go
git commit -m "feat(reconcile): shared Mutation, Stats, Result, DryRunResult types"
```

---

## Phase 10: Rule 1 — expired leases

### Task 10.1: `rule1_leases.go` — release expired claims + revert tasks

**Files:**
- Create: `internal/reconcile/rule1_leases.go`
- Create: `internal/reconcile/rule1_leases_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/rule1_leases_test.go`:

```go
package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// seedLeaseFixture inserts one requirement, one task (status=claimed), and
// one claim with expires_at < now. Returns (task_id, claim_id).
func seedLeaseFixture(t *testing.T, h *db.DB, claimExpiresAt int64) (string, string) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','claimed',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,?,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`, claimExpiresAt)
		return nil
	})
	return "T-1", "CL-1"
}

func TestRule1_ReleasesExpiredAndRevertsTask(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	seedLeaseFixture(t, h, 5000) // expires in the past

	var result reconcile.RuleResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule1ReleaseExpiredLeases(tx, events.NewAppender(clk), clk, "RC-1")
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule1ClaimsReleased != 1 {
		t.Errorf("released = %d, want 1", result.Stats.Rule1ClaimsReleased)
	}
	if result.Stats.Rule1TasksReverted != 1 {
		t.Errorf("reverted = %d, want 1", result.Stats.Rule1TasksReverted)
	}

	// Side effects on DB.
	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "open" {
		t.Errorf("task status = %q, want open", status)
	}
	var released int64
	_ = h.SQL().QueryRow(`SELECT released_at FROM claims WHERE id='CL-1'`).Scan(&released)
	if released != 10000 {
		t.Errorf("released_at = %d, want 10000", released)
	}

	// Events emitted.
	var n int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events
	                     WHERE kind IN ('claim_released','task_status_changed','reconcile_rule_applied')`).Scan(&n)
	if n < 3 {
		t.Errorf("events emitted = %d, want >=3", n)
	}
}

func TestRule1_LiveClaimIsIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	seedLeaseFixture(t, h, 99999999) // not expired

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := reconcile.Rule1ReleaseExpiredLeases(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		return nil
	})

	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "claimed" {
		t.Errorf("task should still be claimed, got %q", status)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestRule1 -v
```
Expected: FAIL — `Rule1ReleaseExpiredLeases` not implemented.

- [ ] **Step 3: Implement rule 1**

Create `internal/reconcile/rule1_leases.go`:

```go
package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// Rule1ReleaseExpiredLeases releases expired claims and reverts their tasks
// to 'open' if no live claim remains.
//
// INVARIANT: runs inside BEGIN IMMEDIATE. SQLite holds RESERVED/WRITE lock
// from start-of-tx; no concurrent writer can interleave between the two
// statements below. The NOT IN subquery is race-free under this serialization.
func Rule1ReleaseExpiredLeases(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	now := clk.NowMilli()
	result := RuleResult{Rule: 1}

	// 1) Release expired claims, capture (id, task_id) for event emission.
	rows, err := tx.Query(
		`UPDATE claims SET released_at = ?
		 WHERE expires_at < ? AND released_at IS NULL
		 RETURNING id, task_id`,
		now, now,
	)
	if err != nil {
		return result, fmt.Errorf("release expired claims: %w", err)
	}

	type releasedClaim struct{ ID, TaskID string }
	var released []releasedClaim
	for rows.Next() {
		var rc releasedClaim
		if err := rows.Scan(&rc.ID, &rc.TaskID); err != nil {
			rows.Close()
			return result, err
		}
		released = append(released, rc)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	for _, rc := range released {
		if err := appender.Append(tx, events.Record{
			Kind:       "claim_released",
			EntityKind: "claim",
			EntityID:   rc.ID,
			Payload: map[string]any{
				"task_id": rc.TaskID,
				"reason":  "expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 1, EntityID: rc.ID, Action: "release", Reason: "expired",
		})
	}
	result.Stats.Rule1ClaimsReleased = len(released)

	// 2) Revert tasks whose claim is gone. Track which tasks flip so we can
	// emit per-task events. RETURNING gives us the old status via the
	// UPDATE syntax in SQLite (supported since 3.35 via RETURNING clause on
	// the row's pre-UPDATE values is NOT standard). Instead, we first
	// SELECT, then UPDATE.
	sel, err := tx.Query(
		`SELECT id, status FROM tasks
		 WHERE status IN ('claimed','in_progress','gate_pending')
		   AND id NOT IN (
		     SELECT task_id FROM claims
		      WHERE released_at IS NULL AND expires_at >= ?)`,
		now,
	)
	if err != nil {
		return result, fmt.Errorf("select tasks to revert: %w", err)
	}
	type pending struct{ ID, From string }
	var flips []pending
	for sel.Next() {
		var p pending
		if err := sel.Scan(&p.ID, &p.From); err != nil {
			sel.Close()
			return result, err
		}
		flips = append(flips, p)
	}
	sel.Close()

	for _, p := range flips {
		_, err := tx.Exec(
			`UPDATE tasks SET status='open', updated_at=? WHERE id=?`,
			now, p.ID,
		)
		if err != nil {
			return result, fmt.Errorf("revert task: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "task_status_changed",
			EntityKind: "task",
			EntityID:   p.ID,
			Payload: map[string]any{
				"from":   p.From,
				"to":     "open",
				"reason": "lease_expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 1, EntityID: p.ID, Action: "revert_to_open", Reason: "lease_expired",
		})
	}
	result.Stats.Rule1TasksReverted = len(flips)

	// 3) Emit reconcile_rule_applied if anything happened.
	if len(released)+len(flips) > 0 {
		affected := make([]string, 0, len(released)+len(flips))
		for _, rc := range released {
			affected = append(affected, rc.ID)
		}
		for _, p := range flips {
			affected = append(affected, p.ID)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                1,
				"affected_entity_ids": affected,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/rule1_leases.go internal/reconcile/rule1_leases_test.go
git commit -m "feat(reconcile): rule 1 — release expired leases + revert tasks"
```

---

## Phase 11: Rule 2 — spec-drift staleness

### Task 11.1: `rule2_staleness.go` — flip `done → stale` on drifted gates

**Files:**
- Create: `internal/reconcile/rule2_staleness.go`
- Create: `internal/reconcile/rule2_staleness_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/rule2_staleness_test.go`:

```go
package reconcile_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// hash64 produces a 64-char hex string from a seed character; convenience
// for fixtures where the actual value doesn't matter, only equality.
func hash64(c byte) string {
	return strings.Repeat(string(c), 64)
}

// seedStalenessFixture inserts one requirement with one gate, one task,
// one claim, one run, and one verdict (status=pass). Caller can optionally
// drift the gate_def_hash on the gates row to simulate spec edits.
func seedStalenessFixture(t *testing.T, h *db.DB, gateHash, verdictGateHash string) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO gates (id, requirement_id, kind, definition_json,
		                  gate_def_hash, producer_kind, producer_config)
		                  VALUES ('AC-1','REQ-1','test','{}',?,'executable','{}')`, gateHash)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','["AC-1"]','done',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,9999999999999,'01HNBXBT9J6MGK3Z5R7WVXTM0A')`)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at, ended_at, outcome)
		                 VALUES ('RUN-1','T-1','CL-1',0,1,'done')`)
		_, _ = tx.Exec(`INSERT INTO verdicts (id, run_id, gate_id, status, score_json,
		                   producer_hash, gate_def_hash, inputs_hash,
		                   evidence_id, bound_at, sequence)
		                 VALUES ('V-1','RUN-1','AC-1','pass',NULL,?,?,?,NULL,1,1)`,
			hash64('p'), verdictGateHash, hash64('i'))
		return nil
	})
}

func TestRule2_FlipsStaleOnGateDrift(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Current gate_def_hash = 'a'*64; verdict was bound against 'b'*64.
	seedStalenessFixture(t, h, hash64('a'), hash64('b'))

	var result reconcile.RuleResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule2TasksFlippedStale != 1 {
		t.Errorf("flipped = %d, want 1", result.Stats.Rule2TasksFlippedStale)
	}
	var status string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&status)
	if status != "stale" {
		t.Errorf("task status = %q, want stale", status)
	}
}

func TestRule2_LeavesFreshDone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Current gate and verdict hashes match.
	seedStalenessFixture(t, h, hash64('a'), hash64('a'))

	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			return err
		}
		if r.Stats.Rule2TasksFlippedStale != 0 {
			t.Errorf("flipped = %d, want 0", r.Stats.Rule2TasksFlippedStale)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRule2_LatestVerdictPrecedence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)

	// Seed: gate hash='a'. Verdict 1 (earlier) is fresh+pass. Verdict 2
	// (later) has drifted hash. Rule 2 must see the latest and mark stale.
	seedStalenessFixture(t, h, hash64('a'), hash64('a'))
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := tx.Exec(`INSERT INTO verdicts (id, run_id, gate_id, status, score_json,
		                      producer_hash, gate_def_hash, inputs_hash,
		                      evidence_id, bound_at, sequence)
		                    VALUES ('V-2','RUN-1','AC-1','pass',NULL,?,?,?,NULL,2,2)`,
			hash64('p'), hash64('b'), hash64('i'))
		return err
	})

	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule2FlipStaleTasks(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			return err
		}
		if r.Stats.Rule2TasksFlippedStale != 1 {
			t.Errorf("latest-verdict precedence failed: flipped=%d, want 1",
				r.Stats.Rule2TasksFlippedStale)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Reference verdict package to keep imports stable.
var _ = verdict.LatestResult{}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestRule2 -v
```
Expected: FAIL — `Rule2FlipStaleTasks` not implemented.

- [ ] **Step 3: Implement rule 2**

Create `internal/reconcile/rule2_staleness.go`:

```go
package reconcile

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// Rule2FlipStaleTasks iterates over `done` tasks and flips any whose
// required gates include a drifted/missing/non-passing latest verdict to
// `status='stale'`. Reuses verdict.Store.IsFreshPass (Ship 1, tested) for
// the per-gate check.
//
// Implementation is a Go loop over tasks × gates. Design spec §5.4 flags
// this as a Ship 4 optimization candidate if rule_2_latency_ms exceeds
// 100ms in dogfood.
func Rule2FlipStaleTasks(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	start := time.Now()
	result := RuleResult{Rule: 2}
	now := clk.NowMilli()

	// Pull all done tasks + their required_gates arrays.
	rows, err := tx.Query(`SELECT id, required_gates_json FROM tasks WHERE status='done'`)
	if err != nil {
		return result, fmt.Errorf("select done tasks: %w", err)
	}

	type doneTask struct {
		ID        string
		GateIDs   []string
	}
	var tasks []doneTask
	for rows.Next() {
		var t doneTask
		var gatesJSON string
		if err := rows.Scan(&t.ID, &gatesJSON); err != nil {
			rows.Close()
			return result, err
		}
		if err := json.Unmarshal([]byte(gatesJSON), &t.GateIDs); err != nil {
			rows.Close()
			return result, fmt.Errorf("unmarshal required_gates_json for %s: %w", t.ID, err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	// verdict.Store is constructed with a nil evidence store because rule 2
	// only calls IsFreshPass/Latest (reads). No evidence.Verify is invoked
	// in these paths. The generator is only used by Report, also unused here.
	vstore := verdict.NewStore(tx, appender, (*ids.Generator)(nil), nil, clk)

	var flippedIDs []string
	for _, t := range tasks {
		stale := false
		for _, gateID := range t.GateIDs {
			fresh, _, err := vstore.IsFreshPass(gateID)
			if err != nil {
				return result, fmt.Errorf("IsFreshPass %s/%s: %w", t.ID, gateID, err)
			}
			if !fresh {
				stale = true
				break
			}
		}
		if !stale {
			continue
		}

		if _, err := tx.Exec(
			`UPDATE tasks SET status='stale', updated_at=? WHERE id=? AND status='done'`,
			now, t.ID,
		); err != nil {
			return result, fmt.Errorf("flip stale: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "task_status_changed",
			EntityKind: "task",
			EntityID:   t.ID,
			Payload: map[string]any{
				"from":   "done",
				"to":     "stale",
				"reason": "spec_drift",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 2, EntityID: t.ID, Action: "flip_stale", Reason: "spec_drift",
		})
		flippedIDs = append(flippedIDs, t.ID)
	}

	result.Stats.Rule2TasksFlippedStale = len(flippedIDs)
	result.Stats.Rule2LatencyMs = time.Since(start).Milliseconds()

	if len(flippedIDs) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                2,
				"affected_entity_ids": flippedIDs,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/rule2_staleness.go internal/reconcile/rule2_staleness_test.go
git commit -m "feat(reconcile): rule 2 — flip done→stale on gate drift"
```

---

## Phase 12: Rule 3 — evidence invalidation (mutation + re-stat)

### Task 12.1: `rule3_evidence.go` — apply candidates with re-stat defense

**Files:**
- Create: `internal/reconcile/rule3_evidence.go`
- Create: `internal/reconcile/rule3_evidence_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/rule3_evidence_test.go`:

```go
package reconcile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestRule3_InvalidatesMissingBlob(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	// Seed two evidence rows.
	shas := seedEvidence(t, h, blobRoot, 2)

	// Delete blob[0] on disk.
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = os.Remove(uri0)

	// Probe outside tx.
	candidates, err := reconcile.RunEvidenceProbe(context.Background(), h, reconcile.ProbeOpts{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	var result reconcile.RuleResult
	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Rule3EvidenceInvalid != 1 {
		t.Errorf("invalidated = %d, want 1", result.Stats.Rule3EvidenceInvalid)
	}
	var inv int64
	_ = h.SQL().QueryRow(`SELECT invalidated_at FROM evidence WHERE sha256=?`, shas[0]).Scan(&inv)
	if inv != 10000 {
		t.Errorf("invalidated_at = %d, want 10000", inv)
	}
}

func TestRule3_ReStatSkipsRecoveredBlob(t *testing.T) {
	// Scenario: probe says "missing", but before the tx commits, another
	// process `evidence put` recreates the blob at the exact sha256.
	// Re-stat inside the tx must observe presence + matching hash and skip.
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 1)
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)

	// Simulate stale probe: craft a candidate manually (file still present).
	candidates := []reconcile.EvidenceCandidate{{
		EvidenceID: "E-bogus", // id is not used in the re-stat
		Sha256:     shas[0],
		URI:        uri0,
		Reason:     "missing", // probe's stale conclusion
	}}

	// Look up real evidence_id so the mutation can touch the row.
	var realID string
	_ = h.SQL().QueryRow(`SELECT id FROM evidence WHERE sha256=?`, shas[0]).Scan(&realID)
	candidates[0].EvidenceID = realID

	var result reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		if err != nil {
			t.Fatal(err)
		}
		result = r
		return nil
	})
	if result.Stats.Rule3EvidenceInvalid != 0 {
		t.Errorf("re-stat should have skipped; got invalidated=%d",
			result.Stats.Rule3EvidenceInvalid)
	}
	var inv *int64
	_ = h.SQL().QueryRow(`SELECT invalidated_at FROM evidence WHERE sha256=?`, shas[0]).Scan(&inv)
	if inv != nil {
		t.Errorf("evidence should NOT be marked invalidated; got %v", *inv)
	}
}

func TestRule3_IsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	shas := seedEvidence(t, h, blobRoot, 1)
	var uri0 string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri0)
	_ = os.Remove(uri0)

	var realID string
	_ = h.SQL().QueryRow(`SELECT id FROM evidence WHERE sha256=?`, shas[0]).Scan(&realID)
	candidates := []reconcile.EvidenceCandidate{{
		EvidenceID: realID,
		Sha256:     shas[0],
		URI:        uri0,
		Reason:     "missing",
	}}

	// First application invalidates.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-1", candidates)
		return nil
	})
	// Second application must be a no-op (row already invalidated).
	var second reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule3ApplyEvidenceInvalidations(tx, events.NewAppender(clk), clk,
			"RC-2", candidates)
		if err != nil {
			t.Fatal(err)
		}
		second = r
		return nil
	})
	if second.Stats.Rule3EvidenceInvalid != 0 {
		t.Errorf("second run should be no-op, got invalidated=%d", second.Stats.Rule3EvidenceInvalid)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestRule3 -v
```
Expected: FAIL.

- [ ] **Step 3: Implement rule 3**

Create `internal/reconcile/rule3_evidence.go`:

```go
package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// Rule3ApplyEvidenceInvalidations consumes probe-phase candidates, re-stats
// each one inside the tx (design spec §5.5 re-stat invariant), and issues
// UPDATE evidence SET invalidated_at for survivors.
//
// Re-stat invariant: both file presence AND hash match must FAIL to invalidate.
// A file that is now present and hashes cleanly → skip (probe was stale).
//
// Idempotent: `invalidated_at IS NULL` guard in WHERE prevents double-flipping.
func Rule3ApplyEvidenceInvalidations(
	tx *db.Tx,
	appender events.Appender,
	clk clock.Clock,
	reconcileID string,
	candidates []EvidenceCandidate,
) (RuleResult, error) {
	result := RuleResult{Rule: 3}
	if len(candidates) == 0 {
		return result, nil
	}
	now := clk.NowMilli()

	var affected []string
	for _, c := range candidates {
		// Re-stat: confirm the candidate's condition still holds.
		reason, stillInvalid := reStatInvalid(c.URI, c.Sha256)
		if !stillInvalid {
			// File present + hash matches; probe was stale, skip.
			continue
		}

		// Single-column UPDATE; triggers permit it. Guard on
		// invalidated_at IS NULL keeps this idempotent.
		res, err := tx.Exec(
			`UPDATE evidence SET invalidated_at = ?
			 WHERE id = ? AND invalidated_at IS NULL`,
			now, c.EvidenceID,
		)
		if err != nil {
			return result, fmt.Errorf("invalidate evidence %s: %w", c.EvidenceID, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue // already invalidated by a concurrent/prior run
		}

		if err := appender.Append(tx, events.Record{
			Kind:       "evidence_invalidated",
			EntityKind: "evidence",
			EntityID:   c.EvidenceID,
			Payload: map[string]any{
				"reason": reason,
				"sha256": c.Sha256,
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 3, EntityID: c.EvidenceID, Action: "invalidate", Reason: reason,
		})
		affected = append(affected, c.EvidenceID)
	}
	result.Stats.Rule3EvidenceInvalid = len(affected)

	if len(affected) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                3,
				"affected_entity_ids": affected,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}

// reStatInvalid returns (reason, true) if the blob at uri is still missing
// OR still hash-mismatched vs expected. Returns ("", false) if the blob is
// present AND matches — probe was stale.
func reStatInvalid(uri, expected string) (string, bool) {
	r, ok := checkBlob(uri, expected)
	if ok {
		return "", false
	}
	return r, true
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS (including `TestRule3_ReStatSkipsRecoveredBlob`).

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/rule3_evidence.go internal/reconcile/rule3_evidence_test.go
git commit -m "feat(reconcile): rule 3 — evidence invalidation with re-stat defense"
```

---

## Phase 13: Rule 4 — orphan sweep

### Task 13.1: `rule4_orphans.go` — mark runs whose grace elapsed

**Files:**
- Create: `internal/reconcile/rule4_orphans.go`
- Create: `internal/reconcile/rule4_orphans_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/rule4_orphans_test.go`:

```go
package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// seedOrphanFixture inserts one task, one claim (released_at set), one run
// in-progress. Caller controls released_at.
func seedOrphanFixture(t *testing.T, h *db.DB, releasedAt int64) {
	t.Helper()
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','open',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at,
		                                     released_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,1,?,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`, releasedAt)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at)
		                 VALUES ('RUN-1','T-1','CL-1',0)`)
		return nil
	})
}

func TestRule4_OrphansAfterGrace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000) // 10,000,000ms

	// Released 11 min ago (11*60*1000 = 660,000ms prior = 9,340,000).
	seedOrphanFixture(t, h, 9_340_000)

	var result reconcile.RuleResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule4OrphanExpiredRuns(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		result = r
		return nil
	})
	if result.Stats.Rule4RunsOrphaned != 1 {
		t.Errorf("orphaned = %d, want 1", result.Stats.Rule4RunsOrphaned)
	}
	var outcome string
	_ = h.SQL().QueryRow(`SELECT outcome FROM runs WHERE id='RUN-1'`).Scan(&outcome)
	if outcome != "orphaned" {
		t.Errorf("outcome = %q, want orphaned", outcome)
	}
}

func TestRule4_WithinGraceIsSkipped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)

	// Released 5 min ago (within 10min grace).
	seedOrphanFixture(t, h, 9_700_000)

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		r, err := reconcile.Rule4OrphanExpiredRuns(tx, events.NewAppender(clk), clk, "RC-1")
		if err != nil {
			t.Fatal(err)
		}
		if r.Stats.Rule4RunsOrphaned != 0 {
			t.Errorf("orphaned = %d, want 0", r.Stats.Rule4RunsOrphaned)
		}
		return nil
	})
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestRule4 -v
```
Expected: FAIL.

- [ ] **Step 3: Implement rule 4**

Create `internal/reconcile/rule4_orphans.go`:

```go
package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// orphanGraceMs is the fixed 10-minute grace window from the claim's
// released_at before rule 4 marks the associated run orphaned.
// Design spec §5.6 / Q6.
const orphanGraceMs = 10 * 60 * 1000

// Rule4OrphanExpiredRuns sweeps in-progress runs whose claim was released
// more than orphanGraceMs ago. Sets runs.ended_at = now, outcome='orphaned',
// emits run_ended events.
//
// INVARIANT: must run AFTER rule 1 in the same tx. Rule 1 populates
// claims.released_at on expired leases with now; rule 4's 10-min grace
// correctly misses those (absorbs clock skew, gives agents a chance to
// finish a run whose heartbeat just failed).
func Rule4OrphanExpiredRuns(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	result := RuleResult{Rule: 4}
	now := clk.NowMilli()

	// SELECT candidate runs first so we can emit events per-row.
	rows, err := tx.Query(
		`SELECT runs.id, runs.task_id
		 FROM runs
		 JOIN claims ON claims.id = runs.claim_id
		 WHERE runs.ended_at IS NULL
		   AND claims.released_at IS NOT NULL
		   AND claims.released_at + ? < ?`,
		orphanGraceMs, now,
	)
	if err != nil {
		return result, fmt.Errorf("select orphan candidates: %w", err)
	}
	type row struct{ ID, TaskID string }
	var picked []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.TaskID); err != nil {
			rows.Close()
			return result, err
		}
		picked = append(picked, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	var ids []string
	for _, r := range picked {
		if _, err := tx.Exec(
			`UPDATE runs SET ended_at = ?, outcome = 'orphaned' WHERE id = ?`,
			now, r.ID,
		); err != nil {
			return result, fmt.Errorf("orphan run: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "run_ended",
			EntityKind: "run",
			EntityID:   r.ID,
			Payload: map[string]any{
				"task_id": r.TaskID,
				"outcome": "orphaned",
				"reason":  "grace_expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 4, EntityID: r.ID, Action: "orphan", Reason: "grace_expired",
		})
		ids = append(ids, r.ID)
	}
	result.Stats.Rule4RunsOrphaned = len(ids)

	if len(ids) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                4,
				"affected_entity_ids": ids,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/rule4_orphans.go internal/reconcile/rule4_orphans_test.go
git commit -m "feat(reconcile): rule 4 — orphan runs past 10min grace"
```

---

## Phase 14: Rule 5 — authoring errors (read-only)

### Task 14.1: `rule5_authoring.go` — detect tasks referencing missing gates

**Files:**
- Create: `internal/reconcile/rule5_authoring.go`
- Create: `internal/reconcile/rule5_authoring_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/rule5_authoring_test.go`:

```go
package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestRule5_FindsMissingGateRefs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		// Only AC-1 exists; task references AC-1 + AC-BOGUS.
		_, _ = tx.Exec(`INSERT INTO gates (id, requirement_id, kind, definition_json,
		                  gate_def_hash, producer_kind, producer_config)
		                  VALUES ('AC-1','REQ-1','test','{}',
		                          '0000000000000000000000000000000000000000000000000000000000000001',
		                          'executable','{}')`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]',
		                          '["AC-1","AC-BOGUS"]','open',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-2','REQ-1','p','h','[]','["AC-1"]','open',0,0)`)
		return nil
	})

	var errs []reconcile.AuthoringError
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		var err error
		errs, err = reconcile.Rule5CollectAuthoringErrors(tx)
		return err
	})
	if len(errs) != 1 {
		t.Fatalf("authoring errors = %d, want 1: %+v", len(errs), errs)
	}
	if errs[0].TaskID != "T-1" || errs[0].MissingGateID != "AC-BOGUS" {
		t.Errorf("unexpected: %+v", errs[0])
	}
}

func TestRule5_EmitsNoEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()

	// Seed a case with one authoring error.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','["AC-BOGUS"]','open',0,0)`)
		return nil
	})

	var before, after int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := reconcile.Rule5CollectAuthoringErrors(tx)
		return err
	})
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after)
	if before != after {
		t.Errorf("rule 5 must emit no events; count changed %d → %d", before, after)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestRule5 -v
```
Expected: FAIL.

- [ ] **Step 3: Implement rule 5**

Create `internal/reconcile/rule5_authoring.go`:

```go
package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// Rule5CollectAuthoringErrors scans tasks.required_gates_json for gate IDs
// that do not exist in the gates table. Read-only: emits zero events.
// Findings are surfaced in the reconcile_ended payload and the JSON response.
func Rule5CollectAuthoringErrors(tx *db.Tx) ([]AuthoringError, error) {
	rows, err := tx.Query(
		`SELECT tasks.id, j.value
		 FROM tasks, json_each(tasks.required_gates_json) j
		 LEFT JOIN gates ON gates.id = j.value
		 WHERE gates.id IS NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("select authoring errors: %w", err)
	}
	defer rows.Close()

	var out []AuthoringError
	for rows.Next() {
		var e AuthoringError
		if err := rows.Scan(&e.TaskID, &e.MissingGateID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/rule5_authoring.go internal/reconcile/rule5_authoring_test.go
git commit -m "feat(reconcile): rule 5 — authoring errors (read-only)"
```

---

## Phase 15: Reconcile orchestrator + dry-run simulator

### Task 15.1: `reconcile.go` orchestrator

**Files:**
- Create: `internal/reconcile/reconcile.go`
- Create: `internal/reconcile/reconcile_orch_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/reconcile/reconcile_orch_test.go`:

```go
package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestOrchestrator_EmitsStartedEnded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(1000)
	blobRoot := t.TempDir()

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	result, err := orch.Run(context.Background(), reconcile.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReconcileID == "" {
		t.Error("empty reconcile_id")
	}
	if result.DryRun {
		t.Error("expected dry_run=false")
	}

	// Events: started + ended pair always present on real runs.
	var kinds []string
	rows, _ := h.SQL().Query(`SELECT kind FROM events ORDER BY id`)
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	if len(kinds) < 2 {
		t.Fatalf("expected >=2 events; got %v", kinds)
	}
	if kinds[0] != "reconcile_started" || kinds[len(kinds)-1] != "reconcile_ended" {
		t.Errorf("bracket events not surrounding: %v", kinds)
	}
}

func TestOrchestrator_Rule4DependsOnRule1Ordering(t *testing.T) {
	// Scenario: one task, one claim with expires_at just past now; one
	// in-progress run against it. A single `Run` must:
	//   1. Release the claim (rule 1 sets released_at = now).
	//   2. NOT orphan the run (rule 4 sees released_at + 10min > now, skips).
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)
	blobRoot := t.TempDir()

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','[]','claimed',0,0)`)
		// Expired a microsecond ago.
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-1','T-1','a',0,9_999_999,
		                         '01HNBXBT9J6MGK3Z5R7WVXTM0A')`)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at)
		                 VALUES ('RUN-1','T-1','CL-1',0)`)
		return nil
	})

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	if _, err := orch.Run(context.Background(), reconcile.Opts{}); err != nil {
		t.Fatal(err)
	}

	var outcome *string
	_ = h.SQL().QueryRow(`SELECT outcome FROM runs WHERE id='RUN-1'`).Scan(&outcome)
	if outcome != nil {
		t.Errorf("run must NOT be orphaned within grace; outcome=%v", *outcome)
	}
	var released int64
	_ = h.SQL().QueryRow(`SELECT released_at FROM claims WHERE id='CL-1'`).Scan(&released)
	if released != 10_000_000 {
		t.Errorf("claim not released by rule 1; released_at=%d", released)
	}
}

func TestOrchestrator_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10000)
	blobRoot := t.TempDir()

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	r1, _ := orch.Run(context.Background(), reconcile.Opts{})
	clk.Set(20000)
	r2, _ := orch.Run(context.Background(), reconcile.Opts{})

	// Second run has different reconcile_id (accurate record of two invocations)
	// but stats show zero mutations.
	if r1.ReconcileID == r2.ReconcileID {
		t.Error("reconcile_ids should differ between invocations")
	}
	totalSecond := r2.Stats.Rule1ClaimsReleased + r2.Stats.Rule1TasksReverted +
		r2.Stats.Rule2TasksFlippedStale + r2.Stats.Rule3EvidenceInvalid +
		r2.Stats.Rule4RunsOrphaned
	if totalSecond != 0 {
		t.Errorf("second run not idempotent; stats: %+v", r2.Stats)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestOrchestrator -v
```
Expected: FAIL.

- [ ] **Step 3: Implement the orchestrator**

Create `internal/reconcile/reconcile.go`:

```go
package reconcile

import (
	"context"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// Opts controls a single reconcile invocation.
type Opts struct {
	DryRun               bool
	EvidenceSampleFull   bool
	// SamplePct / SampleCap may be set by tests; defaults apply when zero.
	SamplePct float64
	SampleCap int
}

// Orchestrator ties together the probe + five rules. Held via NewOrchestrator.
type Orchestrator struct {
	db       *db.DB
	clock    clock.Clock
	ids      *ids.Generator
	blobRoot string
}

// NewOrchestrator constructs the runner. blobRoot matters only for rule 3's
// probe (file paths stored in evidence.uri are absolute, so blobRoot is
// reserved for future symmetry with evidence.Store).
func NewOrchestrator(h *db.DB, clk clock.Clock, g *ids.Generator, blobRoot string) *Orchestrator {
	return &Orchestrator{db: h, clock: clk, ids: g, blobRoot: blobRoot}
}

// Run executes the reconcile in two phases: probe (outside tx), mutation
// (one BEGIN IMMEDIATE). Dry-run short-circuits to the pure-read simulator.
func (o *Orchestrator) Run(ctx context.Context, opts Opts) (Result, error) {
	if opts.DryRun {
		dr, err := o.dryRun(ctx, opts)
		if err != nil {
			return Result{}, err
		}
		// Adapt DryRunResult to Result shape so the CLI can emit uniformly.
		return Result{DryRun: true, AuthoringErrors: extractAuthoringFromDry(dr)}, nil
	}

	// =================================================================
	// PROBE PHASE — NO TX. Filesystem I/O only; zero writes, zero events.
	// Collects candidate mutations into an in-memory struct.
	// DO NOT move these reads inside the mutation tx — doing so
	// reintroduces the Q8 lock-contention problem (100-blob sha256
	// under BEGIN IMMEDIATE starves concurrent writers).
	// =================================================================
	probeOpts := ProbeOpts{Full: opts.EvidenceSampleFull, SamplePct: opts.SamplePct, SampleCap: opts.SampleCap}
	candidates, err := RunEvidenceProbe(ctx, o.db, probeOpts)
	if err != nil {
		return Result{}, fmt.Errorf("probe: %w", err)
	}
	sampled, err := SampleSize(o.db, probeOpts)
	if err != nil {
		return Result{}, err
	}
	var total int
	if err := o.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM evidence`).Scan(&total); err != nil {
		return Result{}, err
	}

	// =================================================================
	// MUTATION PHASE — ONE BEGIN IMMEDIATE. All rule writes + events.
	// Rule ordering: 1 → 2 → 3 → 4 → 5.
	//   - Rule 4 depends on rule 1 running first (fresh released_at is
	//     within 10min grace; orphan sweep correctly skips).
	//   - Rule 5 is read-only; emits no events; findings in stats.
	// =================================================================
	reconcileID := o.ids.ULID()
	appender := events.NewAppender(o.clock)

	var result Result
	result.ReconcileID = reconcileID
	result.DryRun = false

	err = o.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_started",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"reconcile_id": reconcileID,
			},
		}); err != nil {
			return err
		}

		r1, err := Rule1ReleaseExpiredLeases(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		r2, err := Rule2FlipStaleTasks(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		r3, err := Rule3ApplyEvidenceInvalidations(tx, appender, o.clock, reconcileID, candidates)
		if err != nil {
			return err
		}
		r4, err := Rule4OrphanExpiredRuns(tx, appender, o.clock, reconcileID)
		if err != nil {
			return err
		}
		authErrs, err := Rule5CollectAuthoringErrors(tx)
		if err != nil {
			return err
		}

		// Merge stats.
		result.Stats = mergeStats(r1, r2, r3, r4)
		result.Stats.Rule3Sampled = sampled
		result.Stats.Rule3OfTotal = total
		if opts.EvidenceSampleFull {
			result.Stats.Rule3Mode = "full"
		} else {
			result.Stats.Rule3Mode = "sample"
		}
		result.Stats.Rule5AuthoringErrors = len(authErrs)
		result.AuthoringErrors = authErrs

		return appender.Append(tx, events.Record{
			Kind:       "reconcile_ended",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"reconcile_id":     reconcileID,
				"stats":            result.Stats,
				"authoring_errors": authErrs,
			},
		})
	})
	if err != nil {
		return Result{}, err
	}
	if result.AuthoringErrors == nil {
		result.AuthoringErrors = []AuthoringError{}
	}
	return result, nil
}

func mergeStats(rs ...RuleResult) Stats {
	var out Stats
	for _, r := range rs {
		s := r.Stats
		out.Rule1ClaimsReleased += s.Rule1ClaimsReleased
		out.Rule1TasksReverted += s.Rule1TasksReverted
		out.Rule2TasksFlippedStale += s.Rule2TasksFlippedStale
		if s.Rule2LatencyMs > out.Rule2LatencyMs {
			out.Rule2LatencyMs = s.Rule2LatencyMs
		}
		out.Rule3EvidenceInvalid += s.Rule3EvidenceInvalid
		out.Rule4RunsOrphaned += s.Rule4RunsOrphaned
	}
	return out
}

func extractAuthoringFromDry(dr DryRunResult) []AuthoringError {
	for _, r := range dr.Rules {
		if r.Rule == 5 {
			return r.AuthoringErrors
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: first two `TestOrchestrator_*` PASS. `TestOrchestrator_Idempotent` may fail until dry-run is skipped too — confirm path.

Note: `TestOrchestrator_Idempotent` does NOT use dry-run, so it should pass after Task 15.1 alone. The dry-run simulator lives in Task 15.2.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/reconcile.go internal/reconcile/reconcile_orch_test.go
git commit -m "feat(reconcile): orchestrator runs probe + 5 rules in hybrid tx"
```

---

### Task 15.2: `dryrun.go` — pure-read simulator

**Files:**
- Create: `internal/reconcile/dryrun.go`
- Modify: `internal/reconcile/reconcile.go` (expose via `dryRun` method)

- [ ] **Step 1: Write failing test**

Create `internal/reconcile/dryrun_test.go`:

```go
package reconcile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestDryRun_EmitsZeroEventsAndNoWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()
	clk := clock.NewFake(10_000_000)
	blobRoot := t.TempDir()

	// Seed one expired claim so rule 1 would mutate.
	seedLeaseFixture(t, h, 5000)

	// Seed one evidence row with a deleted blob so rule 3 would mutate.
	shas := seedEvidence(t, h, blobRoot, 1)
	var uri string
	_ = h.SQL().QueryRow(`SELECT uri FROM evidence WHERE sha256=?`, shas[0]).Scan(&uri)
	_ = os.Remove(uri)

	var eventsBefore int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsBefore)
	var taskStatusBefore string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&taskStatusBefore)

	orch := reconcile.NewOrchestrator(h, clk, ids.NewGenerator(clk), blobRoot)
	dr, err := orch.DryRun(context.Background(), reconcile.Opts{EvidenceSampleFull: true})
	if err != nil {
		t.Fatal(err)
	}

	// Zero writes: events count unchanged, task status unchanged.
	var eventsAfter int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventsAfter)
	if eventsAfter != eventsBefore {
		t.Errorf("events changed %d → %d in dry-run", eventsBefore, eventsAfter)
	}
	var statusAfter string
	_ = h.SQL().QueryRow(`SELECT status FROM tasks WHERE id='T-1'`).Scan(&statusAfter)
	if statusAfter != taskStatusBefore {
		t.Errorf("task status changed %q → %q in dry-run", taskStatusBefore, statusAfter)
	}

	// DryRunResult contains the would-be mutations.
	totalWouldMutate := 0
	for _, r := range dr.Rules {
		totalWouldMutate += len(r.Mutations)
	}
	if totalWouldMutate < 2 {
		t.Errorf("expected >=2 would-mutate entries (rule 1 release + revert + rule 3 invalidate); got %d",
			totalWouldMutate)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
go test ./internal/reconcile/... -run TestDryRun -v
```
Expected: FAIL — `orch.DryRun` not public or not implemented.

- [ ] **Step 3: Implement dry-run simulator**

Create `internal/reconcile/dryrun.go`:

```go
package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// DryRun simulates a real Run without writing state or emitting events.
// Returns a DryRunResult with per-rule would-be mutations for rules 1..4
// and authoring findings for rule 5.
func (o *Orchestrator) DryRun(ctx context.Context, opts Opts) (DryRunResult, error) {
	return o.dryRun(ctx, opts)
}

func (o *Orchestrator) dryRun(ctx context.Context, opts Opts) (DryRunResult, error) {
	result := DryRunResult{DryRun: true}
	now := o.clock.NowMilli()

	// Rule 1: candidate claims/tasks.
	rel, rev, err := dryRule1(ctx, o.db, now)
	if err != nil {
		return result, err
	}
	var m1 []Mutation
	for _, id := range rel {
		m1 = append(m1, Mutation{Rule: 1, EntityID: id, Action: "release", Reason: "expired"})
	}
	for _, id := range rev {
		m1 = append(m1, Mutation{Rule: 1, EntityID: id, Action: "revert_to_open", Reason: "lease_expired"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 1, Mutations: m1})

	// Rule 2: would-flip tasks.
	flips, err := dryRule2(ctx, o.db)
	if err != nil {
		return result, err
	}
	var m2 []Mutation
	for _, id := range flips {
		m2 = append(m2, Mutation{Rule: 2, EntityID: id, Action: "flip_stale", Reason: "spec_drift"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 2, Mutations: m2})

	// Rule 3: probe, then re-stat each candidate (same defense as real run).
	probeOpts := ProbeOpts{Full: opts.EvidenceSampleFull, SamplePct: opts.SamplePct, SampleCap: opts.SampleCap}
	candidates, err := RunEvidenceProbe(ctx, o.db, probeOpts)
	if err != nil {
		return result, err
	}
	var m3 []Mutation
	for _, c := range candidates {
		if reason, stillInvalid := reStatInvalid(c.URI, c.Sha256); stillInvalid {
			m3 = append(m3, Mutation{Rule: 3, EntityID: c.EvidenceID, Action: "invalidate", Reason: reason})
		}
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 3, Mutations: m3})

	// Rule 4: would-orphan runs.
	orphans, err := dryRule4(ctx, o.db, now)
	if err != nil {
		return result, err
	}
	var m4 []Mutation
	for _, id := range orphans {
		m4 = append(m4, Mutation{Rule: 4, EntityID: id, Action: "orphan", Reason: "grace_expired"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 4, Mutations: m4})

	// Rule 5: authoring errors via read-only query (no tx required).
	authErrs, err := dryRule5(ctx, o.db)
	if err != nil {
		return result, err
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 5, AuthoringErrors: authErrs})

	return result, nil
}

func dryRule1(ctx context.Context, h *db.DB, now int64) ([]string, []string, error) {
	var released []string
	rows, err := h.SQL().QueryContext(ctx,
		`SELECT id FROM claims WHERE expires_at < ? AND released_at IS NULL`, now)
	if err != nil {
		return nil, nil, fmt.Errorf("dry rule 1 claims: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		released = append(released, id)
	}
	rows.Close()

	var reverted []string
	rows, err = h.SQL().QueryContext(ctx, `
		SELECT id FROM tasks
		 WHERE status IN ('claimed','in_progress','gate_pending')
		   AND id NOT IN (
		     SELECT task_id FROM claims
		      WHERE released_at IS NULL AND expires_at >= ?)`, now)
	if err != nil {
		return nil, nil, fmt.Errorf("dry rule 1 tasks: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		reverted = append(reverted, id)
	}
	rows.Close()
	return released, reverted, nil
}

func dryRule2(ctx context.Context, h *db.DB) ([]string, error) {
	// Pull done tasks + their required gates, then per-gate check whether
	// the latest verdict is a fresh pass (same logic as verdict.IsFreshPass,
	// but we can't use verdict.Store without a tx; so we inline the query).
	rows, err := h.SQL().QueryContext(ctx,
		`SELECT id, required_gates_json FROM tasks WHERE status='done'`)
	if err != nil {
		return nil, err
	}
	type task struct {
		ID      string
		GateIDs []string
	}
	var tasks []task
	for rows.Next() {
		var t task
		var gatesJSON string
		if err := rows.Scan(&t.ID, &gatesJSON); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(gatesJSON), &t.GateIDs); err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	rows.Close()

	var flips []string
	for _, t := range tasks {
		stale := false
		for _, g := range t.GateIDs {
			var curGateHash string
			if err := h.SQL().QueryRowContext(ctx,
				`SELECT gate_def_hash FROM gates WHERE id=?`, g,
			).Scan(&curGateHash); err != nil {
				stale = true
				break
			}
			var vGateHash, vStatus string
			err := h.SQL().QueryRowContext(ctx,
				`SELECT gate_def_hash, status FROM verdicts
				 WHERE gate_id=?
				 ORDER BY bound_at DESC, sequence DESC LIMIT 1`, g,
			).Scan(&vGateHash, &vStatus)
			if err != nil || vGateHash != curGateHash || vStatus != "pass" {
				stale = true
				break
			}
		}
		if stale {
			flips = append(flips, t.ID)
		}
	}
	return flips, nil
}

func dryRule4(ctx context.Context, h *db.DB, now int64) ([]string, error) {
	rows, err := h.SQL().QueryContext(ctx, `
		SELECT runs.id
		  FROM runs
		  JOIN claims ON claims.id = runs.claim_id
		 WHERE runs.ended_at IS NULL
		   AND claims.released_at IS NOT NULL
		   AND claims.released_at + ? < ?`, orphanGraceMs, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func dryRule5(ctx context.Context, h *db.DB) ([]AuthoringError, error) {
	rows, err := h.SQL().QueryContext(ctx, `
		SELECT tasks.id, j.value
		  FROM tasks, json_each(tasks.required_gates_json) j
		  LEFT JOIN gates ON gates.id = j.value
		 WHERE gates.id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthoringError
	for rows.Next() {
		var e AuthoringError
		if err := rows.Scan(&e.TaskID, &e.MissingGateID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// keep strings imported via dependency.
var _ = strings.Contains
```

- [ ] **Step 4: Run tests to verify pass**

Run:
```bash
go test ./internal/reconcile/... -v
```
Expected: all PASS, including `TestDryRun_EmitsZeroEventsAndNoWrites`.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/dryrun.go internal/reconcile/dryrun_test.go
git commit -m "feat(reconcile): dry-run pure-read simulator with re-stat parity"
```

---

## Phase 16: Reconcile CLI

### Task 16.1: `cli/reconcile.go` + `cmd/cairn/reconcile.go`

**Files:**
- Create: `internal/cli/reconcile.go`
- Create: `cmd/cairn/reconcile.go`
- Modify: `cmd/cairn/main.go` (register the command)

- [ ] **Step 1: Write handler**

Create `internal/cli/reconcile.go`:

```go
package cli

import (
	"context"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

// ReconcileInput mirrors the CLI flags.
type ReconcileInput struct {
	DryRun             bool
	EvidenceSampleFull bool
}

// Reconcile runs `cairn reconcile`. Dispatches to Orchestrator.Run or DryRun
// depending on the input.
func Reconcile(ctx context.Context, h *db.DB, clk clock.Clock, gen *ids.Generator, blobRoot string, in ReconcileInput) (any, error) {
	orch := reconcile.NewOrchestrator(h, clk, gen, blobRoot)
	opts := reconcile.Opts{
		DryRun:             in.DryRun,
		EvidenceSampleFull: in.EvidenceSampleFull,
	}
	if in.DryRun {
		return orch.DryRun(ctx, opts)
	}
	return orch.Run(ctx, opts)
}
```

The return is `any` because the two result shapes differ (`Result` vs `DryRunResult`). The envelope layer already handles arbitrary values.

- [ ] **Step 2: Write cobra wiring**

Create `cmd/cairn/reconcile.go`:

```go
package main

import (
	"github.com/spf13/cobra"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

func newReconcileCmd(app *appCtx) *cobra.Command {
	var in cli.ReconcileInput
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Sweep state: release expired leases, flip stale tasks, invalidate missing evidence, orphan stuck runs, report authoring errors.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := cli.Reconcile(cmd.Context(), app.db, app.clock, app.ids, app.blobRoot, in)
			return app.emit(r, err)
		},
	}
	cmd.Flags().BoolVar(&in.DryRun, "dry-run", false, "preview mutations without writing state or emitting events")
	cmd.Flags().BoolVar(&in.EvidenceSampleFull, "evidence-sample-full", false, "scan every evidence row (default: 5%% sampled, capped at 100)")
	return cmd
}
```

Register in `cmd/cairn/main.go` by adding `rootCmd.AddCommand(newReconcileCmd(app))`.

- [ ] **Step 3: Smoke-test**

```bash
go build ./...
go test ./... -v
```

Hand-exercise:

```bash
./bin/cairn reconcile
./bin/cairn reconcile --dry-run
```
Expected: JSON envelope responses with the shapes from §5.8/§5.9.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/reconcile.go cmd/cairn/reconcile.go cmd/cairn/main.go
git commit -m "feat(cli): reconcile subcommand + --dry-run + --evidence-sample-full"
```

---

## Phase 17: Integration tests

### Task 17.1: Memory end-to-end

**Files:**
- Create: `internal/integration/memory_e2e_test.go`

- [ ] **Step 1: Write test**

Follow Ship 1's subprocess harness pattern from `internal/integration/e2e_helpers_test.go` (built binary at test setup, invoked via `exec.Command`, JSON response parsed). Cover:

1. `cairn memory append --kind decision --body "x" --tags foo,bar` → parse response, assert `memory_id` non-empty, `at > 0`, `tags == ["foo","bar"]`.
2. Append 15 more entries across kinds. `cairn memory list` defaults: `returned == 10`, `total_matching == 16`.
3. `cairn memory list --limit 0` → `returned == 16`.
4. `cairn memory list --kind outcome` → count matches the seeded outcome rows.
5. `cairn memory search "x" --limit 3` → `returned == 3`, each hit has `relevance > 0`.
6. `cairn memory search "AND AND"` → exit 1 with error `kind=invalid_fts_query`; envelope must not contain `sqlite`, `fts5:`, `near "`.
7. Op-ID replay: two `memory append` with identical `--op-id` yield identical `memory_id`; only one row in the DB.
8. Entity XOR: `memory append --kind decision --body x --entity-kind task` (no id) → exit 1, kind `entity_kind_id_mismatch`.

Base the file on `internal/integration/dogfood_test.go` for structure. Use the same helpers.

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestMemoryE2E -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/memory_e2e_test.go
git commit -m "test(integration): memory e2e — append → list → search + filters + errors"
```

---

### Task 17.2: Reconcile end-to-end (PLAN Ship 2 dogfood steps 2 + 5)

**Files:**
- Create: `internal/integration/reconcile_e2e_test.go`

- [ ] **Step 1: Write test covering PLAN.md §"Ship 2 dogfood"**

Reference: `docs/PLAN.md` §"Ship 2 dogfood" has six numbered steps. This test implements them as a single subprocess flow.

Scenarios to cover:

1. **Rule 2 drift flip.** Seed REQ-001 + TASK-001 + AC-001 in a temp repo. Run full Ship 1 cycle through `cairn task complete`. Edit the spec so `gate_def_hash` would change. `cairn task plan` again (this re-materializes gate). `cairn reconcile` → response's `stats.rule_2_tasks_flipped_stale == 1`. Assert event sequence.

2. **Memory append + FTS search.** `cairn memory append --kind decision --body "chose to hash evidence before binding"`. `cairn memory search "evidence"` → `total_matching == 1`. Assert `memory_appended` event present.

3. **Rule 1 expired lease.** `cairn task claim --ttl 1ms`, wait 50ms, `cairn reconcile` → `stats.rule_1_claims_released == 1`, task reverted to `open`.

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestReconcileE2E -v -timeout 60s
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/reconcile_e2e_test.go
git commit -m "test(integration): Ship 2 dogfood — rule 2 drift + memory + rule 1 expiry"
```

---

### Task 17.3: Evidence invalidation end-to-end

**Files:**
- Create: `internal/integration/evidence_invalidation_e2e_test.go`

- [ ] **Step 1: Write test**

Flow:
1. `cairn evidence put <file>` in subprocess → SHA recorded.
2. Delete the blob file on disk directly.
3. `cairn reconcile --evidence-sample-full`.
4. Assert: exit 0, `stats.rule_3_evidence_invalidated == 1`.
5. `cairn evidence verify <sha>` → exit 1, kind `evidence_invalidated`.
6. `cairn verdict latest <gate>` (after binding a pre-invalidation verdict to this evidence) → response includes `evidence_invalidated: true`.
7. Try `cairn verdict report` against the invalidated sha → exit 1, kind `evidence_invalidated`.
8. `cairn task complete` still succeeds if the previously-bound fresh verdict was recorded before invalidation (§5.10 row 3 — complete does NOT consider invalidation).

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestEvidenceInvalidationE2E -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/evidence_invalidation_e2e_test.go
git commit -m "test(integration): evidence invalidation covers three surfaces from §5.10"
```

---

### Task 17.4: Concurrent reconcile (5 goroutines + 2 subprocesses)

**Files:**
- Create: `internal/integration/reconcile_concurrent_test.go`

- [ ] **Step 1: Write test**

Pattern mirrors Ship 1's `TestConcurrentClaim` — mix in-process goroutines and subprocess invocations against the same DB.

Structure:
1. Seed fixtures: one expired claim (rule 1 has work), one done task with drifted gate (rule 2 has work).
2. Launch 5 goroutines, each calling `Orchestrator.Run` against the shared `*db.DB`.
3. Launch 2 subprocesses (`exec.Command("./bin/cairn", "reconcile")`).
4. Wait for all 7 to complete; collect exit codes / errors.
5. Assertions:
   - Every invocation exits successfully (no BUSY leak).
   - `SELECT COUNT(*) FROM events WHERE kind='reconcile_started'` == 7.
   - `SELECT COUNT(*) FROM events WHERE kind='reconcile_ended'` == 7.
   - Exactly one `reconcile_rule_applied` chain (the first winner did the work); the other six see zero mutations.
   - All 7 `reconcile_id` values are distinct.

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestConcurrentReconcile -v -timeout 120s
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/reconcile_concurrent_test.go
git commit -m "test(integration): concurrent reconcile — 5 goroutines + 2 subprocesses"
```

---

### Task 17.5: Dry-run parity (snapshot/restore)

**Files:**
- Create: `internal/integration/reconcile_dryrun_parity_test.go`

- [ ] **Step 1: Write test matching design spec §8.4 protocol**

Structure:
1. Seed fixtures that exercise rules 1, 2, 3, 4, 5 (one concrete trigger per rule).
2. Snapshot: copy `state.db` (single file) + walk `blobRoot` and copy each blob to a temp dir. Record `hash := sha256(state.db bytes)`.
3. Run `cairn reconcile --dry-run --evidence-sample-full`. Parse the `rules[*].would_mutate` arrays into a set of `{rule, entity_id, action, reason}` tuples.
4. Assert event count before == event count after the dry-run (no writes).
5. Assert `hash(state.db)` unchanged.
6. Restore snapshot: overwrite `state.db` and blob files from snapshot.
7. Run real `cairn reconcile --evidence-sample-full`. Capture emitted events into a set of tuples.
8. Map event → tuple:
   - `claim_released` → `{1, claim_id, "release", "expired"}` (if reason=expired)
   - `task_status_changed (to='open', reason='lease_expired')` → `{1, task_id, "revert_to_open", "lease_expired"}`
   - `task_status_changed (to='stale', reason='spec_drift')` → `{2, task_id, "flip_stale", "spec_drift"}`
   - `evidence_invalidated` → `{3, evidence_id, "invalidate", reason}`
   - `run_ended (outcome='orphaned')` → `{4, run_id, "orphan", "grace_expired"}`
9. Assert: `drySet == realSet` (set equality via `testify/require.ElementsMatch`).
10. Assert: rule-5 authoring errors match between dry-run's `rules[4].authoring_errors` and real-run's `stats.rule_5_authoring_errors`/`authoring_errors`.

Clock pinning: both runs use the same fake/fixed clock value — the in-process path uses `clock.NewFake`, subprocess reads from `CAIRN_CLOCK_MS` env var (if Ship 1 exposed one; if not, the test runs in-process only for the sake of clock determinism).

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestDryRunParity -v -timeout 60s
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/reconcile_dryrun_parity_test.go
git commit -m "test(integration): dry-run parity via snapshot/restore"
```

---

### Task 17.6: Task-complete ignores evidence invalidation

**Files:**
- Create: `internal/integration/task_complete_ignores_invalidation_test.go`

- [ ] **Step 1: Write test**

Flow:
1. Full Ship 1 cycle: init → plan → claim → evidence put → verdict report (pass, fresh) → task status becomes `claimed` with pending gate verdicts.
2. Mark evidence row invalidated directly via SQL (simulating a prior reconcile), since verdict was already bound pre-invalidation.
3. `cairn task complete <claim_id>` → exit 0, task transitions to `done`.
4. Assert: task status is `done` despite the bound evidence having `invalidated_at` set.

- [ ] **Step 2: Run**

```bash
go test ./internal/integration/... -run TestTaskCompleteIgnoresInvalidation -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/task_complete_ignores_invalidation_test.go
git commit -m "test(integration): task complete ignores evidence_invalidated (§5.10 row 3)"
```

---

## Phase 18: CI + event-log completeness extension

### Task 18.1: Extend event-log completeness assertion

**Files:**
- Modify: `internal/integration/dogfood_test.go` (or wherever Ship 1's completeness assertion lives)
- Modify: `.github/workflows/ci.yml` (if the completeness assertion runs via a CI step rather than a Go test)

- [ ] **Step 1: Locate the Ship 1 completeness assertion**

Run:
```bash
grep -nE 'sort -u|events since 0|completenessKinds' -r internal/integration .github/workflows
```

Identify the test function or CI step that defines the expected set of event kinds. It likely lives in `internal/integration/dogfood_test.go`.

- [ ] **Step 2: Add Ship 2 kinds to the expected set**

Find the slice or map that lists expected kinds. Append:

```go
// Ship 2 additions — new mutations.
"reconcile_started",
"reconcile_ended",
"reconcile_rule_applied",
"evidence_invalidated",
"memory_appended",
```

Ship 1 kinds already present (do not re-add): `claim_released`, `task_status_changed`, `run_ended`, `task_planned`, `spec_materialized`, `claim_acquired`, `run_started`, `evidence_stored`, `verdict_bound`.

- [ ] **Step 3: Update the dogfood scenario that drives the assertion**

The Ship 1 dogfood flow doesn't exercise reconcile or memory. Extend the Ship 2 dogfood integration flow (from Task 17.2) to:
1. After the Ship 1 cycle, do `cairn memory append ...` → triggers `memory_appended`.
2. Run `cairn reconcile` at least once → triggers `reconcile_started` + `reconcile_ended`.
3. Trigger at least one of rule 1 / rule 3 to emit `reconcile_rule_applied` + `evidence_invalidated`.

Update the dogfood test so `cairn events since 0 | jq -r '.kind' | sort -u` covers all Ship 1 + Ship 2 kinds.

- [ ] **Step 4: Run the full integration suite**

```bash
go test ./internal/integration/... -v -timeout 180s
```
Expected: all PASS, including the completeness assertion now covering 14 kinds.

- [ ] **Step 5: Commit**

```bash
git add internal/integration/dogfood_test.go internal/integration/*e2e*.go .github/workflows/ci.yml
git commit -m "ci: event-log completeness covers Ship 2 kinds (reconcile_* + evidence_invalidated + memory_appended)"
```

---

### Task 18.2: Final matrix + offline CI smoke

**Files:**
- Modify (if needed): `.github/workflows/ci.yml`

- [ ] **Step 1: Verify matrix runs Ship 2 tests**

The existing matrix runs `go test ./...` across Linux/Windows/macOS × Go 1.25.x. Ship 2 adds no new network deps, so offline CI is unchanged. Run locally:

```bash
go test ./... -v -race
```
Expected: all PASS.

- [ ] **Step 2: Verify offline job**

Locally, approximate offline by unsetting `GOPROXY`:

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./... 2>&1 | head -30  # or whatever Ship 1 offline uses
```

Ship 1's offline CI already uses iptables severance; Ship 2 inherits the job without change.

- [ ] **Step 3: Push branch, observe CI**

```bash
git push -u origin feature/ship-2-reconcile-memory
```

(User confirms push before execution; this is a shared-state action.)

Watch GitHub Actions run. All matrix jobs should be green. Offline may run on push-to-master only (per Ship 1's workflow); in that case, expect it to be gated off PRs.

- [ ] **Step 4: Commit any workflow tweaks + open PR**

If ci.yml needed any adjustments, commit them:

```bash
git add .github/workflows/ci.yml
git commit -m "ci: any Ship 2 workflow adjustments"
git push
```

Open a PR from `feature/ship-2-reconcile-memory` → `master` via `gh pr create`. Use the PR body template — summarize the five event kinds added, the two PLAN.md amendments (already landed), and link the design spec.

---

## Self-Review (writing-plans skill)

**1. Spec coverage.** Every spec section maps to at least one task:
- §1 Scope → implicit in phase decomposition.
- §2 Decision log → embedded in Conventions + per-rule implementation.
- §3 Migration 002 → Task 0.1 + 0.2.
- §4 Memory CLI (append/search/list) → Tasks 3.1 (Append), 4.1 (List), 5.1 (Search), 6.1 (CLI wiring).
- §4.6 FTS error translation → Task 2.1 + integration Task 17.1 step 6.
- §5 Reconcile → Tasks 8.1–16.1.
- §5.5 re-stat invariant → Task 12.1 (`reStatInvalid` + `TestRule3_ReStatSkipsRecoveredBlob`).
- §5.10 three-surface table → Task 7.1 (Verify), 7.2 (Latest/History), 17.6 (task complete ignores).
- §6 Event-log completeness → Task 18.1.
- §7 Package layout → File Structure section at top.
- §8 Testing — unit (in every phase), integration (Phase 17), CI (Phase 18). §8.3 concurrent → 17.4. §8.4 dry-run parity → 17.5. §8.5 FTS error translation → 17.1 step 6.
- §9 Done-when → covered by integration tests in Phase 17 + CI in Phase 18.
- §10 Lessons carry-forward → Conventions section calls out both Ship 1 lessons.
- §11 PLAN.md amendments → **done in prep PR** (merged to master at commit `6813a7f` before implementation).
- §12 Deferred items → Rule 2 Ship 4 flag captured in Task 11.1.

**2. Placeholder scan.** No "TBD", "TODO", "fill in", or "similar to previous task". Every step has either concrete code, a concrete command, or a concrete assertion list. Task 17.1–17.6 deliberately reference Ship 1's subprocess harness pattern rather than duplicating 500 lines of harness boilerplate; the reference is to a specific file (`internal/integration/e2e_helpers_test.go`), which counts as concrete.

**3. Type consistency.**
- `memory.AppendInput`, `AppendResult`, `ListInput`, `ListResult`, `SearchInput`, `SearchResult`, `SearchHit`, `Entry` — defined once in `store.go`, consumed uniformly by CLI handlers and tests.
- `reconcile.Mutation`, `Stats`, `RuleResult`, `Result`, `DryRunResult`, `DryRunRule`, `AuthoringError`, `EvidenceCandidate`, `ProbeOpts`, `Opts`, `Orchestrator` — defined once in `types.go` / `probe.go` / `reconcile.go`, consumed uniformly.
- `Orchestrator.Run` returns `Result` (real run) and `Orchestrator.DryRun` returns `DryRunResult` (dry-run). CLI's `Reconcile` returns `any` because the two shapes differ — envelope layer handles.
- Rule signatures uniform: `RuleN...(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string, ...) (RuleResult, error)` — rule 3 additionally takes the `candidates []EvidenceCandidate`; rule 5 returns `[]AuthoringError` instead (it's read-only, not a RuleResult mutation).
- `Rule5CollectAuthoringErrors` returns `[]AuthoringError` directly (no RuleResult) because it produces no mutations and no events.

**4. Spec-requirement traceability**
- §5.5 race-note "`evidence.invalidated_at IS NULL` guard in WHERE" → Task 12.1 rule 3 SQL.
- §5.5 re-stat invariant "both file presence AND hash match" → `reStatInvalid` → `checkBlob`.
- §5.6 rule 4 "must run after rule 1" → Orchestrator in Task 15.1 orders rules 1→5; test `TestOrchestrator_Rule4DependsOnRule1Ordering` enforces.
- §5.9 dry-run shape includes no `reconcile_id` → Task 9.1 `DryRunResult` struct omits it.
- §3 migration order (Part A memory, Part B evidence) → Task 0.1 SQL matches.

All checks pass.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-18-ship-2-reconcile-memory.md`. Two execution options:**

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
