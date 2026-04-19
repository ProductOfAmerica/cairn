---
name: subagent-driven-development-with-verdicts
description: Use when executing an implementation plan inside a cairn-tracked repo. Composition over superpowers:subagent-driven-development — same dispatch flow, with cairn task claim / evidence / verdict / complete checkpoints inserted at named steps.
---

## 1. Preamble

Follow `superpowers:subagent-driven-development` exactly. Layer these cairn calls at the listed checkpoints. The checkpoint ids reference numbered steps in the SP skill. If SP renumbers in a future version, this table WILL break at invocation time, which is correct — reconcile explicitly rather than drifting silently.

## 2. Checkpoint table (greppable, auditable)

| SP SDD step (verbatim anchor) | Cairn addition | Command |
|---|---|---|
| Before "Read plan, extract all tasks" | Materialize plan tasks in cairn | `cairn spec validate && cairn task plan` |
| "Dispatch implementer subagent" (per-task) | Acquire claim | `cairn task claim <task_id> --agent <subagent-id> --ttl 30m --op-id <op>` |
| Implementer reports DONE | No cairn call. Capture gate output to `/tmp/cairn-gate-<run_id>-<gate_id>.out` for later. | (capture only) |
| Spec reviewer approves | No cairn call. | — |
| Code quality reviewer approves | **Bind test-gate verdicts for every required test gate** | `cairn evidence put <captured-output> && cairn verdict report --gate <id> --run <run_id> --status pass --evidence <captured-output> --producer-hash <placeholder> --inputs-hash <placeholder>` |
| (during quality review) | Bind rubric-gate verdicts via `code-reviewer-pattern.md` | (see spoke) |
| "Mark task complete in TodoWrite" | Complete the cairn claim | `cairn task complete <claim_id> --op-id <op>` |
| Implementer reports BLOCKED | Release claim | `cairn task release <claim_id> --op-id <op>` |
| After "all tasks complete" | Sanity check event trail | `cairn events since <session-start>` |

## 3. Verdict-binding timing rationale

Verdict = pass/fail claim with evidence, bound only after BOTH review passes. Work that spec-review or quality-review rejects never gets a pass verdict in the log. Append-only + latest-wins semantics preserved — no inconclusive placeholder needed.

If test gate output indicates failure (non-zero exit), implementer reports DONE_WITH_CONCERNS or BLOCKED, not DONE. No pass verdict ever binds for failing work.

## 4. Gate output capture lifetime

Between "Implementer reports DONE" (capture) and "Code quality reviewer approves" (verdict bind), captured output lives at `/tmp/cairn-gate-<run_id>-<gate_id>.out`.

## 5. Non-reuse rule on crash-reclaim

If the orchestrator crashes after capture but before verdict binding, the captured file MAY exist on disk but MUST NOT be reused. On reclaim (next agent picks up the task after the claim expires and gets re-claimed), the agent MUST re-run the gate to produce fresh output. Enforcement: verdict-binding step MUST parse the captured file's embedded `<run_id>` from its filename and compare to active `run_id`; mismatch → re-run gate, overwrite capture with current `run_id`, proceed.

## 6. Hash placeholders

Compute per `hash-placeholders.md` spoke. Do not improvise.

## 7. Failure modes

- `cairn task claim` returns `conflict` (another agent holds claim) → re-dispatch with exponential backoff OR escalate to human.
- `cairn verdict report` returns `validation` with `kind: evidence_invalidated` → evidence was invalidated by a prior reconcile (Ship 2 §5.10 surface 1). Re-capture evidence, retry.
- `cairn task complete` returns `validation` with `kind: gate_not_fresh` → at least one required gate's latest verdict is stale. Re-run gate, bind fresh verdict, retry complete.

## 8. Red flags (delta from SP original)

| Thought | Reality |
|---|---|
| "Claim was held by another agent — proceed anyway" | NEVER. Conflict means two agents will step on each other. |
| "Gate failed but the test is wrong" | Don't bind a pass verdict. Fix the gate definition in the prose spec, regenerate YAML, re-run. |
| "The captured output file exists on disk — reuse it" | NEVER. Check `run_id` embedded in filename; mismatch = stale, re-run the gate. |
