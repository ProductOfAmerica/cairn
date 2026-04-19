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

> "Elicitation exceeded 3 questions for REQ-NNN. Per Ship 3 C1 constraint, flag this requirement for design-level rework before YAML emission."

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
