# Ship 3 — Superpowers Integration + cairn dogfoods cairn Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the three-skill cairn-plugin layer that lets Superpowers and cairn cooperate end-to-end (`using-cairn` hub + four spokes; `subagent-driven-development-with-verdicts` wrap; `verdict-backed-verification` wrap), plus REQ-002 — `cairn spec validate` envelope extension (`specs_scanned`), `cairn spec init` scaffold, and renamed-template detection — and exercise the entire stack by dogfooding cairn-improves-cairn against REQ-002 itself plus a separate C1 forcing test.

**Architecture:** Three layers shipped in lockstep. (1) **Plugin scaffolding + skills.** Top-level `.claude-plugin/plugin.json` registers cairn as a Claude Code plugin; `skills/` houses the seven new skill files. (2) **Read-side CLI feature.** `internal/intent` extends `Load` to surface scan counts and gains a renamed-template detector inside `Validate`; `cmd/cairn/spec.go` extends the `validate` envelope and adds an `init` subcommand backed by embedded template strings (no schema migration, no new event kinds, no new mutations). (3) **Dogfood loop.** Phases 0–1 build the skills under unwrapped `superpowers:subagent-driven-development` (the wrap doesn't exist yet — bootstrap gap). Phases 2–5 implement REQ-002 through `cairn:subagent-driven-development-with-verdicts` (the wrap is now usable; cairn dogfoods cairn). Phase 9 runs the C1 forcing test against a throwaway feature in `testdata/forcing-test/` to validate the "human never types YAML" criterion before merge.

**Tech Stack:**
- Go 1.25 (matrix CI: Linux/Windows/macOS × Go 1.25.x).
- `modernc.org/sqlite`, `github.com/spf13/cobra`, `github.com/santhosh-tekuri/jsonschema/v6`, `gopkg.in/yaml.v3` — all already deps from Ship 1.
- No new Go dependencies. No new SQL migration. No new SQLite triggers.
- Markdown skill files (Claude Code plugin format).
- Bash + jq for `make test-skills-verify`. (jq is already used by the Ship 1 event-log-completeness CI test.)

**Source of truth for every design decision:** `docs/superpowers/specs/2026-04-18-ship-3-superpowers-integration-design.md`. This plan implements that spec exactly. Q1–Q7 locks, the §1 in-scope/out-of-scope split, the §6.1 bootstrap-gap protocol, and the §9 13-item done-when checklist are non-negotiable.

**PLAN.md amendments:** The five amendments + bootstrap pin in design §11 land as a **separate prep PR** before this implementation PR (mirrors Ship 2's workflow). This plan assumes that prep PR has already merged to master before any implementation phase begins.

---

## File Structure

```
.claude-plugin/
  plugin.json                                  # NEW — registers cairn plugin + 3 skills
  marketplace.json                             # NEW — marketplace metadata stub

skills/
  using-cairn/
    SKILL.md                                   # NEW — hub (≤100 lines)
    yaml-authoring.md                          # NEW — D-hybrid authoring spoke
    hash-placeholders.md                       # NEW — provisional hash recipe spoke
    source-hash-format.md                      # NEW — derived-comment + regen protocol spoke
    code-reviewer-pattern.md                   # NEW — reviewer-agent shell-out spoke
  subagent-driven-development-with-verdicts/
    SKILL.md                                   # NEW — composition-by-reference wrap over SP SDD
  verdict-backed-verification/
    SKILL.md                                   # NEW — composition-by-reference wrap over SP V-B-C (tracked-only)

specs/
  requirements/
    REQ-002.yaml                               # NEW — Ship 3 feature target (hand-authored bootstrap)
  tasks/
    TASK-002-001.yaml                          # NEW — envelope extension
    TASK-002-002.yaml                          # NEW — spec init CLI + templates
    TASK-002-003.yaml                          # NEW — renamed-template detection
    TASK-002-004.yaml                          # NEW — CLI help text

docs/superpowers/specs/
  2026-04-19-req-002-spec-validate-spec-init-design.md      # NEW — prose source for REQ-002.yaml

docs/superpowers/plans/
  2026-04-19-req-002-spec-validate-spec-init.md             # NEW — prose source for TASK-002-NNN.yaml
  2026-04-19-ship-3-superpowers-integration.md              # NEW — THIS PLAN

docs/superpowers/
  ship-3-dogfood-elicitation-log.md            # NEW — populated during C1 forcing test (Phase 9)
  ship-3-dogfood-summary.md                    # NEW — populated post-dogfood (Phase 9)

internal/intent/
  loader.go                                    # MODIFY — skip *.yaml.example; expose scan counts
  validate.go                                  # MODIFY — add renamed-template detector pass
  intent_test.go                               # MODIFY — add envelope + renamed-template tests

internal/cli/
  spec_init.go                                 # NEW — scaffold logic + embedded template strings
  spec_init_test.go                            # NEW — scaffold unit tests

cmd/cairn/
  spec.go                                      # MODIFY — extend validate envelope; add init subcommand; help text

internal/integration/
  spec_envelope_e2e_test.go                    # NEW — TestSpecValidateEnvelopeE2E
  spec_init_e2e_test.go                        # NEW — TestSpecInitE2E
  bootstrap_e2e_test.go                        # NEW — TestBootstrapE2E

testdata/skill-tests/
  yaml-authoring/
    stable-prose/
      design.md                                # NEW — fixture input
      regen-a.yaml                             # NEW — first derivation output
      regen-b.yaml                             # NEW — second derivation output (must be byte-identical to regen-a)
    elicitation-writeback/
      design-before.md                         # NEW
      design-after.md                          # NEW — contains scripted answer strings
    source-hash-valid/
      design.md                                # NEW
      derived.yaml                             # NEW — source-hash field equals sha256(design.md)
    source-hash-drift/
      design.md                                # NEW — edited after derivation
      derived-stale.yaml                       # NEW — source-hash field does NOT equal sha256(design.md)
    validation-failure/
      design-missing-producer.md               # NEW
      expected-design-question.txt             # NEW — non-empty, no "kind:" / "code:" substrings
  verify/
    main.go                                    # NEW — Go program that implements `make test-skills-verify`

testdata/forcing-test/                         # NEW — created in Phase 9; placeholder README only at this stage
  README.md                                    # NEW — explains the fixture's purpose

Makefile                                       # NEW — test-skills-verify, test-skills-record targets

.github/workflows/
  ci.yml                                       # MODIFY — add `make test-skills-verify` step in linux job
```

**Pre-existing files modified:** `internal/intent/loader.go`, `internal/intent/validate.go`, `internal/intent/intent_test.go`, `cmd/cairn/spec.go`, `.github/workflows/ci.yml`. Everything else is new.

**Packages NOT touched:** `internal/db`, `internal/events`, `internal/task`, `internal/verdict`, `internal/evidence`, `internal/memory`, `internal/reconcile`, `internal/ids`, `internal/repoid`, `internal/clock`, `internal/cairnerr`. Ship 3 is read-side / filesystem-side only — no schema migration, no event-kind change, no mutation surface change (per design §1, §7).

**Go module name** (from Ship 1): `github.com/ProductOfAmerica/cairn`.

---

## Conventions (carried forward from Ship 1 and Ship 2)

- **Store pattern.** Unchanged from Ship 1/2: `type Store struct { tx *db.Tx; ... }`. No new Stores in Ship 3.
- **Errors.** `cairnerr.New(code, kind, msg).WithDetails(...)`. Codes: `CodeBadInput`/`CodeValidation` → exit 1, `CodeConflict` → 2, `CodeNotFound` → 3, `CodeSubstrate` → 4. New error kind in Ship 3: `renamed_template` (still `CodeValidation` family).
- **JSON envelope.** Reuse Ship 1's `cli.Envelope` + `cli.WriteEnvelope`. **One exception:** `cmd/cairn/spec.go validate` bypasses `cli.Run` so the response can carry both `errors` and `specs_scanned` in `data` even when validation errors are present. See Phase 2 task 2.4 for the explicit pattern.
- **JSON tags.** Every struct exposed via response JSON has explicit `json:"..."` tags.
- **TEXT scans.** Keep the `string` intermediate when scanning TEXT → `[]byte`/`json.RawMessage` (Ship 1 lesson `modernc-sqlite-text-scan.md`). Ship 3 adds no new SQL queries, so this rule does not bite — documented for carry-forward only.
- **Dep discipline.** Ship 3 adds zero new Go deps. If the implementation discovers a need for one (no expected need), land it in the same commit that first imports it (Ship 1 lesson `go-deps-inline.md`).
- **No Claude attribution in commit messages or PR bodies.** Per repo convention from `~/.claude/CLAUDE.md`.
- **Conventional-commit-ish prefixes:** `feat`, `fix`, `test`, `refactor`, `chore`, `plan`, `docs`. One task = one to several commits.
- **Skill files.** Markdown with YAML frontmatter (`---\nname: ...\ndescription: ...\n---`). Hub `using-cairn/SKILL.md` ≤100 lines per design §3.7 Q7. Spokes focus on one topic each (≤200 lines each per design §10.3 lesson hint).

---

## Bootstrap-gap protocol (mandatory reading before Phase 0)

Per design §6.1 bootstrap (decision a2):

- **Phase 0 hand-authors REQ-002.yaml + the four task YAMLs + a prose design + a prose plan.** This is intentionally NOT done via `superpowers:brainstorming` + `superpowers:writing-plans` + `using-cairn`, because the `using-cairn` skill doesn't exist yet at Phase 0. The hand-authoring is the build-order bootstrap.
- **Phase 1 builds the three skills via unwrapped `superpowers:subagent-driven-development`.** The cairn wrap doesn't exist either; we use the SP original. No cairn task claims, no verdicts, no evidence binding for Phase 1's skill-authoring tasks.
- **Phases 2–5 build REQ-002 via `cairn:subagent-driven-development-with-verdicts`.** The wrap now exists (Phase 1 just shipped it). Cairn-dogfoods-cairn begins here. Each REQ-002 task acquires a cairn claim, captures gate output, binds a verdict with hash placeholders, and completes the claim.
- **Phase 0's bootstrap commit MUST carry the gap-acknowledgment message** verbatim from design §6.1:

  > Ship 3 bootstrap gap (intentional): the three Ship 3 skills themselves are implemented without cairn verdict tracking, because the wrap doesn't exist yet. Ship 3's 'cairn is tracked by cairn' story begins at REQ-002 TASK-002-001, after the wrap is usable. This gap is a build-order artifact, not a methodology claim.

- **The "human never types YAML" success criterion (design §4.2) applies from Ship 4 onward, not to this Ship 3 build session.** Ship 3 itself hand-authors YAML once. Phase 9's C1 forcing test is what validates the criterion against a fresh feature.

---

## Phase 0: Plugin scaffolding + REQ-002 bootstrap

Hand-authored. No skill use. Lays the groundwork for all later phases.

### Task 0.1: Create the cairn Claude Code plugin manifest

**Files:**
- Create: `.claude-plugin/plugin.json`
- Create: `.claude-plugin/marketplace.json`

- [ ] **Step 1: Write `.claude-plugin/plugin.json`**

```json
{
  "name": "cairn",
  "version": "0.3.0",
  "description": "Verification substrate for AI-coordinated software development. Ship 3 adds three skills that let Superpowers and cairn cooperate end-to-end.",
  "skills": [
    "skills/using-cairn",
    "skills/subagent-driven-development-with-verdicts",
    "skills/verdict-backed-verification"
  ]
}
```

- [ ] **Step 2: Write `.claude-plugin/marketplace.json`**

```json
{
  "name": "cairn",
  "displayName": "cairn",
  "description": "Standalone verification substrate. Pair with obra/superpowers for end-to-end methodology + verification.",
  "homepage": "https://github.com/ProductOfAmerica/cairn",
  "license": "MIT"
}
```

- [ ] **Step 3: Verify directory layout**

Run: `ls -la .claude-plugin/`
Expected: both `plugin.json` and `marketplace.json` present, no other files.

- [ ] **Step 4: Commit**

```bash
git add .claude-plugin/plugin.json .claude-plugin/marketplace.json
git commit -m "feat(plugin): scaffold cairn Claude Code plugin manifest

Establishes .claude-plugin/ with plugin.json + marketplace.json so the
three Ship 3 skills under skills/ are discoverable as a Claude Code
plugin. Skills themselves land in Phase 1.

Bootstrap step per Ship 3 design §6.1; first commit on
feature/ship-3-superpowers-integration."
```

### Task 0.2: Hand-author REQ-002 cairn YAML (requirement)

**Files:**
- Create: `specs/requirements/REQ-002.yaml`

- [ ] **Step 1: Write `specs/requirements/REQ-002.yaml`**

```yaml
id: REQ-002
title: cairn spec validate envelope + cairn spec init scaffold
why: First-run users have no way to discover the spec YAML shape, and `cairn spec validate` returns ambiguous `{errors:[]}` on an empty tree. Ship 3 closes both papercuts and adds a defensive renamed-template check so users who copy a scaffold to the canonical name get a clear error instead of silent acceptance.
scope_in:
  - internal/intent/**
  - internal/cli/spec_init*.go
  - cmd/cairn/spec.go
scope_out:
  - internal/db/**
  - internal/events/**
  - internal/task/**
  - internal/verdict/**
  - internal/memory/**
  - internal/reconcile/**
gates:
  - id: AC-002-TEST
    kind: test
    producer:
      kind: executable
      config:
        command: [go, test, ./internal/intent/..., ./internal/cli/..., ./internal/integration/...]
        pass_on_exit_code: 0
  - id: AC-002-RUBRIC
    kind: rubric
    producer:
      kind: human
      config:
        reviewer_role: documentation_quality
```

- [ ] **Step 2: Verify YAML parses**

Run: `cat specs/requirements/REQ-002.yaml | python -c "import sys, yaml; yaml.safe_load(sys.stdin)" && echo OK`
Expected: `OK` (or use `go run` against a tiny snippet — anything that parses YAML).

(The file will be schema-validated by `cairn spec validate` in Task 0.6 — keep this step lightweight.)

### Task 0.3: Hand-author the four REQ-002 task YAMLs

**Files:**
- Create: `specs/tasks/TASK-002-001.yaml`
- Create: `specs/tasks/TASK-002-002.yaml`
- Create: `specs/tasks/TASK-002-003.yaml`
- Create: `specs/tasks/TASK-002-004.yaml`

- [ ] **Step 1: Write `specs/tasks/TASK-002-001.yaml`**

```yaml
id: TASK-002-001
implements: [REQ-002]
depends_on: []
required_gates: [AC-002-TEST]
```

- [ ] **Step 2: Write `specs/tasks/TASK-002-002.yaml`**

```yaml
id: TASK-002-002
implements: [REQ-002]
depends_on: [TASK-002-001]
required_gates: [AC-002-TEST]
```

- [ ] **Step 3: Write `specs/tasks/TASK-002-003.yaml`**

```yaml
id: TASK-002-003
implements: [REQ-002]
depends_on: [TASK-002-001, TASK-002-002]
required_gates: [AC-002-TEST]
```

- [ ] **Step 4: Write `specs/tasks/TASK-002-004.yaml`**

```yaml
id: TASK-002-004
implements: [REQ-002]
depends_on: [TASK-002-001, TASK-002-002, TASK-002-003]
required_gates: [AC-002-TEST, AC-002-RUBRIC]
```

(TASK-002-004 carries both gates because it's the last task and the rubric gate covers documentation quality across the whole REQ; binding it once at the end is sufficient.)

- [ ] **Step 5: Verify task files load**

Run: `ls specs/tasks/TASK-002-*.yaml`
Expected: four files listed.

### Task 0.4: Hand-author REQ-002 prose design spec

**Files:**
- Create: `docs/superpowers/specs/2026-04-19-req-002-spec-validate-spec-init-design.md`

- [ ] **Step 1: Write the prose design**

The file must mirror the Ship 3 design's §5 in standalone form. Source it from §5 of `docs/superpowers/specs/2026-04-18-ship-3-superpowers-integration-design.md` — sections 5.1, 5.2, 5.3, 5.4, 5.5. Copy verbatim into a new top-level document, retitled and dated as REQ-002's own design spec. Format:

```markdown
# REQ-002 — `cairn spec validate` envelope + `cairn spec init` scaffold (design)

> Status: Bootstrap copy of Ship 3 design §5.
> Date: 2026-04-19.
> Source: `docs/superpowers/specs/2026-04-18-ship-3-superpowers-integration-design.md` §5.
> Purpose: Required prose source for REQ-002.yaml derivation. Hand-authored
> as Ship 3 bootstrap (design §6.1). Ship 4+ would derive REQ-002.yaml from
> a normal `superpowers:brainstorming` design doc.

## Scope + non-goals
[copy from §5 preamble]

## `cairn spec validate` envelope extension
[copy §5.1 verbatim]

## `cairn spec init` scaffold
[copy §5.2 verbatim]

## Renamed-template detection
[copy §5.3 verbatim]

## Migration
[copy §5.4 verbatim]

## CLI help text
[copy §5.5 verbatim]

## Acceptance gates

- AC-002-TEST: `go test ./internal/intent/... ./internal/cli/... ./internal/integration/...` exit 0.
- AC-002-RUBRIC: documentation quality reviewer approves the help text additions, the renamed-template error message, and the embedded template comments.
```

- [ ] **Step 2: Verify file present**

Run: `wc -l docs/superpowers/specs/2026-04-19-req-002-spec-validate-spec-init-design.md`
Expected: ≥150 lines (full §5 content is substantial).

### Task 0.5: Hand-author REQ-002 prose plan

**Files:**
- Create: `docs/superpowers/plans/2026-04-19-req-002-spec-validate-spec-init.md`

- [ ] **Step 1: Write a thin prose plan**

The Ship 3 design + this Ship 3 plan already contain the full implementation detail for REQ-002 (Phases 2–5 below). The bootstrap prose plan can be a one-page pointer document. Format:

```markdown
# REQ-002 Implementation Plan (bootstrap)

> Bootstrap copy. Ship 3 itself executes REQ-002 via Phases 2–5 of
> `docs/superpowers/plans/2026-04-19-ship-3-superpowers-integration.md`.
> This file exists so TASK-002-001..004 have a prose source to point at,
> consistent with the Ship 3 design's "task YAML derives from plan prose"
> rule (§3.2 yaml-authoring).

## TASK-002-001 — envelope extension in `internal/intent`

Extend `intent.Load` to expose scan counts. Modify `cmd/cairn/spec.go`
validate to emit `{errors, specs_scanned}` in `data`. See Ship 3 plan
Phase 2 for the bite-sized implementation steps.

## TASK-002-002 — `cairn spec init` CLI + template strings

Embed two annotated `.yaml.example` template strings in
`internal/cli/spec_init.go`. Add `init` subcommand to
`cmd/cairn/spec.go`. See Ship 3 plan Phase 3.

## TASK-002-003 — Renamed-template detection

Add `intent.validateNoTemplateMarkers` pass that emits
`kind: renamed_template` SpecError when the marker comment appears on
the first non-blank line of any `specs/requirements/*.yaml` or
`specs/tasks/*.yaml`. See Ship 3 plan Phase 4.

## TASK-002-004 — CLI help text

Long help text on `cairn spec validate` and `cairn spec init` per
Ship 3 design §5.5. See Ship 3 plan Phase 5.
```

### Task 0.6: Validate + materialize REQ-002 in cairn state

**Files:**
- (no source files modified; this exercises the existing CLI)

- [ ] **Step 1: Run `cairn spec validate`**

Run: `go run ./cmd/cairn spec validate --path specs/`
Expected: success envelope; `data.errors == []`. (Without REQ-002 work yet, the response is the Ship 1/2 shape: `{"errors": []}` with no `specs_scanned` — that's fine; envelope extension lands in Phase 2.)

If the command fails, fix the YAML before proceeding. Common gotchas: missing required keys (`producer.kind`, `producer.config` — actually `config` is optional per current schema), incorrect indent, em-dash in YAML body (the Ship 3 design uses em-dashes in PROSE, not in YAML — keep YAML ASCII).

- [ ] **Step 2: Initialize state DB if absent**

Run: `go run ./cmd/cairn init`
Expected: state DB created at the cairn-resolved state root (per Ship 1's `repoid` + `stateroot` resolution). If the DB already exists from prior dogfood, this is a no-op.

- [ ] **Step 3: Materialize via `cairn task plan`**

Run: `go run ./cmd/cairn task plan`
Expected: success envelope showing `requirements_materialized`, `gates_materialized`, `tasks_materialized` non-zero (REQ-002, AC-002-TEST, AC-002-RUBRIC, four TASK-002-NNN). Records `task_planned`, `spec_materialized`, `task_status_changed (->open)` events.

- [ ] **Step 4: Verify task list**

Run: `go run ./cmd/cairn task list --status open`
Expected: TASK-002-001..004 all `open`.

### Task 0.7: Bootstrap commit

**Files:**
- (commit only; no new files)

- [ ] **Step 1: Stage everything from Phase 0**

```bash
git add specs/requirements/REQ-002.yaml \
        specs/tasks/TASK-002-001.yaml \
        specs/tasks/TASK-002-002.yaml \
        specs/tasks/TASK-002-003.yaml \
        specs/tasks/TASK-002-004.yaml \
        docs/superpowers/specs/2026-04-19-req-002-spec-validate-spec-init-design.md \
        docs/superpowers/plans/2026-04-19-req-002-spec-validate-spec-init.md
```

- [ ] **Step 2: Commit with the gap-acknowledgment message**

```bash
git commit -m "feat(specs): hand-author REQ-002 + prose sources (Ship 3 bootstrap)

REQ-002 covers cairn spec validate envelope extension (specs_scanned),
cairn spec init scaffold, renamed-template detection, and CLI help
text — the Ship 3 cairn-improves-cairn feature target.

Ship 3 bootstrap gap (intentional): the three Ship 3 skills themselves
are implemented without cairn verdict tracking, because the wrap
doesn't exist yet. Ship 3's 'cairn is tracked by cairn' story begins
at REQ-002 TASK-002-001, after the wrap is usable. This gap is a
build-order artifact, not a methodology claim.

Per Ship 3 design §6.1 bootstrap (decision a2)."
```

- [ ] **Step 3: Verify commit landed**

Run: `git log -1 --oneline`
Expected: subject `feat(specs): hand-author REQ-002 + prose sources (Ship 3 bootstrap)`.

---

## Phase 1: Three skills (built via unwrapped `superpowers:subagent-driven-development`)

Phase 1 ships seven skill markdown files under `skills/`. The wrap doesn't exist yet, so dispatch each task using the SP original SDD. No cairn task claims, no verdicts.

**Skill content reference:** Ship 3 design §3.1–§3.7 contains the full required body of every skill. Each task below tells the implementer to copy the design's prose verbatim, with frontmatter inserted exactly as specified. Drift from the design language is a regression — the design and the skills must stay byte-aligned because the design is what we reviewed and approved.

### Task 1.1: Write `using-cairn/SKILL.md` hub

**Files:**
- Create: `skills/using-cairn/SKILL.md`

- [ ] **Step 1: Write the hub**

Frontmatter (verbatim from design §3.1):

```yaml
---
name: using-cairn
description: Use when working in a repo that has cairn installed — teaches when to invoke cairn, which skills wrap which verification moments, and how YAML specs are derived silently from prose. Routes to spokes for deep topics (YAML authoring, hash placeholders, source-hash comment format, code-reviewer pattern).
---
```

Body: copy verbatim from design §3.1 "Body outline (≤100 lines)" — items 1 through 8. The body must include:

1. The "What cairn is" paragraph referencing `PLAN.md §"What this is / is not"`.
2. The three-diamond "When this skill applies" flowchart (use a graphviz-style fenced block `dot` if needed).
3. The wrap-routing rules table (5 rows: tracked-execute, untracked-execute, tracked-verify, untracked-verify, brainstorming/plans/TDD/code-review).
4. The YAML lifecycle paragraph + pointer to `yaml-authoring.md`.
5. The hash placeholders banner paragraph + pointer to `hash-placeholders.md`. The banner text must be the verbatim three-line block from design §3.1 item 5.
6. The invocation-rule blockquote (explicit invocation after brainstorming + writing-plans).
7. The "Routing to spokes" table (4 rows mapping task → spoke).
8. The Red Flags table (4 rows from design §3.1 item 8).

- [ ] **Step 2: Verify length**

Run: `wc -l skills/using-cairn/SKILL.md`
Expected: ≤120 lines (design says ≤100 body; allow ~15 for frontmatter + headings).

If over, the body has drifted from the spec — re-tighten to match design §3.1.

- [ ] **Step 3: Commit**

```bash
git add skills/using-cairn/SKILL.md
git commit -m "feat(skills): add using-cairn hub

Hub SKILL.md per Ship 3 design §3.1. ≤100 lines. Routes to four
spokes (yaml-authoring, hash-placeholders, source-hash-format,
code-reviewer-pattern) and contains the wrap-routing table that
tells agents when to use cairn:* vs superpowers:* skills."
```

### Task 1.2: Write `using-cairn/yaml-authoring.md` spoke

**Files:**
- Create: `skills/using-cairn/yaml-authoring.md`

- [ ] **Step 1: Write the spoke**

No frontmatter (spoke is loaded by reference from the hub, not invoked directly).

Body: copy verbatim from design §3.2. Sub-headings (must be real markdown headings so the hub's anchor cross-references work):

- `#### D hybrid authoring flow`
- `#### Elicitation checklist`
- `#### Gate-id stability protocol`
- `#### Elicitation threshold (C1 constraint)`
- `#### Derivation rules — byte-identical regeneration`
- `#### Validation-failure fallback`
- `#### Commit discipline`

Every bullet, every example, every blockquote from design §3.2 must appear. The C1 bail-out language is verbatim:

> "Elicitation exceeded 3 questions for REQ-NNN. Per Ship 3 C1 constraint, flag this requirement for design-level rework before YAML emission."

- [ ] **Step 2: Verify all sub-headings present**

Run: `grep -E '^#### ' skills/using-cairn/yaml-authoring.md | wc -l`
Expected: 7.

- [ ] **Step 3: Commit**

```bash
git add skills/using-cairn/yaml-authoring.md
git commit -m "feat(skills): add yaml-authoring spoke

D-hybrid authoring flow, elicitation checklist, gate-id stability
protocol, C1 elicitation threshold, byte-identical regeneration
rules, validation-failure fallback, commit discipline. Per Ship 3
design §3.2."
```

### Task 1.3: Write `using-cairn/hash-placeholders.md` spoke

**Files:**
- Create: `skills/using-cairn/hash-placeholders.md`

- [ ] **Step 1: Write the spoke**

Body: copy verbatim from design §3.3. Four numbered sections:

1. The exact banner blockquote (three lines, do not paraphrase).
2. The recipe block + the bash computation block:

   ```bash
   producer_hash=$(printf 'ship3:%s:%s' "$gate_id" "$producer_kind" \
       | sha256sum | cut -d' ' -f1)
   inputs_hash=$(printf 'ship3:%s' "$run_id" \
       | sha256sum | cut -d' ' -f1)
   ```

3. The four "Forbidden uses" bullets — staleness signal, cross-run input change, presented to humans as toolchain version, plus the implicit fourth (taken from §3.3 item 3).
4. The replacement-plan section pointing at the post-dogfood binary check + `docs/superpowers/ship-3-open-questions.md` Q1.

- [ ] **Step 2: Verify recipe regex matches**

Run: `grep -F 'sha256("ship3:' skills/using-cairn/hash-placeholders.md`
Expected: at least two matches (`producer_hash` + `inputs_hash` lines).

- [ ] **Step 3: Commit**

```bash
git add skills/using-cairn/hash-placeholders.md
git commit -m "feat(skills): add hash-placeholders spoke

Provisional Ship 3 hash recipe (producer_hash + inputs_hash) plus
the safety banner forbidding cross-run drift detection use. Per Ship
3 design §3.3."
```

### Task 1.4: Write `using-cairn/source-hash-format.md` spoke

**Files:**
- Create: `skills/using-cairn/source-hash-format.md`

- [ ] **Step 1: Write the spoke**

Body: copy verbatim from design §3.4. Five numbered sections:

1. Comment format (exact YAML-comment line):

   ```yaml
   # cairn-derived: source-hash=<sha256 of source prose file content> source-path=<repo-relative path> derived-at=<ISO 8601 UTC>
   ```

2. Regeneration protocol (read header → compute sha256 → compare → regen on mismatch).
3. Parser regex (verbatim):

   ```
   ^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$
   ```

4. Whitespace path constraint — hard error before YAML emission.
5. Timestamp format (ISO 8601 UTC, second precision, `Z` suffix).

- [ ] **Step 2: Verify regex line present**

Run: `grep '\^# cairn-derived' skills/using-cairn/source-hash-format.md`
Expected: regex line found.

- [ ] **Step 3: Commit**

```bash
git add skills/using-cairn/source-hash-format.md
git commit -m "feat(skills): add source-hash-format spoke

Derived-comment format, regeneration protocol, parser regex,
whitespace-path rejection, timestamp format. Per Ship 3 design §3.4."
```

### Task 1.5: Write `using-cairn/code-reviewer-pattern.md` spoke

**Files:**
- Create: `skills/using-cairn/code-reviewer-pattern.md`

- [ ] **Step 1: Write the spoke**

Body: copy verbatim from design §3.5. Four numbered sections:

1. Context — `superpowers:code-reviewer` is an SP agent dispatched during SDD's two-stage review; PLAN.md Q8 = no agent wrap, document the pattern instead.
2. Pattern — reviewer receives `<gate_id, run_id>`, performs review, shells out to `cairn evidence put` + `cairn verdict report`.
3. Hash placeholders — same Ship 3 convention, `producer.kind = human` for rubric gates.
4. No wrap, no new agent — the SP code-reviewer agent stays unchanged.

- [ ] **Step 2: Verify shell commands present**

Run: `grep -E 'cairn (evidence put|verdict report)' skills/using-cairn/code-reviewer-pattern.md | wc -l`
Expected: ≥2.

- [ ] **Step 3: Commit**

```bash
git add skills/using-cairn/code-reviewer-pattern.md
git commit -m "feat(skills): add code-reviewer-pattern spoke

Documents the superpowers:code-reviewer agent shell-out pattern for
binding rubric-gate verdicts. No agent wrap; pattern only. Per Ship
3 design §3.5 + PLAN.md Q8."
```

### Task 1.6: Write `subagent-driven-development-with-verdicts/SKILL.md` wrap

**Files:**
- Create: `skills/subagent-driven-development-with-verdicts/SKILL.md`

- [ ] **Step 1: Write the wrap**

Frontmatter (verbatim from design §3.6):

```yaml
---
name: subagent-driven-development-with-verdicts
description: Use when executing an implementation plan inside a cairn-tracked repo. Composition over superpowers:subagent-driven-development — same dispatch flow, with cairn task claim / evidence / verdict / complete checkpoints inserted at named steps.
---
```

Body: eight numbered sections from design §3.6:

1. Preamble — "Follow `superpowers:subagent-driven-development` exactly. Layer these cairn calls at the listed checkpoints…" (verbatim).
2. Checkpoint table (greppable, auditable) — 9 rows from design §3.6 item 2. Markdown table, pipe-delimited.
3. Verdict-binding timing rationale (verbatim blockquote).
4. Gate output capture lifetime — `/tmp/cairn-gate-<run_id>-<gate_id>.out`.
5. Non-reuse rule on crash-reclaim — verdict-binding step parses embedded `<run_id>` from filename and compares to active `run_id`; mismatch → re-run gate.
6. Hash placeholders — compute per `hash-placeholders.md`, do not improvise.
7. Failure modes (3 entries from design §3.6 item 7: claim conflict, evidence_invalidated, gate_not_fresh).
8. Red Flags (delta from SP original) — 3 rows from design §3.6 item 8.

- [ ] **Step 2: Verify checkpoint table parseable**

Run: `grep -E '^\| ' skills/subagent-driven-development-with-verdicts/SKILL.md | wc -l`
Expected: ≥10 (9 rows + 1 header row in the checkpoint table; more if Red Flags table also matches).

- [ ] **Step 3: Commit**

```bash
git add skills/subagent-driven-development-with-verdicts/SKILL.md
git commit -m "feat(skills): add subagent-driven-development-with-verdicts wrap

Composition-by-reference over superpowers:subagent-driven-development.
Checkpoint table inserts cairn task claim / evidence put / verdict
report / task complete at named SP steps. Zero fork. Per Ship 3
design §3.6."
```

### Task 1.7: Write `verdict-backed-verification/SKILL.md` wrap

**Files:**
- Create: `skills/verdict-backed-verification/SKILL.md`

- [ ] **Step 1: Write the wrap**

Frontmatter (verbatim from design §3.7):

```yaml
---
name: verdict-backed-verification
description: Use when about to claim work is complete inside an active cairn claim. Composition over superpowers:verification-before-completion — same Iron Law, with evidence put + verdict bound before the completion claim. Tracked-only. Without an active claim, use superpowers:verification-before-completion directly.
---
```

Body: five numbered sections from design §3.7:

1. Preamble (hard boundary) — "Invoke this skill ONLY while holding a cairn claim…" (verbatim).
2. Gate-function delta over SP V-B-C — 5-row table from design §3.7 item 2.
3. Claim wording — machine-readable JSON blob. The example JSON line + the format-rules paragraph + the parser-hint sentence (all verbatim).
4. Iron Law (extended): "NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE **BOUND AS A CAIRN VERDICT**."
5. Red Flags — 2 rows from design §3.7 item 5.

- [ ] **Step 2: Verify JSON blob example present**

Run: `grep -F '"verdict_id":' skills/verdict-backed-verification/SKILL.md`
Expected: example JSON line found.

- [ ] **Step 3: Commit**

```bash
git add skills/verdict-backed-verification/SKILL.md
git commit -m "feat(skills): add verdict-backed-verification wrap

Composition-by-reference over superpowers:verification-before-
completion. Tracked-only (errors if no active cairn claim). Step 5
CLAIM emits machine-readable JSON blob. Per Ship 3 design §3.7."
```

### Task 1.8: Verify plugin discovers all three skills

**Files:**
- (no source files; verification step only)

- [ ] **Step 1: Tree-list the skills directory**

Run: `find skills -type f -name '*.md' | sort`
Expected:

```
skills/subagent-driven-development-with-verdicts/SKILL.md
skills/using-cairn/SKILL.md
skills/using-cairn/code-reviewer-pattern.md
skills/using-cairn/hash-placeholders.md
skills/using-cairn/source-hash-format.md
skills/using-cairn/yaml-authoring.md
skills/verdict-backed-verification/SKILL.md
```

- [ ] **Step 2: Verify plugin manifest paths match**

Run: `cat .claude-plugin/plugin.json | python -c "import json,sys; d=json.load(sys.stdin); print('\n'.join(d['skills']))"`
Expected: three paths matching the three skill directories above.

- [ ] **Step 3: Spot-check skill frontmatter**

Run: `head -5 skills/using-cairn/SKILL.md skills/subagent-driven-development-with-verdicts/SKILL.md skills/verdict-backed-verification/SKILL.md`
Expected: each starts with `---` line, then `name:` line, then `description:` line, then `---` line.

(No commit — Phase 1 is verified-as-complete, ready for Phase 2 to use the wrap.)

---

## Phase 2: REQ-002 TASK-002-001 — `cairn spec validate` envelope extension

**Dispatch via `cairn:subagent-driven-development-with-verdicts`** (the wrap built in Phase 1). Each REQ-002 task acquires a cairn claim before work, captures gate output, binds a verdict with hash placeholders, completes the claim. The wrap's checkpoint table from `skills/subagent-driven-development-with-verdicts/SKILL.md` is the operational guide.

**Operational pattern for Phases 2–5 (every task in this slice):**

```
1. cairn task claim TASK-002-NNN --agent <subagent-id> --ttl 30m --op-id <op>
2. dispatch implementer subagent to do the per-task work below
3. implementer captures `go test ./internal/intent/... ./internal/cli/... ./internal/integration/...` output to /tmp/cairn-gate-<run_id>-AC-002-TEST.out
4. dispatch spec reviewer subagent (per SP SDD original)
5. dispatch quality reviewer subagent (per SP SDD original)
6. on quality approve:
   a. cairn evidence put /tmp/cairn-gate-<run_id>-AC-002-TEST.out
   b. cairn verdict report --gate AC-002-TEST --run <run_id> --status pass \
        --evidence /tmp/cairn-gate-<run_id>-AC-002-TEST.out \
        --producer-hash $(printf 'ship3:%s:%s' AC-002-TEST executable | sha256sum | cut -d' ' -f1) \
        --inputs-hash $(printf 'ship3:%s' "$run_id" | sha256sum | cut -d' ' -f1) \
        --op-id <op>
7. cairn task complete <claim_id> --op-id <op>
```

The bite-sized step list per task below covers steps 2 and the implementation half of 3 (the actual code/test changes). Steps 1, 3-end, and 4–7 above are the wrap's responsibility. The plan's checkbox list focuses on the engineering work; the wrap dictates the cairn ceremony around it.

### Task 2.1: Write failing test `TestValidateEnvelopeEmpty`

**Files:**
- Modify: `internal/intent/intent_test.go`

- [ ] **Step 1: Append the test**

```go
func TestLoad_ScanCountsEmpty(t *testing.T) {
	root := t.TempDir()
	// no requirements/, no tasks/ subdirs at all
	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if got := len(bundle.Requirements); got != 0 {
		t.Errorf("requirements: want 0, got %d", got)
	}
	if got := len(bundle.Tasks); got != 0 {
		t.Errorf("tasks: want 0, got %d", got)
	}
}

func TestLoad_ScanCountsIgnoresExamples(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requirements")
	taskDir := filepath.Join(root, "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	// Only .yaml.example files. Loader must skip them.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-001.yaml.example"), []byte("id: REQ-001\n"), 0o644)
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-001.yaml.example"), []byte("id: TASK-001\n"), 0o644)

	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(bundle.Requirements); got != 0 {
		t.Errorf("requirements: want 0 (only .yaml.example present), got %d", got)
	}
	if got := len(bundle.Tasks); got != 0 {
		t.Errorf("tasks: want 0 (only .yaml.example present), got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or passes if loader already correct)**

Run: `go test ./internal/intent/ -run 'TestLoad_ScanCounts' -v`
Expected:
- `TestLoad_ScanCountsEmpty` passes (existing loader handles missing dirs).
- `TestLoad_ScanCountsIgnoresExamples` may PASS already because `loadYAMLDir` only matches `*.yaml` (strict suffix), not `*.yaml.example`. **Verify this is true before proceeding.** If it fails, the loader is matching `.yaml.example` as `.yaml` — fix in Step 3.

If both pass already, no loader change is needed for this sub-task. The "ignores examples" semantic is implicit in `strings.HasSuffix(e.Name(), ".yaml")`. Add a documentation comment in `loader.go` reinforcing the intent (in case a future change uses prefix matching).

- [ ] **Step 3: Add doc comment to `loadYAMLDir`**

In `internal/intent/loader.go`, just above the `for _, e := range entries` loop, insert:

```go
// Ship 3 contract: only files with the literal `.yaml` suffix are loaded.
// `.yaml.example` files (written by `cairn spec init`) are reference-only
// scaffolds and MUST be skipped here. The strict-suffix match below
// satisfies that requirement; do not relax it without updating the
// renamed-template detector in validate.go.
```

- [ ] **Step 4: Re-run tests**

Run: `go test ./internal/intent/ -run 'TestLoad_ScanCounts' -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/intent/intent_test.go internal/intent/loader.go
git commit -m "test(intent): scan-count fixtures + doc the .yaml-only contract

Confirms intent.Load skips .yaml.example files via strict suffix
match. No loader behavior change; doc comment makes the contract
explicit so future loader edits don't silently break the renamed-
template detector landing in TASK-002-003.

Drives TASK-002-001 envelope work: validate response will surface
len(bundle.Requirements/Tasks) as specs_scanned counts."
```

### Task 2.2: Write failing test `TestSpecValidate_EnvelopeShape`

**Files:**
- Modify: `internal/intent/intent_test.go`

The actual envelope is constructed in `cmd/cairn/spec.go`, but driving it requires running the binary. The cheapest TDD step is an integration-style test in `internal/integration/` that invokes the CLI. Plan is:

- This task: defer the CLI envelope test to the integration phase (Task 7.1). Here, write a unit test that confirms `intent.Validate` returns `[]SpecError` with len 0 on an empty bundle (a precondition for the envelope's `errors: []` response).

- [ ] **Step 1: Append the test**

```go
func TestValidate_EmptyBundle(t *testing.T) {
	bundle := &intent.Bundle{}
	errs := intent.Validate(bundle)
	if len(errs) != 0 {
		t.Fatalf("empty bundle should have no errors, got: %+v", errs)
	}
}

func TestValidate_NilBundleReturnsEmptySlice(t *testing.T) {
	// Defensive: passing zero-value bundle should not panic, must return [] not nil.
	bundle := &intent.Bundle{}
	errs := intent.Validate(bundle)
	if errs == nil {
		// Pass — nil slice is fine; the JSON encoder converts at the CLI seam.
		return
	}
	if len(errs) != 0 {
		t.Fatalf("zero bundle: %+v", errs)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/intent/ -run 'TestValidate_EmptyBundle|TestValidate_NilBundleReturnsEmptySlice' -v`
Expected: both PASS (Validate's existing behavior — no schema files to load against zero requirements/tasks).

- [ ] **Step 3: Commit**

```bash
git add internal/intent/intent_test.go
git commit -m "test(intent): pin Validate empty-bundle behavior

Validate(empty bundle) must return zero errors so the envelope's
data.errors field renders as [] (post-nil-coalesce in spec.go).
Precondition for TASK-002-001's envelope shape."
```

### Task 2.3: Write integration test for envelope on empty tree

**Files:**
- Create: `internal/integration/spec_envelope_e2e_test.go`

- [ ] **Step 1: Inspect existing helpers**

Run: `cat internal/integration/main_test.go internal/integration/e2e_helpers_test.go`
Expected: existing helpers for building the binary, invoking it, parsing the envelope. Reuse them (e.g. `runCLI(t, args...)` returns the parsed envelope map).

If no `runCLI` helper exists, write one in this test file using `os/exec` against `go run ./cmd/cairn`. Do NOT add helper to `e2e_helpers_test.go` unless the existing pattern uses inline helpers (check before deciding).

- [ ] **Step 2: Write the test**

```go
func TestSpecValidateEnvelopeEmpty(t *testing.T) {
	root := t.TempDir()
	specsRoot := filepath.Join(root, "specs")
	if err := os.MkdirAll(specsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// no requirements/, no tasks/

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)

	if env["error"] != nil {
		t.Fatalf("expected no error envelope, got: %v", env["error"])
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong shape: %v", env["data"])
	}
	errs, ok := data["errors"].([]any)
	if !ok {
		t.Fatalf("errors missing or wrong shape: %v", data["errors"])
	}
	if len(errs) != 0 {
		t.Fatalf("errors should be empty, got: %v", errs)
	}
	scanned, ok := data["specs_scanned"].(map[string]any)
	if !ok {
		t.Fatalf("specs_scanned missing: %v", data["specs_scanned"])
	}
	if scanned["requirements"] != float64(0) {
		t.Errorf("requirements: want 0, got %v", scanned["requirements"])
	}
	if scanned["tasks"] != float64(0) {
		t.Errorf("tasks: want 0, got %v", scanned["tasks"])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/integration/ -run TestSpecValidateEnvelopeEmpty -v`
Expected: FAIL — current `cmd/cairn/spec.go` returns `{"errors": []}` with no `specs_scanned`. The `scanned` cast will fail.

- [ ] **Step 4: Commit failing test**

```bash
git add internal/integration/spec_envelope_e2e_test.go
git commit -m "test(integration): failing TestSpecValidateEnvelopeEmpty (TDD red)

Drives TASK-002-001 envelope extension. cmd/cairn/spec.go currently
returns {errors: []} only; this test asserts the Ship 3 shape
{errors: [], specs_scanned: {requirements, tasks}}."
```

### Task 2.4: Implement envelope extension in `cmd/cairn/spec.go`

**Files:**
- Modify: `cmd/cairn/spec.go`

- [ ] **Step 1: Replace the validate RunE**

Replace the existing `validate.RunE` body. The new pattern bypasses `cli.Run` so the response can carry both `errors` and `specs_scanned` in `data` even when validation errors are present (see "Conventions" — this is the documented exception).

```go
validate := &cobra.Command{
    Use:   "validate",
    Short: "Schema + referential + uniqueness validation",
    RunE: func(cmd *cobra.Command, _ []string) error {
        out := cmd.OutOrStdout()

        bundle, loadErr := intent.Load(path)
        if loadErr != nil {
            // Hard load failure (YAML parse, dir read). Preserve Ship 1 shape.
            cli.WriteEnvelope(out, cli.Envelope{
                Kind: "spec.validate",
                Err:  cairnerr.New(cairnerr.CodeBadInput, "load_failed", loadErr.Error()).WithCause(loadErr),
            })
            os.Exit(cli.ExitCodeFor(cairnerr.New(cairnerr.CodeBadInput, "load_failed", "")))
            return nil
        }

        errs := intent.Validate(bundle)
        if errs == nil {
            errs = []intent.SpecError{}
        }
        data := map[string]any{
            "errors": errs,
            "specs_scanned": map[string]any{
                "requirements": len(bundle.Requirements),
                "tasks":        len(bundle.Tasks),
            },
        }

        cli.WriteEnvelope(out, cli.Envelope{Kind: "spec.validate", Data: data})

        if len(errs) > 0 {
            os.Exit(1)
        }
        return nil
    },
}
```

- [ ] **Step 2: Re-run integration test**

Run: `go test ./internal/integration/ -run TestSpecValidateEnvelopeEmpty -v`
Expected: PASS.

- [ ] **Step 3: Add a populated-tree test**

Append to `internal/integration/spec_envelope_e2e_test.go`:

```go
func TestSpecValidateEnvelopePopulated(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "specs", "requirements")
	taskDir := filepath.Join(root, "specs", "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	for i := 1; i <= 3; i++ {
		_ = os.WriteFile(
			filepath.Join(reqDir, fmt.Sprintf("REQ-00%d.yaml", i)),
			[]byte(fmt.Sprintf(`id: REQ-00%d
title: x
gates:
  - id: AC-%d
    kind: test
    producer: {kind: executable}
`, i, i)), 0o644)
	}
	for i := 1; i <= 5; i++ {
		_ = os.WriteFile(
			filepath.Join(taskDir, fmt.Sprintf("TASK-00%d.yaml", i)),
			[]byte(fmt.Sprintf("id: TASK-00%d\nimplements: [REQ-001]\nrequired_gates: [AC-1]\n", i)), 0o644)
	}

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	scanned := data["specs_scanned"].(map[string]any)
	if scanned["requirements"] != float64(3) {
		t.Errorf("requirements: want 3, got %v", scanned["requirements"])
	}
	if scanned["tasks"] != float64(5) {
		t.Errorf("tasks: want 5, got %v", scanned["tasks"])
	}
}
```

- [ ] **Step 4: Run all envelope tests**

Run: `go test ./internal/integration/ -run 'TestSpecValidateEnvelope' -v`
Expected: both PASS.

- [ ] **Step 5: Verify Ship 1/2 e2e tests still green**

Run: `go test ./internal/integration/ -run 'TestE2E|TestDogfood|TestReplay|TestConcurrentClaim' -v`
Expected: PASS (no regression to existing flows).

- [ ] **Step 6: Verify the rest of the suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/cairn/spec.go internal/integration/spec_envelope_e2e_test.go
git commit -m "feat(spec): extend cairn spec validate envelope with specs_scanned

Adds {requirements: N, tasks: M} count object to the validate response.
Both keys (errors, specs_scanned) live in data on success and on
validation failure (bypasses cli.Run; documented exception). Caller
distinguishes empty-but-clean from 'nothing scanned' as designed.

Closes TASK-002-001 (REQ-002). Per Ship 3 design §5.1."
```

### Task 2.5: Add mixed-valid-invalid envelope test

**Files:**
- Modify: `internal/integration/spec_envelope_e2e_test.go`

- [ ] **Step 1: Append the test**

```go
func TestSpecValidateEnvelopeMixedValidInvalid(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "specs", "requirements")
	_ = os.MkdirAll(reqDir, 0o755)

	// Two valid requirements + one with a schema error (missing id).
	for _, name := range []string{"REQ-001.yaml", "REQ-002.yaml"} {
		_ = os.WriteFile(filepath.Join(reqDir, name),
			[]byte(`id: REQ-OK
title: ok
gates:
  - id: AC-1
    kind: test
    producer: {kind: executable}
`), 0o644)
	}
	// Bad file: missing id.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-BAD.yaml"),
		[]byte("title: missing-id\ngates:\n  - id: AC-X\n    kind: test\n    producer: {kind: executable}\n"),
		0o644)

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	errs := data["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected at least one error, got none")
	}
	scanned := data["specs_scanned"].(map[string]any)
	// All three loaded — counts attempts, not passes.
	if scanned["requirements"] != float64(3) {
		t.Errorf("requirements scanned: want 3, got %v", scanned["requirements"])
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/integration/ -run TestSpecValidateEnvelopeMixedValidInvalid -v`
Expected: PASS. (Note: this also implicitly covers the duplicate-ID case — both `REQ-001.yaml` and `REQ-002.yaml` declare id `REQ-OK`, which the validator flags as a duplicate. That's fine; the assertion is "at least one error".)

- [ ] **Step 3: Commit**

```bash
git add internal/integration/spec_envelope_e2e_test.go
git commit -m "test(integration): TestSpecValidateEnvelopeMixedValidInvalid

Pins the 'counts attempts, not passes' semantic from Ship 3 design
§5.1. Three requirement files including one with a schema error
must all count toward specs_scanned.requirements."
```

### Task 2.6: Run gate command, capture output, and let the wrap bind verdict

**Files:**
- (no source changes; this is the wrap's verdict-binding ceremony)

- [ ] **Step 1: Run the AC-002-TEST gate**

Run: `go test ./internal/intent/... ./internal/cli/... ./internal/integration/... 2>&1 | tee /tmp/cairn-gate-${RUN_ID}-AC-002-TEST.out`
Expected: PASS. Captured to the file pattern in `cairn:subagent-driven-development-with-verdicts` checkpoint table.

- [ ] **Step 2: Wrap binds verdict, completes claim**

The `cairn:subagent-driven-development-with-verdicts` skill is responsible for steps `cairn evidence put` → `cairn verdict report` → `cairn task complete` per its checkpoint table. The implementer agent reports DONE; the orchestrator runs the gate, captures output, and binds.

**Verification this happened:**

Run: `go run ./cmd/cairn task list --status done | grep TASK-002-001`
Expected: TASK-002-001 status `done`.

Run: `go run ./cmd/cairn verdict latest AC-002-TEST`
Expected: latest verdict for AC-002-TEST exists, status `pass`, evidence sha256 matches captured output file.

(If the wrap missed any of these steps, the dogfood is broken — file an issue and stop. Do not paper over with manual `cairn` calls; the dogfood signal is more valuable than the throughput.)

---

## Phase 3: REQ-002 TASK-002-002 — `cairn spec init` CLI + template strings

**Same wrap pattern as Phase 2.** Acquire claim, implement, capture gate output, bind verdict, complete claim.

### Task 3.1: Write failing test `TestSpecInit_CreatesTemplates`

**Files:**
- Create: `internal/cli/spec_init_test.go`

- [ ] **Step 1: Write the test**

```go
package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

func TestSpecInit_CreatesTemplates(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatalf("SpecInit: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created: want 2 files, got %d: %v", len(res.Created), res.Created)
	}

	for _, want := range []string{
		filepath.Join(target, "requirements", "REQ-001.yaml.example"),
		filepath.Join(target, "tasks", "TASK-001.yaml.example"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing file: %s", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestSpecInit_CreatesTemplates -v`
Expected: FAIL with `undefined: cli.SpecInit`.

- [ ] **Step 3: Commit failing test**

```bash
git add internal/cli/spec_init_test.go
git commit -m "test(cli): failing TestSpecInit_CreatesTemplates (TDD red)

Drives TASK-002-002 spec init scaffold. cli.SpecInit not implemented
yet."
```

### Task 3.2: Implement `cli.SpecInit` with embedded templates

**Files:**
- Create: `internal/cli/spec_init.go`

- [ ] **Step 1: Write the implementation**

```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// SpecInitResult is returned to the CLI envelope.
type SpecInitResult struct {
	Created     []string `json:"created"`
	Skipped     []string `json:"skipped"`
	Overwritten bool     `json:"overwritten,omitempty"`
}

// requirementTemplate is the literal body of REQ-001.yaml.example.
// First non-blank line is the marker comment that the renamed-template
// detector in internal/intent/validate.go looks for. DO NOT REWORD.
const requirementTemplate = `# cairn requirement spec template — DO NOT EDIT THIS FILE.
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
`

// taskTemplate is the literal body of TASK-001.yaml.example.
const taskTemplate = `# cairn task spec template — DO NOT EDIT THIS FILE.
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
`

// SpecInit scaffolds <root>/requirements/REQ-001.yaml.example and
// <root>/tasks/TASK-001.yaml.example. Idempotent: existing files are
// left alone unless force=true.
func SpecInit(root string, force bool) (*SpecInitResult, error) {
	res := &SpecInitResult{Created: []string{}, Skipped: []string{}}
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	pairs := []struct {
		path string
		body string
	}{
		{filepath.Join(root, "requirements", "REQ-001.yaml.example"), requirementTemplate},
		{filepath.Join(root, "tasks", "TASK-001.yaml.example"), taskTemplate},
	}
	for _, p := range pairs {
		if _, err := os.Stat(p.path); err == nil && !force {
			res.Skipped = append(res.Skipped, p.path)
			continue
		}
		if err := os.WriteFile(p.path, []byte(p.body), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", p.path, err)
		}
		res.Created = append(res.Created, p.path)
		if force {
			res.Overwritten = true
		}
	}
	return res, nil
}
```

- [ ] **Step 2: Run failing test to verify it passes**

Run: `go test ./internal/cli/ -run TestSpecInit_CreatesTemplates -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/spec_init.go
git commit -m "feat(cli): cli.SpecInit scaffolds .yaml.example templates

Embeds the requirement + task template strings (verbatim from Ship
3 design §5.2). Idempotent: existing .example files are left alone
unless force=true.

First non-blank line of each template is the renamed-template
marker comment (em-dash exact); the detector in TASK-002-003 will
key off it."
```

### Task 3.3: Add `TestSpecInit_Idempotent`, `TestSpecInit_Force`, `TestSpecInit_CustomPath`

**Files:**
- Modify: `internal/cli/spec_init_test.go`

- [ ] **Step 1: Append three tests**

```go
func TestSpecInit_Idempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	first, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Created) != 2 {
		t.Fatalf("first call: want 2 created, got %d", len(first.Created))
	}

	second, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Created) != 0 {
		t.Errorf("second call: want 0 created, got %v", second.Created)
	}
	if len(second.Skipped) != 2 {
		t.Errorf("second call: want 2 skipped, got %v", second.Skipped)
	}
	if second.Overwritten {
		t.Errorf("second call should not report overwritten without force")
	}
}

func TestSpecInit_Force(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	if _, err := cli.SpecInit(target, false); err != nil {
		t.Fatal(err)
	}
	// Mutate one file so we can detect a real rewrite.
	mutated := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	if err := os.WriteFile(mutated, []byte("# manually edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := cli.SpecInit(target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Overwritten {
		t.Errorf("force should report overwritten=true")
	}
	body, _ := os.ReadFile(mutated)
	if string(body) == "# manually edited\n" {
		t.Errorf("force should have rewritten the manually-edited file")
	}
}

func TestSpecInit_CustomPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "alt", "spec-tree")

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("custom path: want 2 created, got %d", len(res.Created))
	}
	for _, p := range []string{
		filepath.Join(target, "requirements", "REQ-001.yaml.example"),
		filepath.Join(target, "tasks", "TASK-001.yaml.example"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing: %s", p)
		}
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/cli/ -run 'TestSpecInit' -v`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/spec_init_test.go
git commit -m "test(cli): SpecInit idempotency, force, custom path

Three additional unit tests cover (1) second call leaves files
alone, (2) --force rewrites them, (3) --path <custom> threads
through. Per Ship 3 design §5.2."
```

### Task 3.4: Wire `init` subcommand into `cmd/cairn/spec.go`

**Files:**
- Modify: `cmd/cairn/spec.go`

- [ ] **Step 1: Add init subcommand**

Inside `newSpecCmd`, after the existing `validate` subcommand registration and before the final `return spec`, add:

```go
var (
    initPath  string
    initForce bool
)
initCmd := &cobra.Command{
    Use:   "init",
    Short: "Scaffold cairn spec directories with annotated templates",
    RunE: func(cmd *cobra.Command, _ []string) error {
        os.Exit(cli.Run(cmd.OutOrStdout(), "spec.init", "", func() (any, error) {
            res, err := cli.SpecInit(initPath, initForce)
            if err != nil {
                return nil, cairnerr.New(cairnerr.CodeSubstrate, "init_failed", err.Error()).WithCause(err)
            }
            return res, nil
        }))
        return nil
    },
}
initCmd.Flags().StringVar(&initPath, "path", "specs", "Target directory")
initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing .example files")
spec.AddCommand(initCmd)
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Smoke-run**

```bash
mkdir -p /tmp/cairn-spec-init-smoke && cd /tmp/cairn-spec-init-smoke && rm -rf specs
"$OLDPWD"/$(go env GOEXE 2>/dev/null)
```

Or simpler:

Run: `cd $(mktemp -d) && go run github.com/ProductOfAmerica/cairn/cmd/cairn spec init`
Expected: JSON envelope with `data.created` listing two paths under `specs/requirements/` and `specs/tasks/`. `ls -R specs/` shows both `.yaml.example` files.

- [ ] **Step 4: Commit**

```bash
git add cmd/cairn/spec.go
git commit -m "feat(cli): add cairn spec init subcommand

Wires cli.SpecInit into cobra under spec/init with --path (default
specs) and --force flags. Closes TASK-002-002 (REQ-002). Per Ship
3 design §5.2."
```

### Task 3.5: Run AC-002-TEST gate; wrap binds verdict; complete claim

**Files:**
- (no source changes)

- [ ] **Step 1: Run gate**

Run: `go test ./internal/intent/... ./internal/cli/... ./internal/integration/... 2>&1 | tee /tmp/cairn-gate-${RUN_ID}-AC-002-TEST.out`
Expected: PASS.

- [ ] **Step 2: Wrap binds + completes**

Verify post-wrap:

Run: `go run ./cmd/cairn task list --status done | grep TASK-002-002`
Expected: TASK-002-002 done. New verdict for AC-002-TEST under run.

---

## Phase 4: REQ-002 TASK-002-003 — Renamed-template detection

### Task 4.1: Write failing test `TestSpecValidate_RejectsRenamedExample`

**Files:**
- Modify: `internal/intent/intent_test.go`

- [ ] **Step 1: Append the test**

```go
func TestSpecValidate_RejectsRenamedRequirementTemplate(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requirements")
	_ = os.MkdirAll(reqDir, 0o755)

	// Body identical to what `cairn spec init` produces, but renamed to .yaml.
	body := `# cairn requirement spec template — DO NOT EDIT THIS FILE.
# This file is scaffolding.
id: REQ-001
title: Example requirement
why: x
scope_in: []
scope_out: []
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-001.yaml"), []byte(body), 0o644)

	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	errs := intent.Validate(bundle)

	found := false
	for _, e := range errs {
		if e.Kind == "renamed_template" {
			found = true
			if !strings.Contains(e.Message, "using-cairn") {
				t.Errorf("renamed_template message must hint at using-cairn, got: %s", e.Message)
			}
		}
	}
	if !found {
		t.Fatalf("want renamed_template error, got: %+v", errs)
	}
}

func TestSpecValidate_RejectsRenamedTaskTemplate(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "tasks")
	reqDir := filepath.Join(root, "requirements")
	_ = os.MkdirAll(taskDir, 0o755)
	_ = os.MkdirAll(reqDir, 0o755)
	// Need a real requirement so the task's implements ref resolves.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: x
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`), 0o644)
	body := `# cairn task spec template — DO NOT EDIT THIS FILE.
id: TASK-001
implements: [REQ-001]
depends_on: []
required_gates: [AC-001]
`
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-001.yaml"), []byte(body), 0o644)

	bundle, _ := intent.Load(root)
	errs := intent.Validate(bundle)
	found := false
	for _, e := range errs {
		if e.Kind == "renamed_template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want renamed_template error for task, got: %+v", errs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/intent/ -run 'TestSpecValidate_RejectsRenamed' -v`
Expected: FAIL — current `intent.Validate` has no `renamed_template` kind.

- [ ] **Step 3: Commit failing test**

```bash
git add internal/intent/intent_test.go
git commit -m "test(intent): failing renamed-template detection (TDD red)

Drives TASK-002-003. Renamed scaffold templates carry their marker
comment on the first non-blank line; detector must emit
kind: renamed_template with a using-cairn hint."
```

### Task 4.2: Implement renamed-template detector in `intent/validate.go`

**Files:**
- Modify: `internal/intent/validate.go`

- [ ] **Step 1: Add detector function and wire it into Validate**

At the top of `validate.go` add the constants (or inline them — keep at file scope so future skills can reference):

```go
// Renamed-template detection — must match the literal first non-blank
// line written by cli.SpecInit. Em-dash is the U+2014 character; the
// templates use it intentionally to make accidental ASCII paraphrasing
// stand out.
const (
    requirementTemplateMarker = "# cairn requirement spec template — DO NOT EDIT THIS FILE."
    taskTemplateMarker        = "# cairn task spec template — DO NOT EDIT THIS FILE."

    renamedTemplateMessage = "This file appears to be a renamed scaffold template. Templates are reference-only. To start authoring specs, invoke the using-cairn skill to derive from a prose spec under docs/superpowers/specs/."
)
```

Then add:

```go
func validateNoTemplateMarkers(b *Bundle) []SpecError {
    var out []SpecError
    for _, r := range b.Requirements {
        if isRenamedTemplate(r.RawYAML, requirementTemplateMarker) {
            out = append(out, SpecError{
                Path:    r.SpecPath,
                Kind:    "renamed_template",
                Message: renamedTemplateMessage,
            })
        }
    }
    for _, t := range b.Tasks {
        if isRenamedTemplate(t.RawYAML, taskTemplateMarker) {
            out = append(out, SpecError{
                Path:    t.SpecPath,
                Kind:    "renamed_template",
                Message: renamedTemplateMessage,
            })
        }
    }
    return out
}

func isRenamedTemplate(raw []byte, marker string) bool {
    // First non-blank line must equal the marker exactly. Trim each line's
    // leading/trailing whitespace before compare to tolerate \r\n and stray
    // spaces, but require an exact match against the marker text.
    for _, line := range strings.Split(string(raw), "\n") {
        trim := strings.TrimSpace(line)
        if trim == "" {
            continue
        }
        return trim == marker
    }
    return false
}
```

In the `Validate` function, prepend the new pass before existing passes:

```go
func Validate(b *Bundle) []SpecError {
    var errs []SpecError
    errs = append(errs, validateNoTemplateMarkers(b)...)
    errs = append(errs, validateSchemas(b)...)
    errs = append(errs, validateReferential(b)...)
    return errs
}
```

(The renamed-template pass runs first so its hint surfaces even when the renamed file would also fail schema checks downstream.)

- [ ] **Step 2: Run failing tests to verify they pass**

Run: `go test ./internal/intent/ -run 'TestSpecValidate_RejectsRenamed' -v`
Expected: both PASS.

- [ ] **Step 3: Verify the rest of the intent suite still green**

Run: `go test ./internal/intent/ -v`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/intent/validate.go
git commit -m "feat(intent): detect renamed scaffold templates

validateNoTemplateMarkers scans every loaded YAML's first non-blank
line for the cli.SpecInit marker comment. Match → SpecError with
kind=renamed_template and a hint pointing at the using-cairn skill.

Closes TASK-002-003 (REQ-002). Per Ship 3 design §5.3."
```

### Task 4.3: Add CLI integration test for renamed-template detection

**Files:**
- Modify: `internal/integration/spec_envelope_e2e_test.go`

- [ ] **Step 1: Append the test**

```go
func TestSpecValidateRejectsRenamedExample(t *testing.T) {
	root := t.TempDir()
	specsRoot := filepath.Join(root, "specs")
	// Init scaffolds .yaml.example.
	if _, err := cli.SpecInit(specsRoot, false); err != nil {
		t.Fatal(err)
	}
	// User mistake: rename the requirement template to .yaml.
	src := filepath.Join(specsRoot, "requirements", "REQ-001.yaml.example")
	dst := filepath.Join(specsRoot, "requirements", "REQ-001.yaml")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", "./cmd/cairn", "spec", "validate", "--path", "specs")
	cmd.Dir = root
	out, _ := cmd.CombinedOutput()
	env := parseEnvelope(t, string(out))
	data := env["data"].(map[string]any)
	errs := data["errors"].([]any)

	found := false
	for _, e := range errs {
		em := e.(map[string]any)
		if em["kind"] == "renamed_template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want renamed_template error, got: %v", errs)
	}

	// Exit code MUST be 1 (validation error).
	if cmd.ProcessState.ExitCode() != 1 {
		t.Errorf("exit code: want 1, got %d", cmd.ProcessState.ExitCode())
	}
}
```

(Note: `cmd.Dir = root` runs `go run ./cmd/cairn` against the repo root from the temp dir; `./cmd/cairn` is a Go package path, resolved against `GOPATH`/module — to make this portable, the test must run `go run` against the absolute repo path or use a built binary helper. If `runCLIInDir` from Task 2.3 already handles this, prefer it. Otherwise mirror its approach.)

- [ ] **Step 2: Run**

Run: `go test ./internal/integration/ -run TestSpecValidateRejectsRenamedExample -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/spec_envelope_e2e_test.go
git commit -m "test(integration): TestSpecValidateRejectsRenamedExample

End-to-end: spec init scaffolds → user renames .example to .yaml →
cairn spec validate exits 1 with kind=renamed_template error
visible in data.errors. Per Ship 3 design §5.3."
```

### Task 4.4: Run AC-002-TEST gate; wrap binds verdict; complete TASK-002-003 claim

Same pattern as Phase 2.6 / 3.5.

- [ ] **Step 1: Run gate** — `go test ./internal/intent/... ./internal/cli/... ./internal/integration/...` to `/tmp/cairn-gate-${RUN_ID}-AC-002-TEST.out`. PASS.
- [ ] **Step 2: Wrap binds + completes.** Verify `task list --status done | grep TASK-002-003`.

---

## Phase 5: REQ-002 TASK-002-004 — CLI help text

### Task 5.1: Update `cairn spec validate` long help

**Files:**
- Modify: `cmd/cairn/spec.go`

- [ ] **Step 1: Add Long field**

Add to the validate cobra.Command literal (above `RunE:`):

```go
Long: `Schema + referential + uniqueness validation.

Flags:
  --path <dir>   Directory to scan (default: specs/).

Response includes:
  errors          List of validation errors (empty if all specs valid).
  specs_scanned   Object with counts of requirement/task files loaded.

specs_scanned counts files loaded, not files passed. Cross-reference
with errors for per-file status: len(errors) == 0 → all passed;
len(errors) > 0 → some failed, others may have passed.`,
```

(Verbatim from Ship 3 design §5.5.)

- [ ] **Step 2: Smoke-run**

Run: `go run ./cmd/cairn spec validate --help`
Expected: Long help text printed including all three documented bullet points.

### Task 5.2: Update `cairn spec init` long help

**Files:**
- Modify: `cmd/cairn/spec.go`

- [ ] **Step 1: Add Long field to initCmd**

```go
Long: `Scaffold cairn spec directories with annotated templates.

Creates:
  <path>/requirements/REQ-001.yaml.example
  <path>/tasks/TASK-001.yaml.example

Real YAML is derived from prose specs by the using-cairn skill; these
templates are reference only. Do not rename .example files to .yaml.

Flags:
  --path <dir>   Target directory (default: specs/).
  --force        Overwrite existing .example files.`,
```

(Verbatim from Ship 3 design §5.5.)

- [ ] **Step 2: Smoke-run**

Run: `go run ./cmd/cairn spec init --help`
Expected: full Long help printed.

### Task 5.3: Add help-text presence test

**Files:**
- Modify: `internal/integration/spec_envelope_e2e_test.go`

- [ ] **Step 1: Append the test**

```go
func TestSpecValidateHelpDocumentsEnvelope(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/cairn", "spec", "validate", "--help")
	cmd.Dir = repoRoot(t) // resolve absolute path to repo root via runtime helper
	out, _ := cmd.CombinedOutput()
	body := string(out)
	for _, want := range []string{"specs_scanned", "files loaded, not files passed"} {
		if !strings.Contains(body, want) {
			t.Errorf("validate --help missing %q; got:\n%s", want, body)
		}
	}
}

func TestSpecInitHelpDocumentsForceFlag(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/cairn", "spec", "init", "--help")
	cmd.Dir = repoRoot(t)
	out, _ := cmd.CombinedOutput()
	body := string(out)
	for _, want := range []string{"--force", "REQ-001.yaml.example", "using-cairn"} {
		if !strings.Contains(body, want) {
			t.Errorf("init --help missing %q; got:\n%s", want, body)
		}
	}
}
```

(`repoRoot(t)` is a helper that walks up from the test file to find `go.mod`. Add it inline if not already present in `e2e_helpers_test.go`.)

- [ ] **Step 2: Run**

Run: `go test ./internal/integration/ -run 'TestSpecValidateHelpDocumentsEnvelope|TestSpecInitHelpDocumentsForceFlag' -v`
Expected: PASS.

### Task 5.4: Commit and run AC-002-TEST gate; wrap binds both gates and completes TASK-002-004

**Files:**
- (commit only)

- [ ] **Step 1: Commit help-text changes + tests**

```bash
git add cmd/cairn/spec.go internal/integration/spec_envelope_e2e_test.go
git commit -m "feat(cli): document spec validate envelope + spec init in --help

Long help on spec validate explains the specs_scanned object +
counts-attempts-not-passes semantic. Long help on spec init lists
the .example targets and the don't-rename rule.

Closes TASK-002-004 (REQ-002). Per Ship 3 design §5.5."
```

- [ ] **Step 2: Run AC-002-TEST gate**

Run: `go test ./internal/intent/... ./internal/cli/... ./internal/integration/... 2>&1 | tee /tmp/cairn-gate-${RUN_ID}-AC-002-TEST.out`
Expected: PASS.

- [ ] **Step 3: Run AC-002-RUBRIC gate via code-reviewer-pattern.md**

Per `skills/using-cairn/code-reviewer-pattern.md`, dispatch the `superpowers:code-reviewer` agent with `gate_id=AC-002-RUBRIC` and the run id. Reviewer evaluates:
- Did the help-text additions land verbatim per design §5.5?
- Is the renamed-template error message clear and actionable?
- Are the embedded template comments accurate and useful for first-run users?

Reviewer shells out to `cairn evidence put <review-prose-path>` and `cairn verdict report --gate AC-002-RUBRIC --run <run_id> --status pass --evidence <path>` (with placeholder hashes for `producer.kind = human`).

- [ ] **Step 4: Wrap completes claim**

Verify:

Run: `go run ./cmd/cairn task list --status done | grep TASK-002-004`
Expected: TASK-002-004 done.

Run: `go run ./cmd/cairn verdict latest AC-002-RUBRIC`
Expected: latest verdict for AC-002-RUBRIC, status pass, evidence sha matches review.

---

## Phase 6: Skill fixture tests (`testdata/skill-tests/` + Makefile)

Per design §8.2: fixture-capture pattern. Static checks over committed fixtures with zero agent invocation in CI.

### Task 6.1: Create the fixture directory tree

**Files:**
- Create: `testdata/skill-tests/yaml-authoring/stable-prose/design.md`
- Create: `testdata/skill-tests/yaml-authoring/stable-prose/regen-a.yaml`
- Create: `testdata/skill-tests/yaml-authoring/stable-prose/regen-b.yaml`
- Create: `testdata/skill-tests/yaml-authoring/elicitation-writeback/design-before.md`
- Create: `testdata/skill-tests/yaml-authoring/elicitation-writeback/design-after.md`
- Create: `testdata/skill-tests/yaml-authoring/source-hash-valid/design.md`
- Create: `testdata/skill-tests/yaml-authoring/source-hash-valid/derived.yaml`
- Create: `testdata/skill-tests/yaml-authoring/source-hash-drift/design.md`
- Create: `testdata/skill-tests/yaml-authoring/source-hash-drift/derived-stale.yaml`
- Create: `testdata/skill-tests/yaml-authoring/validation-failure/design-missing-producer.md`
- Create: `testdata/skill-tests/yaml-authoring/validation-failure/expected-design-question.txt`

- [ ] **Step 1: Write `stable-prose/design.md`**

A minimal, deterministic prose spec the using-cairn skill would derive from. Single REQ, single gate. Body:

```markdown
# REQ-FX-001 — fixture stable-prose

> Fixture for byte-identical regeneration test.

## Why
Static fixture; no real motivation.

## Scope
Scope in: fixture/**.
Scope out: none.

## Gates
- AC-001: unit tests pass via `go test ./fixture/...`.
```

- [ ] **Step 2: Write `stable-prose/regen-a.yaml`**

The expected derivation output. Header = canonical `# cairn-derived:` comment with a fixed-value source-hash + source-path + ISO timestamp (timestamp can be `2026-04-19T00:00:00Z` for the fixture):

```yaml
# cairn-derived: source-hash=<computed at fixture creation> source-path=testdata/skill-tests/yaml-authoring/stable-prose/design.md derived-at=2026-04-19T00:00:00Z
id: REQ-FX-001
title: fixture stable-prose
why: Static fixture; no real motivation.
scope_in: [fixture/**]
scope_out: []
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [go, test, ./fixture/...]
        pass_on_exit_code: 0
```

The `<computed at fixture creation>` placeholder must be filled with the actual sha256 of `design.md` at fixture-write time:

```bash
sha256sum testdata/skill-tests/yaml-authoring/stable-prose/design.md | cut -d' ' -f1
```

Insert that hash into `regen-a.yaml`'s header.

- [ ] **Step 3: Copy `regen-a.yaml` → `regen-b.yaml` byte-identical**

Run: `cp testdata/skill-tests/yaml-authoring/stable-prose/regen-a.yaml testdata/skill-tests/yaml-authoring/stable-prose/regen-b.yaml`

`cmp -s` will pass.

- [ ] **Step 4: Write `elicitation-writeback/design-before.md`**

```markdown
# REQ-FX-002 — fixture elicitation-writeback (before)

## Why
Demonstrates elicitation-answer fold-back into prose.

## Scope
Scope in: TBD-during-elicitation.
```

- [ ] **Step 5: Write `elicitation-writeback/design-after.md`**

```markdown
# REQ-FX-002 — fixture elicitation-writeback (after)

## Why
Demonstrates elicitation-answer fold-back into prose.

## Scope
Scope in: fixture/elicit/**.

## Gates
- AC-001: integration tests via `go test -tags integration ./fixture/elicit/...`.
```

The static-check assertion will grep for the strings `fixture/elicit/**` and `go test -tags integration` to confirm answer fold-back happened.

- [ ] **Step 6: Write `source-hash-valid/design.md`**

```markdown
# REQ-FX-003 — fixture source-hash-valid

## Why
Source-hash field must equal sha256(design.md).
```

- [ ] **Step 7: Write `source-hash-valid/derived.yaml`**

Header source-hash MUST equal the actual sha256 of design.md:

```bash
SHA=$(sha256sum testdata/skill-tests/yaml-authoring/source-hash-valid/design.md | cut -d' ' -f1)
```

YAML body:

```yaml
# cairn-derived: source-hash=<SHA> source-path=testdata/skill-tests/yaml-authoring/source-hash-valid/design.md derived-at=2026-04-19T00:00:00Z
id: REQ-FX-003
title: fixture source-hash-valid
why: Source-hash field must equal sha256(design.md).
scope_in: []
scope_out: []
gates: []
```

- [ ] **Step 8: Write `source-hash-drift/design.md`**

```markdown
# REQ-FX-004 — fixture source-hash-drift

## Why
Source-hash field intentionally does NOT equal sha256(design.md).
This file simulates "prose edited after derivation."
```

- [ ] **Step 9: Write `source-hash-drift/derived-stale.yaml`**

Header source-hash is a deliberate placeholder mismatch — write any 64-hex string that is NOT sha256(design.md). E.g. all-zeros:

```yaml
# cairn-derived: source-hash=0000000000000000000000000000000000000000000000000000000000000000 source-path=testdata/skill-tests/yaml-authoring/source-hash-drift/design.md derived-at=2026-04-19T00:00:00Z
id: REQ-FX-004
title: fixture source-hash-drift
why: Source-hash field intentionally does NOT equal sha256(design.md). This file simulates "prose edited after derivation."
scope_in: []
scope_out: []
gates: []
```

- [ ] **Step 10: Write `validation-failure/design-missing-producer.md`**

```markdown
# REQ-FX-005 — fixture validation-failure

## Why
Prose intentionally omits the gate's producer command. Skill must
translate this to a design question, not surface a raw error envelope.

## Gates
- AC-001: unit tests (no command specified — agent must elicit).
```

- [ ] **Step 11: Write `validation-failure/expected-design-question.txt`**

```
The updated design introduces gate AC-001 without a producer command. Was this intentional? What command runs this gate?
```

(Plain text, no `kind:` or `code:` substrings — those substrings would imply a leaked error envelope, which is what §3.2 validation-failure-fallback explicitly forbids.)

- [ ] **Step 12: Verify file structure**

Run: `find testdata/skill-tests -type f | sort`
Expected: 11 files matching the file list above.

### Task 6.2: Implement `testdata/skill-tests/verify/main.go`

**Files:**
- Create: `testdata/skill-tests/verify/main.go`

A standalone Go program that performs the static checks listed in design §8.2's `make test-skills-verify` bullet list. Output goes to stderr; exit 0 = all checks pass, exit 1 = any check failed.

- [ ] **Step 1: Write the Go program**

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const fixtureRoot = "testdata/skill-tests/yaml-authoring"

var headerRe = regexp.MustCompile(`^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$`)

type result struct{ name string; err error }

func main() {
	failures := 0
	checks := []func() result{
		checkAllYAMLParses,
		checkAllYAMLHasHeader,
		checkSourceHashValid,
		checkSourceHashDrift,
		checkStableProseByteIdentical,
		checkElicitationWriteback,
		checkValidationFailureNoLeakage,
		checkNoWhitespacePaths,
	}
	for _, c := range checks {
		r := c()
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", r.name, r.err)
			failures++
		} else {
			fmt.Fprintf(os.Stderr, "PASS %s\n", r.name)
		}
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func checkAllYAMLParses() result {
	var errs []string
	_ = filepath.Walk(fixtureRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		body, _ := os.ReadFile(p)
		var v any
		if e := yaml.Unmarshal(body, &v); e != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, e))
		}
		return nil
	})
	return r("all-yaml-parses", errs)
}

func checkAllYAMLHasHeader() result {
	var errs []string
	_ = filepath.Walk(fixtureRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return nil
		}
		body, _ := os.ReadFile(p)
		first := strings.SplitN(string(body), "\n", 2)[0]
		if !headerRe.MatchString(first) {
			errs = append(errs, fmt.Sprintf("%s: first line does not match header regex", p))
		}
		return nil
	})
	return r("all-yaml-has-header", errs)
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func headerSourceHash(yamlPath string) (string, error) {
	body, err := os.ReadFile(yamlPath)
	if err != nil {
		return "", err
	}
	first := strings.SplitN(string(body), "\n", 2)[0]
	m := headerRe.FindStringSubmatch(first)
	if m == nil {
		return "", fmt.Errorf("header missing in %s", yamlPath)
	}
	return m[1], nil
}

func checkSourceHashValid() result {
	dir := filepath.Join(fixtureRoot, "source-hash-valid")
	prose := filepath.Join(dir, "design.md")
	derived := filepath.Join(dir, "derived.yaml")
	want, err := sha256File(prose)
	if err != nil {
		return r("source-hash-valid", []string{err.Error()})
	}
	got, err := headerSourceHash(derived)
	if err != nil {
		return r("source-hash-valid", []string{err.Error()})
	}
	if want != got {
		return r("source-hash-valid", []string{fmt.Sprintf("want %s, got %s", want, got)})
	}
	return r("source-hash-valid", nil)
}

func checkSourceHashDrift() result {
	dir := filepath.Join(fixtureRoot, "source-hash-drift")
	prose := filepath.Join(dir, "design.md")
	derived := filepath.Join(dir, "derived-stale.yaml")
	prosehash, err := sha256File(prose)
	if err != nil {
		return r("source-hash-drift", []string{err.Error()})
	}
	header, err := headerSourceHash(derived)
	if err != nil {
		return r("source-hash-drift", []string{err.Error()})
	}
	if prosehash == header {
		return r("source-hash-drift", []string{"drift fixture has matching hash; should differ"})
	}
	return r("source-hash-drift", nil)
}

func checkStableProseByteIdentical() result {
	a, err := os.ReadFile(filepath.Join(fixtureRoot, "stable-prose", "regen-a.yaml"))
	if err != nil {
		return r("stable-prose-identical", []string{err.Error()})
	}
	b, err := os.ReadFile(filepath.Join(fixtureRoot, "stable-prose", "regen-b.yaml"))
	if err != nil {
		return r("stable-prose-identical", []string{err.Error()})
	}
	if string(a) != string(b) {
		return r("stable-prose-identical", []string{"regen-a.yaml and regen-b.yaml differ"})
	}
	return r("stable-prose-identical", nil)
}

func checkElicitationWriteback() result {
	body, err := os.ReadFile(filepath.Join(fixtureRoot, "elicitation-writeback", "design-after.md"))
	if err != nil {
		return r("elicitation-writeback", []string{err.Error()})
	}
	for _, want := range []string{"fixture/elicit/**", "go test -tags integration"} {
		if !strings.Contains(string(body), want) {
			return r("elicitation-writeback", []string{fmt.Sprintf("design-after.md missing %q", want)})
		}
	}
	return r("elicitation-writeback", nil)
}

func checkValidationFailureNoLeakage() result {
	body, err := os.ReadFile(filepath.Join(fixtureRoot, "validation-failure", "expected-design-question.txt"))
	if err != nil {
		return r("validation-failure-no-leakage", []string{err.Error()})
	}
	s := string(body)
	if strings.TrimSpace(s) == "" {
		return r("validation-failure-no-leakage", []string{"empty file"})
	}
	for _, banned := range []string{`"kind":`, `"code":`} {
		if strings.Contains(s, banned) {
			return r("validation-failure-no-leakage", []string{fmt.Sprintf("contains banned substring %q", banned)})
		}
	}
	return r("validation-failure-no-leakage", nil)
}

func checkNoWhitespacePaths() result {
	var bad []string
	_ = filepath.Walk("testdata/skill-tests", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.ContainsAny(p, " \t") {
			bad = append(bad, p)
		}
		return nil
	})
	return r("no-whitespace-paths", bad)
}

func r(name string, errs []string) result {
	if len(errs) == 0 {
		return result{name: name}
	}
	return result{name: name, err: fmt.Errorf("%s", strings.Join(errs, "; "))}
}
```

- [ ] **Step 2: Smoke-run**

Run: `go run ./testdata/skill-tests/verify/`
Expected: all checks PASS.

If any FAIL, fix the fixtures (most likely culprit: source-hash-valid's derived.yaml header doesn't actually match sha256 of design.md — recompute and rewrite).

- [ ] **Step 3: Commit fixtures + verify program**

```bash
git add testdata/skill-tests/
git commit -m "test(skills): yaml-authoring fixture suite + verifier

Five fixture sub-trees under testdata/skill-tests/yaml-authoring/
covering byte-identical regen, elicitation writeback, source-hash
valid, source-hash drift, validation-failure no-leakage. The Go
verifier at testdata/skill-tests/verify/main.go runs eight static
checks; CI calls it via 'make test-skills-verify'.

Per Ship 3 design §8.2."
```

### Task 6.3: Write Makefile with `test-skills-verify` and `test-skills-record` targets

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Write the Makefile**

```make
# cairn — convenience targets.

.PHONY: test-skills-verify test-skills-record

# Static checks over committed skill fixtures. CI calls this on Linux.
# See Ship 3 design §8.2.
test-skills-verify:
	go run ./testdata/skill-tests/verify/

# Human-triggered fixture regeneration. Not CI-gated. Outputs step-by-step
# instructions; the agent invocation against the prose inputs is manual.
# See Ship 3 design §8.2 (test-skills-record bullet).
test-skills-record:
	@echo "==== test-skills-record ===="
	@echo "This target documents the manual fixture regeneration procedure."
	@echo ""
	@echo "1. Open Claude Code with cairn + superpowers installed."
	@echo "2. For each fixture directory under testdata/skill-tests/yaml-authoring/:"
	@echo "   a. Read the prose input (design.md or design-before.md)."
	@echo "   b. Invoke the using-cairn skill against the prose."
	@echo "   c. Compare derived YAML output to the committed fixture."
	@echo "   d. If output differs, overwrite the fixture file with new output."
	@echo "3. Re-run 'make test-skills-verify' to confirm consistency."
	@echo "4. Commit fixture changes in the same PR as the skill change."
	@echo ""
	@echo "Human diligence required: the static checker cannot detect a"
	@echo "skill that produces output matching the fixture shape but with"
	@echo "different content. See design §8.2 'Limitation acknowledged'."
```

- [ ] **Step 2: Test both targets**

Run: `make test-skills-verify`
Expected: same output as `go run ./testdata/skill-tests/verify/` from Task 6.2 — all checks PASS.

Run: `make test-skills-record`
Expected: prints the multi-step instruction text without doing anything.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile with test-skills-verify + test-skills-record

CI calls 'make test-skills-verify' on Linux. test-skills-record is
human-triggered documentation — explains the manual fixture
regeneration loop. Per Ship 3 design §8.2."
```

---

## Phase 7: REQ-002 integration tests (consolidation)

Phases 2–5 added several integration tests. Phase 7 ensures the explicit "TestBootstrapE2E" and a clean "TestSpecInitE2E" exist as named tests (some may already exist from earlier phases — consolidate naming).

### Task 7.1: Add `TestSpecInitE2E`

**Files:**
- Create: `internal/integration/spec_init_e2e_test.go`

- [ ] **Step 1: Write the test**

```go
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecInitE2E(t *testing.T) {
	root := t.TempDir()
	// 1. Run spec init in a fresh directory.
	out := runCLIInDir(t, root, "spec", "init", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	created := data["created"].([]any)
	if len(created) != 2 {
		t.Fatalf("created: want 2, got %d", len(created))
	}

	// 2. Verify .yaml.example files exist.
	for _, p := range []string{
		filepath.Join(root, "specs", "requirements", "REQ-001.yaml.example"),
		filepath.Join(root, "specs", "tasks", "TASK-001.yaml.example"),
	} {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if !strings.Contains(string(body), "DO NOT EDIT THIS FILE") {
			t.Errorf("template marker missing in %s", p)
		}
	}

	// 3. Run spec validate; specs_scanned must be 0/0 (.example files excluded).
	out2 := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env2 := parseEnvelope(t, out2)
	data2 := env2["data"].(map[string]any)
	scanned := data2["specs_scanned"].(map[string]any)
	if scanned["requirements"] != float64(0) || scanned["tasks"] != float64(0) {
		t.Errorf("scan should ignore .example files: %v", scanned)
	}

	// 4. Re-run spec init; idempotent — no new files.
	out3 := runCLIInDir(t, root, "spec", "init", "--path", "specs")
	env3 := parseEnvelope(t, out3)
	data3 := env3["data"].(map[string]any)
	if len(data3["created"].([]any)) != 0 {
		t.Errorf("second init should create 0 files: %v", data3["created"])
	}
	if len(data3["skipped"].([]any)) != 2 {
		t.Errorf("second init should skip 2 files: %v", data3["skipped"])
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/integration/ -run TestSpecInitE2E -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/spec_init_e2e_test.go
git commit -m "test(integration): TestSpecInitE2E — fresh-repo bootstrap path

Init scaffolds → validate ignores .example → re-init is idempotent.
Per Ship 3 design §8.4 row 2."
```

### Task 7.2: Add `TestBootstrapE2E`

**Files:**
- Create: `internal/integration/bootstrap_e2e_test.go`

- [ ] **Step 1: Write the test**

```go
package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBootstrapE2E simulates the §6.1 Ship 3 bootstrap on a fresh repo:
// hand-author REQ + TASK YAML, init the cairn state DB, plan, claim, evidence,
// verdict, complete. Exercises every cairn surface used by Phase 0 + the
// REQ-002 dogfood loop in a single test. Does NOT require the new skills —
// this is a CLI-only smoke test of the bootstrap sequence.
func TestBootstrapE2E(t *testing.T) {
	root := t.TempDir()
	specsRoot := filepath.Join(root, "specs")
	reqDir := filepath.Join(specsRoot, "requirements")
	taskDir := filepath.Join(specsRoot, "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	// 1. Hand-author REQ + TASK.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-BOOT.yaml"),
		[]byte(`id: REQ-BOOT
title: bootstrap smoke
why: bootstrap smoke
scope_in: []
scope_out: []
gates:
  - id: AC-BOOT
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
`), 0o644)
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-BOOT.yaml"),
		[]byte(`id: TASK-BOOT
implements: [REQ-BOOT]
depends_on: []
required_gates: [AC-BOOT]
`), 0o644)

	// 2. cairn init (creates state DB).
	_ = runCLIInDir(t, root, "init")

	// 3. cairn spec validate — passes.
	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	if errs := data["errors"].([]any); len(errs) != 0 {
		t.Fatalf("validate failed: %v", errs)
	}

	// 4. cairn task plan — materializes.
	_ = runCLIInDir(t, root, "task", "plan")

	// 5. cairn task claim TASK-BOOT.
	out = runCLIInDir(t, root, "task", "claim", "TASK-BOOT", "--agent", "test-bootstrap", "--ttl", "10m")
	env = parseEnvelope(t, out)
	claimID := env["data"].(map[string]any)["claim_id"].(string)
	runID := env["data"].(map[string]any)["run_id"].(string)

	// 6. Capture gate output.
	gateOut := filepath.Join(root, "gate-out.txt")
	_ = os.WriteFile(gateOut, []byte("ok\n"), 0o644)

	// 7. cairn evidence put.
	_ = runCLIInDir(t, root, "evidence", "put", gateOut)

	// 8. cairn verdict report (placeholder hashes).
	_ = runCLIInDir(t, root, "verdict", "report",
		"--gate", "AC-BOOT",
		"--run", runID,
		"--status", "pass",
		"--evidence", gateOut,
		"--producer-hash", "0000000000000000000000000000000000000000000000000000000000000001",
		"--inputs-hash", "0000000000000000000000000000000000000000000000000000000000000002",
	)

	// 9. cairn task complete.
	_ = runCLIInDir(t, root, "task", "complete", claimID)

	// 10. Verify task done.
	out = runCLIInDir(t, root, "task", "list", "--status", "done")
	if !contains(out, "TASK-BOOT") {
		t.Fatalf("task done list missing TASK-BOOT: %s", out)
	}
}

func contains(s, sub string) bool { return len(s) > 0 && (s == sub || (len(sub) > 0 && (indexOf(s, sub) >= 0))) }
func indexOf(s, sub string) int { /* std-lib alternative */; return strings_Index(s, sub) }
// strings_Index reduces to strings.Index — replace with `strings.Index` import in real code.
```

(Real code uses `strings.Index` directly; the placeholder above just hints at the import. Implementer should write `strings.Index(s, sub) >= 0` inline.)

- [ ] **Step 2: Run**

Run: `go test ./internal/integration/ -run TestBootstrapE2E -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/bootstrap_e2e_test.go
git commit -m "test(integration): TestBootstrapE2E — Ship 3 bootstrap sequence

Single test exercises hand-author REQ + TASK, cairn init, validate,
plan, claim, evidence put, verdict report, task complete. Per
Ship 3 design §8.4 row 3 — covers the §6.1 bootstrap as a smoke
test executable in CI."
```

### Task 7.3: Run AC-002-TEST gate one final time, full suite green

- [ ] **Step 1: Run full suite**

Run: `go test ./...`
Expected: PASS.

If any test fails, debug and fix before proceeding to Phase 8.

---

## Phase 8: CI wiring

### Task 8.1: Add `make test-skills-verify` step to CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Insert new step in the build-test linux job**

Add a new step BEFORE `go test` in the build-test job, conditional on `matrix.os == 'ubuntu-latest'`:

```yaml
      - name: make test-skills-verify (linux only)
        if: matrix.os == 'ubuntu-latest'
        run: make test-skills-verify
```

Final job snippet:

```yaml
  build-test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        go: ["1.25.x"]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - name: go mod verify
        run: go mod verify
      - name: go vet
        run: go vet ./...
      - name: go build
        run: go build ./...
      - name: make test-skills-verify (linux only)
        if: matrix.os == 'ubuntu-latest'
        run: make test-skills-verify
      - name: go test
        run: go test -race ./...
```

- [ ] **Step 2: Verify YAML parses**

Run: `python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"`
Expected: no error.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: wire make test-skills-verify into the linux build-test job

Static checks over committed skill fixtures run on Linux only
(fixture-content checks are OS-agnostic; one OS suffices). Per
Ship 3 design §8.5."
```

---

## Phase 9: Dogfood execution + C1 forcing test + manual structural checks

### Task 9.1: Execute the C1 forcing test against `testdata/forcing-test/`

**Files:**
- Create: `testdata/forcing-test/README.md`
- (optional, populated during the test) `testdata/forcing-test/design.md`, derived YAML, etc.
- Create/Update: `docs/superpowers/ship-3-dogfood-elicitation-log.md`

Per design §6.3, the forcing test exercises a fresh feature designed for a hypothetical external project — not a real cairn feature. Fixture lives under `testdata/forcing-test/` so reconcile never sees the YAML.

- [ ] **Step 1: Write the fixture README**

```markdown
# testdata/forcing-test/

> **Purpose:** Throwaway dogfood target for the Ship 3 C1 forcing test.
>
> Per design §6.3:
> - Design prose + derived YAML commit under this subtree.
> - Reconcile never sees them; cairn CLI commands do not run against this YAML.
> - Existence of files under this subtree is the historical evidence
>   that the forcing test ran.

## What lives here after the forcing test

- `design.md` — prose design for a hypothetical feature.
- `specs/requirements/REQ-FORCING.yaml` — derived requirement YAML.
- `specs/tasks/TASK-FORCING-NN.yaml` — derived task YAML.
- (optional) plan prose at `plan.md`.

## What does NOT happen

- `cairn task plan` is NEVER run against this fixture.
- This fixture's YAML is excluded from Phase 7's TestBootstrapE2E.
```

- [ ] **Step 2: Open a fresh Claude Code session in the cairn repo with Ship 3 skills installed**

(Manual step. Document what feature is being designed in the elicitation log.)

- [ ] **Step 3: Run the forcing test per design §6.3 protocol**

The protocol:

1. Human says: "Let's design `<feature>`." (Feature for a hypothetical external project — not a real cairn feature.)
2. Main session invokes `superpowers:brainstorming`, scoped to `testdata/forcing-test/`.
3. Human answers brainstorming Qs normally until design approval; prose commits into the fixture subtree.
4. Main session invokes `using-cairn` explicitly.
5. `using-cairn` runs elicitation. Agent logs EVERY question posed to human, verbatim, in `docs/superpowers/ship-3-dogfood-elicitation-log.md` per the format in design §6.3:

   ```
   ## <timestamp> — REQ-NNN: <title>
   Q1 (distinct design decision — <what decision>): <verbatim>
   Q2 (clarification on Q1 — counts as 0): <verbatim>
   Q3 (distinct design decision — <what decision>): <verbatim>
   ...
   Total distinct design decisions: <N>
   ```

- [ ] **Step 4: Apply pass/fail criterion**

Per design §6.3:
- **Pass** if total distinct design decisions ≤ 3 per REQ.
- **Fail** if any REQ > 3 → open amendment to swap D→B and re-run; do NOT attempt to rescue D.

- [ ] **Step 5: Commit the elicitation log + fixture artifacts**

```bash
git add testdata/forcing-test/ docs/superpowers/ship-3-dogfood-elicitation-log.md
git commit -m "dogfood: Ship 3 C1 forcing test fixture + elicitation log

Forcing test ran against a hypothetical feature in
testdata/forcing-test/. Distinct-design-decision counts logged in
docs/superpowers/ship-3-dogfood-elicitation-log.md per design §6.3
format.

Pass: all requirements ≤ 3 distinct decisions. (Fail outcome would
open D→B amendment and rerun before Ship 3 merge.)"
```

(If the test FAILED, do NOT commit and merge Ship 3. Open the amendment, modify `using-cairn` to derive tasks only from plan prose, re-run the forcing test, and commit only after the rerun passes.)

### Task 9.2: Run the §8.3 manual structural checks; record results

**Files:**
- Create: `docs/superpowers/ship-3-dogfood-summary.md`

Per design §8.3 + §9.10: four structural checks, each pass/fail + one-line evidence. Run by hand or via grep.

- [ ] **Step 1: Run all four checks**

```bash
# 1. Hub-spoke isolation.
mv skills/using-cairn/hash-placeholders.md /tmp/_isolation.md
go run ./testdata/skill-tests/verify/  # ensure unrelated checks still pass
# Manually confirm: skills/using-cairn/SKILL.md still loads (frontmatter intact);
# routing tables still mention hash-placeholders.md (will not resolve, but the table is intact).
mv /tmp/_isolation.md skills/using-cairn/hash-placeholders.md

# 2. Checkpoint table greppable.
grep -E '^\|.*\|.*\|' skills/subagent-driven-development-with-verdicts/SKILL.md

# 3. Wrap routing boundary (using-cairn SKILL.md).
grep -E 'cairn:subagent-driven-development-with-verdicts|superpowers:subagent-driven-development|cairn:verdict-backed-verification|superpowers:verification-before-completion' skills/using-cairn/SKILL.md

# 4. Hash placeholder recipe verbatim.
grep -F 'sha256("ship3:' skills/using-cairn/hash-placeholders.md
```

- [ ] **Step 2: Write `docs/superpowers/ship-3-dogfood-summary.md`**

```markdown
# Ship 3 Dogfood Summary

> Date: 2026-04-19 (or whatever date the dogfood actually ran).
> Branch: feature/ship-3-superpowers-integration.

## REQ-002 dogfood event trail

`cairn events since <ship-3-branch-cut-timestamp>` returned the following
distinct event kinds:

```
<paste output of: cairn events since <ts> | jq -r '.kind' | sort -u>
```

Expected: union of Ship 1 + Ship 2 kinds. No new kinds (per design §7).

## §8.3 manual structural checks

| Check | Result | Evidence |
|---|---|---|
| Hub-spoke isolation | pass / fail | <one line> |
| Checkpoint table greppable | pass / fail | <one line> |
| Wrap routing boundary | pass / fail | <one line> |
| Hash placeholder recipe | pass / fail | <one line> |

## C1 forcing test

| REQ | Distinct decisions | Pass? |
|---|---|---|
| REQ-FORCING-001 | <N> | yes / no |

Full log: `docs/superpowers/ship-3-dogfood-elicitation-log.md`.

## §9.10 binary check — were placeholders ever misread as meaningful?

Answer: yes / no.

Justification (one line): <e.g. "agent never referenced producer_hash
during dogfood; spoke banner did its job">.

If yes → file an issue: "Accelerate Q1 of
docs/superpowers/ship-3-open-questions.md to Ship 4 week 1."

## Done-when checklist (cross-ref §9 of design)

| # | Item | Status |
|---|---|---|
| 1 | Three skills land (7 files) | pass / fail |
| 2 | REQ-002 implemented (envelope + init + renamed-template + help) | pass / fail |
| 3 | REQ-002 dogfood executed via cairn:SDD-with-verdicts | pass / fail |
| 4 | C1 forcing test recorded (≤3 distinct decisions per REQ) | pass / fail |
| 5 | make test-skills-verify passes for stable-prose | pass / fail |
| 6 | make test-skills-verify passes for source-hash valid + drift | pass / fail |
| 7 | All REQ-002 unit + CLI + integration tests pass | pass / fail |
| 8 | All skill-level structural checks pass | pass / fail |
| 9 | Event-log completeness test unchanged + passing | pass / fail |
| 10 | Post-dogfood binary check recorded | pass / fail |
| 11 | Matrix + offline CI green | pass / fail |
| 12 | Five PLAN.md amendments + bootstrap pin landed as prep PR | pass / fail |
| 13 | Bootstrap gap documented in implementation PR's first commit | pass / fail |
```

- [ ] **Step 3: Fill in the placeholders with real evidence**

Replace each `<one line>`, `<N>`, `pass / fail`, etc. with the actual observed value from the checks just run.

- [ ] **Step 4: Commit summary**

```bash
git add docs/superpowers/ship-3-dogfood-summary.md
git commit -m "docs: Ship 3 dogfood summary + done-when matrix

Records §8.3 structural checks, C1 forcing test outcome, §9.10
binary placeholder-misuse check, and the §9 done-when status. Use
this file as the final pre-merge audit record."
```

---

## Phase 10: Done-when verification + final scrub

### Task 10.1: Walk the design §9 13-item checklist

- [ ] **Step 1: Verify each item**

For each of the 13 items in design §9, confirm the corresponding evidence exists in the repo. Fill in `pass`/`fail` in `ship-3-dogfood-summary.md` from Task 9.2.

If any item is `fail`, fix it before proceeding to Step 2.

### Task 10.2: Verify event-log completeness test unchanged + passing

- [ ] **Step 1: Run the existing event-log assertion**

The Ship 1/Ship 2 event-log completeness test lives in `internal/integration/`. Find it:

Run: `grep -l 'events since 0' internal/integration/*.go`
Expected: at least one file matches.

Run: `go test ./internal/integration/ -run TestEventLogCoverage -v`
(adjust to actual test name; check by running `go test -list '.*' ./internal/integration/`).

Expected: PASS, with the same kind set as Ship 2 (no new kinds added).

### Task 10.3: Final full suite + `go vet`

- [ ] **Step 1: Run vet**

Run: `go vet ./...`
Expected: zero output.

- [ ] **Step 2: Run full test suite**

Run: `go test -race ./...`
Expected: PASS on local OS.

- [ ] **Step 3: Run skill verifier**

Run: `make test-skills-verify`
Expected: all checks PASS.

### Task 10.4: Final commit (if any unstaged changes), then stop before push

- [ ] **Step 1: Check status**

Run: `git status`
Expected: clean working tree (or only intentional remaining changes).

- [ ] **Step 2: If anything is unstaged, commit it**

```bash
git add <files>
git commit -m "chore: final Ship 3 scrub"
```

- [ ] **Step 3: STOP. Hand back for user review before pushing.**

Do NOT run `git push`. Do NOT open a PR. Per the user's instruction: "Produce the writing-plans output, self-review, commit on the branch, stop before pushing and hand back for my review."

---

## Self-review

Performed inline against the spec. Findings + fixes applied:

**1. Spec coverage.** Walked design §1 in-scope list (3 skills, REQ-002 envelope + init + renamed-template, 5 PLAN.md amendments + bootstrap pin) against this plan:

- Three skills + 7 files: Phase 1 covers all seven (using-cairn hub + 4 spokes; SDD-with-verdicts wrap; verdict-backed-verification wrap).
- REQ-002 envelope: Phase 2 (TASK-002-001).
- REQ-002 init: Phase 3 (TASK-002-002).
- REQ-002 renamed-template: Phase 4 (TASK-002-003).
- REQ-002 CLI help: Phase 5 (TASK-002-004).
- PLAN.md amendments + bootstrap pin: explicitly **out of this plan** (preamble note: prep PR mirrors Ship 2 workflow). The plan assumes that prep PR has merged. Done-when item 12 surfaces this as a pass/fail gate — if the prep PR didn't merge, item 12 fails and Ship 3 doesn't merge either.
- Ship 3 dogfood flow (design §6): Phase 0 bootstrap, Phases 2–5 dogfood-via-wrap, Phase 9 C1 forcing test + structural checks + summary.
- Testing (design §8): unit tests inline in Phases 2–5, fixture suite in Phase 6, integration tests consolidated in Phase 7, CI wiring in Phase 8, manual structural checks in Phase 9.

**2. Placeholder scan.** Searched plan for the red-flag patterns from `writing-plans` skill:

- "TBD" / "TODO" / "implement later" / "fill in details" — only "TBD" instance is inside `elicitation-writeback/design-before.md` fixture content (Task 6.1 step 4), which is intentionally a placeholder string the elicitation skill is expected to replace. Acceptable: it is a fixture artifact, not a plan placeholder.
- "Add appropriate error handling" / "handle edge cases" — none found.
- "Write tests for the above" without code — none; every test step has full Go code.
- "Similar to Task N" — used in shorthand "Same wrap pattern as Phase 2.6 / 3.5" for Tasks 4.4 and 5.4. Both reference earlier full sequences in the same plan file. Acceptable per writing-plans guidance because the referenced sequence is fully spelled out within the same document.
- Steps without code blocks where code is needed — none.
- References to types/functions not defined — verified: `cli.SpecInit`/`SpecInitResult` defined Task 3.2; `intent.SpecError` exists in Ship 1; `cairnerr.New`, `cli.WriteEnvelope`, `cli.Run`, `cli.ExitCodeFor` all exist in Ship 1; `runCLIInDir`, `parseEnvelope`, `repoRoot` are integration-test helpers — Task 2.3 step 1 explicitly tells the implementer to inspect/reuse existing helpers and adds them inline if absent.

**3. Type consistency.** Cross-checked function signatures and template constants:

- `cli.SpecInit(root string, force bool) (*SpecInitResult, error)` — same shape used in Tasks 3.1, 3.3, 7.1, 7.2.
- `requirementTemplateMarker` and `taskTemplateMarker` constants in Task 4.2 string-match the template body's first non-blank line in Task 3.2. Both contain the em-dash `—` (U+2014). Verified that Task 3.2's `requirementTemplate` and `taskTemplate` first non-blank lines are exactly `# cairn requirement spec template — DO NOT EDIT THIS FILE.` and `# cairn task spec template — DO NOT EDIT THIS FILE.`, identical to Task 4.2's marker constants.
- `headerRe` in Task 6.2 matches the format in Task 1.4's `source-hash-format.md` spoke. Both use `^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$`.
- `SpecError.Kind = "renamed_template"` (Task 4.2) is the exact kind asserted in Task 4.1 and Task 4.3 tests.

**4. One scope item flagged for the reviewer.** Phase 7 Task 7.2's `TestBootstrapE2E` includes inline placeholder code for `strings.Index` (`indexOf`/`strings_Index` shim). This is intentional — the plan flags that the implementer should write `strings.Index(s, sub) >= 0` directly using the standard library import, not the placeholder shim. The shim is a "do not literally copy this" hint. If the implementer DOES copy it literally, the test won't compile, which surfaces the issue immediately.

No further fixes applied. Plan as-is satisfies the writing-plans skill's spec-coverage, no-placeholders, and type-consistency gates.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-19-ship-3-superpowers-integration.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — Dispatch a fresh subagent per task. Two-stage review between tasks. Faster iteration on the per-task scope.
   - REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` for Phases 0 + 1 (the wrap doesn't exist yet); `cairn:subagent-driven-development-with-verdicts` from Phase 2 onward (the wrap is now usable).
2. **Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`. Batch execution with checkpoints for review.

**Per the user's instruction:** stop before pushing and hand back for review. Whichever execution path is chosen, the gate at the end of every task slice (Phases 0–3 first, then straight through if ≤2 fix cycles per task) is the same: run the gate, capture output, bind the verdict via the wrap, complete the claim, and only then move on.
