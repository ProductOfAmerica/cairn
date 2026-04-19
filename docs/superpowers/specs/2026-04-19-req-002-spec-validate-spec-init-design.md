# REQ-002 — `cairn spec validate` envelope + `cairn spec init` scaffold (design)

> Status: Bootstrap copy of Ship 3 design §5.
> Date: 2026-04-19.
> Source: `docs/superpowers/specs/2026-04-18-ship-3-superpowers-integration-design.md` §5.
> Purpose: Required prose source for REQ-002.yaml derivation. Hand-authored
> as Ship 3 bootstrap (design §6.1). Ship 4+ would derive REQ-002.yaml from
> a normal `superpowers:brainstorming` design doc.

## Scope + non-goals

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

## `cairn spec validate` envelope extension

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

## `cairn spec init` scaffold

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

## Renamed-template detection

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

## Migration

**None.** Both changes are read-side (envelope extension) or
filesystem-side (scaffold writes). No SQL migration. No new
event kinds.

## CLI help text

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

## Acceptance gates

- AC-002-TEST: `go test ./internal/intent/... ./internal/cli/... ./internal/integration/...` exit 0.
- AC-002-RUBRIC: documentation quality reviewer approves the help text additions, the renamed-template error message, and the embedded template comments.
