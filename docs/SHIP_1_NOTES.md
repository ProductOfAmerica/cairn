# Ship 1 — Handoff notes for the Ship 2 brainstorm

Status: Ship 1 complete. 46 commits on `feature/ship-1-substrate`. Matrix
CI green (Linux/Windows/macOS × Go 1.25.x). Offline job runs on push to
master (IPv6 disable workaround for `golang/go#76375`).

This document is a one-page handoff for the Ship 2 brainstorm session. It
captures what's worth carrying forward, what surprised us, and the open
questions that block the Ship 2 design.

## What worked in the dispatch pattern

- **Store pattern as a compile-time-ish invariant.** Domain Stores wrap
  `*db.Tx`, not `*db.DB`. Cross-domain calls construct the other Store
  inside the caller's transaction. This was spec-enforced, review-caught
  when an agent deviated in Task 8.2, and ended up uniform across 4 domain
  packages. Keep this for Ship 2.
- **Structured exit codes + fine-grained `error.code`.** The 0/1/2/3/4
  mapping, plus a `Kind` string inside `cairnerr.Err` that surfaces in the
  envelope, let tests and CLI scripts both branch on concrete conditions
  without parsing prose. Useful throughout.
- **Two-stage review (spec compliance, then code quality).** Caught two
  Critical bugs:
  1. Task 5.3's commit-time BUSY retry loop was dead code because
     `database/sql.Tx.Commit` atomically marks the tx done before calling
     the driver; a second `Commit()` returns `ErrTxDone` regardless.
  2. Task 8.2's first pass wrapped `*db.DB` instead of `*db.Tx`, which
     would have broken cross-domain same-txn calls in Ship 3+.
- **TDD per task.** Write a failing test, then the implementation, then
  commit. The test was often the load-bearing artifact — for integration
  slices like Phase 15 there was no new source code at all, and the tests
  still added real regression coverage.
- **Verbatim-from-plan for mechanical tasks.** Low-judgment tasks (helper
  types, thin cobra wrappers, schema JSON) went faster when the plan had
  the full code block and the prompt said "reproduce verbatim". Sonnet was
  reserved for integration / concurrency / design-judgment tasks.

## What surprised us during implementation

- **Go module tidy strips unused requires.** See `docs/ship-1-lessons/go-deps-inline.md`.
  Plan deviations followed: deps now land in the commit that first imports
  them, never earlier. Affected Tasks 0.1, 0.2, every subsequent phase that
  re-added a stripped dep.
- **`modernc.org/sqlite` returns TEXT as `string`.** See
  `docs/ship-1-lessons/modernc-sqlite-text-scan.md`. Reviewer's "remove this
  intermediate, it's redundant" suggestion broke Task 6.1's tests; reverting
  to the intermediate fixed it.
- **`database/sql` makes commit-time retry architecturally impossible.**
  After the first `Commit()` fails, the tx is terminated. The spec's
  §5b "commit-time BUSY keeps the tx open and retries" was corrected to
  "commit-time BUSY is handled at the SQLite C layer via `busy_timeout=5000`
  in the DSN". Go-level retry only covers begin and in-fn BUSY.
- **GH free-tier runner pools have inconsistent IPv6.** `proxy.golang.org`
  TLS handshake hangs indefinitely over IPv6 on some pools
  (`golang/go#76375`). Matrix jobs happened to land on working pools;
  offline job's pool did not. Same code, same commit, same `go.sum` hash.
  Fix: disable IPv6 on the runner before any Go network op.
- **Kernel-stalled syscalls don't honor `timeout-minutes`.** GH's step and
  job timeouts deliver signals; a process hung inside a blocked TCP connect
  syscall doesn't return to user space to receive them. Only runner VM
  teardown actually kills it.
- **`go` directive auto-bumps.** `go get` with a newer toolchain on
  `modernc.org/sqlite` bumped `go.mod`'s directive to `1.25.0` via
  transitive requirements. Plan targeted 1.24; CI matrix was updated.
- **The plan's "three-hash composition" stayed clean.** `gate_def_hash`
  (computed by cairn, read from `gates` at bind time), `producer_hash`
  (caller-supplied hex64), `inputs_hash` (caller-supplied hex64). Binary
  staleness on `gate_def_hash + status` alone proved sufficient for Ship 1.
  The open question is whether inputs_hash ever earns its place in the
  staleness formula — see below.

## Five open questions for Ship 2

1. **`inputs_hash` comparison semantics.** Ship 1 stores `inputs_hash` on
   every verdict but never compares it (binary staleness uses
   `gate_def_hash + status` only, per Decision §1.1). Ship 2 or later may
   want "inputs changed → stale" semantics. That requires either
   declaring input globs in gate YAML (so cairn can recompute a current
   inputs hash) or plumbing `--inputs-hash` into `cairn verdict latest`.
   Both have UX consequences. Which direction, if any?

2. **Reconcile rule thresholds.** `cairn reconcile` runs five rules. Ship 1
   only inlines rule 1 (expired leases) into `cairn task claim`. Ship 2
   adds the explicit `reconcile` command. Rule 3 (evidence probe) samples
   5% capped at 100; rule 4 (orphaned runs) uses "a configurable
   threshold". Neither threshold is pinned. Ship 2 needs concrete defaults
   plus a decision on whether they're overrideable via flags, env, or
   config file.

3. **Memory schema details.** The Ship 2 scope adds `memory_entries` +
   `memory_fts`. Questions: what's the full set of `kind` values
   (`decision|rationale|outcome|failure` per spec, or broader)? Does
   `entity_kind`/`entity_id` FK to the actual tables, or stay loose text
   fields? What does `cairn memory list` default to — newest-first, all
   kinds, limit 10? Is `--since` integer-ms only (matching `events since`)?

4. **`cairn replay --as-of`: command or query pattern?** Spec §"Explicitly
   deferred" removed the command and said the event-log query covers the
   use case. Ship 2 / 3 use will tell us whether that's true. If callers
   end up writing the same ad-hoc projection repeatedly, we add the command.
   The brainstorm should pick: wait-and-see, or commit now?

5. **Ship 3 target selection.** Plan §"Ship 2 — Reconcile, memory" says
   *pick one concrete small cairn-on-cairn improvement at the end of
   Ship 2, based on what is actually painful after Ship 1–2 use*.
   Candidates listed in the plan: `cairn task tree`, `verdict diff`,
   `events --watch`. Ship 2's brainstorm needs to surface what's painful
   from Ship 1 dogfood use and pick. Do not commit a Ship 3 target in the
   Ship 2 spec itself.

## Where to look

- Design spec: `docs/superpowers/specs/2026-04-17-ship-1-core-substrate-design.md`
- Plan: `docs/superpowers/plans/2026-04-17-ship-1-core-substrate.md`
- Phase 15 plan: `docs/superpowers/plans/2026-04-17-phase-15-e2e-tests.md`
- Original multi-ship plan: `docs/PLAN.md`
- Lessons: `docs/ship-1-lessons/`
