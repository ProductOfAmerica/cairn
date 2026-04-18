# Ship 3 open questions — design-level input for the next brainstorm

Gaps surfaced during the 2026-04-18 canary that the Ship 2 spec
doesn't address. **Not ergonomic polish** (those live in
`ship-3-polish-notes.md`) — these are design-level ambiguities that
need a decision before a Ship 3 brainstorm can settle scope.

## Question 1 — Semantics of `producer_hash` and `inputs_hash`

**What the canary hit.** `cairn verdict report` requires
`--producer-hash <64-hex>` and `--inputs-hash <64-hex>`. The validator
accepts any lowercase 64-char hex string. No documented guidance on
what the caller should hash.

The canary's honest-agent guess:

- `producer_hash = sha256("gradle 9.4.0 junit 5.11.0 jdk-11 …")`
  (toolchain identity)
- `inputs_hash   = sha256(cat of related .java files)`
  (source-input identity)

Another honest agent on the same repo, same commit, same task would
almost certainly produce different hashes: different toolchain
version strings, different file ordering, different line-ending
normalization, different decisions about what "related" means.

**Why it matters.** The staleness signal stores these hashes on
every verdict but only `gate_def_hash` drives rule 2's binary
staleness. `producer_hash` and `inputs_hash` are currently
diagnostic-only fields. If Ship 3 or later wants to use
`inputs_hash` as a staleness driver (per PLAN.md §"Ship 2 — Reconcile,
memory" open question 1), the semantics must be pinned first, or
"did the inputs change?" becomes noise between agents.

**Two families of resolution:**

1. **Narrow spec.** Document a canonical shape. For example:
   - `inputs_hash = sha256(JCS(sorted [sha256 of each blob in
     requirement.scope_in glob]))` — deterministic, scope_in-driven.
   - `producer_hash = sha256(JCS(sorted [k=v entries from producer
     context]))` — agents must record what they decided to include.
   - Pros: deterministic, agent-portable.
   - Cons: locks agents into cairn's chosen scope definition; any
     scope change at the callsite breaks the hash's meaning.

2. **Helper commands.** Add `cairn inputs-hash <path>...` and
   `cairn producer-hash <key=value>...` that compute the canonical
   hash from the caller's input. Callers pass the result to
   `verdict report --inputs-hash $(cairn inputs-hash ...)`.
   - Pros: one canonical implementation, agents can't subtly diverge.
   - Cons: narrow definition may not fit every producer (human
     reviewer's `producer_hash`? an LLM's context?).

3. **Hybrid.** Document a canonical shape for the common cases
   (executable producer → file-based inputs_hash) and leave the
   fallback opaque for exotic cases (human producer, pipeline
   producer). Helper commands cover the common case.

**Preliminary lean, pending Ship 3 brainstorm.** If pushed for an
opinion today, option 3 (hybrid) feels least-bad: specify a
canonical shape for `executable`-kind producers (the only kind
Ship 1/2 use), add helper commands, leave the fallback opaque for
exotic producers (human, agent, pipeline) until they land. But this
is a lean, not a decision — the brainstorm should test the premise
before committing. Reasonable alternatives (including "do nothing
until Ship 4 use forces the question") should be surfaced during
brainstorm exploration, not filtered out by this note.

**Why this is Ship 3 brainstorm material, not polish.** Any
implementation direction narrows cairn's flexibility in ways that
need user discussion before coding starts. Not a 30-minute fix.

---

## Meta

This file is input for the NEXT brainstorm — not a commitment.
Ship 3 may choose to defer this entirely in favor of other scope
(CLI polish, Superpowers skill wrapping, dogfood-on-cairn, etc.).
The file records that the question exists and the canary produced
the friction to motivate it.

If this file is the only remaining open-question file when Ship 3
starts, consider whether Ship 3's focus IS hash semantics or whether
the more valuable brainstorm topic is something else entirely (Q5
from the Ship 2 brainstorm pointed at Ship 3 being "cairn dogfoods
cairn" — that's still waiting).
