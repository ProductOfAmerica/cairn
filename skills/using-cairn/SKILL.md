---
name: using-cairn
description: Use when working in a repo that has cairn installed — teaches when to invoke cairn, which skills wrap which verification moments, and how YAML specs are derived silently from prose. Routes to spokes for deep topics (YAML authoring, hash placeholders, source-hash comment format, code-reviewer pattern).
---

## What cairn is

Cairn is a verification substrate for AI-coordinated software development. See `PLAN.md §"What this is / is not"` for the full substrate summary. This skill teaches when to invoke cairn's three wrappers, how YAML specs derive silently from prose, and which spoke to load for specific tasks.

## When this skill applies

Three scenarios trigger this skill:

1. **YAML lifecycle** — Is there a `specs/` dir with cairn YAML? YAML derives from prose. Read spoke `yaml-authoring.md`.
2. **Wrap routing** — Is the agent executing an implementation plan inside a cairn-tracked claim? Route to the appropriate wrap.
3. **Code reviewer dispatch** — Is a reviewer agent being dispatched against a rubric gate? Read `code-reviewer-pattern.md`.

## Wrap routing rules

| Situation | Use |
|---|---|
| Executing a plan via subagent dispatch inside a cairn-tracked repo | `cairn:subagent-driven-development-with-verdicts` |
| Executing a plan outside a cairn-tracked repo (no `specs/`) | `superpowers:subagent-driven-development` |
| Verification before claiming complete, while holding an active cairn claim | `cairn:verdict-backed-verification` |
| Verification before claiming complete, no active cairn claim | `superpowers:verification-before-completion` |
| Brainstorming, plan writing, test-driven development, receiving code review | `superpowers:*` originals unchanged. No cairn wrap. |

## YAML lifecycle

In a cairn-tracked repo, `specs/` YAML is always derived from prose. The human never edits YAML directly. Requirements YAML derives from `docs/superpowers/specs/*.md` (brainstorming output); task YAML derives from `docs/superpowers/plans/*.md` (writing-plans output). Derivation is deterministic and byte-identical on re-run. See `yaml-authoring.md` for the full protocol.

## Hash placeholders banner

When binding a verdict, `producer_hash` and `inputs_hash` use provisional Ship 3 placeholders.

> These hashes are placeholders. They do not reflect toolchain version or input state. Verdicts bound with these values are NOT safe to rely on for cross-run drift detection.

See `hash-placeholders.md` for the recipe and the future-replacement plan.

## Invocation rule

> This skill MUST be invoked explicitly by the orchestrating session after each of `superpowers:brainstorming` and `superpowers:writing-plans` commits. Do not rely on agent-noticing or auto-triggering — skill discipline failure modes come from implicit invocation.

## Routing to spokes

| Task you're about to do | Load first |
|---|---|
| Author or regenerate YAML from prose | `yaml-authoring.md` |
| Compute `producer_hash` or `inputs_hash` for a verdict | `hash-placeholders.md` |
| Read or write the `# cairn-derived:` comment | `source-hash-format.md` |
| Dispatch `superpowers:code-reviewer` against a rubric gate | `code-reviewer-pattern.md` |

## Red flags

| Thought | Reality |
|---|---|
| "I'll just edit the YAML directly" | YAML is derived. Edit the prose; regeneration follows. |
| "The prose spec is fine, skip the elicitation" | Elicitation checks for cairn-required fields not present in prose. Skipping = malformed YAML downstream. |
| "Verdict is close enough without evidence" | Core Invariant 3 — no verdict without hash-verified evidence. |
| "Agent said it's done, skip `cairn task complete`" | Core Invariant 10 — `cairn events since` must show the completion. |
