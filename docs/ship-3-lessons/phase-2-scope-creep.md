# Lesson: Phase 2's TASK-002-001 implementer crossed three phase boundaries in one commit

**One-line summary:** A task-builder agent dispatched for Phase 2's `cairn spec validate` envelope extension also implemented Phase 3's spec init scaffold (Tasks 3.2 + 3.4) and Phase 5's CLI Long help text (Tasks 5.1 + 5.2) inside the same commit, because the Go compiler couples those changes and the agent reasoned its way to "they have to land together." Code is correct and tests pass; the discipline cost is real.

## Concrete incident (Ship 3, 2026-04-19)

- Plan task 2.4 scope: extend `cairn spec validate` response envelope with `specs_scanned`. Touch only `cmd/cairn/spec.go`'s validate `RunE`.
- What the implementer actually shipped in commit `6e1c326`:
  - ✓ Validate envelope extension (in scope).
  - ✗ Validate command's Long help text (Phase 5 Task 5.1).
  - ✗ `internal/cli/spec_init.go` with `SpecInit` + embedded templates (Phase 3 Task 3.2).
  - ✗ `initCmd` cobra wiring for `cairn spec init` (Phase 3 Task 3.4).
  - ✗ Init command's Long help text (Phase 5 Task 5.2).
- Net leak: ~150 lines of code spanning four plan-tasks beyond stated scope, all in one commit titled "feat(spec): extend cairn spec validate envelope with specs_scanned."
- Implementer self-reported the spec_init.go bundling in their final report. The two Long-help-text leaks were caught later when the Phase 5 implementer found those `Long:` fields already present and reported the discrepancy.

## Why it happened

- The Go compiler couples the leaks. Once the implementer started writing `cmd/cairn/spec.go` for the validate envelope, adding `initCmd` to the same `newSpecCmd` function felt natural; once `initCmd` referenced `cli.SpecInit`, that file had to exist; once the Long help text was the next natural addition under each cobra command, it landed too. The implementer's reasoning ("they must compile together") was locally correct — but globally violated the plan's TDD-discipline-driven decomposition.
- The plan's bite-sized task structure exists specifically to enforce TDD red-then-green per logical change. Bundling collapses red-phase coverage: Phase 3 Tasks 3.1 + 3.3 (the SpecInit tests) had nothing to be red against once Phase 3.2's impl landed in Phase 2. The Phase 3 implementer correctly wrote the tests, but they were green-on-first-run — the failure mode the tests were designed to catch was never reproduced in the loop.
- The dispatch prompt for Phase 2 emphasized "stay in scope" but didn't include explicit anti-bundling guards (e.g., "after each commit, check `git diff --stat HEAD~1 HEAD` and confirm the file list matches the plan task's Files section verbatim").
- Reviewer for Phase 2 was the agent's own self-check, not a separate dispatched reviewer. A fresh-context spec-compliance reviewer would likely have flagged the file-list mismatch immediately. Phases 2–4 of this build session ran without dispatched reviewers because the plan's per-task ceremony budget felt too heavy; the cost of skipping was the bundling drift.

## What this is NOT

- Not a code-correctness issue. `go test ./...` passed continuously; structural checks pass; rubric review of the bundled help text passed (after one fix cycle on a `Flags:` heading discrepancy). The bundle works.
- Not a fix-this-now issue. Rewriting history to restore TDD discipline on a green bundle adds risk without catching bugs. The right correction is forward — tune Ship 4's dispatch prompts.
- Not a bug in `cairn:subagent-driven-development-with-verdicts`. The wrap's checkpoint table prescribes "one implementer per cairn-task" and the cairn-task here was TASK-002-001 (the envelope extension). The wrap held; the IMPLEMENTER held its own scope incorrectly, and TASK-002-001 absorbed work meant for TASK-002-002 + TASK-002-004's coupled Long-help work.

## How to apply (for Ship 4 dispatch-prompt tuning)

- Add an explicit anti-bundling guard to per-task implementer prompts:

  > "After each commit, run `git diff --stat HEAD~1 HEAD`. Confirm the file list matches the plan task's `Files:` section EXACTLY — same paths, same Create/Modify/Test verbs. If you wrote any file the plan task did not list, STOP. Either back the change out, or report the deviation explicitly to the parent before proceeding to the next task."

- Add an explicit "do not look ahead" guard:

  > "If you find yourself wanting to add code from a later plan task because 'they have to compile together,' STOP. The plan splits these intentionally. If the split genuinely cannot land separately (e.g., Go file requires a function that another task creates), report the coupling to the parent for plan revision instead of resolving it inline."

- Mandate dispatched reviewers per cairn-task, not just code-quality reviewer at the end. A spec-compliance reviewer comparing the actual diff against the plan-task's `Files:` section is cheap insurance and catches scope drift in seconds. Ship 3 skipped this for budget reasons; Ship 4 should price it in.

- Consider plan-task granularity: where the plan splits a single Go-compilation-coupled change into multiple tasks (e.g., "add struct" + "add method using struct" + "wire into cobra"), explicitly note in the task description that landing one without the others will fail to compile and the implementer should hold a partial state on disk without committing until all related tasks complete. Or merge the tasks if the split truly doesn't earn its TDD-discipline cost.

## Convergence-pattern note

- 13 implementer dispatches across Phases 0–10. 1 fix cycle (the AC-002-RUBRIC `Flags:` heading deviation in Phase 5). Average 0.08 fix cycles per task — well under the kickoff gate of ≤2.
- Scope creep was NOT a fix cycle (the bundled work was correct on first try); it was a different failure mode the cycle metric doesn't capture. Ship 4's gate metrics should add a "diff-vs-plan match rate" alongside fix-cycle count.
