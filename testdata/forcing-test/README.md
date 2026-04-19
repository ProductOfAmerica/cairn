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
