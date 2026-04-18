# Pending spec amendments

> **Delete this file when the amendment below lands.**
>
> Its presence is the signal to future sessions that at least one
> spec ambiguity remains unresolved.

## Resolved (2026-04-18 post-canary amendment PR)

The canary session against `dreambot-scripts` surfaced evidence for
two of the three original post-Ship-2 items plus two new ambiguities
not anticipated at Ship 2 merge time. All four landed as amendments
to `docs/superpowers/specs/2026-04-18-ship-2-reconcile-memory-design.md` §13:

- **§13.A** — `memory_appended` event payload omits absent entity keys
  (not empty strings). Evidence: canary events 12 and 13.
- **§13.B** — `gate_def_hash` scope documented (gate subtree only;
  requirement-level fields don't drift). Evidence: canary re-plans
  with `why`/`scope_in`/`scope_out` edits left the hash stable at
  `8c2c9100…`.
- **§13.C** — `evidence_stored` emitted once per distinct sha256;
  dedupe paths are silent. Evidence: canary events 6 and 7 (same sha,
  two events).
- **§13.D** — Evidence `content_type` first-writer-wins on dedupe.
  Evidence: canary event 6 (`application/xml`) vs event 7
  (`application/octet-stream`) on same sha — silent data loss.

## Still pending — one amendment outstanding

**§5.5 re-stat error semantics.** Spec documents the happy-path skip
semantic but is silent on what to do when the re-stat itself fails
(EACCES, EMFILE, transient I/O, read failure mid-stream). The Ship 2
implementation propagates the error and aborts reconcile — correct
behavior — but the spec doesn't say so explicitly.

**Why still deferred.** The canary against dreambot-scripts did not
exercise a scenario where re-stat hits an I/O error mid-reconcile.
Drafting amendment text from implementation memory alone reproduces
the original "drafted-not-experienced" quality problem that the
post-canary pause was designed to avoid.

**Trigger for resolution.** A future canary (or real use) that forces
EACCES or a transient disk error on an evidence blob during
`cairn reconcile --evidence-sample-full`. Possible synthetic triggers:

- `chmod 000` on a blob file between probe and tx.
- Mount a read-only filesystem for the blob root mid-run.
- Use a bind-mount or Linux namespace that intermittently denies I/O.

Once a real scenario is documented, draft the §5.5 amendment with
concrete expected behavior (abort reconcile vs. partial-apply with
warning event, error kind surfaced to caller, etc.).

## Sequence (updated)

1. ~~Wait for Windows + offline CI on PRs #2 and #3.~~ Done.
2. ~~Merge #2 (prep), then #3 (Ship 2 implementation).~~ Done
   (`e11945a`, `fec9c9f`).
3. ~~Use cairn on something small — an hour of real work.~~ Done
   (JUnit decorator tests in `dreambot-scripts`).
4. ~~Draft the amendment PR from what you learned.~~ In progress
   (this PR: `feature/amendments-ship-2`).
5. **Current:** Merge this amendment PR; this file stays because
   §5.5 is still pending.
6. **Next:** implementation PR catching up the four resolved amendments
   (memory payload conditional, evidence dedupe event gating,
   evidence dedupe content_type preservation) + regression tests.
7. **Later:** when an I/O-error scenario arises, draft §5.5
   amendment, merge, delete this file.
8. Fresh session → Ship 3 brainstorm — **blocked while this file
   exists**.

Do **not** start Ship 3 brainstorm while this file exists.
