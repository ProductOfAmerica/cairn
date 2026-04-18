# Ship 3 polish notes — CLI ergonomics from canary

Items surfaced during the 2026-04-18 canary run against
`dreambot-scripts`. **Not spec ambiguities** — no substrate behavior
is ambiguous. These are DX wrinkles that make cairn harder to use
than it needs to be. Input for a Ship 3 or later CLI-polish pass.

Organized by command surface. Each item: what the user saw, why it's
awkward, suggested fix.

## `cairn verdict` — flag vs positional inconsistency

**Observed.** `cairn verdict report` takes `--gate <gate_id>` as a
flag; `cairn verdict latest` takes `<gate_id>` as a positional and
rejects `--gate` with `unknown flag: --gate`.

**Why awkward.** Two related subcommands under the same noun group
use different shapes for the same argument. Users muscle-memory one,
trip on the other. Error message is terse.

**Suggested fix options:**
- Unify on positional: change `verdict report` to take `<gate_id>`
  positionally, deprecate `--gate` with a one-version warning.
- Unify on flag: change `verdict latest` / `history` to accept
  `--gate` (and keep positional for backward compat).
- If both shapes must stay, make `verdict latest`'s error message
  suggest the positional form: `use: cairn verdict latest <gate_id>`.

Cheapest: the error-message fix. Safest: unify on whichever matches
Ship 1's other verb groups (check `task`, `evidence`).

## `cairn verdict latest` — no `--run` filter

**Observed.** `verdict latest` returns the latest verdict bound to a
gate, globally. No way to ask "is this verdict fresh for this run?"
without reading the verdict row and comparing run ids yourself.

**Why awkward.** For multi-run gates (same gate bound by multiple
runs over time), narrowing "latest for THIS run" requires
client-side filtering.

**Suggested fix.** Add `--run <run_id>` to `verdict latest` (and
`verdict history`). Filters to verdicts bound to that run.
Orthogonal to `--gate` positional.

## `cairn task complete <claim_id>` — ergonomic trap

**Observed.** `task complete` takes `<claim_id>`, not `<task_id>`.
The verb ("task complete") reads like the noun should be a task id.
Easy to paste the task id by muscle memory.

**Why awkward.** The semantic is correct — completing a specific
claim of a task, not the task-in-general — but the surface hides
that subtlety. Ship 1 decision; not a bug, but a trip hazard.

**Suggested fix options:**
- Add a "did you mean claim X for task Y?" hint when the argument
  matches a task id instead of a claim id.
- Accept either and disambiguate: if `<id>` matches a task with
  exactly one open claim, use that claim. If multiple, error with
  the list.
- Rename the subcommand to `claim complete` — noun matches argument.
  Breaking change; keep deferred until a larger CLI revamp.

Cheapest: the hint. Highest-impact: accept-either-with-disambiguation.

## `cairn spec validate` — silent on empty tree

**Observed.** `cairn spec validate` on an empty `specs/` tree
returns `{errors: []}` with exit 0. Caller can't tell whether the
validator found zero specs or zero errors.

**Why awkward.** `errors: []` is ambiguous between "all good" and
"nothing to check." A first-run user (or CI catching a missing
`specs/` dir) has no clear signal.

**Suggested fix.** Extend the response envelope:

```json
{
  "errors": [],
  "specs_scanned": {
    "requirements": 3,
    "tasks": 5
  }
}
```

If both counts are zero, the caller knows to treat it as a no-op
pass, not a silent success.

## `cairn task plan` — indistinguishable no-op

**Observed.** `task plan` response shape is
`{requirements_materialized: N, gates_materialized: M, tasks_materialized: K}`.
Same values whether the call inserted net-new rows or just upserted
unchanged ones.

**Why awkward.** Running `task plan` twice in a row returns identical
stats; no signal that the second call was a no-op. For dogfood flow
(edit spec → re-plan → reconcile), a clearer breakdown would help.

**Suggested fix.** Split each count:

```json
{
  "requirements": {"inserted": 0, "updated": 1, "unchanged": 2},
  "gates":        {"inserted": 1, "updated": 0, "unchanged": 3},
  "tasks":        {"inserted": 0, "updated": 1, "unchanged": 4}
}
```

If every `inserted + updated == 0`, caller knows the plan was a
pure no-op.

## `cairn spec init` (or `cairn spec example`) — missing scaffold

**Observed.** There's no `cairn spec init` or `cairn spec example`
command. A first-run user without access to the cairn repo itself
has nowhere to look for the spec YAML shape. Canary reporter found
the shape by reading `~/GitHub/cairn/testdata/e2e/*/specs/*.yaml`
and `internal/intent/types.go`.

**Why awkward.** The `cairn` binary is a plugin dep; the intent-spec
format is the contract; yet the contract is only discoverable by
reading cairn's own test fixtures.

**Suggested fix options:**
- `cairn spec init [--path specs/]` scaffolds `specs/requirements/`
  and `specs/tasks/` with one minimal example each (commented).
- `cairn spec example` prints an annotated example YAML to stdout.
- Embed the JSON Schema via `cairn spec schema requirement|task` so
  editors with YAML language server support can lint on save.

Cheapest: embed example YAML strings in the binary + print on
`cairn spec example`. Highest-value: the JSON Schema export
(enables IDE integration).

---

## Meta note

Most of these are one-commit polish items, each small enough to land
as a standalone PR. A Ship 3 brainstorm doesn't have to fold all of
them in — the substrate is complete; these are papercuts that make
the substrate more pleasant to use.
