# Ship 2 — Reconcile + Memory (design)

> Status: Draft for user review.
> Date: 2026-04-18.
> Scope: PLAN.md §"Ship 2 — Reconcile, memory" (week 2).
> Supersedes: prior drafts of Ship 2 scope. Locks Q1–Q10 from the Ship 2
> brainstorm session. Amends two items in PLAN.md explicitly listed in §11.

## 1. Scope + non-goals

**Ship 2 adds:**

- `internal/memory/` — append + FTS5 search + list.
- `internal/reconcile/` — five rules, hybrid-tx, idempotent.
- CLI: `cairn memory append|search|list`, `cairn reconcile`.
- Migration `002_ship2.sql`:
  - memory tables + FTS5 + append-only triggers;
  - `evidence.invalidated_at` column;
  - **evidence append-only triggers** (closes a Ship 1 gap — §5.5).
- Extend event-log completeness CI assertion to cover new event kinds.

**Out of Ship 2 (deferred; tracked here for audit):**

- `inputs_hash` comparison semantics (Q1 from Ship 1 handoff — Ship 3+ decision).
- `cairn replay --as-of` command (PLAN.md §"Explicitly deferred").
- Ship 3 target selection (picked at end of Ship 2 per PLAN.md §"Ship 2 —
  Reconcile, memory"; captured as `specs/requirements/REQ-002.yaml` +
  task YAMLs after dogfood reveals real pain).
- Superpowers skill integration (Ship 3 only).
- Evidence audit as separate command; `--evidence-sample-full` flag on
  `cairn reconcile` covers the full-scan use case (Q7).

Ship 2 is **code-only** within cairn. No changes to Superpowers, no new
hooks, no session-start wiring.

## 2. Decision log (Q1–Q10)

| ID  | Decision                                                                                             |
| --- | ---------------------------------------------------------------------------------------------------- |
| Q2  | Memory `kind` enum frozen at `decision|rationale|outcome|failure` (4 values).                        |
| Q3  | `entity_kind` / `entity_id` nullable TEXT, no FK. CLI validates `entity_kind` enum. CHECK enforces XOR (both NULL or both NOT NULL). |
| Q4  | `cairn memory list` default: newest-first, limit 10. `--limit 0` = unlimited (documented in `--help`). |
| Q4' | Response shape overridden to envelope object `{entries, total_matching, returned}` — see §4.3.       |
| Q5  | `--since` accepts integer-ms only (symmetric with Ship 1 `events since`).                            |
| Q6  | Reconcile rule 4 (orphan sweep) derived from `claims.released_at + 10min < now`. No external knob.   |
| Q7  | Rule 3 defaults 5% sample / cap 100. `--evidence-sample-full` flag for audits. Adds migration column `evidence.invalidated_at`. Invalidation is a separate signal, not a staleness flip. |
| Q8  | Hybrid transaction: probe phase outside tx (filesystem I/O); mutation phase inside one `BEGIN IMMEDIATE`. |
| Q9  | `--dry-run` is a pure read: zero writes, zero events, no `reconcile_started`/`reconcile_ended` emitted. Amends PLAN.md event-kind table. |
| Q10 | `cairn reconcile` accepts no `--op-id`. SQLite `BEGIN IMMEDIATE` serializes concurrent runs. Replay-by-op_log adds no value for an idempotent sweep. |

## 3. Migration 002

```sql
-- 002_ship2.sql
-- Ship 2 adds: memory tables + FTS5 index + evidence invalidation column.

-- Part A: memory
CREATE TABLE memory_entries (
    id          TEXT PRIMARY KEY,          -- ULID
    at          INTEGER NOT NULL,          -- ms since epoch
    kind        TEXT NOT NULL,             -- decision|rationale|outcome|failure (CLI-enum)
    entity_kind TEXT,                      -- nullable; CLI-enum when present
    entity_id   TEXT,                      -- nullable; free text, no FK
    body        TEXT NOT NULL,
    tags_json   TEXT NOT NULL DEFAULT '[]', -- structured output
    tags_text   TEXT NOT NULL DEFAULT '',   -- space-joined, FTS5-indexed
    CHECK ((entity_kind IS NULL) = (entity_id IS NULL))
);

CREATE INDEX idx_memory_at     ON memory_entries(at DESC);
CREATE INDEX idx_memory_kind   ON memory_entries(kind, at DESC);
CREATE INDEX idx_memory_entity ON memory_entries(entity_kind, entity_id);

CREATE VIRTUAL TABLE memory_fts USING fts5(body, tags);

CREATE TRIGGER memory_fts_ai AFTER INSERT ON memory_entries BEGIN
    INSERT INTO memory_fts(rowid, body, tags) VALUES (new.rowid, new.body, new.tags_text);
END;

CREATE TRIGGER memory_no_delete BEFORE DELETE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

CREATE TRIGGER memory_no_update BEFORE UPDATE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

-- Part B: evidence invalidation signal (rule 3 output; surfaced via verdict queries)
ALTER TABLE evidence ADD COLUMN invalidated_at INTEGER;
-- No index on invalidated_at. No Ship 2 query filters on `IS NOT NULL`.
-- Rule 3's UPDATE hits the PK; verdict JOIN reads the column for projection.
-- Add index if/when a future command (e.g. `cairn evidence list --invalidated`)
-- needs it.

-- Evidence append-only enforcement. Ship 1 omitted these triggers; Ship 2
-- adds them alongside invalidated_at. UPDATE is permitted ONLY for
-- invalidated_at (required by rule 3); all other columns are frozen.
-- DELETE is blocked outright. Matches the discipline applied to
-- memory_entries above.
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

CREATE TRIGGER evidence_no_delete
BEFORE DELETE ON evidence
BEGIN
    SELECT RAISE(ABORT, 'evidence rows cannot be deleted');
END;
```

**ALTER TABLE cost:** SQLite `ADD COLUMN` with a nullable column and no
constant-required default is O(1) metadata-only. Large evidence stores
upgrade instantly.

**FTS5 storage choice:** regular FTS5 (no `content=` clause), meaning
FTS5 stores its own shadow copy of `body` + `tags`. Two alternatives
were considered and rejected:

- **External-content** (`content='memory_entries'`): saves space by
  reading from the base table. Requires FTS5 column names to match
  base-table columns; our `memory_fts(body, tags)` vs.
  `memory_entries(... tags_text)` mismatch would force either a rename
  or dropping external-content. Since the AFTER INSERT trigger already
  populates FTS5 manually, the space saving is the only value — and
  memory bodies are short, so the trade-off doesn't earn its coupling.
- **Contentless** (`content=''`): FTS5 stores neither the content nor
  a pointer to it; `SELECT body FROM memory_fts` returns empty. Breaks
  search-result projection, which needs `body` in the response. Rejected.

**Tag format contract (CLI-enforced):**

- Regex: `^[a-zA-Z0-9_]+$` (ASCII alphanumeric + underscore).
- Max length: 64 chars per tag.
- Max count: 20 tags per entry.
- Invalid tag → `cairnerr.Err{Code: CodeBadInput, Kind: "invalid_tag"}`, exit 1.
- Silent rewrites (e.g. hyphen → underscore) are **not** done; reject
  explicitly so agents learn the contract.

## 4. Memory CLI

All commands output JSON by default. All accept `--format human` where
it meaningfully helps.

### 4.1 `cairn memory append`

```
cairn memory append
    --kind decision|rationale|outcome|failure
    --body <text>
  [ --entity-kind requirement|task|gate|verdict|run|claim|evidence|memory
    --entity-id   <id> ]                  # both-or-neither; CLI + CHECK enforced
  [ --tags t1,t2,... ]                    # regex ^[a-zA-Z0-9_]+$, ≤20, ≤64 chars
  [ --op-id <id> ]
```

Behavior:

- Validates kind enum, entity XOR, tag format.
- Generates ULID for `id`; sets `at = clock.Now().UnixMilli()`.
- Transaction: `INSERT memory_entries` → `INSERT op_log` (if `--op-id`
  supplied or generated) → `INSERT events(kind='memory_appended')` →
  `COMMIT`.
- AFTER INSERT trigger populates `memory_fts` in the same tx.
- Response:

  ```json
  {
    "memory_id": "...",
    "at": 1713...,
    "kind": "decision",
    "entity_kind": "task",
    "entity_id": "TASK-017",
    "tags": ["x","y"],
    "op_id": "..."
  }
  ```

- Replay (same `op_id`): returns cached `result_json` from `op_log`;
  no second insert.

### 4.2 `cairn memory search`

```
cairn memory search <query>
  [ --limit N ]                           # default 10; 0 = unlimited
  [ --kind decision|... ]
  [ --entity-kind EK ]                    # XOR rule same as append
  [ --entity-id   EID ]
  [ --since <ms> ]                        # integer ms since epoch
```

Behavior:

- `query` is an FTS5 MATCH expression, passed through. Caller-supplied.
- SQL:

  ```sql
  SELECT me.*, (-memory_fts.rank) AS relevance
    FROM memory_entries me
    JOIN memory_fts ON memory_fts.rowid = me.rowid
   WHERE memory_fts MATCH :query
     AND (:kind IS NULL OR me.kind = :kind)
     AND (:entity_kind IS NULL OR me.entity_kind = :entity_kind)
     AND (:entity_id   IS NULL OR me.entity_id   = :entity_id)
     AND (:since IS NULL OR me.at >= :since)
   ORDER BY memory_fts.rank ASC        -- ascending because FTS5 rank is negative bm25
   LIMIT :limit;
  ```

- Response envelope:

  ```json
  {
    "results": [
      {"memory_id": "...", "at": 1713..., "kind": "decision",
       "entity_kind": "task", "entity_id": "TASK-017",
       "body": "...", "tags": ["x","y"], "relevance": 1.42}
    ],
    "total_matching": 47,
    "returned": 10
  }
  ```

- `relevance` is `-rank` (float, higher = stronger match). Internally
  ordered by `rank ASC`; API never leaks raw negative rank. `--help`
  documents: "relevance is FTS5 bm25 inverted; higher = stronger match;
  not comparable across different queries."
- `total_matching` = count of rows satisfying the WHERE clause before
  LIMIT; `returned` = rows in `results`. Caller detects clipping by
  `returned < total_matching`.

### 4.3 `cairn memory list`

```
cairn memory list
  [ --entity-kind EK ] [ --entity-id EID ]
  [ --kind K ]
  [ --since <ms> ]
  [ --limit N ]                           # default 10; 0 = unlimited
```

Behavior:

- Zero filters + zero flags: newest 10 entries.
- `ORDER BY at DESC, id DESC`.
- Response envelope:

  ```json
  {
    "entries": [
      {"memory_id": "...", "at": 1713..., "kind": "...", "entity_kind": ..., "entity_id": ..., "body": "...", "tags": [...]}
    ],
    "total_matching": 47,
    "returned": 10
  }
  ```

- `--limit 0` → no LIMIT clause; `total_matching == returned` always.

### 4.4 Exit codes (Ship 1 canonical mapping — reused)

| Code | Error category                     |
| ---- | ---------------------------------- |
| 0    | success                            |
| 1    | `bad_input` / `validation`         |
| 2    | `conflict`                         |
| 3    | `not_found`                        |
| 4    | `substrate`                        |

All memory-layer user errors fall in `bad_input` → exit 1.
Substrate-level failures (DB corruption, FTS index missing, disk I/O)
bubble through `cairnerr.Err{Code: CodeSubstrate}` → exit 4. Not a
memory-specific concern, but the path exists and is tested.

### 4.5 Error kinds

`invalid_kind`, `invalid_entity_kind`, `invalid_tag`,
`entity_kind_id_mismatch`, `invalid_op_id`, `invalid_fts_query`,
`invalid_limit`, `invalid_since`.

### 4.6 FTS5 error translation contract

SQLite FTS5 returns errors like `fts5: syntax error near "AND AND"` for
malformed MATCH queries. These must not leak into the JSON envelope.

- Caller passes query → FTS5 returns `SQLITE_ERROR` with raw text.
- CLI catches, wraps as `cairnerr.Err{Code: CodeBadInput, Kind: "invalid_fts_query", Message: "query syntax invalid", Details: {"position": N}}` if position parseable; otherwise `Message: "query syntax invalid; see FTS5 query syntax docs"`.
- `Cause` is the raw SQLite error (accessible via `errors.Unwrap` for
  trace/debug), but **not serialized to the envelope**. Envelope shows
  only `Kind` + sanitized `Message`.
- Test asserts envelope `error.message` contains no `sqlite`, no `fts5:`,
  no `near "` substrings — only cairn-native wording.

## 5. Reconcile

### 5.1 Command

```
cairn reconcile [ --dry-run ] [ --evidence-sample-full ]
```

No `--op-id` (Q10). Rule 3 sampling defaults to 5% capped at 100.
`--evidence-sample-full` scans every evidence row.

### 5.2 Orchestrator: two phases, one `Run`

```go
func (r *Orchestrator) Run(ctx context.Context, opts Opts) (*Result, error) {
    // =================================================================
    // PROBE PHASE — NO TX. Filesystem I/O only; zero writes, zero events.
    // Collects candidate mutations into an in-memory struct.
    // DO NOT move these reads inside the mutation tx — doing so
    // reintroduces the Q8 lock-contention problem (100-blob sha256
    // under BEGIN IMMEDIATE starves concurrent writers).
    // =================================================================
    probeResult, err := r.runEvidenceProbe(ctx, opts)
    if err != nil { return nil, err }

    // =================================================================
    // MUTATION PHASE — ONE BEGIN IMMEDIATE. All rule writes + events.
    // Rule ordering: 1 → 2 → 3 → 4 → 5.
    //   - Rule 4 depends on rule 1 running first (fresh released_at
    //     is within 10min grace; orphan sweep correctly skips).
    //   - Rule 5 is read-only; emits no events; findings in stats.
    // =================================================================
    return r.applyMutations(ctx, probeResult, opts)
}
```

**Stores constructed inside the mutation tx:**

| Store                | Used by rule(s)     | Reason                                                |
| -------------------- | ------------------- | ----------------------------------------------------- |
| `task.Store`         | 1, 2, 4             | task status flips, claim releases                     |
| `verdict.Store`      | 2                   | staleness check (`Latest` per gate)                   |
| `evidence.Store`     | 3                   | invalidation marking                                  |
| `intent.Store`       | 5                   | gate lookup for authoring-error scan                  |
| `events.Appender`    | all mutating rules  | event emission in-tx                                  |

No `memory.Store` in reconcile — memory append is a separate CLI path.

### 5.3 Rule 1 — expired leases

Pseudocode (real impl uses `QueryContext` for the RETURNING statement
and `ExecContext` for the second; both share the same `*db.Tx`):

```go
// BEGIN IMMEDIATE holds SQLite's WRITE lock from start-of-tx; no
// concurrent writer can interleave between the two statements below.
// The NOT IN subquery is race-free under this serialization.
rows, _ := tx.QueryContext(ctx, `
    UPDATE claims SET released_at = ?
     WHERE expires_at < ? AND released_at IS NULL
    RETURNING id, task_id`,
    now, now)
// iterate rows → collect releasedClaims

_, _ = tx.ExecContext(ctx, `
    UPDATE tasks SET status = 'open', updated_at = ?
     WHERE status IN ('claimed','in_progress','gate_pending')
       AND id NOT IN (
         SELECT task_id FROM claims
          WHERE released_at IS NULL AND expires_at >= ?)`,
    now, now)
```

For each released claim → `claim_released(reason=expired)`.
For each reverted task → `task_status_changed(from, to='open', reason='lease_expired')`.
If any mutations → `reconcile_rule_applied(rule=1, affected_entity_ids=[...])`.

### 5.4 Rule 2 — spec-drift staleness

Implementation: Go loop over `tasks` where `status='done'`. Per task,
per gate in `required_gates_json`, call `verdict.Store.Latest(gate_id)`
and check Ship 1's binary-staleness formula (`gate_def_hash match +
status=pass`).

**Correlated-SQL alternative (triple-nested NOT EXISTS with
latest-verdict precedence) was considered and rejected** for Ship 2:

- Reuses Ship 1's tested `verdict.Store.Latest` = free correctness.
- Go loop is review-friendly; triple-nested correlated subquery is
  write-once/read-painful.
- Ship 2 dogfood scale (tens of tasks) is event-write-bound, not
  rule-2-bound.

**Telemetry:** emit `rule_2_latency_ms` in `reconcile_ended` stats.

**Ship 4 review flag:** if dogfood shows rule-2 latency >100ms on real
repos, port to correlated SQL using `idx_verdicts_latest`.

Per stale task → `task_status_changed(from='done', to='stale', reason='spec_drift')`.
If any mutations → `reconcile_rule_applied(rule=2, ...)`.

### 5.5 Rule 3 — evidence invalidation

Probe phase (outside tx):

- Sample evidence rows. Default: `ORDER BY RANDOM() LIMIT min(100, ceil(count * 0.05))`.
  Full mode: `--evidence-sample-full` → all rows.
- Per row: `os.Stat(uri)` + stream-hash, compare to `sha256`.
- Collect `{evidence_id, reason: missing|hash_mismatch}` into candidates.

Mutation phase (inside tx):

```sql
UPDATE evidence SET invalidated_at = :now
 WHERE id = :evidence_id AND invalidated_at IS NULL;
```

Per candidate → `evidence_invalidated(evidence_id, reason)`.
If any mutations → `reconcile_rule_applied(rule=3, ...)`.

**Race note.** Between probe and mutation phase, two interleavings
matter:

1. Concurrent `cairn evidence put` with the **same** sha256 as a
   probed-missing row. The existing blob row stays (UNIQUE(sha256)
   guarantees no duplicate); the concurrent put may materialize the
   file at `<blob-path>/<sha[:2]>/<sha>`. In that case the probe's
   "missing" conclusion is stale when we mutate.
2. Concurrent `cairn evidence put` with a **different** sha256.
   Irrelevant — probe sampled a specific set of ids.

**Mitigation for (1): re-stat inside the mutation tx.** Ship 2
implements the re-stat defense. Fail-open was considered and
**rejected**: spurious `evidence_invalidated` events pollute the
event log, which is the source of truth per Invariant 10 — emitting
"invalidated" when the file is present and hashes cleanly is a lie
in the log, and downstream callers (skills, reviewer agents) may
react to it.

**Re-stat invariant:** per candidate, inside the mutation tx, **both**
checks must still hold before issuing `UPDATE evidence SET invalidated_at = now`:

1. `os.Stat(uri)` succeeds (file present), AND
2. Streaming `sha256(uri)` equals `evidence.sha256`.

If re-stat shows file present AND hash matches → **skip** this
candidate (probe was stale; blob reappeared cleanly). If file
missing OR hash mismatches → proceed with invalidation, carrying
the re-stat's reason (`missing` or `hash_mismatch`) into the
`evidence_invalidated` event.

**Cost bound:** re-stat is O(candidates), not O(total evidence).
Default cap 100; `--evidence-sample-full` raises it to O(all). At
100 candidates, re-stat adds ≤100 `os.Stat` + stream-hash calls
inside the tx — milliseconds to low-hundreds-of-ms. The probe's
role of doing the bulk I/O outside the tx is preserved; re-stat is
a narrow last-moment verification.

**Evidence append-only enforcement (migration 002 part B, schema-level).**
Unlike memory_entries and verdicts, Ship 1 did not add triggers to
make evidence append-only. Ship 2 adds the missing discipline with
one restricted-UPDATE trigger + one DELETE trigger, matching the
pattern already applied to memory in §3:

```sql
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

CREATE TRIGGER evidence_no_delete
BEFORE DELETE ON evidence
BEGIN
    SELECT RAISE(ABORT, 'evidence rows cannot be deleted');
END;
```

The `evidence_only_invalidated_at_updatable` trigger permits UPDATE
only when every column except `invalidated_at` is unchanged. Rule 3's
mutation statement (`UPDATE evidence SET invalidated_at = :now WHERE
id = :id AND invalidated_at IS NULL`) passes this check trivially.
Any future code path that tries to mutate other columns fires RAISE
and rolls back the tx.

Unit test to add under `internal/evidence/`:

- `TestEvidenceUpdateRestricted`: attempt `UPDATE evidence SET
  sha256=... WHERE id=...`; assert `sqlite: evidence is append-only
  except invalidated_at` error + rollback.
- `TestEvidenceDeleteBlocked`: attempt `DELETE FROM evidence WHERE
  id=...`; assert RAISE error.

### 5.6 Rule 4 — orphaned runs

```go
// MUST run AFTER rule 1 in the same tx. Rule 1 sets
// claims.released_at=now on expired claims; rule 4's
// `released_at + 10min < now` check correctly misses those
// (grace period hasn't passed yet). Reordering rules 1 and 4
// would orphan runs immediately on claim expiry, collapsing
// the 10-minute grace window.
```

```sql
UPDATE runs SET ended_at = :now, outcome = 'orphaned'
 WHERE ended_at IS NULL
   AND claim_id IN (
     SELECT id FROM claims
      WHERE released_at IS NOT NULL
        AND released_at + 600000 < :now);
```

(600000 ms = 10 min grace, Q6.)

Per orphaned run → `run_ended(outcome='orphaned', reason='grace_expired')`.
If any mutations → `reconcile_rule_applied(rule=4, ...)`.

### 5.7 Rule 5 — authoring errors (read-only)

```sql
SELECT tasks.id AS task_id, j.value AS missing_gate_id
  FROM tasks, json_each(tasks.required_gates_json) j
  LEFT JOIN gates ON gates.id = j.value
 WHERE gates.id IS NULL;
```

Zero mutations, zero events. Findings go into `reconcile_ended` payload
under `authoring_errors`.

### 5.8 Real-run response

```json
{
  "reconcile_id": "01H...",
  "dry_run": false,
  "stats": {
    "rule_1_claims_released": 0,
    "rule_1_tasks_reverted": 0,
    "rule_2_tasks_flipped_stale": 0,
    "rule_2_latency_ms": 4,
    "rule_3_evidence_invalidated": 0,
    "rule_3_sampled": 100,
    "rule_3_of_total": 1847,
    "rule_3_mode": "sample",
    "rule_4_runs_orphaned": 0,
    "rule_5_authoring_errors": 0
  },
  "authoring_errors": []
}
```

### 5.9 Dry-run response

```json
{
  "dry_run": true,
  "rules": [
    {"rule": 1, "would_mutate": [{"claim_id": "...", "action": "release", "reason": "expired"}]},
    {"rule": 2, "would_mutate": [{"task_id": "...", "from": "done", "to": "stale"}]},
    {"rule": 3, "would_mutate": [{"evidence_id": "...", "reason": "missing"}]},
    {"rule": 4, "would_mutate": [{"run_id": "...", "outcome": "orphaned"}]},
    {"rule": 5, "authoring_errors": [{"task_id": "...", "missing_gate_id": "..."}]}
  ]
}
```

No `reconcile_id` (Q9 amendment: dry-run didn't happen; no event
references an id).

### 5.10 Invalidation semantics — three surfaces

| Surface                                    | Behavior on `evidence_invalidated = true`                                                                 |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------- |
| `cairn verdict report` (binding new verdict) | **Blocks.** `evidence.Verify` returns `cairnerr.Err{Kind:"evidence_invalidated", Code: CodeValidation}` → exit 1. New kind; Ship 2 behavior change vs. Ship 1. |
| `cairn verdict latest` / `cairn verdict history` | Informational. Response includes `evidence_invalidated: bool`. No blocking.                               |
| `cairn task complete` gate-freshness check | **Does NOT consider.** Binary staleness formula unchanged: `gate_def_hash match + status=pass` only. Invalidation is a separate signal per Q7. |

Rationale: task completion uses staleness; verdict binding uses evidence
integrity. Different invariants, different gates. Agents who care about
invalidated evidence bind a fresh verdict with fresh evidence before
completing — agent discipline, not cairn enforcement.

## 6. Event-log completeness invariant extension

Ship 1's assertion — `cairn events since 0 | jq -r '.kind' | sort -u`
must cover every event kind exercised by the flow — stays. Ship 2 adds
to the expected-kinds set:

- `reconcile_started`
- `reconcile_ended`
- `reconcile_rule_applied`
- `evidence_invalidated`
- `memory_appended`

Reused kinds (already asserted by Ship 1): `claim_released`,
`task_status_changed`, `run_ended`.

## 7. Package + file layout

```
internal/
├── memory/
│   ├── store.go            # Store wraps *db.Tx (Ship 1 invariant).
│   │                       # Funcs: Append, Search, List.
│   ├── validate.go         # tag regex, kind enum, entity_kind enum
│   ├── fts_error.go        # TranslateFTSError: SQLite → cairnerr.Err
│   ├── memory_test.go      # unit tests
│   └── fts_test.go         # FTS5 tokenization + tag filtering + error translation
├── reconcile/
│   ├── reconcile.go        # Orchestrator.Run — phase markers (§5.2)
│   ├── probe.go            # runEvidenceProbe — OUTSIDE tx
│   ├── rule1_leases.go
│   ├── rule2_staleness.go  # Go loop; Ship 4 SQL optimization flagged
│   ├── rule3_evidence.go   # mutation half of rule 3 (probe in probe.go)
│   ├── rule4_orphans.go
│   ├── rule5_authoring.go  # read-only
│   ├── dryrun.go           # pure-read simulator
│   └── reconcile_test.go
└── cli/                    # existing; extend
    ├── memory_append.go
    ├── memory_search.go
    ├── memory_list.go
    └── reconcile.go
```

**Store pattern (Ship 1 invariant):** `type Store struct { tx *db.Tx }`.
Cross-domain calls construct the other Store inside the caller's tx.

**Clock injection:** `memory.Store` and reconcile mutations use Ship 1's
`clock.Clock` interface. Fake clock in tests.

## 8. Testing

### 8.1 Unit tests

| Package                | Covers                                                                                                                                                      |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/memory`      | Append happy path, kind enum, entity XOR CLI+CHECK, tag validation, op_log replay, UPDATE/DELETE trigger RAISE, FTS5 MATCH, FTS error sanitization          |
| `internal/reconcile`   | Per-rule fixtures, rule 2 latest-verdict precedence, rule 3 probe/mutation separation, rule 4 depends-on-rule-1 ordering, evidence_invalidated in verdict response, dry-run parity (see §8.2) |

### 8.2 Integration tests

- `memory_e2e_test.go` — append → list → search with all filter
  permutations, clip detection via `total_matching`.
- `reconcile_e2e_test.go` — PLAN.md Ship 2 dogfood steps 2 + 5.
- `evidence_invalidation_e2e_test.go` — put → delete on disk →
  reconcile → assert `evidence_invalidated` event + verdict-query
  surface + `verdict report` block.
- `task_complete_ignores_invalidation_test.go` — verify §5.10 surface
  table row 3.
- `reconcile_concurrent_test.go` — see §8.3.
- `reconcile_dryrun_parity_test.go` — see §8.4.

### 8.3 Concurrency test (mirrors Ship 1 `TestConcurrentClaim`)

```go
func TestConcurrentReconcile(t *testing.T) {
    // In-process: 5 goroutines on shared *db.DB pool.
    // Subprocess:  2 `cairn reconcile` exec.Command invocations
    //              (separate connection pools; file-level SQLite locking).
    //
    // Assertions:
    //   - All 7 complete exit 0.
    //   - Exactly one invocation finds mutations; other 6 emit
    //     reconcile_started/reconcile_ended with zero rule_applied.
    //   - No BUSY errors surface (absorbed by Ship 1 retry + busy_timeout).
    //   - Event log has 7 reconcile_started + 7 reconcile_ended pairs,
    //     all with distinct reconcile_id ULIDs.
}
```

Goroutine-only tests exercise the in-process BUSY path (shared pool,
Go-level retry); subprocess tests exercise file-level SQLite locking.
Both paths must pass — real callers hit subprocesses (skill/hook
invocations), tests benefit from goroutines (fast).

### 8.4 Dry-run parity test (snapshot/restore protocol)

```go
func TestDryRunParity(t *testing.T) {
    // 1. Seed DB + evidence blobs to exercise all 4 mutating rules +
    //    rule 5 authoring errors.
    seedAllRules(t, stateDir)

    // 2. SNAPSHOT state.db + evidence files to tmpdir.
    snap := captureSnapshot(t, stateDir)
    eventsBefore := countEvents(t, db)

    // 3. Run `cairn reconcile --dry-run`. Parse response.
    dryResult := runCLI(t, "reconcile", "--dry-run")
    drySet := extractMutationSet(dryResult)  // set of {rule, entity_id, action, reason}

    // 4. Assert dry-run was silent (no events, no state change).
    eventsAfter := countEvents(t, db)
    assert.Equal(t, eventsBefore, eventsAfter)
    assert.Equal(t, snap.StateHash, hashStateFile(stateDir))

    // 5. RESTORE snapshot (clock remains pinned).
    restoreSnapshot(t, stateDir, snap)

    // 6. Run real `cairn reconcile`. Capture emitted events.
    realResult := runCLI(t, "reconcile")
    realSet := extractMutationSetFromEvents(t, db, sinceEventID)

    // 7. Assert bijection on {rule, entity_id, action, reason} tuples.
    assert.ElementsMatch(t, drySet, realSet)
    assert.Equal(t, len(drySet), len(realSet))

    // 8. Rule 5 parity.
    assert.ElementsMatch(t,
        dryResult.Rules[4].AuthoringErrors,
        realResult.Stats.AuthoringErrors)
}
```

**Mutation tuple shape:** `{rule int, entity_id string, action string, reason string}`.
Examples: `{1, "CLM_01H…", "release", "expired"}`, `{2, "TASK-001", "flip_stale", "spec_drift"}`, `{3, "EVD_01H…", "invalidate", "missing"}`, `{4, "RUN_01H…", "orphan", "grace_expired"}`.

**Clock pinning:** fake clock identical across dry-run + real-run so
`now`-dependent values (released_at, invalidated_at, ended_at) match.

### 8.5 FTS error translation test

Table-driven:

| input                  | expect                                |
| ---------------------- | ------------------------------------- |
| `AND AND`              | Kind: `invalid_fts_query`             |
| `"unclosed`            | Kind: `invalid_fts_query`             |
| `nonexistent_col:foo`  | Kind: `invalid_fts_query`             |

Assertion: JSON envelope `error.message` contains no `sqlite`, no `fts5:`,
no `near "` substrings. Only cairn-native wording.

### 8.6 CI

- Matrix CI unchanged: Linux/Windows/macOS × Go 1.25.x.
- Offline CI unchanged (network severed via iptables + IPv6 disable
  workaround for `golang/go#76375`). Ship 2 adds no network deps;
  memory + reconcile are pure-local.

## 9. Done-when (exit criteria)

1. PLAN.md Ship 2 dogfood scenarios pass end-to-end (§"Ship 2 dogfood"
   steps 1–6).
2. Event-log completeness test extends to new kinds (§6).
3. Matrix CI green on all three OSs.
4. Offline CI green.
5. `evidence.invalidated_at` surfaces as `evidence_invalidated: bool` in
   `cairn verdict latest` / `history` responses.
6. `cairn memory search "<term>"` returns FTS5-ranked results with
   `relevance` field.
7. `cairn reconcile` idempotent: running twice back-to-back produces
   zero mutations on the second run (integration test).
8. `cairn reconcile --dry-run` produces zero writes + zero events
   (integration test §8.4).
9. Ship 3 target selected and captured as `specs/requirements/REQ-002.yaml`
   + task YAMLs (deferred to end of Ship 2 per PLAN.md; this spec lists
   it as a **post-implementation** milestone of Ship 2, not a build task).

## 10. Lessons-learned audit (Ship 1 → Ship 2 carry-forward)

Ship 1 produced two lesson files under `docs/ship-1-lessons/`. Both
apply to Ship 2:

- **`go-deps-inline.md`** — no dep added before its first import. Ship 2
  adds no new deps (modernc FTS5 ships with the existing SQLite driver),
  so no dep-tidy risk.
- **`modernc-sqlite-text-scan.md`** — keep the `string` intermediate when
  scanning TEXT → `json.RawMessage`/`[]byte`. Ship 2's memory scan pulls
  `tags_json` as TEXT → follow the lesson.

No new Ship 2 lesson file is expected unless an unforeseen surprise
surfaces during implementation.

## 11. PLAN.md amendments (separate prep PR, before implementation)

Two small amendments to the master plan land as a **separate prep PR**,
**before** any Ship 2 implementation commits. Rationale:

- Ship 2's implementation PR will be large (~40 commits, Ship 1 scale).
  Mixing PLAN.md amendments into it muddies review.
- Amendments are small and self-contained — trivial to review standalone.
- If the amendment PR reveals something the brainstorm missed, cheaper
  to catch before implementation starts.
- Separates documentation-truth changes from implementation changes in
  the git log. Future readers diffing PLAN.md history see clean semantic
  commits, not "amended dry_run as part of rule 3 refactor."

**Workflow:**

1. Branch `feature/ship-2-plan-amendments` cut off `master` (or off
   `feature/ship-2-reconcile-memory` then merged to master first).
2. Land the two amendments below.
3. Merge to master.
4. Rebase `feature/ship-2-reconcile-memory` on updated master.
5. Proceed to implementation plan + build.

**Amendments:**

1. **§"Event-log completeness invariant" table** — row for
   `reconcile_started` has payload "reconcile_id, dry_run". Ship 2
   removes `dry_run` from the payload (Q9: dry-run emits no events). New
   payload: `reconcile_id`.
2. **§"Reconciliation rules" rule 4** — "runs in-progress older than a
   configurable threshold with no recent heartbeat" → "runs where
   `claim.released_at + 10min < now` and `runs.ended_at IS NULL`". Q6.

No other PLAN.md edits. Scope stays frozen.

## 12. Open for Ship 3+ (explicitly deferred)

| Item                                 | Trigger for revisit                                             |
| ------------------------------------ | --------------------------------------------------------------- |
| `inputs_hash` comparison semantics   | When a concrete Ship 3+ use case exists; pick input-globs-in-YAML vs. `--inputs-hash` flag on `verdict latest`. |
| `cairn replay --as-of` command       | If callers repeatedly write the same ad-hoc events projection.  |
| Rule 2 correlated-SQL optimization   | If `rule_2_latency_ms` telemetry exceeds 100ms on real repos.   |
| Evidence invalidation index          | If `cairn evidence list --invalidated` or similar query lands.  |
| Memory `kind` enum expansion         | If agents repeatedly misuse `outcome`/`failure` for off-kind entries (Ship 4 retro). |
