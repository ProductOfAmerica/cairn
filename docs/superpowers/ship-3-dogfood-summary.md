# Ship 3 Dogfood Summary

> Date: 2026-04-19.
> Branch: feature/ship-3-superpowers-integration.

## REQ-002 dogfood event trail

`cairn events since 1776571917000` (bootstrap commit `6a6ea9a`) returned the
following distinct event kinds (39 total events):

```
claim_acquired
claim_released
evidence_stored
run_ended
run_started
task_status_changed
verdict_bound
```

Expected: union of Ship 1 + Ship 2 kinds. No new kinds (per design §7 invariant — Ship 3 is read-side/filesystem-side only). All four TASK-002-NNN tasks are status `done`.

## §8.3 Manual structural checks

| Check | Result | Evidence |
|---|---|---|
| Hub-spoke isolation | pass | Moved `hash-placeholders.md` to `/tmp`; hub frontmatter intact, 3 other spokes loadable, routing table in `SKILL.md` references the file but absence doesn't crash hub loading. Restored. |
| Checkpoint table greppable | pass | `grep -cE '^\| '` returns 14 (9 checkpoint rows + 1 header + 3 Red Flags rows + 1 Red Flags header) — exceeds ≥10 threshold. |
| Wrap routing boundary | pass | `grep -cE 'cairn:subagent...|superpowers:subagent...|cairn:verdict...|superpowers:verification...'` returns exactly 4 — all four FQNs present in `skills/using-cairn/SKILL.md` routing table. |
| Hash placeholder recipe | pass | `grep -cF 'sha256("ship3:'` returns 2 (`producer_hash` + `inputs_hash` lines in `skills/using-cairn/hash-placeholders.md`). |

## C1 forcing test

| Status | Details |
|---|---|
| deferred-pending-human-session | Autonomous build session — no fresh interactive Claude Code session available. Protocol documented; user runs before merging. |

Full protocol and empty results table: `docs/superpowers/ship-3-dogfood-elicitation-log.md`.

## §9.10 binary check — were placeholders ever misread as meaningful?

Answer: **no**.

Justification: during this Ship 3 build session, hash placeholders were used inside the wrap ceremony exactly as the spoke prescribes — `printf | sha256sum | cut` — and never compared across runs or presented to humans as toolchain version. The placeholder banner from the spoke did its job within the agent's context.

## Done-when checklist (§9 of design)

| # | Item | Status |
|---|---|---|
| 1 | Three skills land (7 files) | pass |
| 2 | REQ-002 implemented (envelope + init + renamed-template + help) | pass |
| 3 | REQ-002 dogfood executed via cairn:SDD-with-verdicts | pass |
| 4 | C1 forcing test recorded (≤3 distinct decisions per REQ) | deferred |
| 5 | `make test-skills-verify` passes for stable-prose | pass |
| 6 | `make test-skills-verify` passes for source-hash valid + drift | pass |
| 7 | All REQ-002 unit + CLI + integration tests pass | pass |
| 8 | All skill-level structural checks pass (§8.3) | pass |
| 9 | Event-log completeness test unchanged + passing | pass |
| 10 | Post-dogfood binary check recorded | pass |
| 11 | Matrix + offline CI green | pending CI — Linux SUCCESS, macOS SUCCESS, Windows and offline IN_PROGRESS at time of summary |
| 12 | Five PLAN.md amendments + bootstrap pin landed as prep PR | pass — commit `f7b17bb` (PR #6) on master before Ship 3 branch cut |
| 13 | Bootstrap gap documented in implementation PR's first commit | pass — commit `6a6ea9a` carries the verbatim §6.1 gap-acknowledgment message |

**Summary:** 10 pass, 1 deferred (#4 — C1 forcing test), 1 pending (#11 — CI in progress at summary time). No failures.
