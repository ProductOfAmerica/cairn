# Lesson: Go module deps must land with the code that imports them

**One-line summary:** Never pre-add Go dependencies via `go get` ahead of
importing source. Module commands implicitly run `go mod tidy`, which strips
any require that no file imports, silently reverting the change.

## Concrete incident (cairn Ship 1, 2026-04-17)

- Task 0.1 added `ulid/v2` + `testify` via `go get`; `go mod verify` populated
  the full `go.sum`.
- Task 0.2 added `gowebpki/jcs`, used it in a throwaway smoke test, then
  deleted the smoke test before commit.
- Module operations between those two steps triggered tidy. Net result:
  `go.mod` collapsed to only `jcs // indirect`; `ulid/v2`, `testify`,
  `cobra`, `sqlite`, `jsonschema/v6`, and `yaml.v3` all disappeared from
  `go.mod` / `go.sum`. Both commits (`04e9ba7`, `2eb199a`) became functional
  no-ops despite their subjects claiming to add those deps.

## Why this happens

- Since Go 1.17, `go mod tidy` and several module subcommands normalize
  `go.mod` by dropping requires that no package imports.
- `go get <pkg>@latest` adds the require, but the module is only retained if
  a Go source file imports it.
- Bootstrap-style phases that add deps ahead of any code are incompatible
  with this behavior.

## How to apply (for Ship 2 and beyond)

- In plans and task prompts, move dependency additions into the **first task
  whose code imports the dep**. Examples from Ship 1:
  - `ulid/v2` → added in the ULID generator task (Task 2.1).
  - `modernc.org/sqlite` → added in the DB open task (Task 5.1).
  - `jsonschema/v6` → added in the schema validation task (Task 7.2).
  - `yaml.v3` → added in the YAML loader task (Task 7.1).
  - `gowebpki/jcs` → added in the `gate_def_hash` task (Task 7.4).
  - `cobra` → added in the first `cmd/cairn/*.go` task (Task 11.x).
- Task prompt pattern: *"write the code that imports X, then run
  `go get X@latest && go mod verify`, then commit both the code and the
  module changes in one commit."*
- Never run `go mod tidy` in a task that does not also land importing code.

## Fix-forward vs history rewrite

When this happens mid-branch, fix-forward (re-add deps inline with future
tasks) over `git reset --hard`. History rewrite is destructive; future
commits will repopulate `go.mod` / `go.sum` naturally.
