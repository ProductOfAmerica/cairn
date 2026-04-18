# Ship 2 Implementation Plan — Corrections

> Companion file to `2026-04-18-ship-2-reconcile-memory.md`. Lists
> divergences the implementation had to make, with the root cause
> (plan wrong vs. spec ambiguous) so future agents know where to look.
>
> **Root-cause legend:**
> - **P** = plan was wrong about implementation detail. Plan-only amendment.
> - **S** = spec was ambiguous. Plan *and* spec should be amended.

---

## 1. `h.DB()` → `h.SQL()` (all task test snippets)

**P.** The `*db.DB` handle's SQL accessor in Ship 1 is `SQL()`, not `DB()`.
Plan's verbatim test code used the wrong name; none of it would have
compiled. Mass-replaced across the plan at commit `e294db6` after Task
0.1 surfaced it.

Spec is silent on this — it's a pure Go API detail.

## 2. `seedEvidence` helper dedupes on sha256

**P.** Plan's helper (Task 8.1) used loop-local `i` in
`string(rune('a'+i))`. When a test calls `seedEvidence` twice, `i`
resets to 0, sha256 dedupes, and the second call's evidence rows
silently collapse into the first call's. Broke
`TestProbe_SampleRespectsCapAndPct`'s "seed 500 + 1500 = 2000" scenario.

Fix: package-level `seedCounter` in `probe_test.go`. Monotonic,
unique across calls.

Spec is correct — evidence IS content-addressed and MUST dedupe.
The plan's helper just didn't compose with its own schema guarantees.

## 3. `checkBlob` / `reStatInvalid` signature — add `error` return

**S.** Plan's Task 8.1 had `checkBlob(uri, expected) (string, bool)` —
any non-ErrNotExist open/read failure (EACCES, EMFILE, transient I/O)
was flattened to `"missing"`, which would cause rule 3 to invalidate
otherwise-healthy evidence.

Code quality review on Task 8.1 caught this. Fix landed at `b974f8d`:
`checkBlob(uri, expected) (string, bool, error)`. Rule 3's
`reStatInvalid` and the dry-run simulator's rule 3 inlining both adapted
to the 3-value return in Tasks 12.1 and 15.2.

**Spec also needs amendment.** `docs/superpowers/specs/2026-04-18-ship-2-reconcile-memory-design.md`
§5.5 "Re-stat invariant" documents the happy-path skip semantic but is
silent on error semantics. Recommend adding: *"If re-stat encounters
an I/O error that is NOT ErrNotExist (permissions, FD exhaustion,
read failure mid-stream), abort the reconcile — do not invalidate.
The file's real state is unknown and silently marking it invalid
would risk data loss."*

## 4. Gate hash drift via `scope_in` is a no-op

**S.** Plan's Task 17.2 subtest 1 instructed: "Edit `scope_in` entry to
drift `gate_def_hash`." This doesn't work — `internal/intent/hash.go::GateDefHash`
canonicalizes only `{id, kind, producer.kind, producer.config}`.
`scope_in` belongs to the *requirement*, not the gate, and doesn't
participate in gate_def_hash by design.

Fix at Task 17.2 (commit `661fefa`): drift the gate's `command` field
(`[echo, ok]` → `[echo, changed]`) instead. Mirrors the existing
Ship 1 test `TestShip1Dogfood_SpecEditFlipsStale`.

**Spec also needs amendment.** §Intent spec format / §Staleness should
enumerate *which fields contribute to `gate_def_hash`*. Current spec
treats gate_def_hash as opaque, but callers (including tests) need
to know how to cause it to change. Recommend adding a sentence under
§Staleness: *"`gate_def_hash` is computed from the gate's own fields
(id, kind, producer.kind, producer.config) — not from enclosing
requirement fields. Changes to `scope_in`, `why`, or `title` do NOT
drift the hash."*

## 5. Dead branch in `Store.List`

**P.** Task 4.1 plan code contained:
```go
if limit == 0 && (in.Limit != 0 || total == 0) {
    // explicit unlimited, or no rows at all
}
```
After `limit := in.Limit`, the inner condition is unreachable. Dead
marker. Removed in follow-up commit `c568a33`.

Spec unaffected. Plan should drop the block and, if needed, move the
explanation into a `//` comment on the `effective == 0 → 10` fallback
that actually handles the case.

## 6. `isExplicitUnlimited` is vestigial

**P.** Same Task 4.1 — always-false helper documenting the
library-vs-CLI contract. Functionally inert; harmless but confusing.
Left in place per "verbatim from plan" discipline; the CLI translates
`--limit 0` to `math.MaxInt32` regardless.

Plan amendment: delete the helper, replace with a comment on
`ListInput.Limit`.

## 7. Dead `n < 0` guard in `SampleSize`

**P.** Task 8.1 plan had:
```go
if n < 0 { n = 0 }
```
Unreachable (total ≥ 0, pct > 0, Ceil ≥ 0). Removed in `b974f8d`.

## 8. Manual anonymous interface in `RunEvidenceProbe`

**P.** Task 8.1 used `var rows interface { Next; Scan; Close; Err }` to
unify two `*sql.Rows` returns across the Full/sampled branches.
Simplified in `b974f8d` by composing the `args` vector conditionally
and falling through to a single `QueryContext` call. Idiomatic Go.

Spec unaffected.

## 9. CLI layer uses `*App`, not `appCtx` / `app.emit` / `app.db`

**P.** Plan Task 6.1 (memory CLI) and Task 16.1 (reconcile CLI) contained
sample cobra code referencing a hypothetical `appCtx` struct with
`app.opID`, `app.db`, `app.clock`, `app.ids`, `app.emit`, `app.blobRoot`.
None of those exist. Actual Ship 1 pattern (`cmd/cairn/main.go`):
```go
type App struct {
    Clock clock.Clock
    IDs   *ids.Generator
    Flags *cli.GlobalFlags
}
```
Plus helpers: `openStateDB(app *App) (*db.DB, error)` from `task.go`,
`cli.Run(writer, kind, opID, fn)` envelope wrapper from `cli/run.go`,
`app.Flags.ResolveOpID(app.IDs)` for op-id resolution.

Both implementation tasks (6.1 and 16.1) were dispatched with explicit
"plan sample code is stale — adapt to real Ship 1 patterns in
`cmd/cairn/task.go`" guidance.

Spec unaffected (it's an implementation convention).

## 10. `internal/cli/memory_*.go` helper files are overkill

**P.** Plan Task 6.1 listed three helper files under `internal/cli/`
(`memory_append.go`, `memory_search.go`, `memory_list.go`). Ship 1
convention is to inline the store-construction in each subcommand's
`RunE` directly in `cmd/cairn/*.go` (see `task.go`, `verdict.go`).
Adding helper files would have duplicated that abstraction for zero
gain.

Implementation dropped them. All three memory subcommands live inline
in `cmd/cairn/memory.go`. Same for `cmd/cairn/reconcile.go` in Task 16.1.

Plan amendment: drop the three `internal/cli/memory_*.go` entries
from the file-structure section.

## 11. `TestMigrate_Idempotent` regression in Ship 1

**P.** Not really a plan bug per se — Task 0.1's migration 002 adds a
second row to `schema_migrations`, and Ship 1's
`TestMigrate_Idempotent` hard-coded `count == 1`. Ship 1 test's real
invariant is "apply twice → no duplicate rows." Implementer refactored
it count-independent (capture `first`, compare `second == first`).
Fix lives alongside Task 0.1's commit `d210c7f`.

Plan should note that Ship 1 tests with row-count assertions need to
be checked against migration 002's additive changes.

## 12. SQLite WAL checkpoint required for snapshot/restore

**P.** Plan Task 17.5 protocol said "SNAPSHOT state.db to a temp file"
via `os.ReadFile`. Under WAL mode (cairn's default), uncheckpointed
pages live in `state.db-wal`, not the main file. Without a checkpoint,
the snapshot captures a stale view of committed state.

Fix in Task 17.5 (commit `27e6935`): issue
`PRAGMA wal_checkpoint(TRUNCATE)` before `os.ReadFile`. On restore,
remove `-wal` and `-shm` sidecar files so the next opener rebuilds
them from the fully-checkpointed main DB.

Spec unaffected. This is a correct-implementation-of-snapshot detail.

## 13. Rule 4 omitted from dry-run parity set

**P.** Plan Task 17.5 listed rule 4 as in-scope for parity. In practice,
rule 4 requires `claims.released_at + 10min < now()` — not driveable
in a test timeout under wall clock. Fake clock can't span the test
harness (subprocesses don't share fake clock state).

Implementation (commit `27e6935`) covers rules 1/2/3/5 in parity and
documents rule 4's omission inline. Rule 4's unit-level coverage
(`rule4_orphans_test.go`, Task 13.1) still exists.

Plan amendment: Task 17.5 should say "rules 1/2/3/5" not "all 5 rules."

## 14. Event payload carries empty strings for omitted entity fields

**S (minor).** `memory.Append` emits `memory_appended` events whose
payload contains `"entity_kind": "", "entity_id": ""` when the caller
omitted the entity pair. Downstream consumers (skills, reviewer
agents) must tolerate empty-string fields rather than expecting the
keys to be absent.

Not fixed — flagged in Task 3.1's quality review as a low-risk shape
lock-in. If Phase 4+ consumers start reading these events and
empty-string handling proves painful, revisit.

**Spec §6** should document the payload shape explicitly: either "keys
always present, empty string when absent" (current behavior) or
"omit the key entirely when absent." Pick one and note it in the
event-log invariant table.

---

## Summary table

| # | Correction | Root cause | Fix committed at | Spec needs amendment? |
| - | ---------- | ---------- | ---------------- | --------------------- |
| 1 | `h.DB()` → `h.SQL()` | P | `e294db6` | No |
| 2 | `seedEvidence` dedupe fix | P | `8948958` | No |
| 3 | `checkBlob` error propagation | S | `b974f8d` | Yes (§5.5) |
| 4 | `scope_in` doesn't drift hash | S | `661fefa` | Yes (§Staleness) |
| 5 | `Store.List` dead branch | P | `c568a33` | No |
| 6 | `isExplicitUnlimited` vestigial | P | — (left in) | No |
| 7 | `SampleSize` dead `n<0` guard | P | `b974f8d` | No |
| 8 | Manual interface in `RunEvidenceProbe` | P (style) | `b974f8d` | No |
| 9 | `*App` vs `appCtx` | P | In Tasks 6.1/16.1 | No |
| 10 | Helper files under `internal/cli/` | P | In Tasks 6.1/16.1 | No |
| 11 | `TestMigrate_Idempotent` regression | P | `d210c7f` | No |
| 12 | WAL checkpoint for snapshot | P | `27e6935` | No |
| 13 | Rule 4 omitted from parity set | P | `27e6935` | No |
| 14 | Event payload empty-string shape | S (minor) | — (flagged) | Yes (§6) |

**Spec amendments recommended:** #3 (§5.5 re-stat error semantics),
#4 (§Staleness gate_def_hash scope), #14 (§6 event payload shape).
File a follow-up PR after Ship 2 merges to land these.

**Plan amendments recommended:** items #5, #6, #9, #10, #13 should be
folded back into `2026-04-18-ship-2-reconcile-memory.md` either
inline (using `> [Amended]` markers) or via a future revision pass.
Items #1, #2, #7, #8, #11, #12 are already reflected in the working
code; plan amendments there are historical clarity only.
