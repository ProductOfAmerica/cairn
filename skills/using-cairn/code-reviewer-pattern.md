# Code-Reviewer Pattern

## 1. Context

`superpowers:code-reviewer` is a Superpowers agent (not a skill). Dispatched during SDD's two-stage review. PLAN.md Q8: no agent wrap in Ship 3; the pattern is documented instead.

## 2. Pattern

Reviewer agent receives the task's rubric gate id (`AC-NNN`) and the run id from the dispatching orchestrator.

Reviewer agent performs review, produces verdict (pass/fail) + prose.

Reviewer agent shells out to:
- `cairn evidence put <review-prose-path>` — stores review as evidence.
- `cairn verdict report --gate <gate-id> --run <run-id> --status <pass|fail> --evidence <review-prose-path> --producer-hash <placeholder> --inputs-hash <placeholder>` — binds verdict.

Reviewer reports back to orchestrator with verdict id.

## 3. Hash Placeholders

Same Ship 3 convention as `hash-placeholders.md` spoke. `producer.kind = human` for rubric gates.

## 4. No Wrap, No New Agent

The Superpowers `code-reviewer` agent stays unchanged. This spoke documents the shell-out pattern so callers of `code-reviewer` know what to pass and how reviewer integrates with cairn.
