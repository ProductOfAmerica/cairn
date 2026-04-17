# Ship 1 — Core substrate (design spec)

Date: 2026-04-17
Status: design approved; implementation plan not yet written
Scope: Ship 1 only (plan's Week 1). Ships 2/3/4 are separate design cycles.

## 0. Preamble

This document is the design spec for Ship 1 of cairn. It builds on and resolves
ambiguities in `docs/PLAN.md` (the foundational planning document). Where this
spec and `PLAN.md` conflict, this spec wins for Ship 1 scope. `PLAN.md` remains
the authoritative source for Ship 2/3/4 direction.

The terminal goal of Ship 1 is: manual end-to-end dogfood of the
init → plan → claim → evidence → verdict → complete cycle, plus the mandatory
event-log completeness CI test (Invariant 10).

Nothing here adds scope beyond `PLAN.md` § "Ship 1 — Core substrate". Where
this spec narrows or pins behavior that `PLAN.md` left implicit, the narrowing
is called out in § "Decisions made in this spec" below.

## 1. Decisions made in this spec (summary)

These decisions are each load-bearing and each resolve an ambiguity or gap in
`PLAN.md`. Implementation must match. Later ships may revisit; Ship 1 may not.

1. **Staleness formula in Ship 1 ignores `inputs_hash`.** Fresh iff
   `V.gate_def_hash == G.gate_def_hash AND V.status == 'pass'`. `inputs_hash`
   is stored on every verdict for forward-compat but never compared. (Resolves
   the undefined `current_inputs_hash` in `PLAN.md` § "Staleness (binary)".)
2. **`gate_def_hash` = sha256 of RFC 8785 JCS canonicalization of the gate's
   YAML subtree, converted YAML→JSON first.** Computed by cairn at
   `cairn task plan` time. Never caller-supplied. `cairn verdict report`
   has no `--gate-def-hash` flag; the value stored on `verdicts.gate_def_hash`
   is always read from `gates.gate_def_hash` inside the bind transaction.
3. **`producer_hash` and `inputs_hash` are caller-supplied at
   `cairn verdict report` as 64-lowercase-hex strings** (regex
   `^[0-9a-f]{64}$`). Cairn stores them opaquely; Ship 1 never interprets them.
   Malformed input → exit `1`.
4. **`cairn spec validate` runs schema + referential + uniqueness checks in
   one pass**, reports all errors together as a JSON array of
   `{path, kind, message}`, exits non-zero if any. No orphan-warnings noise.
5. **`cairn task claim` refuses to claim a task whose `depends_on` contains
   any task not in status `done`.** Dep check and CAS happen inside the same
   `BEGIN IMMEDIATE` transaction (TOCTOU prevention). Error lists blocking
   deps with their current status.
6. **`cairn evidence put` defaults `content_type` to
   `application/octet-stream`.** No sniff, no extension map. Optional
   `--content-type` override. Ship 1 has no consumer for this field;
   detection is deferred to the ship that consumes it.
7. **Every command returns a uniform JSON envelope**
   `{op_id?, kind, data | error}`. `data` is always an object, never null or
   scalar; commands with no meaningful return value emit `data: {}`.
8. **Exit codes are structured, not binary.**
   `0 success / 1 user error / 2 conflict / 3 not found / 4 substrate error`.
   Scripts branch on exit for common cases; parse `error.code` in JSON for
   fine-grained distinctions.
9. **`--format human` is NOT implemented in Ship 1.** JSON is the only output
   mode. `jq` is the human-rendering tool.
10. **Repo-id canonicalization:** shell out to `git rev-parse --git-common-dir`;
    `filepath.Abs` → `filepath.EvalSymlinks` → on Windows lowercase drive
    letter + normalize separators to forward-slash → sha256. If git fails or
    cwd is not a git repo, exit `1` with a clear error. No fallback.
11. **`go.mod` reconciliation:** use `jsonschema/v6` (current, already in
    `go.mod`). Add `oklog/ulid/v2` (plan-required, currently missing). A JCS
    library is picked during the writing-plans phase (`gowebpki/jcs` if viable,
    inline implementation if not).
12. **Clock:** `time.Now().UnixMilli()`, UTC, never local. JSON timestamps are
    integer ms. Tests use an injectable `clock.Clock` interface with a fake
    impl for determinism.
13. **Package API boundaries enforced by the Store pattern.** `internal/db`
    exports only transaction primitives; each domain package exports a
    `*Store` type that owns its tables. Cross-domain mutations go through
    the owner's Store. Convention + review; no lint or AST check in Ship 1.
14. **Verdicts survive release-then-reclaim cycles.** `cairn task complete`
    evaluates *latest verdict per required `gate_id`* regardless of which
    run produced it. A reclaimed task does not need to re-produce gates whose
    `gate_def_hash` still matches.

## 2. Architecture

### 2a. Package layout

```
cmd/cairn/              # cobra root + subcommand glue only (≤10 LOC per command)
internal/
  clock/                # Clock interface (NowMilli() int64) + wall + fake impls
  ids/                  # ULID generation, op_id validation
  repoid/               # git-common-dir canonicalization → sha256
  db/                   # Open, WAL setup, migrate (embed), WithTx, CAS helper
  events/               # Appender (in-txn), Since, Kinds (coverage helper)
  intent/               # YAML loader, schema validate, referential check,
                        # gate_def_hash via JCS, materialize
  evidence/             # put (content-address, two-hex shard, dedupe),
                        # verify, get
  task/                 # plan, list, claim (CAS + dep check + inline rule-1),
                        # heartbeat, release, complete
  verdict/              # report, latest, history, staleness
  cairnerr/             # typed error with Code (maps to exit code)
  cli/                  # JSON envelope, exit-code translation, flag parsing
```

### 2b. Dependency DAG (build order is bottom-up)

```
clock ─┐
ids ───┤  (leaf; no cross-internal deps)
repoid ┤
cairnerr
       │
db ────┴→ events ──┬→ intent ─────┬→ task ─────┐
                   ├→ evidence ───┤            ├→ cli → cmd/cairn
                   └──────────────┴→ verdict ──┘
```

Acyclic by construction. `events` depends only on `db` + `cairnerr`.
`cli` is the only package that imports every domain.

### 2c. Store pattern (API boundary)

`internal/db` exports only:

```go
func Open(path string) (*DB, error)                              // opens, sets WAL, migrates
func (*DB) WithTx(ctx, func(tx *Tx) error) error                 // BEGIN IMMEDIATE + retry
type Tx struct{ ... }
func (*Tx) Exec(q string, args ...any) (sql.Result, error)
func (*Tx) Query(q string, args ...any) (*sql.Rows, error)
func (*Tx) QueryRow(q string, args ...any) *sql.Row
```

No entity-level methods on `*DB` or `*Tx`. Each domain package exposes a
`Store`:

```go
// internal/task
type Store struct {
    tx     *db.Tx
    events events.Appender
    clock  clock.Clock
}
func NewStore(tx *db.Tx, ev events.Appender, clk clock.Clock) *Store
func (s *Store) Claim(opID, taskID, agentID string, ttl time.Duration) (ClaimResult, error)
// etc.
```

Cross-domain mutations inside one txn construct the other store:

```go
func (s *task.Store) Complete(opID, claimID string) error {
    verdicts := verdict.NewStore(s.tx, s.events, s.clock)
    for _, g := range requiredGates {
        ok, err := verdicts.IsFreshPass(g)
        ...
    }
}
```

`*db.Tx` still technically exposes `Exec/Query`, so bypass is possible.
Discipline: never issue SQL outside a `Store` method. **Deliberate Ship 1
tradeoff:** enforcement = convention + code review. Tripwire: if a bypass
ever lands in main, Ship 2 adds an AST-based lint that parses `tx.Exec/
Query/QueryRow` call sites and asserts each originates inside a `Store`
method of the table's owning package.

### 2d. Dependencies

Added to `go.mod` during the plan phase:

- `github.com/oklog/ulid/v2` (plan-required).
- JCS library: `github.com/gowebpki/jcs` if current + viable; otherwise an
  inline RFC 8785 implementation with a documented algorithm.

Kept / corrected:

- `modernc.org/sqlite` (plan-required, present).
- `gopkg.in/yaml.v3` (plan-required, present).
- `github.com/santhosh-tekuri/jsonschema/v6` (current release; `PLAN.md` says
  `/v5` but that was advisory. Plan doc updated to match).
- `github.com/spf13/cobra` (plan-required, present).

No other runtime deps. No ORM. No web framework. No network clients. No git
library (shell out to `git`).

## 3. Data flow

Every command body is: parse flags → call a `Store` method inside `db.WithTx`
→ marshal envelope → set exit code. No business logic in `cmd/cairn/*.go`.

### 3a. Setup

**`cairn init [--repo-root <path>]`**
- Resolves state-root: `CAIRN_HOME` env var wins, else platform defaults
  (Linux: `$XDG_DATA_HOME/cairn` or `$HOME/.cairn`; macOS: `$HOME/.cairn`;
  Windows: `%USERPROFILE%\.cairn`).
- Computes `repo-id` via §2 repo-id pipeline.
- Creates `<state-root>/<repo-id>/` + `blobs/` with mode `0o700`.
- Opens `state.db`, applies migrations.
- No domain event (init is not a domain mutation).
- Response: `{kind:"init", data:{repo_id, state_dir, db_path}}`.
- Exit `1` if cwd is not a git repo or git is not installed.

**`cairn spec validate [--path specs/]`**
- Read-only. Walks `<path>/requirements/*.yaml` and `<path>/tasks/*.yaml`.
- For each file: parse YAML → validate against JSON Schema
  (requirements.schema.json, tasks.schema.json).
- Referential pass (all checks; single pass, all errors collected):
  - `task.implements` → every entry must be an existing requirement id.
  - `task.depends_on` → every entry must be an existing task id. No self-ref.
    No cycles (DFS for back-edges).
  - `task.required_gates` → every entry must be a gate id declared on a
    requirement that the task implements.
  - IDs unique within kind (requirement ids unique among requirements; task
    ids unique among tasks; gate ids unique within their requirement).
- Response: `{kind:"spec.validate", data:{errors:[{path,kind,message}]}}`.
  `errors` is `[]` on success. Exit `1` if `errors` non-empty, else `0`.

### 3b. Task planning

**`cairn task plan`** — single `BEGIN IMMEDIATE`:
1. Run validate inline. Errors → rollback, exit `1`.
2. For each requirement YAML:
   - Compute `spec_hash = sha256(raw YAML bytes)`.
   - Upsert into `requirements` by `id`. If the row existed and `spec_hash`
     changed, emit `spec_materialized {spec_path, old_hash, new_hash}`.
   - For each gate in the requirement's `gates:` list:
     - Convert the gate subtree YAML → JSON (via `yaml.v3` decode + `json`
       encode).
     - Canonicalize via RFC 8785 JCS.
     - `gate_def_hash = sha256(jcs_bytes)`, lowercase hex.
     - Upsert into `gates` (id, requirement_id, kind, definition_json,
       gate_def_hash, producer_kind, producer_config).
3. For each task YAML:
   - Compute `spec_hash`.
   - Upsert into `tasks`. If new, status = `open`; if existing, status is
     preserved (don't stomp in-flight work).
   - Emit `task_planned {task_id, requirement_id, spec_hash}` on insert.
4. Commit.

Response: `{op_id, kind:"task.plan", data:{requirements_materialized, gates_materialized, tasks_materialized}}`.

### 3c. Task list (read-only)

**`cairn task list [--status <s>]`**
- `SELECT` from `tasks`; optional status filter.
- Response: `{kind:"task.list", data:{tasks:[{id, status, required_gates, depends_on, ...}]}}`.

### 3d. Claim cycle

**`cairn task claim <task_id> --agent <id> --ttl <duration>`** — single `BEGIN IMMEDIATE`:

1. **op_id idempotency:** `SELECT result_json FROM op_log WHERE op_id=?`.
   Hit + kind matches `task.claim` → return cached result, commit, exit `0`,
   no events. Hit + kind mismatch → exit `2` `{code:"op_id_kind_mismatch"}`.
2. **Inline reconcile rule 1:**
   `UPDATE claims SET released_at=? WHERE expires_at<? AND released_at IS NULL`.
   For each task whose only live claim just expired, flip
   `claimed|in_progress|gate_pending → open`. Each mutation emits
   `claim_released {reason:"expired"}` and, when flipping task status,
   `task_status_changed {from, to, reason:"lease_expired"}`.
3. **Dep check (Q4):**
   `SELECT id, status FROM tasks WHERE id IN (depends_on) AND status != 'done'`.
   Rows present → rollback, exit `2`
   `{code:"dep_not_done", blocking:[{id,status}, ...]}`.
4. **CAS claim:**
   `UPDATE tasks SET status='claimed', updated_at=? WHERE id=? AND status='open' RETURNING id`.
   Zero rows → rollback, exit `2`
   `{code:"task_not_claimable", current_status:"<status>"}`.
5. **Insert claim + run:** generate `claim_id`, `run_id` (ULIDs).
   `INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)`.
   `INSERT INTO runs (id, task_id, claim_id, started_at)`.
6. **Emit events** (in order):
   `claim_acquired {claim_id, task_id, agent_id, expires_at}`,
   `run_started {run_id, claim_id, task_id}`,
   `task_status_changed {task_id, from:"open", to:"claimed", reason:"claim"}`.
7. **Record op_log.**
8. Commit.

Response: `{op_id, kind:"task.claim", data:{claim_id, run_id, task_id, expires_at}}`.

**`cairn task heartbeat <claim_id>`**
- CAS `UPDATE claims SET expires_at=NowMilli+ttl_ms WHERE id=? AND released_at IS NULL RETURNING expires_at`.
- TTL reused from the claim row (not per-heartbeat flag in Ship 1).
- Zero rows → exit `2` `{code:"claim_released_or_expired"}`.
- Emit `claim_heartbeat {claim_id, new_expires_at}`.
- Response: `{op_id, kind:"task.heartbeat", data:{expires_at}}`.

**`cairn task release <claim_id>`**
- CAS `UPDATE claims SET released_at=? WHERE id=? AND released_at IS NULL`.
- Zero rows → exit `2` `{code:"claim_already_released"}`.
- If run is still active, `UPDATE runs SET ended_at=?, outcome='orphaned'`.
- Task status transitions: only flip to `open` if no other live claim exists
  for the task.
- Emit: `claim_released {reason:"voluntary"}`,
  `run_ended {outcome:"orphaned"}` if applicable,
  `task_status_changed` if task flipped.
- Response: `{op_id, kind:"task.release", data:{}}`.

### 3e. Evidence

**`cairn evidence put <path> [--content-type <ct>]`** — single `BEGIN IMMEDIATE`:

1. op_id check.
2. Read file, compute sha256 (streaming).
3. Destination = `<state-root>/<repo-id>/blobs/<sha[:2]>/<sha>`.
4. **Write ordering** (see § 5e for crash-window discussion):
   - Write bytes to `blobs/<sha[:2]>/.tmp-<ulid>`.
   - `fsync` the file.
   - If final path already exists, `os.Stat` + stream-sha256 of the existing
     file; match source sha → dedupe (discard temp); mismatch → exit `4`
     `{kind:"blob_collision"}`. Pre-check required for Windows where
     `os.Rename` fails on existing destinations; also correct on POSIX.
     See §5e for rationale.
   - Otherwise `os.Rename` atomic to final path.
5. `INSERT INTO evidence (id, sha256, uri, bytes, content_type, created_at)`
   with `ON CONFLICT(sha256) DO NOTHING` — dedupe on sha collision.
   `content_type = --content-type flag || "application/octet-stream"`.
6. Emit `evidence_stored {evidence_id, sha256, bytes, content_type, dedupe}`
   (dedupe=true if the insert was skipped by the conflict clause).
7. Commit.

Response: `{op_id, kind:"evidence.put", data:{evidence_id, sha256, uri, bytes, content_type, dedupe}}`.

**Visibility race (documented, not fixed):** between step 4 (rename) and step
7 (commit), the blob is on disk but no committed DB row exists. A concurrent
`cairn evidence verify <sha>` from another process will return exit `3`
`{code:"not_stored"}`. This is correct: `verify` asks "has cairn committed
this evidence as bindable?" The answer during the window is "no." See § 5e.

**`cairn evidence verify <sha256>`** (read-only)
- `SELECT` the evidence row; not found → exit `3`.
- Open blob file; stream-sha256; compare.
- Mismatch → exit `4` `{code:"hash_mismatch"}` + emit
  `evidence_invalidated {evidence_id, reason:"hash_mismatch"}` in its own
  short txn.
- Response: `{kind:"evidence.verify", data:{sha256, bytes, verified_at}}`.

**`cairn evidence get <sha256>`** (read-only)
- Returns the file URI (absolute path) in JSON. Does not stream contents
  through cairn; callers can `cat` the path. Response:
  `{kind:"evidence.get", data:{sha256, uri, bytes, content_type, created_at}}`.

### 3f. Verdicts

**`cairn verdict report --gate <gate_id> --run <run_id>
                       --status <pass|fail|inconclusive>
                       --evidence <path>
                       --producer-hash <hex64>
                       --inputs-hash <hex64>
                       [--score-json <json>]`** — single `BEGIN IMMEDIATE`:

1. op_id check.
2. Validate `--producer-hash` and `--inputs-hash` against
   `^[0-9a-f]{64}$`. Fail → exit `1`
   `{code:"bad_input", details:{flag:"--producer-hash"}}` etc.
3. Validate `--status` ∈ {pass, fail, inconclusive}; else exit `1`.
4. Compute sha256 of the file at `--evidence`. `SELECT` the evidence row by
   sha256. Not stored → exit `3` `{code:"evidence_not_stored"}` (caller must
   `evidence put` first).
5. **Re-verify:** re-stream the on-disk blob, re-sha256, compare to the stored
   row. Mismatch → emit `evidence_invalidated`, rollback, exit `4`
   `{code:"evidence_hash_mismatch"}`.
6. `SELECT` gate by id. Not found → exit `3` `{code:"gate_not_found"}`.
   `gate_def_hash` is read from this row. No `--gate-def-hash` flag.
7. `SELECT` run by id. Not found → exit `3`. Run already ended → exit `1`
   `{code:"run_already_ended"}`.
8. Generate `verdict_id` (ULID).
   `sequence = (SELECT COALESCE(MAX(sequence), 0) + 1 FROM verdicts WHERE gate_id=?)`.
9. `INSERT INTO verdicts (id, run_id, gate_id, status, score_json,
   producer_hash, gate_def_hash, inputs_hash, evidence_id, bound_at, sequence)`
   — `gate_def_hash` is the value from step 6, not CLI input.
10. Emit `verdict_bound {verdict_id, gate_id, run_id, status, producer_hash,
    gate_def_hash, inputs_hash, sequence}`.
11. Commit.

Response: `{op_id, kind:"verdict.report", data:{verdict_id, gate_id, run_id, status, sequence, bound_at}}`.

**`cairn verdict latest <gate_id>`** (read-only)
- Latest verdict row by `(gate_id, bound_at DESC, sequence DESC)`.
- Computes `fresh` = `verdict.gate_def_hash == gate.gate_def_hash AND verdict.status == 'pass'` (Q1-A formula).
- Response: `{kind:"verdict.latest", data:{verdict:{...}, fresh:bool}}`.
- Gate not found → exit `3`.
- Gate exists, no verdicts → `data:{verdict:null, fresh:false}`.

**`cairn verdict history <gate_id> [--limit <n>]`** (read-only)
- Verdicts ordered by `(bound_at DESC, sequence DESC)`. Default limit 50.
- Each row includes derived `fresh` computed as above.

### 3g. Completion

**`cairn task complete <claim_id>`** — single `BEGIN IMMEDIATE`:

1. op_id check.
2. `SELECT` claim. Not found → exit `3`. Released → exit `2`
   `{code:"claim_released"}`.
3. `SELECT` task. For each gate id in `required_gates`:
   - `SELECT` latest verdict for that gate (`ORDER BY bound_at DESC, sequence DESC LIMIT 1`).
   - `SELECT` gate to get current `gate_def_hash`.
   - Gate is fresh+pass iff
     `latest.gate_def_hash == gate.gate_def_hash AND latest.status == 'pass'`.
   - Collect all gates that are not fresh+pass.
4. Any failures → rollback, exit `2`
   `{code:"gates_not_fresh_pass", details:{failing:[{gate_id, reason:"stale"|"no_verdict"|"status_not_pass"}]}}`.
5. All pass → update:
   - `UPDATE tasks SET status='done', updated_at=?`.
   - `UPDATE runs SET ended_at=?, outcome='done'`.
   - `UPDATE claims SET released_at=?`.
6. Emit: `task_status_changed {from:<prev>, to:"done", reason:"complete"}`,
   `run_ended {outcome:"done"}`, `claim_released {reason:"voluntary"}`.
7. Commit.

Response: `{op_id, kind:"task.complete", data:{task_id, run_id}}`.

**Released-then-reclaimed semantics:** because step 3 evaluates *latest
verdict per gate regardless of run*, a reclaim does not need to re-produce
gates whose `gate_def_hash` still matches. Consequence: resuming a task never
wastes prior verified gates. Design doc § 7 includes a dogfood test for this.

### 3h. Events

**`cairn events since <timestamp_ms> [--limit <n>]`** (read-only)
- `<timestamp_ms>` must be a non-negative integer (milliseconds since epoch).
  Other formats (RFC 3339, unix seconds, durations) rejected with exit `1`.
  See §6c.
- `SELECT * FROM events WHERE at > ? ORDER BY id ASC LIMIT ?`. Default limit 100.
- Response: `{kind:"events.since", data:{events:[{id, at, kind, entity_kind, entity_id, payload, op_id}]}}`.

## 4. Invariants + enforcement

| # | Invariant | Enforced by | Ship 1 test |
|---|-----------|-------------|------------|
| 1 | Mutation only via CLI | Store pattern + code review; `db.Tx` exposed methods don't include entity logic. Deliberate Ship 1 tradeoff: no grep/AST/lint enforcement — convention + review only. **Tripwire:** if a bypass (raw SQL outside a Store) ever lands in main, add an AST-based lint in Ship 2 that parses `tx.Exec/Query/QueryRow` call sites and asserts they originate from inside a `Store` method of the table's owning package. | Architecture note; no automated test in Ship 1. |
| 2 | Spec in git, schema-validated | `internal/intent` loads from FS only; requirements/gates mutate only via `cairn task plan`. | Unit: invalid spec → no rows inserted. |
| 3 | Evidence content-addressed + verified | `evidence.put` hashes before insert; `verdict.report` re-verifies in bind txn. | Unit: corrupt blob → `verdict.report` exits `4`. |
| 4 | Verdicts append-only | No UPDATE/DELETE on `verdicts` in any store. | Unit: attempt in test → fails. |
| 5 | Leases time-bound, CAS acquisition | `task.Claim` CAS inside `BEGIN IMMEDIATE`; inline rule-1 cleanup same txn. | Unit `TestConcurrentClaim`: ≥5 goroutines + ≥3 subprocesses. |
| 6 | Every mutation carries op_id | CLI generates if omitted; stores require `opID string` arg. | Unit: replay same op_id → cached result, no duplicate events. |
| 7 | Offline-capable | No network imports; `go.mod` audit. | CI: network-isolated job proves build + test green. |
| 8 | Reconcile stateless + on-demand | Ship 2. In Ship 1: inline rule-1 cleanup is idempotent (CAS on `released_at IS NULL`). | Unit: claim → expire → re-run claim, no double-release. |
| 9 | Library-first | CLI `RunE` bodies ≤10 LOC. | Review; no automated check. |
| 10 | Event log = single source of truth | Every store mutation calls `events.Append(tx, ...)` in same txn. | CI: `TestShip1DogfoodEventCoverage` asserts `set(emitted.kind) ⊇ expected`. |

## 5. Error handling, concurrency, durability

### 5a. Error taxonomy

```go
package cairnerr

type Code string
const (
    CodeBadInput   Code = "bad_input"     // exit 1
    CodeValidation Code = "validation"    // exit 1
    CodeConflict   Code = "conflict"      // exit 2
    CodeNotFound   Code = "not_found"     // exit 3
    CodeSubstrate  Code = "substrate"     // exit 4
)

type Err struct {
    Code    Code
    Kind    string           // finer-grained, goes to envelope error.code
    Message string
    Details map[string]any
    Err     error
}
func (e *Err) Error() string
func (e *Err) Unwrap() error
```

Stores return `*Err`. CLI translates `Code` → exit code; surfaces `Kind` +
`Details` in the envelope's `error` field.

### 5b. Transaction discipline

- `db.WithTx(ctx, fn)` is the only txn entry point.
- `BEGIN IMMEDIATE` on entry.
- On `SQLITE_BUSY` at any point (begin, any statement, commit): retry with
  exponential backoff starting at 10ms, doubling, capped at the 500ms outer
  budget. Exhausted → rollback (if possible), return `CodeSubstrate` with
  `Kind:"busy"`.
- **Commit-time BUSY** (load-bearing): the commit did not happen; transaction
  stays open. Retry the commit within the same budget. Exhausted → rollback
  explicitly, return substrate error. This prevents the "mutation succeeded
  but caller thinks it didn't" failure mode.
- Stores never call `Commit()` or `Rollback()`; those are exclusive to
  `WithTx`. Stores return errors; `WithTx` decides.

### 5c. Idempotency (`op_log`)

```go
// At top of every mutation store method:
if cached, hit, err := s.opLog.Check(opID, kind); hit {
    return unmarshal(cached), nil
}
if err != nil { return nil, err }

// ... do mutation, emit events ...

// At end of successful path (SAME transaction as mutation + events):
s.opLog.Record(opID, kind, resultJSON)
```

- **The `op_log` insert runs in the same `BEGIN IMMEDIATE` transaction as the
  mutation and its emitted events.** This is load-bearing: if the insert
  lived in a separate txn, a crash between mutation-commit and op_log-commit
  would produce a mutation with no replay guard — the next caller with the
  same `op_id` would re-execute the mutation, violating Invariant 6.
  `opLog.Check` reads, `opLog.Record` writes, and the domain mutation all
  share one `*db.Tx`; committed together or rolled back together.
- `op_id` + `kind` pair: replay with different kind → exit `2`
  `{kind:"op_id_kind_mismatch"}`.
- `op_log` rows do not expire in Ship 1. Ship 4 may add GC.

### 5d. WAL + concurrency

On `Open`:
```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
```

- `BEGIN IMMEDIATE` for every mutation (no snapshot-read-upgraded-to-write
  deadlocks).
- `busy_timeout=5000` is the driver-level retry; the outer 500ms budget is
  the cairn-level retry. Either may trigger first; both are correct fallbacks.
- Multi-process coordination is native to SQLite at single-host scale.
  Multi-host is out of scope (swap to Postgres if ever needed).

### 5e. Durability + crash windows

**State DB:** WAL handles crash recovery; in-flight txns roll back cleanly.

**Blob store:** atomic rename from temp → final. Two crash windows:
1. Crash between `write temp` and `rename`: temp file may leak. Orphan cleanup
   deferred to Ship 2 reconcile or a Ship 4 GC command. Not a correctness
   bug — blobs are content-addressed; re-running `evidence put` dedupes.
2. Crash between `rename` and `COMMIT`: blob on disk, no committed DB row.
   Concurrent `evidence verify` returns exit `3` `not_stored`. Correct
   semantic: `verify` asks "has cairn committed this as bindable?" Answer
   during the window is "no." Caller retries the put; dedupe handles it.

**Windows rename-exists:** on Windows, `os.Rename` fails if the destination
exists (unlike POSIX which silently overwrites). `cairn evidence put` handles
this by: before attempting rename, `os.Stat` the destination; if present,
stream-sha256 the existing file and compare against the just-computed source
hash — match → dedupe (discard temp, proceed to `INSERT OR IGNORE`); mismatch
→ exit `4` `{kind:"blob_collision", details:{path, existing_sha, new_sha}}`.
A mismatch at a content-addressed path signals on-disk corruption and is
never expected under correct operation. This pre-check unifies Windows +
POSIX behavior so the write path is platform-agnostic above `os.Rename`.

Rejected alternative: INSERT-first, rename-after-commit. Opens a worse window
— crash between commit and rename leaves a DB row claiming a blob that doesn't
exist, making `verify` return `hash_mismatch` for evidence that claims to
exist. Current order is strictly safer.

### 5f. DB path + migrations

- State DB at `<state-root>/<repo-id>/state.db`. Directory created on init
  with mode `0o700`.
- Migrations embedded via `//go:embed internal/db/schema/*.sql`. Applied in
  filename-sorted order; each in its own txn; recorded in `schema_migrations`.
- Schema-ahead (DB written by newer cairn): exit `4`
  `{kind:"schema_ahead"}`. Never auto-downgrade.

### 5g. Logging

- Stderr only. Stdout is exclusively response JSON.
- Default level WARN+. `--verbose` or `CAIRN_LOG=debug` bumps to DEBUG.
- Plain text, one line per event. Consumer is humans; JSON response is the
  machine surface.

## 6. CLI surface (Ship 1 — 15 commands)

### 6a. Global flags

| Flag | Default | Applies to | Notes |
|------|---------|------------|-------|
| `--format` | `json` | all | `human` NOT implemented in Ship 1 (skip per §1 decision 9). |
| `--op-id` | auto-generated ULID | mutations only | Caller-supplied for retry safety. Echoed in response. |
| `--state-root` | resolved default | all | Advanced override; primarily for tests. |
| `--verbose` | false | all | Bumps stderr logs to DEBUG. |

### 6b. Command table

| # | Command | Type | Args / flags | Failure exit codes |
|---|---------|------|--------------|--------------------|
| 1 | `cairn init` | setup | `[--repo-root <path>]` | `1` not a git repo; `4` DB create fails |
| 2 | `cairn spec validate` | read | `[--path specs/]` | `1` any error (array) |
| 3 | `cairn task plan` | mutation | — | `1` spec invalid; `4` DB busy |
| 4 | `cairn task list` | read | `[--status <s>]` | — |
| 5 | `cairn task claim` | mutation | `<task_id> --agent <id> --ttl <duration>` | `2` deps not done / task not claimable; `3` task not found |
| 6 | `cairn task heartbeat` | mutation | `<claim_id>` | `2` released/expired; `3` not found |
| 7 | `cairn task release` | mutation | `<claim_id>` | `2` already released; `3` not found |
| 8 | `cairn task complete` | mutation | `<claim_id>` | `2` gates not fresh+pass; `3` not found |
| 9 | `cairn verdict report` | mutation | `--gate <id> --run <id> --status <pass\|fail\|inconclusive> --evidence <path> --producer-hash <hex64> --inputs-hash <hex64> [--score-json <json>]` | `1` bad input; `3` gate/run/evidence not found; `4` evidence hash mismatch |
| 10 | `cairn verdict latest` | read | `<gate_id>` | `3` gate not found |
| 11 | `cairn verdict history` | read | `<gate_id> [--limit <n>]` | `3` gate not found |
| 12 | `cairn evidence put` | mutation | `<path> [--content-type <ct>]` | `1` path unreadable; `4` blob collision |
| 13 | `cairn evidence verify` | read | `<sha256>` | `3` not stored; `4` hash mismatch |
| 14 | `cairn evidence get` | read | `<sha256>` | `3` not stored |
| 15 | `cairn events since` | read | `<timestamp_ms> [--limit <n>]` | `1` timestamp not integer ms |

**Explicitly NOT in Ship 1:** `cairn memory *`, `cairn reconcile`. Those are Ship 2.

**Explicitly NOT a flag on `verdict report`:** `--gate-def-hash` (value is read
from the gates table at bind time; caller cannot override).

### 6c. Duration + timestamp parsing

`--ttl` accepts Go duration syntax (`30m`, `1h30m`, `2h`). Parsed via
`time.ParseDuration`. Invalid → exit `1` `{kind:"bad_input", details:{flag:"--ttl"}}`.

`cairn events since <timestamp_ms>` accepts **only** integer milliseconds
since epoch (non-negative int64), matching the on-disk timestamp
representation (§ 0 clock decision). Explicitly rejected: RFC 3339 strings
(`2026-04-17T10:00:00Z`), unix seconds, durations, relative forms (`1h ago`).
Malformed input → exit `1` `{kind:"bad_input", details:{flag:"timestamp_ms"}}`.
Rationale: one representation on the wire and on disk prevents silent
conversion bugs; `jq 'select(.at > ...)' | head -1 | .at` trivially gives
the caller the ms value to pass back.

### 6d. Response envelope (normative)

```json
{
  "op_id": "01HNB...",
  "kind": "task.claim",
  "data": { "claim_id": "01H...", "run_id": "01H...", "task_id": "TASK-001", "expires_at": 1713372000000 }
}
```

Error form (mutually exclusive with `data`):

```json
{
  "op_id": "01HNB...",
  "kind": "task.claim",
  "error": { "code": "dep_not_done", "message": "blocked by TASK-016 (failed)", "details": { "blocking": [{"id":"TASK-016","status":"failed"}] } }
}
```

- `op_id` present only on mutations.
- `kind` always present; matches the *command* (dot-separated), not an event kind.
- `data` always an object (never null/scalar); commands without meaningful
  return use `data: {}` (e.g., `task release`).
- `error.code` is the fine-grained kind string (`dep_not_done`,
  `task_not_claimable`, `gates_not_fresh_pass`, etc.). Coarse exit code maps
  per § 5a.

## 7. Testing

### 7a. Test pyramid

**Unit tests** — one `_test.go` per package. Table-driven where natural.

- `clock`: fake advances deterministically.
- `ids`: ULID generation unique + sorted; op_id regex.
- `repoid`: canonicalization matrix. Fixture helper shells out to `git init`.
  Cases: plain repo, worktree, worktree-of-worktree, symlinked clone,
  mixed-case drive letter (Windows), Windows directory junction (Windows
  only, via build tag), bare repo, `GIT_DIR` env override producing a
  relative path (`filepath.Abs` handles).
- `db`: open + migrate idempotent (re-open is safe); `WithTx` commit + rollback
  + retry on BUSY; commit-time BUSY retry regression test.
- `events`: append inside txn; rollback discards event; `Kinds(since)` helper.
- `intent`: schema happy/failure fixtures; referential case per §3a rule;
  cycle detection; `gate_def_hash` JCS determinism (same gate → same hash;
  whitespace/comment YAML edit → same hash since JCS normalizes the JSON
  form; semantic field edit → different hash).
- `evidence`: put idempotency, put with `--content-type` override, dedupe
  (two puts same file → one row, second reports `dedupe:true`), verify
  hash-mismatch invalidates + emits event, race regression (concurrent put
  + verify goroutines; verify returns `not_stored` during the window,
  succeeds after the window).
- `task`: claim CAS (only-one wins), dep check inside same txn (TOCTOU
  regression: inject delay via test hook), inline rule-1 cleanup, heartbeat
  renews, release state machine, complete staleness check, released-then-
  reclaimed completes without re-reporting (§1 decision 14).
- `verdict`: report requires committed evidence (§5e regression: attempt
  report during put's visibility window → exit `3`), `gate_def_hash` sourced
  from gates table (regression: mutate gate hash via direct SQL between two
  reports, assert the second stores the new hash), sequence monotonic per
  gate, latest/history ordering.

**Integration tests** — `internal/integration/*_test.go`. Exercise CLI via
`exec.Command`:

- Envelope shape + exit code per command (success + error paths).
- op_id idempotency across subprocess boundaries.
- Cross-process claim race (subprocess subset of § 7c).

**Concurrency test** — `TestConcurrentClaim`:

- ≥5 goroutines + ≥3 subprocesses race to claim the same task.
- Exactly one wins; others exit `2` `{code:"task_not_claimable"}`.
- Post-test: `PRAGMA integrity_check` returns `ok`.

**Dogfood scenario test** — `TestShip1DogfoodEventCoverage`:

- Runs the full Ship 1 dogfood (init → spec → validate → plan → claim →
  evidence put → verdict report → complete) end-to-end via the CLI.
- Asserts event-log completeness:
  ```go
  emitted := queryDistinctKinds(db, 0)
  expected := []string{
      "task_planned", "spec_materialized",
      "claim_acquired", "run_started",
      "task_status_changed",
      "evidence_stored",
      "verdict_bound",
      "run_ended", "claim_released",
  }
  require.Subset(t, emitted, expected)
  ```
- Asserts spec edit + re-plan flips `verdict latest` to `fresh:false`.
- Asserts idempotency: re-running the scenario with the same op_ids does
  not produce duplicate events / rows.

### 7b. Fixtures

`testdata/`:
- `specs_valid/` — minimal passing spec for dogfood (REQ-001 + TASK-001).
- `specs_invalid_schema/` — YAML failing JSON Schema (multiple files; one
  per schema rule violated).
- `specs_invalid_refs/` — one fixture per referential case from §3a:
  `task-points-at-nonexistent-requirement`, `task-depends-cycle`,
  `task-required-gate-not-on-implemented-req`, `duplicate-task-id`, etc.
- `repo_fixtures/` — shell helper to construct fresh git repos (plain,
  worktree, symlinked, junction). Git is already a runtime dependency;
  using real git in tests is not a new dep.

### 7c. CI

`.github/workflows/ci.yml`:
- Matrix: `{linux/amd64, windows/amd64, darwin/arm64}` × Go 1.24.
- Steps per matrix cell: `go mod verify`, `go vet`, `go build ./...`,
  `go test -race ./...`.
- Network-isolated job (Linux only): runs `go test -race ./...` with network
  access severed (net-namespace or similar). Proves Invariant 7.

No external services. No docker-compose.

### 7d. Not in Ship 1 testing

- Multi-host concurrency.
- Reconcile rules 2/3/4/5 (Ship 2).
- Memory / FTS5 (Ship 2).
- `--format human` (skipped per §1 decision 9).
- Superpowers skill integration (Ship 3).
- UNC path handling on Windows (note-and-defer per Q7 discussion).

## 8. Build order

1. `clock` (wall + fake, unit tests).
2. `ids` (ULID + op_id regex, unit tests).
3. `cairnerr` (error type + Code enum, unit tests).
4. `repoid` (canonicalization pipeline + Windows matrix, unit tests).
5. `db` (Open, WAL pragmas, migrate via embed, `WithTx`, BUSY retry incl.
   commit-time, unit tests).
6. `events` (Appender, Since, Kinds helper, unit tests).
7. `intent` (YAML loader, schema validate, referential check, JCS →
   `gate_def_hash`, materialize inside `task.plan`; unit tests).
8. `evidence` (put/verify/get, dedupe, race regression test).
9. `verdict` (report/latest/history; requires intent + evidence + events).
10. `task` (plan/list/claim/heartbeat/release/complete; requires intent +
    verdict + events).
11. `cli` (envelope, exit-code mapping, flag parsing, integration tests).
12. `cmd/cairn` (cobra wiring; each `RunE` ≤10 LOC).
13. Dogfood scenario test, concurrency test, event-completeness CI.

Each step is one commit (or small series). No step depends on a later step.

Sanity gate before advancing to the next step:
- `go vet ./...` clean.
- `go test -race ./...` green.
- New `go.mod` dep (only two: `oklog/ulid/v2`, chosen JCS lib) has a
  comment on its `require` line explaining the use.

## 9. Explicitly deferred (out of Ship 1 scope)

- All of `PLAN.md` § "Explicitly deferred" applies transitively.
- `--format human` output mode.
- Memory + FTS5 (Ship 2).
- Reconcile command + rules 2/3/4/5 (Ship 2).
- `cairn task tree`, `verdict diff`, `events --watch`, or whichever Ship-3
  target is picked at end of Ship 2.
- Inputs-hash participation in staleness formula (may return in Ship 2/4
  when gate YAML can declare input globs).
- Content-type detection on `evidence put` (returns in the ship that has a
  filter-by-type consumer).
- Architectural lint (AST-based) enforcing the Store pattern. Convention +
  review in Ship 1; automate if bypass ever lands.
- Blob GC for orphans / op_log GC.
- Multi-host, Postgres, networked operation.

## 10. Success criteria

Ship 1 is done when, on a throwaway git repo:

1. `cairn init` succeeds; `state.db` + `blobs/` exist under the resolved
   state-root.
2. `cairn spec validate` on the Ship 1 fixture passes with
   `data:{errors:[]}`.
3. The Ship 1 dogfood scenario (§ 7a `TestShip1DogfoodEventCoverage`)
   executes end-to-end via the CLI, emits the full expected event set, and
   a spec edit flips `verdict latest` to `fresh:false`.
4. `TestConcurrentClaim` (§ 7a) passes on all matrix cells.
5. `go test -race ./...` green across the full matrix.
6. Network-isolated CI job green (Invariant 7).
7. Two deps added to `go.mod` (`oklog/ulid/v2` + chosen JCS lib), both with
   a one-line comment on their `require` line explaining use.

No Ship 2 features snuck in.
