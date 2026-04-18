# Pending spec amendments

> **Delete this file when the amendments below have landed.**
>
> Its presence is the signal to future sessions that unresolved
> ambiguities in the Ship 2 design spec still exist and have not been
> reconciled against real use.

Three amendments to `docs/superpowers/specs/2026-04-18-ship-2-reconcile-memory-design.md`
are queued for a follow-up PR. They are deliberately deferred until
post-merge canary feedback (an hour or two of real `cairn` use on a
toy task) so the amendment text is written against actual ambiguity
experienced, not drafted from implementation memory.

See `docs/superpowers/plans/2026-04-18-ship-2-implementation-plan-corrections.md`
for the full root-cause table. The three items flagged there with
**S** (spec ambiguous, not just plan wrong) are:

1. **§5.5 re-stat error semantics.** Spec documents the happy-path skip
   semantic but is silent on what to do when the re-stat itself fails
   (EACCES, EMFILE, transient I/O). Ship 2 implementation propagates
   the error and aborts reconcile; the spec should say so explicitly.

2. **§Staleness `gate_def_hash` scope.** Spec treats `gate_def_hash` as
   opaque, but callers (including tests) need to know which fields
   contribute. Gate's own `{id, kind, producer.kind, producer.config}`
   drive it; requirement fields like `scope_in`, `why`, `title` do not.
   Missing documentation caused Task 17.2 to initially test the wrong
   field and get a false negative.

3. **§6 event-log payload shape.** `memory_appended` event payloads
   carry `"entity_kind": "", "entity_id": ""` when the caller omitted
   the entity pair. Downstream consumers will have to tolerate this;
   alternatively the payload could omit the keys entirely. Either
   choice works; the spec should lock one and document it.

## Sequence

1. Wait for Windows + offline CI on PRs #2 and #3.
2. Merge #2 (PLAN.md prep), then #3 (Ship 2 implementation).
3. Use `cairn` on something small — an hour of real work, not weeks.
4. Draft the amendment PR from what you learned during that use.
5. Merge amendments; delete this file in the same commit.
6. Fresh session → Ship 3 brainstorm.

Do **not** start Ship 3 brainstorm while this file exists.
