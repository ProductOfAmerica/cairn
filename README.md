# cairn

**A verification substrate for AI coding agents.** Cairn is a small Go binary that gives AI agents durable memory, append-only verdicts, content-addressed evidence, and safe concurrency — the things they need to actually finish work without you re-checking every claim.

Pair it with [Superpowers](https://github.com/obra/superpowers) to get methodology + verification end-to-end. Use it standalone if you just want a trustworthy log of what your agents did.

> Status: Ship 3 merged (2026-04-19). Plugin scaffold + three skills + REQ-002 (`spec init` and `spec validate` envelope) shipped. Ship 4 is the first real-world dogfood.

---

## The problem

AI coding agents fail in four predictable ways:

1. **Context loss.** Each session starts cold. Yesterday's decision is forgotten.
2. **No validation.** "Tests passed" is a claim. Nothing checks it.
3. **No safe parallelism.** Two agents claim the same task, step on each other.
4. **No iteration loop.** A task marked "done" against a spec that has since been edited is silently stale.

Methodology layers like Superpowers encode discipline as skills, hooks, and subagent roles. Discipline is good — but discipline alone is stateless. When an agent reports "done," nothing verifies it. When a rubric is checked, the verdict isn't recorded anywhere durable. When a spec changes, nothing notices the prior verdict is now lying.

Cairn is the missing layer. With cairn, agents can **claim before working, verify before completing, and surface drift between what was promised and what is actually true.**

---

## Quick start (5 minutes to your first verdict)

### Install the CLI

```bash
# Requires Go 1.25+ and git
go install github.com/ProductOfAmerica/cairn/cmd/cairn@latest
```

Or via a package manager:

```powershell
# Windows (scoop)
scoop bucket add cairn https://github.com/ProductOfAmerica/scoop-cairn
scoop install cairn
```

```powershell
# Windows (winget) — once PR microsoft/winget-pkgs#362607 merges
winget install ProductOfAmerica.cairn
```

### Initialize state + install skills

```bash
cd your-repo/
cairn setup           # state bootstrap + installs Claude Code skills
```

`cairn setup` does two things:

1. **State bootstrap.** Same as `cairn init` — creates the state DB and content-addressed blob store outside git at `$CAIRN_HOME/<repo-id>/` (defaults: `~/.local/share/cairn/` on Linux, `~/.cairn/` on macOS, `%USERPROFILE%\.cairn\` on Windows). Repo identity is keyed off `git rev-parse --git-common-dir` so worktrees share state.
2. **Writes cairn's Claude Code skills** to `<repo>/.claude/skills/` — Claude Code auto-loads skills under that path without requiring a `/plugin install` step. Restart Claude Code (or start a new session) and `using-cairn`, `subagent-driven-development-with-verdicts`, and `verdict-backed-verification` are available.

Re-run `cairn setup` safely: state is idempotent, and skill files are skipped if already present. Pass `--force` after upgrading cairn to refresh the skill files.

If you only want the state substrate and intend to register the plugin globally instead, use `cairn init` (state only, no skill writes). Other agentic harnesses (Cursor, Codex, Gemini, Copilot, OpenCode) drive the CLI directly today; skill-layer wraps for them are tracked in `docs/PLAN.md` (Ship 5+).

### Scaffold spec templates

```bash
cairn spec init
# Creates specs/requirements/REQ-001.yaml.example
#         specs/tasks/TASK-001.yaml.example
```

### Write a requirement

`specs/requirements/REQ-001.yaml`:

```yaml
id: REQ-001
title: Login is fast
why: p95 login is 800ms
scope_in: [auth/login, auth/session]
scope_out: [auth/signup]
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [go, test, ./auth/login/...]
        pass_on_exit_code: 0
```

`specs/tasks/TASK-001.yaml`:

```yaml
id: TASK-001
implements: [REQ-001]
depends_on: []
required_gates: [AC-001]
```

### Run the loop

```bash
cairn spec validate           # schema + referential checks
cairn task plan               # materializes specs into state DB
cairn task list               # TASK-001 is now "open"

# Claim with a 30-min lease
cairn task claim TASK-001 --agent me --ttl 30m

# Run the gate, capture output
go test ./auth/login/... > /tmp/gate.out 2>&1

# Store evidence (content-addressed, deduped)
cairn evidence put /tmp/gate.out

# Bind a verdict to that evidence
cairn verdict report --gate AC-001 --run <run_id> --status pass \
  --evidence /tmp/gate.out \
  --producer-hash <hash> --inputs-hash <hash>

# Complete the task (cairn refuses if any gate is stale or failing)
cairn task complete <claim_id>

# Audit everything that happened
cairn events since 0
```

Every mutation emits an event in the same transaction. `cairn events since <ts>` is the single source of truth for "what happened when."

---

## With Superpowers (the full workflow)

Cairn ships as a Claude Code plugin. After installing both plugins, agents follow this loop:

```
1. superpowers:brainstorming         → prose design at docs/superpowers/specs/
2. using-cairn (yaml-authoring)      → derives specs/requirements/REQ-NNN.yaml
3. superpowers:writing-plans         → prose plan at docs/superpowers/plans/
4. using-cairn (yaml-authoring)      → derives specs/tasks/TASK-NNN.yaml
5. cairn task plan                   → state materialized
6. cairn:subagent-driven-development-with-verdicts (per task):
   - cairn task claim
   - subagent implements under TDD
   - capture gate output
   - cairn evidence put + cairn verdict report (after both reviewers approve)
   - cairn task complete
   - cairn memory append
7. cairn events since <session-start> → full audit trail
```

**You never edit YAML by hand.** Agents derive YAML from prose deterministically; regeneration is byte-identical. Edit the prose, regenerate, and a `# cairn-derived:` comment carries the source hash so drift is detectable.

### Wrap routing

| Situation | Use |
|---|---|
| Plan execution inside a cairn-tracked repo | `cairn:subagent-driven-development-with-verdicts` |
| Plan execution, no `specs/` dir | `superpowers:subagent-driven-development` |
| About to claim "done" while holding a cairn claim | `cairn:verdict-backed-verification` |
| About to claim "done" with no claim | `superpowers:verification-before-completion` |
| Brainstorming, plan writing, TDD, code review | Superpowers originals — unchanged |

---

## CLI reference

All commands output JSON. Every mutation accepts `--op-id` for idempotent retry.

```
cairn init                                    Scaffold state DB for current repo
cairn setup         [--force]                 init + install Claude Code skills into .claude/skills/
cairn spec init      [--path specs/] [--force]   Scaffold annotated YAML templates
cairn spec validate  [--path specs/]             Schema + referential + uniqueness
cairn task plan                               Materialize specs into state
cairn task list      [--status ...]
cairn task claim     <task_id> --agent <id> --ttl 30m
cairn task heartbeat <claim_id>
cairn task release   <claim_id>
cairn task complete  <claim_id>               Fails if any required gate stale or failing
cairn verdict report --gate <id> --run <run_id> --status pass|fail|inconclusive \
                     --evidence <path> --producer-hash <h> --inputs-hash <h>
cairn verdict latest <gate_id>
cairn verdict history <gate_id>
cairn evidence put   <path>                   Content-addressed, deduped, sharded
cairn evidence verify <sha256>                Rehash and check
cairn evidence get   <sha256>
cairn memory append  --kind decision|rationale|outcome|failure --body <text> [...]
cairn memory search  <query>                  FTS5
cairn memory list    [--entity-id <id>] [--since <ts>]
cairn events since   <timestamp>              Single source of truth
cairn reconcile      [--dry-run]              5 idempotent rules
```

---

## How it works

Seven layers, each with one canonical substrate:

| Layer | Substrate | Who writes |
|---|---|---|
| Intent | YAML in `specs/` (in git) | Humans via PR |
| Code | Target repo (in git) | Humans + agents |
| State | SQLite at `<state-root>/<repo-id>/state.db` | `cairn` CLI only |
| Memory | SQLite tables, append-only + FTS5 | `cairn` CLI only |
| Evidence | Content-addressed blobs at `<state-root>/<repo-id>/blobs/` | `cairn` CLI only |
| Orchestration | Superpowers + cairn skills + subagents | Claude Code |
| Projection | Anything that reads cairn and renders | Read-only consumers |

**Reconcile** is on-demand, not a daemon. Five idempotent rules run in a single transaction:

1. Expire dead leases, revert their tasks to `open`.
2. Recompute staleness for every `done` task. Flip stale tasks.
3. Probe a sample of evidence blobs. Invalidate missing or corrupted rows.
4. Mark orphaned runs (claim released but run never ended).
5. Surface authoring errors (e.g., a task requires a gate that doesn't exist).

---

## Core invariants (the things you can rely on)

1. **Every mutation goes through the cairn CLI.** No agent writes SQL.
2. **Specs live in git.** Schema-validated. No prose acceptance criteria in the canonical path.
3. **Evidence is content-addressed and hash-verified before verdict binding.** Verdicts without verified evidence are rejected.
4. **Verdicts are append-only.** Staleness is a derived query, never a mutation.
5. **Leases are time-bound with CAS acquisition.** Two agents cannot hold the same task.
6. **Every mutation carries an `op_id`.** Replaying a mutation with a seen `op_id` returns the cached result and does not re-execute. Network blips and skill retries are safe by construction.
7. **Offline-capable.** No network, no vendor dependencies at runtime.
8. **Reconciliation is stateless.** Run it any time, any number of times — same result.
9. **The tool is the library.** Every CLI command is a thin wrapper. Other programs (tests, MCP servers, your own tooling) can import the library directly.
10. **The event log is the single source of truth.** Every mutation emits an event in the same transaction. `cairn events since <ts>` answers the full history without cross-referencing other tables. CI asserts every event kind is covered after dogfood runs.

---

## What cairn is not

- **Not a daemon.** One-shot CLI invocations.
- **Not an orchestrator.** Methodology lives in Superpowers (or whatever skill layer you use).
- **Not a replacement for Superpowers.** Cairn adds substrate; Superpowers adds discipline. They compose.
- **Not multi-tenant, not networked, not a dashboard.** Single host, SQLite, files. If you outgrow that, swap the backend to Postgres — schema and queries translate cleanly.
- **Not opinionated about your test runner, language, or git workflow.** It just records what you did.

---

## Why this exists

We built cairn because every AI-agent flow we tried eventually broke the same way: an agent confidently claimed a task was done, and we had no way to verify, no way to replay, no way to catch drift when the spec moved underneath. Methodology helped — until it didn't, because methodology is trust and trust is unverifiable.

Cairn is the unsexy substrate that makes trust unnecessary. Verdicts are bound to hashed evidence. Tasks can't complete without fresh gates. Spec edits invalidate prior verdicts automatically. Memory survives across sessions. Two agents can't claim the same work. The event log answers "what happened Friday at 3pm" deterministically.

Once that substrate exists, the methodology layer gets teeth.

---

## Status & roadmap

| Ship | What | Status |
|---|---|---|
| 1 | Core substrate: init, spec validate, task lifecycle, evidence, verdicts, events | **Done** |
| 2 | Reconcile (5 rules), memory + FTS5 search | **Done** |
| 3 | Superpowers integration: 3 skills, REQ-002 (`spec init` + envelope) | **Done** (one C1 forcing test deferred to first interactive session) |
| 4 | Use it on real work outside cairn. Find what breaks. Fix only that. | Next |

See [`docs/PLAN.md`](docs/PLAN.md) for the design decisions and Ship roadmap.

---

## Requirements

- **Go 1.25+** (build only — no runtime Go required after install).
- **git** on `PATH` (used for repo identity).
- **SQLite** is bundled via `modernc.org/sqlite` (pure Go, no CGO).

Cross-platform: Linux, macOS, Windows. CI matrix covers all three.

---

## Contributing

Cairn dogfoods cairn — feature work goes through the same `brainstorming → plan → claim → verdict → complete` loop the tool enforces. Read `docs/PLAN.md` and the `using-cairn` skill before opening a PR.

Three rules that are not negotiable:
- Don't add features that don't earn their complexity. Subtraction is the default.
- Every mutation emits an event. The CI completeness test will fail you otherwise.
- Don't try to upstream cairn into Superpowers. They are deliberately separate plugins.

---

## License

MIT. See [`LICENSE`](LICENSE).
