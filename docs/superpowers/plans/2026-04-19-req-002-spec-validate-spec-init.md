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
