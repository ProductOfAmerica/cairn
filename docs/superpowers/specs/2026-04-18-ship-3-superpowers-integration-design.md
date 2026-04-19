# Ship 3 — Superpowers integration + cairn dogfoods cairn (design)

> Status: Draft for user review.
> Date: 2026-04-18.
> Scope: PLAN.md §"Ship 3 — Superpowers integration + cairn dogfoods
> cairn" (week 3).
> Supersedes: PLAN.md §"Spec-format posture" Ship 3 bullet, §"Ship 3
> dogfood", §"Ship 3 — Superpowers integration" (narrowed skill list),
> §"Open risks" YAML-prose-divergence row. Amends PLAN.md explicitly
> per §11.

## 1. Scope + non-goals

**Ship 3 adds:**

Three skills under the cairn plugin:

- `using-cairn/` — hub `SKILL.md` + four flat sibling spokes:
  - `yaml-authoring.md`
  - `hash-placeholders.md`
  - `source-hash-format.md`
  - `code-reviewer-pattern.md`
  Hub contains explicit "when to read which spoke" routing, not
  just a table of contents.
- `subagent-driven-development-with-verdicts/SKILL.md` —
  composition-by-reference over
  `superpowers:subagent-driven-development`. Body = checkpoint
  delta table. Zero fork.
- `verdict-backed-verification/SKILL.md` —
  composition-by-reference over
  `superpowers:verification-before-completion`. Tracked-only (hard
  boundary). Body = checkpoint delta table.

One CLI feature (REQ-002):

- `cairn spec validate` response envelope extended with
  `specs_scanned: {requirements: N, tasks: M}`.
- `cairn spec init [--path specs/] [--force]` scaffolds
  `specs/requirements/REQ-001.yaml.example` +
  `specs/tasks/TASK-001.yaml.example` with annotated template
  bodies.
- Renamed-template detection: `specs/*/REQ-*.yaml` or
  `specs/*/TASK-*.yaml` containing the template marker comment
  fires validation error `kind: renamed_template`.

Five PLAN.md amendments land as a prep PR before Ship 3
implementation (matches Ship 2 pattern). See §11.

**Out of Ship 3 (deferred; tracked here for audit):**

- Wrapping `superpowers:brainstorming`,
  `superpowers:writing-plans`, `superpowers:test-driven-development`,
  `superpowers:requesting-code-review`, or any other Superpowers
  skill. Phase 2+ work.
- Resolution of `producer_hash` / `inputs_hash` semantics (Q1 of
  `docs/superpowers/ship-3-open-questions.md`). Provisional
  placeholders only in Ship 3; full resolution deferred.
- Multi-harness support (Cursor, Codex, Gemini, Copilot,
  OpenCode). Claude Code only.
- Automatic prose-to-YAML extraction as a cairn CLI command
  (`cairn spec derive`). Skill-layer derivation only in Ship 3.
- Any new event kinds. REQ-002 is read-side / filesystem-side;
  no new mutations.
- Schema migration. REQ-002 doesn't need one.
- Resolution of §5.5 re-stat error semantics — Ship 2
  open item tracked in `docs/superpowers/amendments-pending.md`.
  Not Ship 3 scope.
- Resolution of the four resolved items in `amendments-pending.md`
  (memory payload conditional, evidence dedupe event gating,
  content_type preservation, trigger coverage). These resolve
  via a parallel Ship 2 implementation-catchup PR — not Ship 3.
  Future readers diffing Ship 3 scope: those code changes are
  in that PR, not this one.

Ship 3 is **skills + small CLI feature**. No schema change. No
new mutations. No substrate behavior change.

## 2. Decision log (Q1–Q7 + bootstrap + forcing-test protocol)

| ID  | Decision |
| --- | -------- |
| Q1  | YAML authoring = **D hybrid**. Requirements YAML derives from brainstorming prose; task YAML derives from writing-plans prose. Conditional on C1/C2/C3 achievability — see §4.5 bail-out protocol. |
| Q2  | Ship 3 feature target REQ-002 = `cairn spec validate` envelope extension (`specs_scanned`) + `cairn spec init` scaffold. Absorbs two items from `docs/superpowers/ship-3-polish-notes.md` (silent-on-empty-tree, missing scaffold). |
| Q3  | `cairn:subagent-driven-development-with-verdicts` = **B composition-by-reference**. Body = checkpoint delta table over `superpowers:subagent-driven-development`. Zero fork. Table entries reference SP skill's numbered steps so version drift breaks loudly. |
| Q4  | `cairn:verdict-backed-verification` = **A tracked-only (hard)**. Invoked only when agent holds active cairn claim. If no claim, `using-cairn` routes to `superpowers:verification-before-completion` directly. Same composition-by-reference shape as Q3 wrap. |
| Q5  | Hash placeholders = **Path 1 provisional**. `producer_hash = sha256("ship3:" + gate.id + ":" + gate.producer.kind)`; `inputs_hash = sha256("ship3:" + run_id)`. Banner text fixed verbatim (§3.3). Replaced when Q1 of `ship-3-open-questions.md` resolves. |
| Q6  | Drift handling = **A prose canonical, regenerate on drift**. YAML header comment format: `# cairn-derived: source-hash=<sha256> source-path=<relative> derived-at=<ISO>`. Regeneration must be byte-identical (idempotent). Validation failures route to design dialog, not YAML error. |
| Q7  | Skill structure = **B hub-and-spoke**. Hub `SKILL.md` ≤100 lines, explicit "when to read which spoke" routing. Four flat sibling spokes. Isolation invariant: delete any spoke, hub + other spokes still function. |
| —   | Bootstrap = **a2**. REQ-002 YAML hand-authored for Ship 3 build. "Never types YAML" success criterion applies to Ship 4+ sessions, pinned in PLAN.md amendment (§11.3). |
| —   | C1 forcing test runs end of week 3 against a fresh feature design in `testdata/forcing-test/` (throwaway fixture, not cairn's canonical `specs/`). Question-count protocol: "distinct design decision = 1 question; clarifications on same decision = 0." Verbatim log at `docs/superpowers/ship-3-dogfood-elicitation-log.md`. |

## 3. Three skills structure

### 3.1 `using-cairn/SKILL.md` — hub

**Frontmatter:**

```yaml
---
name: using-cairn
description: Use when working in a repo that has cairn installed — teaches when to invoke cairn, which skills wrap which verification moments, and how YAML specs are derived silently from prose. Routes to spokes for deep topics (YAML authoring, hash placeholders, source-hash comment format, code-reviewer pattern).
---
```

**Body outline (≤100 lines):**

1. **What cairn is.** One-paragraph substrate summary. References
   `PLAN.md §"What this is / is not"`.

2. **When this skill applies.** Three-diamond flowchart:
   - Is there a `specs/` dir with cairn YAML? → YAML lifecycle
     applies (read spoke `yaml-authoring.md`).
   - Is agent about to verify completion inside an active cairn
     claim? → wrap routing applies.
   - Is a reviewer agent being dispatched against a rubric gate?
     → read `code-reviewer-pattern.md`.

3. **Wrap routing rules (key table):**

   | Situation | Use |
   |---|---|
   | Executing a plan via subagent dispatch inside a cairn-tracked repo | `cairn:subagent-driven-development-with-verdicts` |
   | Executing a plan outside a cairn-tracked repo (no `specs/`) | `superpowers:subagent-driven-development` |
   | Verification before claiming complete, while holding an active cairn claim | `cairn:verdict-backed-verification` |
   | Verification before claiming complete, no active cairn claim | `superpowers:verification-before-completion` |
   | Brainstorming, plan writing, test-driven development, receiving code review | `superpowers:*` originals unchanged. No cairn wrap. |

4. **YAML lifecycle (one paragraph + pointer):**

   > In a cairn-tracked repo, `specs/` YAML is always derived
   > from prose. The human never edits YAML directly. Requirements
   > YAML derives from `docs/superpowers/specs/*.md` (brainstorming
   > output); task YAML derives from `docs/superpowers/plans/*.md`
   > (writing-plans output). Derivation is deterministic and
   > byte-identical on re-run. See `yaml-authoring.md` for the
   > full protocol.

5. **Hash placeholders banner (one paragraph + pointer):**

   > When binding a verdict, `producer_hash` and `inputs_hash`
   > use provisional Ship 3 placeholders.
   >
   > **These hashes are placeholders. They do not reflect
   > toolchain version or input state. Verdicts bound with these
   > values are NOT safe to rely on for cross-run drift
   > detection.**
   >
   > See `hash-placeholders.md` for the recipe and the
   > future-replacement plan.

6. **Invocation rule:**

   > This skill MUST be invoked explicitly by the orchestrating
   > session after each of `superpowers:brainstorming` and
   > `superpowers:writing-plans` commits. Do not rely on
   > agent-noticing or auto-triggering — skill discipline failure
   > modes come from implicit invocation.

7. **Routing to spokes (explicit "when to load which"):**

   | Task you're about to do | Load first |
   |---|---|
   | Author or regenerate YAML from prose | `yaml-authoring.md` |
   | Compute `producer_hash` or `inputs_hash` for a verdict | `hash-placeholders.md` |
   | Read or write the `# cairn-derived:` comment | `source-hash-format.md` |
   | Dispatch `superpowers:code-reviewer` against a rubric gate | `code-reviewer-pattern.md` |

8. **Red flags (standard Superpowers idiom):**

   | Thought | Reality |
   |---|---|
   | "I'll just edit the YAML directly" | YAML is derived. Edit the prose; regeneration follows. |
   | "The prose spec is fine, skip the elicitation" | Elicitation checks for cairn-required fields not present in prose. Skipping = malformed YAML downstream. |
   | "Verdict is close enough without evidence" | Core Invariant 3 — no verdict without hash-verified evidence. |
   | "Agent said it's done, skip `cairn task complete`" | Core Invariant 10 — `cairn events since` must show the completion. |

### 3.2 `using-cairn/yaml-authoring.md` — spoke

**Body** (subheadings below are real markdown headings so
cross-references like "§3.2 gate-id stability protocol" resolve
to anchor ids):

#### D hybrid authoring flow

Narrative + sequence:

- After `superpowers:brainstorming` commits prose design spec →
  author `specs/requirements/REQ-NNN.yaml` (one per REQ
  identified in prose).
- After `superpowers:writing-plans` commits prose plan → author
  `specs/tasks/TASK-NNN.yaml` (one per plan task).
- Each authoring step runs in the agent's own transaction:
  derive → validate → commit.

#### Elicitation checklist

Fields cairn YAML needs beyond what prose contains.

For requirements:

- `id` — derived from prose section header, or ask human
  ("assign id like REQ-XXX").
- `title` — from prose H1.
- `why` — from prose "Why/Motivation" paragraph.
- `scope_in` / `scope_out` — from prose "Scope + non-goals"
  section. If prose has "all code in `internal/foo/`" →
  `scope_in: [internal/foo/**]`. If prose has no scope list →
  ask human: "What file globs does this requirement cover?"
- `gates[].kind` — prose "Testing" section usually says "unit
  tests + integration" → `kind: test`. "Code review" → `kind:
  rubric`. Ask human only if prose ambiguous.
- `gates[].producer.kind` / `config.command` — from prose
  "Testing" section test command. Default: `go test ./...` for
  Go repos. If prose says custom → use prose's command.
- `gates[].producer.config.pass_on_exit_code` — default `0`.

For tasks:

- `id` — derived from plan section numbering.
- `implements: [REQ-NNN]` — from the requirement being
  implemented. Plan task headers usually say "implements
  REQ-X".
- `depends_on: [TASK-NNN]` — from plan dependency graph.
- `required_gates: [AC-NNN]` — from which requirement gates
  apply to this task.

#### Gate-id stability protocol

Gate ids must be stable across regenerations. Primary source:
prose section labels. If prose labels a gate "AC-001: unit
tests pass," regeneration reads "AC-001" directly. If prose
doesn't label, the agent's first elicitation picks the id and
**writes it back into the prose spec** (e.g., appending a gate
definition section), not into the YAML alone. Future
regenerations read the same label from the prose. This is the
only way byte-identical regen survives elicitation variance.
Requirement ids follow the same rule.

#### Elicitation threshold (C1 constraint)

≤3 design-level questions per requirement. If a requirement's
derivation exceeds 3 questions, escalate to human with bail-out
language:

> "Elicitation exceeded 3 questions for REQ-NNN. Per Ship 3 C1
> constraint, flag this requirement for design-level rework
> before YAML emission."

Questions counted per §6.3 protocol (distinct design decision =
1; clarifications on same decision = 0).

#### Derivation rules — byte-identical regeneration

- Preserve key order: `id, title, why, scope_in, scope_out,
  gates` for requirements; `id, implements, depends_on,
  required_gates` for tasks.
- No timestamps inside YAML body. Only the `# cairn-derived:`
  header comment may contain a timestamp, and that comment is
  separate from the document hash (see `source-hash-format.md`).
- No ULIDs in deterministic slots. Requirement ids and gate ids
  come from prose or elicitation-with-writeback (see §3.2
  gate-id stability protocol), not from `internal/ids`.
- Empty lists: `scope_in: []`, never omitted.
- String values quoted consistently (default: unquoted unless
  contains `:` or `#`).

#### Validation-failure fallback

After authoring, run `cairn spec validate`. On failure:

- Parse error envelope. Extract `error.kind` + offending field.
- Translate to design question:
  - `invalid_gate_producer` → "The updated design introduces a
    gate (AC-NNN) without a producer command. Was this
    intentional? What command runs this gate?"
  - `unknown_required_gate` → "Task TASK-NNN requires gate
    AC-XXX, but AC-XXX is not defined in any requirement. Add
    AC-XXX to the design or remove the dependency."
  - `duplicate_requirement_id` → "The design uses REQ-NNN
    twice. Rename one in the design doc."
- Do NOT show the raw YAML or the raw error to the human.
- Loop: human answers → regenerate affected YAML → re-validate.

#### Commit discipline

- YAML commit is separate from the prose commit that triggered
  it.
- Prose commit message: `"design: <feature>"`.
- YAML commit message: `"specs: derive from <prose-path> at
  <short-sha>"`.
- YAML commit body includes the source prose's short sha so
  `git log --all -- specs/` shows the derivation trail.

### 3.3 `using-cairn/hash-placeholders.md` — spoke

**Body:**

1. **Banner (exact wording):**

   > These hashes are placeholders. They do not reflect
   > toolchain version or input state. Verdicts bound with these
   > values are NOT safe to rely on for cross-run drift
   > detection.

2. **Recipe:**

   ```
   producer_hash = sha256("ship3:" + gate.id + ":" + gate.producer.kind)
   inputs_hash   = sha256("ship3:" + run_id)
   ```

   Shell computation inside skill body:

   ```bash
   producer_hash=$(printf 'ship3:%s:%s' "$gate_id" "$producer_kind" \
       | sha256sum | cut -d' ' -f1)
   inputs_hash=$(printf 'ship3:%s' "$run_id" \
       | sha256sum | cut -d' ' -f1)
   ```

3. **Forbidden uses:**
   - MUST NOT be used as a staleness signal.
   - MUST NOT be compared across runs as evidence of input
     change.
   - MUST NOT be presented to humans as toolchain version.

4. **Replacement plan:**
   - Ship 3 post-dogfood check records a binary flag: "did
     anyone misread provisional hashes as meaningful?" Stored in
     `docs/superpowers/ship-3-dogfood-summary.md`.
   - If yes → accelerate Q1 of
     `docs/superpowers/ship-3-open-questions.md` to Ship 4 week
     1.
   - If no → Q1 stays queued until a concrete use case forces
     it.

### 3.4 `using-cairn/source-hash-format.md` — spoke

**Body:**

1. **Comment format (exact, first line of every derived YAML):**

   ```yaml
   # cairn-derived: source-hash=<sha256 of source prose file content> source-path=<repo-relative path> derived-at=<ISO 8601 UTC>
   ```

2. **Regeneration protocol:**
   - On skill invocation that may author or consume YAML: read
     header comment from each YAML file under `specs/`.
   - For each YAML: compute sha256 of the file at `source-path`.
     Compare to `source-hash`.
   - Mismatch → regenerate YAML from current prose; overwrite
     file including new header comment.
   - Missing or malformed comment → treat as stale; regenerate.

3. **Parser regex (single line, strict):**

   ```
   ^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$
   ```

   Timestamp anchor `Z` enforces UTC suffix.

4. **Whitespace path constraint (hard):** `source-path` MUST
   NOT contain whitespace. If a prose spec file path contains a
   space, the authoring skill errors before emission with the
   design question: "Prose spec path `<path>` contains
   whitespace, which cairn does not support. Rename the file
   (e.g., `kebab-case.md`) or relocate." No YAML is written
   until path is clean.

5. **Timestamp format.** ISO 8601 UTC with `Z` suffix, second
   precision. `2026-04-18T14:23:00Z`. Timestamp is for human
   inspection via `git blame`; not load-bearing for staleness
   detection.

### 3.5 `using-cairn/code-reviewer-pattern.md` — spoke

**Body:**

1. **Context.** `superpowers:code-reviewer` is a Superpowers
   agent (not a skill). Dispatched during SDD's two-stage
   review. PLAN.md Q8: no agent wrap in Ship 3; the pattern is
   documented instead.

2. **Pattern:**
   - Reviewer agent receives the task's rubric gate id
     (`AC-NNN`) and the run id from the dispatching
     orchestrator.
   - Reviewer agent performs review, produces verdict
     (pass/fail) + prose.
   - Reviewer agent shells out to:
     - `cairn evidence put <review-prose-path>` — stores review
       as evidence.
     - `cairn verdict report --gate <gate-id> --run <run-id>
       --status <pass|fail> --evidence <review-prose-path>
       --producer-hash <placeholder> --inputs-hash <placeholder>`
       — binds verdict.
   - Reviewer reports back to orchestrator with verdict id.

3. **Hash placeholders for the reviewer.** Same Ship 3
   convention as `hash-placeholders.md`. `producer.kind = human`
   for rubric gates.

4. **No wrap, no new agent.** The Superpowers `code-reviewer`
   agent stays unchanged. This spoke documents the shell-out
   pattern so callers of `code-reviewer` know what to pass and
   how reviewer integrates with cairn.

### 3.6 `subagent-driven-development-with-verdicts/SKILL.md` — wrap

**Frontmatter:**

```yaml
---
name: subagent-driven-development-with-verdicts
description: Use when executing an implementation plan inside a cairn-tracked repo. Composition over superpowers:subagent-driven-development — same dispatch flow, with cairn task claim / evidence / verdict / complete checkpoints inserted at named steps.
---
```

**Body structure:**

1. **Preamble.**

   > Follow `superpowers:subagent-driven-development` exactly.
   > Layer these cairn calls at the listed checkpoints. The
   > checkpoint ids reference numbered steps in the SP skill. If
   > SP renumbers in a future version, this table WILL break at
   > invocation time, which is correct — reconcile explicitly
   > rather than drifting silently.

2. **Checkpoint table (greppable, auditable):**

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

3. **Verdict-binding timing rationale.**

   > Verdict = pass/fail claim with evidence, bound only after
   > BOTH review passes. Work that spec-review or quality-review
   > rejects never gets a pass verdict in the log. Append-only +
   > latest-wins semantics preserved — no inconclusive
   > placeholder needed.
   >
   > If test gate output indicates failure (non-zero exit),
   > implementer reports DONE_WITH_CONCERNS or BLOCKED, not
   > DONE. No pass verdict ever binds for failing work.

4. **Gate output capture lifetime.** Between "Implementer
   reports DONE" (capture) and "Code quality reviewer approves"
   (verdict bind), captured output lives at
   `/tmp/cairn-gate-<run_id>-<gate_id>.out`.

5. **Non-reuse rule on crash-reclaim.** If the orchestrator
   crashes after capture but before verdict binding, the
   captured file MAY exist on disk but MUST NOT be reused. On
   reclaim (next agent picks up the task after the claim expires
   and gets re-claimed), the agent MUST re-run the gate to
   produce fresh output. Enforcement: verdict-binding step MUST
   parse the captured file's embedded `<run_id>` from its
   filename and compare to active `run_id`; mismatch → re-run
   gate, overwrite capture with current `run_id`, proceed.

6. **Hash placeholders.** Compute per `hash-placeholders.md`
   spoke. Do not improvise.

7. **Failure modes:**
   - `cairn task claim` returns `conflict` (another agent holds
     claim) → re-dispatch with exponential backoff OR escalate
     to human.
   - `cairn verdict report` returns `validation` with `kind:
     evidence_invalidated` → evidence was invalidated by a prior
     reconcile (Ship 2 §5.10 surface 1). Re-capture evidence,
     retry.
   - `cairn task complete` returns `validation` with `kind:
     gate_not_fresh` → at least one required gate's latest
     verdict is stale. Re-run gate, bind fresh verdict, retry
     complete.

8. **Red flags (delta from SP original):**

   | Thought | Reality |
   |---|---|
   | "Claim was held by another agent — proceed anyway" | NEVER. Conflict means two agents will step on each other. |
   | "Gate failed but the test is wrong" | Don't bind a pass verdict. Fix the gate definition in the prose spec, regenerate YAML, re-run. |
   | "The captured output file exists on disk — reuse it" | NEVER. Check `run_id` embedded in filename; mismatch = stale, re-run the gate. |

### 3.7 `verdict-backed-verification/SKILL.md` — wrap

**Frontmatter:**

```yaml
---
name: verdict-backed-verification
description: Use when about to claim work is complete inside an active cairn claim. Composition over superpowers:verification-before-completion — same Iron Law, with evidence put + verdict bound before the completion claim. Tracked-only. Without an active claim, use superpowers:verification-before-completion directly.
---
```

**Body structure:**

1. **Preamble (hard boundary).**

   > Invoke this skill ONLY while holding a cairn claim. If you
   > are not inside a cairn-tracked task, use
   > `superpowers:verification-before-completion` directly. This
   > skill will error if no active claim is in scope.

2. **Gate-function delta over SP V-B-C:**

   | SP V-B-C step | Cairn addition | Command |
   |---|---|---|
   | 1. IDENTIFY (what command proves this claim?) | no change | — |
   | 2. RUN (execute full command) | capture stdout+stderr to file | `<cmd> > /tmp/gate-output.txt 2>&1` |
   | 3. READ (full output, exit code, failures) | store as evidence | `cairn evidence put /tmp/gate-output.txt` |
   | 4. VERIFY (does output confirm claim?) — on PASS | bind verdict | `cairn verdict report --gate <id> --run <run_id> --status pass --evidence /tmp/gate-output.txt --producer-hash <placeholder> --inputs-hash <placeholder>` |
   | 4. VERIFY — on FAIL | bind fail verdict, don't claim complete | `cairn verdict report ... --status fail ...`; return to SP step 1 |
   | 5. CLAIM | emit JSON blob on stdout, single line | (see §3.7 claim wording) |

3. **Claim wording — machine-readable JSON blob.**

   Step 5 CLAIM emits a JSON blob on stdout, terminated by
   newline. Orchestrators parse; humans read the JSON directly
   (keys are self-documenting).

   ```json
   {"verdict_id":"VDCT_01H...","evidence_sha256":"9f3c...","status":"pass","gate_id":"AC-001","run_id":"RUN_01H..."}
   ```

   Format rules: compact JSON (no pretty-print), single line,
   keys in the order shown. Parser hint: the line begins
   `{"verdict_id":` — orchestrators can grep for this prefix to
   locate the claim.

   Prose status text ("verdict bound pass, evidence stored") is
   permitted before or after the JSON blob for human readability
   but MUST NOT replace the JSON. The JSON line is the
   load-bearing artifact.

4. **Iron Law (extended).**

   > NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE
   > **BOUND AS A CAIRN VERDICT**.

5. **Red flags (delta):**

   | Thought | Reality |
   |---|---|
   | "Skip the evidence put, the output was already captured" | NEVER. The verdict binding requires a cairn-stored evidence row. |
   | "Bind the verdict to a different run id to reuse evidence" | NEVER. Run id must match the active claim's run. |

## 4. End-to-end orchestration flow

Validates the "human never types YAML" criterion. One
continuous sequence from "user opens Claude Code" to "PR
merged." Human touchpoints marked **[H]**; cairn calls marked
**[C]**; skill invocations marked **[S]**; derivations marked
**[D]**.

### 4.1 Full sequence

```
Phase A — Design
  [H] user: "Let's add <feature>"
  [S] main session invokes superpowers:brainstorming
      [H] clarifying Qs, design approval
      [C] brainstorming commits docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md
  [S] main session invokes using-cairn explicitly after brainstorming returns
      (not "when noticed" — explicit chain)
      [S] using-cairn loads yaml-authoring.md spoke
      [D] derive specs/requirements/REQ-NNN.yaml from prose
      [C] cairn spec validate → passes or errors
          on error: translate to design question via §3.2 validation-failure fallback;
          loop back to brainstorming dialog
      [C] commit "specs: derive from <prose-path> at <short-sha>"
      elicitation check: if >3 design questions for a single REQ,
          hit C1 bail-out → escalate to human; do NOT emit YAML

Phase B — Plan
  [S] main session invokes superpowers:writing-plans
      [H] plan review
      [C] writing-plans commits docs/superpowers/plans/YYYY-MM-DD-<feature>.md
  [S] main session invokes using-cairn explicitly after writing-plans returns
      [S] using-cairn loads yaml-authoring.md spoke
      [D] derive specs/tasks/TASK-NNN.yaml from plan
      [C] cairn spec validate → passes or errors (same fallback)
      [C] cairn task plan → materializes tasks + gates
      [C] commit "specs: derive from <plan-path> at <short-sha>"

Phase C — Execute
  [S] cairn:subagent-driven-development-with-verdicts invokes
      per task:
        [C] cairn task claim <task_id> --ttl 30m
        dispatch implementer subagent (per SP SDD original)
        implementer: TDD, commits, captures gate output to
            /tmp/cairn-gate-<run_id>-<gate_id>.out, reports DONE
        dispatch spec reviewer subagent (per SP SDD original)
        dispatch quality reviewer subagent (per SP SDD original)
        on quality approve:
          [C] cairn evidence put <captured-output>
          [C] cairn verdict report --gate --run --status pass
                --evidence <path> --producer-hash <placeholder>
                --inputs-hash <placeholder>
          rubric gates: code-reviewer-pattern.md binds via agent shell-out
        [C] cairn task complete <claim_id>
      after all tasks:
        [C] cairn events since <session-start> (sanity check)
        dispatch final code-reviewer subagent (per SP SDD original)

Phase D — Ship
  [S] superpowers:finishing-a-development-branch invokes (unchanged)
      [H] PR review, merge
```

### 4.2 Human touchpoints

From §4.1, human emits:
- Design decisions during **[S] superpowers:brainstorming** —
  prose answers to design Qs.
- Elicitation answers during **[S] using-cairn** — responses to
  ≤3 design-level Qs per REQ, folded back into prose via §3.2
  gate-id stability protocol.
- Plan review during **[S] superpowers:writing-plans** — feedback
  on prose plan.
- PR review during Phase D — prose + code; no YAML.

Human does **not** emit: any YAML keystroke, any cairn command
argument, any hash value, any ULID, any gate/task id.

**Success criterion:** human authors or edits zero YAML. Viewing
YAML in a git diff, commit log, or PR diff during review is
acceptable and expected — auto-hiding the YAML from diff would
hide legitimate change signal. The test is about authoring
(keystrokes), not exposure (reading).

**Transcript validation:** grep session transcript for YAML file
opens in editor mode, direct `specs/*.yaml` edits, or manual
`sed`/`awk`/`vim` invocations against YAML. Finding any →
success criterion failed. Finding zero while `git diff` shows
the YAML → success criterion met.

### 4.3 Cairn command invocations (full inventory)

Per session, executed by agent (via skills), never by human:

| Phase | Command | Skill owner |
|---|---|---|
| A | `cairn spec validate` | using-cairn |
| A | `git add specs/requirements/ && git commit` | using-cairn |
| B | `cairn spec validate` | using-cairn |
| B | `cairn task plan` | using-cairn |
| B | `git add specs/tasks/ && git commit` | using-cairn |
| C | `cairn task claim` | SDD-with-verdicts |
| C | `cairn evidence put` | SDD-with-verdicts |
| C | `cairn verdict report` | SDD-with-verdicts |
| C | `cairn task complete` | SDD-with-verdicts |
| C | `cairn task release` (on BLOCKED) | SDD-with-verdicts |
| C | `cairn events since` | SDD-with-verdicts |
| C (V-B-V context) | `cairn evidence put`, `cairn verdict report` | verdict-backed-verification |
| C (reviewer context) | `cairn evidence put`, `cairn verdict report` | code-reviewer-pattern.md |

### 4.4 Derivation triggers

Every [D] in §4.1 fires under these conditions:

| Trigger | Regen target | Source |
|---|---|---|
| New prose design spec committed | `specs/requirements/*.yaml` | `docs/superpowers/specs/*.md` |
| New prose plan committed | `specs/tasks/*.yaml` | `docs/superpowers/plans/*.md` |
| Skill invocation detects source-hash mismatch | affected YAML | prose at `source-path` |
| Elicitation answer received | prose FIRST (via §3.2 gate-id stability protocol write-back), then YAML derivation | n/a |

### 4.5 Bail-out path (C1 fails)

If any single requirement's elicitation exceeds 3 design-level
questions:

```
  agent stops YAML emission for this REQ
  agent logs in docs/superpowers/ship-3-dogfood-elicitation-log.md:
    REQ-NNN: <count> questions asked, <verbatim list>
  agent surfaces to human:
    "Elicitation exceeded Ship 3 threshold for REQ-NNN.
     Per C1 constraint, this requirement needs a shape revisit
     before YAML emission. Consider: (a) split REQ into smaller
     pieces, (b) add explicit testing section to prose, (c) fall
     back to option B (plan-driven task YAML only) if the pattern
     repeats across REQs."
  Ship 3 merge criterion: if bail-out fires on the C1 forcing
  test, open an amendment to swap D→B before Ship 3 merges.
```

### 4.6 Error recovery summary

| Error | Surfaced by | Resolution |
|---|---|---|
| `cairn spec validate` failure | using-cairn yaml-authoring | Translate to design question; route back to brainstorming/plan dialog |
| `cairn task claim` conflict | SDD-with-verdicts | Exponential backoff retry OR escalate if persistent |
| `cairn verdict report` evidence_invalidated | SDD-with-verdicts | Re-capture evidence (re-run gate), retry |
| `cairn task complete` gate_not_fresh | SDD-with-verdicts | Re-run affected gate, re-bind verdict, retry complete |
| Orchestrator crash between capture and bind | verdict-binding step's `run_id` check | Verdict-binding step MUST parse the captured file's embedded `<run_id>` from its filename and compare to active `run_id`. Mismatch is the crash-detection signal → re-run gate, overwrite capture with current `run_id`, proceed. |
| Elicitation exceeds 3 Qs | using-cairn | C1 bail-out path §4.5 |
| Prose↔YAML drift during active session | using-cairn source-hash check | Silent regen from current prose |

## 5. REQ-002: `spec validate` envelope + `spec init` scaffold

The cairn-improves-cairn feature. Exercised through the new
skills during Ship 3 dogfood.

### 5.1 `cairn spec validate` envelope extension

**Current response:**

```json
{"errors": []}
```

**Extended response:**

```json
{
  "errors": [],
  "specs_scanned": {
    "requirements": 3,
    "tasks": 5
  }
}
```

**Behavior:**

- `specs_scanned.requirements` = count of files matching
  `specs/requirements/*.yaml` that the validator attempted to
  load (total file count).
- `specs_scanned.tasks` = count of files matching
  `specs/tasks/*.yaml` that the validator attempted to load.
- `.yaml.example` files (written by `spec init`; see §5.2) are
  NOT counted — they exist for human reference, not validation.
- Empty tree → `{"errors": [], "specs_scanned":
  {"requirements": 0, "tasks": 0}}`. Caller distinguishes
  empty-but-clean from "nothing scanned."

**Semantic clarification (documented in CLI help):**
`specs_scanned` counts files loaded, not files passed.
Cross-reference with `errors` for per-file status:
`len(errors) == 0` → all passed; `len(errors) > 0` → some
failed, others may have passed.

**No other behavior change.** Schema validation logic
untouched. Exit codes unchanged.

**Caller impact:** using-cairn's yaml-authoring spoke can now
branch on `specs_scanned.requirements == 0` vs. `errors == []`
cleanly. Prior behavior forced ambiguous handling.

### 5.2 `cairn spec init` scaffold

**New command:**

```
cairn spec init [--path specs/] [--force]
```

**Behavior:**

- Creates `<path>/requirements/` and `<path>/tasks/` directories
  if absent.
- Writes two annotated example files:
  - `<path>/requirements/REQ-001.yaml.example`
  - `<path>/tasks/TASK-001.yaml.example`
- If either `.example` file already exists, skip (idempotent).
  With `--force`, overwrite.
- Does NOT create actual `REQ-001.yaml` or `TASK-001.yaml` —
  the `.example` suffix is canonical; real YAML is authored by
  using-cairn from prose, not from the scaffold.

**Example template contents (literal strings embedded in
binary):**

Requirement template:

```yaml
# cairn requirement spec template — DO NOT EDIT THIS FILE.
# This file is scaffolding. Real requirement YAML is derived
# from prose design specs by the using-cairn skill. See cairn
# plugin docs for the full flow.
#
# Fields:
#   id          — stable identifier like REQ-NNN. Must match prose section label.
#   title       — short description matching prose H1.
#   why         — motivation paragraph from prose.
#   scope_in    — list of file globs this requirement covers.
#   scope_out   — list of file globs explicitly excluded.
#   gates       — acceptance gates (see below).
#
# Gate fields:
#   id          — stable identifier like AC-NNN.
#   kind        — one of: test, property, rubric, human, custom.
#   producer    — how this gate is produced:
#     kind      — one of: executable, human, agent, pipeline.
#     config    — producer-specific config.
id: REQ-001
title: Example requirement
why: Short motivation paragraph.
scope_in: [example/**]
scope_out: []
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [go, test, ./example/...]
        pass_on_exit_code: 0
```

Task template:

```yaml
# cairn task spec template — DO NOT EDIT THIS FILE.
# Real task YAML is derived from prose plan docs by the
# using-cairn skill. See cairn plugin docs.
#
# Fields:
#   id              — stable identifier like TASK-NNN.
#   implements      — list of REQ ids this task contributes to.
#   depends_on      — list of TASK ids that must complete first.
#   required_gates  — list of AC gate ids that must pass for this task.
id: TASK-001
implements: [REQ-001]
depends_on: []
required_gates: [AC-001]
```

**Response:**

```json
{
  "created": [
    "specs/requirements/REQ-001.yaml.example",
    "specs/tasks/TASK-001.yaml.example"
  ],
  "skipped": []
}
```

With `--force` and files already present:

```json
{
  "created": [
    "specs/requirements/REQ-001.yaml.example",
    "specs/tasks/TASK-001.yaml.example"
  ],
  "skipped": [],
  "overwritten": true
}
```

### 5.3 Renamed-template detection (hard convention enforcement)

`.yaml.example` is a hard convention, not a soft one.
Enforcement path:

1. `cairn spec validate` excludes `*.yaml.example` from its
   scan (extension filter).
2. Defensive check: if any `specs/requirements/*.yaml` or
   `specs/tasks/*.yaml` file's first non-blank line contains
   the marker comment `# cairn requirement spec template — DO
   NOT EDIT THIS FILE.` or `# cairn task spec template — DO NOT
   EDIT THIS FILE.`, validator emits error `kind:
   renamed_template` with message:

   > "This file appears to be a renamed scaffold template.
   > Templates are reference-only. To start authoring specs,
   > invoke the using-cairn skill to derive from a prose spec
   > under `docs/superpowers/specs/`."

3. Error exits 1 (validation). Does not regenerate or modify
   the file.

New error kind: `renamed_template` added to
`internal/cairnerr` constants.

### 5.4 Migration

**None.** Both changes are read-side (envelope extension) or
filesystem-side (scaffold writes). No SQL migration. No new
event kinds.

### 5.5 CLI help text

`cairn spec validate --help` gains:

```
  --path <dir>   Directory to scan (default: specs/).

Response includes:
  errors          List of validation errors (empty if all specs valid).
  specs_scanned   Object with counts of requirement/task files loaded.

specs_scanned counts files loaded, not files passed. Cross-reference
with errors for per-file status: len(errors) == 0 → all passed;
len(errors) > 0 → some failed, others may have passed.
```

`cairn spec init --help`:

```
Scaffold cairn spec directories with annotated templates.

Creates:
  <path>/requirements/REQ-001.yaml.example
  <path>/tasks/TASK-001.yaml.example

Real YAML is derived from prose specs by the using-cairn skill; these
templates are reference only. Do not rename .example files to .yaml.

Flags:
  --path <dir>   Target directory (default: specs/).
  --force        Overwrite existing .example files.
```

## 6. Ship 3 dogfood flow

### 6.1 Bootstrap (a2)

Day 1 of Ship 3 implementation:

1. **Author REQ-002 YAML by hand** — as human developer, not
   via skills. Ship 3's skills don't exist yet.
   File paths:
   - `specs/requirements/REQ-002.yaml` — one requirement with
     two gates (AC-001 test gate for envelope + init behavior,
     AC-002 rubric gate for documentation quality).
   - `specs/tasks/TASK-002-001.yaml` through
     `TASK-002-004.yaml` — four tasks covering: (1) envelope
     extension in `internal/intent`, (2) `spec init` CLI +
     template strings, (3) renamed-template detection, (4) CLI
     help text updates.

2. **Hand-author prose design + plan** for REQ-002 — these are
   the files the skills would normally emit, but in Ship 3
   they're the source of truth that YAML was derived from
   (conceptually). Files:
   - `docs/superpowers/specs/2026-04-XX-req-002-spec-validate-spec-init-design.md`
   - `docs/superpowers/plans/2026-04-XX-req-002-spec-validate-spec-init.md`

3. Commit bootstrap message:

   > "Ship 3 bootstrap gap (intentional): the three Ship 3
   > skills themselves are implemented without cairn verdict
   > tracking, because the wrap doesn't exist yet. Ship 3's
   > 'cairn is tracked by cairn' story begins at REQ-002
   > TASK-002-001, after the wrap is usable. This gap is a
   > build-order artifact, not a methodology claim."

### 6.2 Dogfood sub-task sequence

Week-3 work proceeds through TASK-002-001..004 using the newly-
built skills:

1. **Implement three skills first** (using
   `superpowers:subagent-driven-development`, unwrapped — Ship
   3 can't wrap until the wrap exists). Tasks:
   - Write `using-cairn/SKILL.md` + four spokes.
   - Write
     `subagent-driven-development-with-verdicts/SKILL.md`.
   - Write `verdict-backed-verification/SKILL.md`.
   The three skills are implemented without cairn tracking;
   dogfood-via-cairn starts at REQ-002.

2. **Implement REQ-002** via
   `cairn:subagent-driven-development-with-verdicts` on
   TASK-002-001..004. Each task exercises:
   - `cairn task claim`
   - implementer subagent (TDD per §3.6 flow)
   - spec + quality review
   - `cairn evidence put`
   - `cairn verdict report` (with ship3 placeholders)
   - `cairn task complete`

3. **Sanity-check event trail:**

   ```bash
   cairn events since <ship-3-branch-cut-timestamp> > events.log
   jq -r '.kind' events.log | sort -u > kinds.txt
   ```

   Expected kinds: all from Ship 1 + Ship 2 completeness sets +
   zero new (REQ-002 is read-side).

4. **Bind final cairn reviewer verdicts** on REQ-002's AC-002
   rubric gate per `code-reviewer-pattern.md`.

### 6.3 C1 forcing test

Run end of week 3, before Ship 3 merge.

**Target:** fresh feature designed for a hypothetical external
project. Fixture lives in `testdata/forcing-test/` — a
throwaway dogfood target, not cairn's canonical
`docs/superpowers/specs/` or `specs/`. Design prose + derived
YAML commit under that fixture subtree. Reconcile never sees
them. No orphaned specs.

**Protocol:**

1. Human opens new Claude Code session in cairn repo with Ship
   3 skills installed.
2. Human: "Let's design `<feature>`." Feature for a
   hypothetical external project; do not describe a real cairn
   feature.
3. Main session invokes `superpowers:brainstorming`, scoped to
   the fixture directory.
4. Human answers brainstorming Qs normally until design
   approval + prose commit (into fixture subtree).
5. Main session invokes `using-cairn` explicitly.
6. using-cairn runs elicitation. Agent logs EVERY question
   posed to human, verbatim, in
   `docs/superpowers/ship-3-dogfood-elicitation-log.md` with
   format:

   ```
   ## <timestamp> — REQ-NNN: <title>
   Q1 (distinct design decision — <what decision>): <verbatim>
   Q2 (clarification on Q1 — counts as 0): <verbatim>
   Q3 (distinct design decision — <what decision>): <verbatim>
   ...
   Total distinct design decisions: <N>
   ```

**Pass threshold:** total distinct design decisions ≤ 3 per
REQ.

**Counting protocol:**
- Distinct design decision → 1 count. Forces human to emit a
  new design choice.
- Clarification on same decision → 0 count. Same underlying
  choice, different framing.
- Example counted as 1: "What command runs this gate?"
- Example counted as 0 (same decision, clarified): "Do you mean
  `go test ./...`, `go test -race`, or `go test ./... -count=1`?"

**Outcomes:**

- **Pass** → Ship 3 merges. Log stays in repo as evidence.
- **Fail (any REQ > 3)** → open amendment to swap D→B. Do NOT
  attempt to rescue D. Land amendment, update using-cairn to
  derive tasks only from plan prose (requirements no longer
  derive from design prose). Re-run forcing test on updated
  flow before Ship 3 merge.

**Fixture posture:** the fixture files commit to repo as
historical evidence of the run. No cairn CLI commands execute
against the fixture's YAML — only the authoring/elicitation
phases are exercised. The fixture's existence is an invariant:
"Ship 3 forcing test evidence" not "specs cairn would ever
plan."

### 6.4 Post-dogfood summary

Record in `docs/superpowers/ship-3-dogfood-summary.md`. Full
done-when gate list in §9.

## 7. Event-log completeness invariant (Ship 3 position)

Ship 1 and Ship 2 established the invariant: every mutation
emits an event in the same transaction; `cairn events since
<ts>` covers every kind exercised.

**Ship 3 adds zero new event kinds.**

Reasoning:
- REQ-002 is read-side (`spec validate` envelope extension) +
  filesystem-side (`spec init` scaffold writes). Neither is a
  cairn-substrate mutation.
- Skills are orchestration-layer artifacts; they invoke
  existing cairn commands. All of these already have their
  mutation event kinds from Ship 1 + Ship 2.
- Hash placeholders are computed in-skill, not stored; no event
  change.
- Regeneration writes to `specs/*.yaml` via git, which is
  outside cairn's substrate surface. No event emitted by cairn.

**Extended event-log completeness CI test unchanged.** The
existing assertion set (Ship 1 + Ship 2 kinds) continues to be
the ground truth. Ship 3 implementation does not extend it.

**Future-ship signal:** if any Ship 3+ wrap discovers it needs
a new substrate mutation, that's a substrate-layer change, not
a skill-layer one — would land as its own ship scope, not
absorbed into Ship 3.

## 8. Testing

Ship 3 adds tests in four categories.

### 8.1 REQ-002 unit + CLI tests

| Package | Test names |
|---|---|
| `internal/intent` | `TestValidateEnvelopeEmpty`, `TestValidateEnvelopePopulated`, `TestValidateEnvelopeIgnoresExamples`, `TestValidateEnvelopeMixedValidInvalid`, `TestSpecValidateRejectsRenamedExample` |
| `internal/cli` | `TestSpecInitCreatesTemplates`, `TestSpecInitIdempotent`, `TestSpecInitForce`, `TestSpecInitCustomPath` |

**`TestValidateEnvelopeMixedValidInvalid` expected:** three
requirement files where one has a schema error →
`{errors:[{path:"...REQ-002.yaml",kind:"..."}],
specs_scanned:{requirements:3,tasks:0}}`. Counts attempts, not
passes.

**`TestSpecValidateRejectsRenamedExample` expected:**
`specs/requirements/REQ-001.yaml` containing the template
marker comment on first line → validation error with `kind:
renamed_template`, exit 1, message includes "using-cairn" hint.

### 8.2 YAML authoring discipline tests (fixture-capture pattern)

Agent invocation isn't scriptable from CI. Fixtures are
committed expected outputs; CI verifies structural invariants
over fixtures without re-running the agent.

**Structure:**

```
testdata/skill-tests/yaml-authoring/
├── stable-prose/              # byte-identical regen fixture
│   ├── design.md
│   ├── regen-a.yaml           # first derivation output
│   └── regen-b.yaml           # second derivation output; must cmp -s to regen-a.yaml
├── elicitation-writeback/     # elicitation-answer-folded-back fixture
│   ├── design-before.md       # prose before elicitation
│   └── design-after.md        # prose after elicited answer folded in; verify grep matches scripted answer strings
├── source-hash-valid/         # source-hash integrity fixture
│   ├── design.md
│   └── derived.yaml           # source-hash field equals sha256(design.md)
├── source-hash-drift/         # source-hash drift-detection fixture
│   ├── design.md              # edited after derivation
│   └── derived-stale.yaml     # source-hash field does NOT equal sha256(design.md); test asserts the inequality
└── validation-failure/        # design-question routing fixture
    ├── design-missing-producer.md
    └── expected-design-question.txt  # expected design-question string; asserts no raw error envelope leaks
```

Whitespace-path rejection (§3.4 constraint) is a skill-level
pre-emission check with no captured-output artifact. It's
exercised via §8.3 manual verification at merge time and
future agent-harness tooling in Ship 4+. The fixture structure
above does not include a whitespace-path subdir because there
is no static fixture that could represent the rejection — the
skill aborts before any file is written. The
fixture-hygiene check ("no committed fixture path under
`testdata/skill-tests/` contains whitespace") below is
unrelated: it enforces that the fixtures themselves follow the
same path-hygiene constraint they document.

**Two Make targets:**

- **`make test-skills-verify`** (runs in CI) — static checks
  over committed fixtures. Zero agent invocation.
  - Every `*.yaml` fixture parses as valid YAML.
  - Every `*.yaml` fixture's first line matches the source-hash
    comment regex.
  - For every fixture pair where both files exist under a
    `source-hash-valid/` dir: sha256(prose) equals
    `source-hash` field.
  - For every fixture pair under `source-hash-drift/`:
    sha256(prose) does NOT equal `source-hash`.
  - `stable-prose/regen-a.yaml` and `regen-b.yaml` are
    byte-identical (`cmp -s`).
  - `elicitation-writeback/design-after.md` contains scripted
    strings representing the elicited answer (grep).
  - `validation-failure/expected-design-question.txt` is
    non-empty and contains no raw error envelope substrings
    (`"kind":`, `"code":`).
  - No fixture path under `testdata/skill-tests/` contains
    whitespace.

- **`make test-skills-record`** (human-triggered, documented,
  not CI-gated) — regenerates fixtures by invoking the agent
  against the prose inputs. Writes results to the fixture dirs.
  Human inspects + commits. Procedure: Makefile target echoes
  step-by-step instructions (agent invocation + expected output
  verification); not fully automated.

**Regression detection flow:**

1. Skill changes land in a PR.
2. PR author runs `make test-skills-record` to regenerate
   fixtures.
3. PR author commits updated fixtures.
4. CI runs `make test-skills-verify` — passes because fixtures
   + checks are consistent.
5. If PR author forgets step 2, `make test-skills-verify` may
   still pass (fixtures are old-consistent) but dogfood summary
   check (§8.3 manual) catches that the skills no longer
   produce output matching the fixtures. Merge-gated.

**Limitation acknowledged:** This pattern does not detect skill
drift that produces output matching the fixture shape but with
different content. `make test-skills-record` requires human
diligence. Ship 4 may add agent-harness tooling if this
limitation surfaces pain.

### 8.3 Skill-level structural checks (manual, gated at merge)

Run once as part of Ship 3 merge-review checklist. Not
continuous regression — drift surfaces at next ship if
structure changes. `scripts/skill-lint/` tooling is Ship 4 work
if drift shows up.

Merge checklist items (executed by human reviewer or agent,
recorded in `docs/superpowers/ship-3-dogfood-summary.md`):

| Check | Method | Pass condition |
|---|---|---|
| Hub-spoke isolation | `rm hash-placeholders.md`, reload hub, verify hub + other three spokes function | All routing tables remain valid; hub loads; other spokes loadable independently |
| Checkpoint table greppable | `grep -E '^\|.*\|.*\|' subagent-driven-development-with-verdicts/SKILL.md` + visual inspection | Table rows present in expected format; every SP step anchor referenced matches a verbatim SP SDD step |
| Wrap routing boundary | Read using-cairn SKILL.md routing table; verify four cases present (tracked-execute, untracked-execute, tracked-verify, untracked-verify) mapping to correct skill FQN | All four present; FQNs resolve to existing skills |
| Hash placeholder recipe | `grep -F 'sha256("ship3:' hash-placeholders.md` | Exact recipe strings present verbatim per §3.3 |

Record `pass`/`fail` + one-line evidence per item in dogfood
summary. Failure on any → blocks merge until fixed.

### 8.4 Integration tests

Under `internal/integration/`:

| Test | Coverage |
|---|---|
| `TestSpecValidateEnvelopeE2E` | Full CLI invocation, JSON response matches §5.1 shape |
| `TestSpecInitE2E` | `cairn spec init` in fresh dir, `cairn spec validate` scans zero (`.example` excluded), confirms bootstrap fresh-repo path |
| `TestBootstrapE2E` | Simulate Ship 3 §6.1 bootstrap: hand-author YAML, `cairn task plan`, claim/verdict/complete cycle on a single task, events trail matches expected kinds |

### 8.5 CI matrix

Unchanged from Ship 2:
- Linux/Windows/macOS × Go 1.25.x.
- Offline CI runs on push to master (IPv6 disable workaround
  for `golang/go#76375`).
- Ship 3 adds no network deps.
- `make test-skills-verify` runs in the Linux job only (fixture
  checks are OS-agnostic; one OS suffices).

## 9. Done-when (consolidated exit criteria)

1. **Three skills land** in cairn plugin:
   - `using-cairn/SKILL.md` + `yaml-authoring.md` +
     `hash-placeholders.md` + `source-hash-format.md` +
     `code-reviewer-pattern.md` (5 files).
   - `subagent-driven-development-with-verdicts/SKILL.md` (1
     file).
   - `verdict-backed-verification/SKILL.md` (1 file).
   - Total: 7 skill files under the cairn plugin directory.

2. **REQ-002 implemented:**
   - `cairn spec validate` response envelope includes
     `specs_scanned: {requirements: N, tasks: M}`.
   - `cairn spec init [--path] [--force]` creates both
     `.yaml.example` templates.
   - Template marker detection: renamed `.yaml.example` →
     `.yaml` triggers `kind: renamed_template` validation
     error with using-cairn hint.
   - CLI help text for both commands documents the new
     behavior.

3. **Ship 3 dogfood executed:** REQ-002's four tasks all ran
   through `cairn:subagent-driven-development-with-verdicts`.
   Event trail via `cairn events since
   <ship-3-branch-timestamp>` shows expected kinds.

4. **C1 forcing test recorded:**
   - Log at
     `docs/superpowers/ship-3-dogfood-elicitation-log.md`,
     verbatim per §6.3 format.
   - Pass: all requirements ≤3 distinct design decisions.
   - Fail: swap D→B (amendment) before merge; re-run forcing
     test on updated flow.

5. **`make test-skills-verify` passes** for the `stable-prose/`
   byte-identical check (§8.2).

6. **`make test-skills-verify` passes** for the
   `source-hash-valid/` and `source-hash-drift/` fixtures
   (§8.2).

7. **All REQ-002 unit + CLI + integration tests pass** — full
   list in §8.1 and §8.4.

8. **All skill-level structural checks pass** — §8.3
   checklist, recorded in dogfood summary.

9. **Event-log completeness test unchanged and passing** — no
   new kinds (§7).

10. **Post-dogfood binary check recorded:** "Did anyone misread
    provisional hashes as meaningful during this session?"
    yes/no + one-line justification in dogfood summary. Yes →
    open issue to accelerate Q1 of
    `docs/superpowers/ship-3-open-questions.md` to Ship 4 week
    1.

11. **Matrix + offline CI green** — Linux/Windows/macOS × Go
    1.25.x.

12. **Five PLAN.md amendments + bootstrap pin landed as prep
    PR** before Ship 3 implementation PR — see §11 (amendments
    A–E) and §11.6 (bootstrap pin).

13. **Bootstrap gap documented** in the Ship 3 implementation
    PR's first commit message (per §6.1).

## 10. Lessons-learned audit (Ship 2 → Ship 3 carry-forward)

### 10.1 Still-applicable lessons

- **`docs/ship-1-lessons/go-deps-inline.md`** — no Go module
  dep added before its first import. Ship 3 adds no new Go deps
  (REQ-002 uses existing `yaml.v3`, `jsonschema`, `cobra`). No
  dep-tidy risk.
- **`docs/ship-1-lessons/modernc-sqlite-text-scan.md`** —
  `string` intermediate when scanning TEXT→`json.RawMessage`/
  `[]byte`. Ship 3 adds no new SQL queries. N/A but documented
  for carry-forward.

### 10.2 Ship 2 → Ship 3 carry-forward

- **Two-stage review pattern** (spec compliance + code quality)
  proven in Ship 1 and Ship 2. Ship 3 dogfood uses it via the
  new wrap. The wrap doesn't modify the pattern; it adds cairn
  checkpoints around it.
- **Store pattern** (`Store { tx *db.Tx }`) irrelevant to Ship
  3 — no new packages that wrap DB.
- **Ship 2 amendments-pending §5.5** (re-stat error semantics)
  stays pending. Not Ship 3 scope.
- **`cairn reconcile` called inline from `cairn task claim`** —
  Ship 1 behavior. Ship 3 wraps assume this; no change needed.

### 10.3 Expected new Ship 3 lessons (open for discovery during implementation)

- Skill-file length discipline (hub ≤100 lines, spokes
  focused). If a spoke bloats past ~200 lines during authoring,
  that's a signal to split further. Record as
  `docs/ship-3-lessons/skill-file-size.md` if encountered.
- Elicitation question drafting (how to write the checklist
  such that it produces ≤3 distinct decisions). Record as
  lesson if C1 forcing test reveals a pattern.
- Fixture-capture workflow friction. If `make
  test-skills-record` becomes cumbersome, record as signal that
  Ship 4 agent-harness tooling is worth the cost.

### 10.4 No new lesson file expected unless surprise

Per Ship 2 pattern. If implementation proceeds cleanly, no new
lesson file. If a surprise surfaces, capture as
`docs/ship-3-lessons/<topic>.md` in the implementation PR.

## 11. PLAN.md amendments (prep PR, before Ship 3 implementation)

Ship 2 workflow: amendments land as a separate prep PR before
the implementation PR. Rationale unchanged (Ship 2 §11): small
amendments are cheap to review standalone; mixing them into a
large implementation PR muddies review; semantic commits in git
log.

**Workflow:**

1. Branch `feature/ship-3-plan-amendments` cut off `master`.
2. Apply the amendments below in the order listed. Ordering
   matters for §11.2 and §11.5 because both touch the same
   PLAN.md bullet (the "Maybe wrap
   `superpowers:test-driven-development`" bullet). Apply
   **§11.5 first** — that amends the bullet in place. Apply
   **§11.2 second** — that inserts three new bullets after the
   (now-amended) bullet. Reversing the order leaves §11.2's
   anchor text referring to the pre-amendment bullet while the
   bullet itself has already changed. Full recommended apply
   order: §11.1 (A) → §11.5 (E) → §11.2 (B) → §11.3 (C) →
   §11.4 (D) → §11.6 (bootstrap pin).
3. Merge to master.
4. Rebase `feature/ship-3-superpowers-integration` on updated
   master.
5. Proceed to implementation plan + build.

### 11.1 Amendment A — §"Spec-format posture in the Superpowers ecosystem"

**Current Ship 3 bullet:**

> **Ship 3:** **additive sidecar**. Prose specs under
> `docs/superpowers/specs/*.md` stay exactly as Superpowers
> produces them. Cairn YAML lives alongside under
> `specs/requirements/*.yaml` and `specs/tasks/*.yaml`,
> hand-authored by the agent after the prose spec is approved.
> The `using-cairn` skill teaches the agent to emit both: prose
> for human review, YAML for machine verification.

**Amended:**

> **Ship 3:** **additive sidecar with silent derivation.**
> Prose specs under `docs/superpowers/specs/*.md` (brainstorming
> output) and `docs/superpowers/plans/*.md` (writing-plans
> output) stay exactly as Superpowers produces them. Cairn YAML
> lives alongside under `specs/requirements/*.yaml` (derived
> from design prose) and `specs/tasks/*.yaml` (derived from
> plan prose). Derivation is silent, deterministic, and
> byte-identical on re-run: the human never authors YAML
> directly. The `using-cairn` skill owns the authoring flow,
> elicitation checklist, and source-hash drift detection. See
> `docs/superpowers/specs/2026-04-18-ship-3-superpowers-integration-design.md`
> for the full protocol.

### 11.2 Amendment B — §"Ship 3 — Superpowers integration" bullet insertion

**Insertion after the existing "Maybe wrap
`superpowers:test-driven-development`" bullet (which is itself
amended per §11.5):**

> - **Ship 3 feature target REQ-002 locked:** `cairn spec
>   validate` response envelope extension (`specs_scanned:
>   {requirements, tasks}`) + new `cairn spec init` command
>   that scaffolds annotated `.yaml.example` templates. Absorbs
>   two items from
>   `docs/superpowers/ship-3-polish-notes.md`. Renamed-template
>   detection: `specs/*/REQ-001.yaml` containing the template
>   marker comment fires validation error `kind:
>   renamed_template`.
> - **Ship 3 bootstrap gap acknowledged:** the three Ship 3
>   skills themselves are implemented via unwrapped
>   `superpowers:subagent-driven-development` (the wrap doesn't
>   exist yet). Dogfood-via-cairn starts at REQ-002's four
>   sub-tasks, after the wrap is usable. This is a build-order
>   artifact.
> - **Provisional hash convention pinned:** `producer_hash =
>   sha256("ship3:" + gate.id + ":" + gate.producer.kind)`;
>   `inputs_hash = sha256("ship3:" + run_id)`. Placeholders
>   only — explicitly NOT safe for cross-run drift detection.
>   Replaced when Q1 of
>   `docs/superpowers/ship-3-open-questions.md` resolves. Full
>   banner + recipe in the `using-cairn` plugin's
>   `hash-placeholders.md` spoke.

### 11.3 Amendment C — §"Ship 3 dogfood — cairn improves cairn"

**Current step 4:**

> Agent (or user) hand-authors the cairn YAML sidecar:
> `specs/requirements/REQ-002.yaml` + 2–4
> `specs/tasks/TASK-00N.yaml` files. `cairn spec validate`
> passes. `cairn task plan` materializes. (Automatic
> prose-to-YAML extraction is deferred past Ship 4.)

**Amended step 4:**

> **Ship 3 build-session bootstrap (a2):** REQ-002 YAML is
> hand-authored one time as the bootstrap, because the
> `using-cairn` skill doesn't exist yet. This hand-authoring
> is a build-order artifact, NOT a methodology claim. The
> "human never types YAML" success criterion applies to Ship
> 4+ sessions, where every fresh feature design flows through
> `superpowers:brainstorming` → `using-cairn` derivation →
> `superpowers:writing-plans` → `using-cairn` derivation →
> `cairn:subagent-driven-development-with-verdicts`. Ship 3
> produces the skills that make this possible; Ship 4 is the
> first session where the criterion is actually testable
> end-to-end.

**Amended step 6 (first two sub-bullets):**

> Agent invokes
> `cairn:subagent-driven-development-with-verdicts` instead of
> the Superpowers original. For each REQ-002 sub-task
> TASK-002-001..004 (hand-authored during Ship 3 bootstrap;
> auto-derived in Ship 4+):

Remaining sub-bullets unchanged.

### 11.4 Amendment D — §"Open risks" row update

**Current row:**

> | YAML + prose divergence (sidecar posture) | Ship 3
> `using-cairn` teaches the agent to write both. If they
> diverge, the prose is canonical for humans; the YAML is
> canonical for cairn. Ship 4 may automate. |

**Amended:**

> | YAML + prose divergence (sidecar posture) | Ship 3
> `using-cairn` derives YAML from prose silently. Source-hash
> comment in YAML detects prose drift; regeneration is
> byte-identical. Prose is canonical end-to-end; YAML is a
> function of prose. Ship 4+ may revisit derivation rules if
> the C1 forcing test surfaces patterns. |

### 11.5 Amendment E — §"Ship 3 — Superpowers integration" TDD bullet

**Current:**

> - **Maybe wrap `superpowers:test-driven-development`** — only
>   if a clean insertion point exists where emitting a verdict
>   after RED-GREEN is natural. If it would require behavioral
>   changes to TDD discipline, skip and revisit in Ship 4.

**Amended:**

> - **Not wrapping `superpowers:test-driven-development` in
>   Ship 3.** Ship 3 brainstorm decided no TDD wrap: the
>   three-skills scope is tight enough, and TDD's RED-GREEN
>   discipline is orthogonal to cairn's
>   claim/verdict/complete cycle — wrapping would layer
>   ceremony without substrate benefit. Revisit in Phase 2+ if
>   a concrete use case surfaces (e.g., binding a verdict at
>   RED to freeze the failing test as evidence). Deferred, not
>   rejected.

### 11.6 Bootstrap pin (freestanding sentence in §"Before you code")

**Insertion after the existing "Do not start Ship 2 features
in Ship 1" sentence:**

> **Do not** treat Ship 3's hand-authored REQ-002 YAML as a
> contradiction of the "human never types YAML" criterion.
> Ship 3's build session is a bootstrap — skills don't exist
> yet. The criterion applies from Ship 4 onward, when the
> skills are present to enforce it.

### 11.7 No other PLAN.md edits

Scope stays frozen. §"Explicitly deferred", §"Dependencies",
§"SQLite schema", §"CLI surface", §"Reconciliation rules",
§"Staleness (binary)", §"Concurrency", §"Idempotency",
§"Memory (FTS5)", §"Event-log completeness invariant", §"Ship
1 — Core substrate", §"Ship 2 — Reconcile, memory", §"Ship 4
— Use it" — all unchanged by Ship 3.

## 12. Open for Ship 4+

Deferred items with explicit revisit triggers.

| Item | Trigger for revisit |
|---|---|
| `inputs_hash` / `producer_hash` semantics (Q1 of `docs/superpowers/ship-3-open-questions.md`) | Ship 3 post-dogfood binary check (§9.10) returns yes → accelerate to Ship 4 week 1. Otherwise: when a concrete staleness-use-case surfaces. |
| D→B fallback (YAML authoring mechanism) | C1 forcing test fails. Amendment lands before Ship 3 merge. |
| `scripts/skill-lint/` automation | If manual §8.3 structural checks catch drift repeatedly across ships, promote to CI tooling in Ship 4. |
| Agent-harness test tooling | If `make test-skills-record` friction surfaces during Ship 3 or early Ship 4 sessions. Would enable running §8.2 tests as true regression tests instead of fixture-capture. |
| Wrapping `superpowers:test-driven-development` | When a concrete use case surfaces (e.g., RED-state evidence binding). Current decision: not Ship 3. |
| Wrapping `superpowers:brainstorming` / `superpowers:writing-plans` | Not a current plan. Would require a reason to override the "don't modify SP behavior" posture. |
| Wrapping `superpowers:requesting-code-review` | Same as above. |
| Multi-harness support (Cursor, Codex, Gemini, Copilot, OpenCode) | After cairn proves out in Claude Code over several Ship 4+ iterations. |
| Automatic prose-to-YAML extraction as a cairn CLI command | If skill-layer derivation becomes painful across many sessions. Candidate: `cairn spec derive <prose-path>`. Not Ship 3. |
| §5.5 re-stat error semantics (from Ship 2 `amendments-pending.md`) | When a real I/O-error scenario surfaces in `cairn reconcile --evidence-sample-full`. Draft amendment, merge, delete `amendments-pending.md`. |
| Spoke file-size discipline (§10.3) | If spokes bloat past ~200 lines. Split further. Record as `docs/ship-3-lessons/skill-file-size.md`. |
| Elicitation checklist drafting (§10.3) | If C1 forcing test reveals a question-count pattern. Record as lesson. |
| Ship 3 polish-notes items not absorbed into REQ-002 | §"`cairn verdict` flag/positional inconsistency", §"`verdict latest --run` filter", §"`cairn task complete` ergonomic trap", §"`cairn task plan` inserted/updated/unchanged split" — each a candidate for Ship 4+ polish PRs. |
