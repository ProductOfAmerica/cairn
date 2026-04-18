# cairn — plan

> A verification substrate for AI-coordinated software development.
> Standalone Go CLI. Invoked by Superpowers skills (or any agent), not run as a daemon.
> This document supersedes all prior drafts. It incorporates investigation of `obra/superpowers` v5.0.7 and the Q1–Q9 decisions captured on 2026-04-17.

## Read this first

This is the foundational planning document for cairn. Ship 1 begins in a **new, empty git repo** — not inside a Superpowers clone. This file was written from within a Superpowers investigation clone; nothing else was modified there.

Recommended bootstrap:

1. Create new empty repo (e.g. `cairn/`).
2. Copy this file to `PLAN.md` (or `CAIRN_PLAN.md`) at repo root.
3. `git init`, first commit.
4. Open fresh Claude Code session in that repo.
5. Confirm Ship 1 scope against §"Ship 1 — Core substrate".
6. Start coding.

Do not attempt to rewrite this plan from the previous-draft memory. The decisions captured in §"Upstream posture", §"Staleness (binary)", §"Idempotency", and §"Event-log completeness invariant" are load-bearing and each cost an argument to land.

---

## Context — the first-principles problem

AI coding agents fail in four recurring ways: context loss, no validation, no safe parallelism, no iteration loop. Methodology layers like Superpowers address some of these by encoding discipline as skills, commands, hooks, and subagent roles. The methodology is good and widely adopted — but methodology alone is stateless. When a subagent reports "task done," nothing verifies it. When a rubric is checked, the verdict isn't recorded anywhere durable. When a spec edit invalidates prior work, nothing notices. When a run fails partway through, the system can't replay or resume with fidelity. Evidence (test output, review notes, benchmark results) has no canonical home. Memory across sessions is ad-hoc.

Cairn is the missing layer: a persistent, queryable store that records what was decided, what was verified, against what evidence, and when. With cairn present, methodology gets teeth — skills can check before claiming, verify before completing, and surface drift between what was promised and what is actually true. Without cairn, methodology runs on trust.

Concrete pain points cairn addresses:

- **Branch-coupled state.** Any state agents write to tracked files (plans, progress notes, run logs) tangles with git branches, worktrees, and commits. Switching branches corrupts or duplicates coordination state. State must live *outside* the branch, keyed by repo identity.
- **Drift between spec and reality.** A task marked "done" against a spec that has since been edited is silently stale. Nothing flags this. Cairn must detect spec-hash drift and invalidate stale verdicts.
- **Unverified verdicts.** "Tests passed" is a claim unless the test output is hashed, stored, and bindable to the verdict. Cairn must content-address evidence and require hash-verified evidence for every gate pass.
- **No replay.** "Why did this task pass Monday and fail Friday" is unanswerable without an append-only verdict log.
- **Cross-session memory loss.** Each Claude Code session starts cold. Decisions, rationales, and outcomes from prior sessions are forgotten unless manually re-surfaced.
- **No safe concurrency.** If two subagents claim the same task, or work on overlapping code without coordination, the methodology has no mechanism to prevent it.

---

## Upstream posture (investigation finding — load-bearing)

**Cairn is not a contribution to Superpowers core.** Direct read of `obra/superpowers` `CLAUDE.md`:

> "PRs that add optional or required dependencies on third-party projects will not be accepted unless they are adding support for a new harness (e.g., a new IDE or CLI tool). Superpowers is a zero-dependency plugin by design. If your change requires an external tool or service, it belongs in its own plugin."

Add: 94% PR rejection rate, "compliance" changes to skills explicitly called out as rejected, fork-specific changes forbidden, `brainstorming` / `writing-plans` / `subagent-driven-development` guarded as tuned behavior-shaping code.

**Decision:** cairn ships as a **standalone plugin**. Users install Superpowers (for methodology) + cairn (for substrate). Any cairn-flavored skills live inside cairn's own plugin directory. A personal fork of Superpowers may layer additional glue skills for private use. **No upstream PRs to `obra/superpowers`.**

Downstream consequences of this posture:

- Cairn owns its own `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, its own `skills/`, its own `hooks/`.
- Cairn skills reference Superpowers skills by fully-qualified name (`superpowers:subagent-driven-development`) but do not modify them.
- Cairn skills borrow Superpowers terminology verbatim ("your human partner", "Red Flags", "Iron Law") to avoid culture collision when agents follow both plugins at once.
- The cairn marketplace must support at minimum Claude Code for Ship 3. Other harnesses (Cursor, Codex, Gemini, Copilot, OpenCode) come later if cairn proves out.

---

## What this is / is not

**Is:** small Go binary, single SQLite DB per repo, one-shot CLI invocations, library-first (every CLI command is a thin wrapper over a library function so other programs can import the library directly).

**Is not:** daemon, orchestrator, replacement for Superpowers, coordination server, multi-tenant, networked, dashboard, review UI.

---

## System shape

Seven layers. Each has one canonical substrate.

| Layer         | Substrate                                                       | Who writes                              |
| ------------- | --------------------------------------------------------------- | --------------------------------------- |
| Intent        | YAML files under `specs/` in the target repo (git)              | Humans via PR                           |
| Code          | Target repo, standard files                                     | Humans + agents                         |
| State         | SQLite at `<state-root>/<repo-id>/state.db`                     | `cairn` CLI invocations (via DB txn)    |
| Memory        | SQLite tables in same DB, append-only + FTS5                    | `cairn` CLI invocations                 |
| Evidence      | Content-addressed blob store at `<state-root>/<repo-id>/blobs/` | `cairn` CLI invocations                 |
| Orchestration | Superpowers skills + cairn skills + Claude Code subagents       | Claude Code                             |
| Projection    | Any skill or command that queries `cairn` and renders           | Read-only consumers                     |

### State-root resolution

- `CAIRN_HOME` env var wins if set.
- Linux: `$XDG_DATA_HOME/cairn` (default `$HOME/.local/share/cairn`), fallback `$HOME/.cairn`.
- macOS: `$HOME/.cairn`.
- Windows: `%USERPROFILE%\.cairn`.

### Repo identity

`sha256(canonical absolute path of 'git rev-parse --git-common-dir')`. Shared across worktrees — all worktrees of one repo resolve to the same `<repo-id>` and share state. Test this explicitly (worktree-of-worktree, submodules) in Ship 1.

### Blob store layout

`<state-root>/<repo-id>/blobs/<sha[:2]>/<sha>`. Two hex chars → 256 subdirs, bounded fanout on large repos. Content-addressed; dedupe on insert.

---

## Core invariants (non-negotiable)

These must hold across every command. Violations are architectural regressions.

1. **Every mutation goes through a cairn CLI command.** Agents and skills never write SQL directly.
2. **Spec lives in git, schema-validated.** No spec in the state DB. No prose acceptance criteria in the canonical path.
3. **Evidence is content-addressed and hash-verified before verdict binding.** Verdicts without verified evidence are rejected.
4. **Verdicts are append-only.** Staleness is a derived query, never a mutation. Old verdicts are never deleted or edited.
5. **Leases are time-bound with CAS acquisition.** Expired leases are invisible to new claim attempts; expired-lease cleanup happens inline during claim attempts or via explicit `cairn reconcile`.
6. **Every mutation carries an `op_id`.** Replaying a mutation with a seen `op_id` returns the cached result and does not re-execute.
7. **Offline-capable.** All core commands work without network. No vendor dependencies at runtime.
8. **Reconciliation is stateless and on-demand.** No daemon. `cairn reconcile` can run any time, any number of times, and produces the same result for the same inputs.
9. **The tool is the library.** Every CLI command is a thin wrapper over a library function. Other programs (tests, other binaries, eventually an MCP server) can import the library directly.
10. **The event log is the single source of truth for "what happened when."** Every mutation — claims, heartbeats, releases, verdicts, evidence insertions, task transitions, reconciliations, memory writes — emits an event in the same transaction as the mutation. `cairn events since <ts>` answers the full history without cross-referencing other tables. See §"Event-log completeness invariant" for the exhaustive event-kind table and the mandatory Ship 1 coverage test.

---

## Directory structure (reference layout for the new repo)

```
cairn/
├── PLAN.md                       # this file, copied into the new repo
├── cmd/
│   └── cairn/
│       ├── main.go               # cobra root, subcommand registration
│       ├── init.go               # `cairn init`
│       ├── spec.go               # `cairn spec validate`, `cairn spec hash`
│       ├── task.go               # claim, heartbeat, complete, release, list, plan
│       ├── verdict.go            # report, latest, history
│       ├── evidence.go           # put, verify, get
│       ├── memory.go             # append, search, list
│       ├── events.go             # since, tail
│       └── reconcile.go          # `cairn reconcile`
├── internal/
│   ├── db/                       # open, migrate, txn, CAS, retry
│   ├── intent/                   # types, loader, schema, hash
│   ├── task/                     # claim, lease, complete
│   ├── verdict/                  # report, latest, history, staleness (binary)
│   ├── evidence/                 # put, verify, store
│   ├── memory/                   # append, search (FTS5)
│   ├── events/                   # append, query, completeness test
│   ├── reconcile/                # five rules, idempotent
│   ├── ids/                      # ULID, op_id
│   └── repoid/                   # repo identity via git-common-dir
├── specs/                        # cairn's own specs (dogfood)
│   ├── requirements/
│   └── tasks/
├── testdata/                     # golden fixtures, worktree scenarios
├── go.mod
├── go.sum
└── README.md
```

Note: **no `replay.go`**. The replay-as-of capability is implicit in event log queries; no dedicated CLI command. See §"Explicitly deferred".

---

## Dependencies (minimum set)

- `modernc.org/sqlite` — pure-Go SQLite driver. No CGO. Cross-platform.
- `gopkg.in/yaml.v3` — spec parsing.
- `github.com/santhosh-tekuri/jsonschema/v5` — JSON Schema validation.
- `github.com/spf13/cobra` — CLI framework.
- `github.com/oklog/ulid/v2` — ULID generation.

Deliberately excluded: ORM, web framework, LLM SDK, git library (shell out to `git` — cairn already requires git to be present for repo identity).

---

## SQLite schema

Revised from prior drafts: `sensitivity` columns removed (cut from Ship 1, reintroduce when producer polymorphism arrives); `producer_user_hash`/`producer_vendor_hash` collapsed to single `producer_hash`; no three-tier staleness state stored on disk.

```sql
-- 001_init.sql

CREATE TABLE requirements (
    id                  TEXT PRIMARY KEY,
    spec_path           TEXT NOT NULL,
    spec_hash           TEXT NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE TABLE gates (
    id                  TEXT PRIMARY KEY,
    requirement_id      TEXT NOT NULL REFERENCES requirements(id),
    kind                TEXT NOT NULL,            -- test|property|rubric|human|custom
    definition_json     TEXT NOT NULL,
    gate_def_hash       TEXT NOT NULL,
    producer_kind       TEXT NOT NULL,            -- executable|human|agent|pipeline
    producer_config     TEXT NOT NULL
);

CREATE TABLE tasks (
    id                  TEXT PRIMARY KEY,
    requirement_id      TEXT NOT NULL REFERENCES requirements(id),
    spec_path           TEXT NOT NULL,
    spec_hash           TEXT NOT NULL,
    depends_on_json     TEXT NOT NULL DEFAULT '[]',
    required_gates_json TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL,            -- open|claimed|in_progress|gate_pending|done|failed|stale
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE TABLE claims (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    agent_id            TEXT NOT NULL,
    acquired_at         INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    released_at         INTEGER,
    op_id               TEXT NOT NULL UNIQUE
);

CREATE TABLE runs (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    claim_id            TEXT NOT NULL REFERENCES claims(id),
    started_at          INTEGER NOT NULL,
    ended_at            INTEGER,
    outcome             TEXT                      -- done|failed|orphaned|NULL
);

CREATE TABLE evidence (
    id                  TEXT PRIMARY KEY,
    sha256              TEXT NOT NULL UNIQUE,
    uri                 TEXT NOT NULL,
    bytes               INTEGER NOT NULL,
    content_type        TEXT NOT NULL,
    created_at          INTEGER NOT NULL
);

-- Append-only. Staleness is derived, not stored. Binary (fresh | stale).
CREATE TABLE verdicts (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES runs(id),
    gate_id             TEXT NOT NULL REFERENCES gates(id),
    status              TEXT NOT NULL,            -- pass|fail|inconclusive
    score_json          TEXT,
    producer_hash       TEXT NOT NULL,            -- collapsed from user/vendor split
    gate_def_hash       TEXT NOT NULL,
    inputs_hash         TEXT NOT NULL,
    evidence_id         TEXT REFERENCES evidence(id),
    bound_at            INTEGER NOT NULL,
    sequence            INTEGER NOT NULL          -- monotonic tiebreaker
);

CREATE INDEX idx_verdicts_latest ON verdicts(gate_id, bound_at DESC, sequence DESC);

CREATE TABLE events (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    at                  INTEGER NOT NULL,
    kind                TEXT NOT NULL,
    entity_kind         TEXT NOT NULL,
    entity_id           TEXT NOT NULL,
    payload_json        TEXT NOT NULL,
    op_id               TEXT
);

CREATE INDEX idx_events_entity ON events(entity_kind, entity_id);
CREATE INDEX idx_events_at ON events(at);

CREATE TABLE memory_entries (
    id                  TEXT PRIMARY KEY,
    at                  INTEGER NOT NULL,
    kind                TEXT NOT NULL,            -- decision|rationale|outcome|failure
    entity_kind         TEXT,
    entity_id           TEXT,
    body                TEXT NOT NULL,
    tags_json           TEXT NOT NULL DEFAULT '[]'
);

CREATE VIRTUAL TABLE memory_fts USING fts5(
    body, tags, content='memory_entries', content_rowid='rowid'
);

CREATE TABLE op_log (
    op_id               TEXT PRIMARY KEY,
    kind                TEXT NOT NULL,
    first_seen_at       INTEGER NOT NULL,
    result_json         TEXT NOT NULL
);
```

Timestamps: integer milliseconds since epoch. IDs: ULIDs (sortable, collision-resistant, human-greppable) except `op_id` which is caller-supplied.

---

## Intent spec format

YAML, JSON-Schema-validated. Same format as prior drafts, minus `sensitivity`.

```yaml
# specs/requirements/REQ-001.yaml
id: REQ-001
title: Fast login path
why: p95 login is 800ms; users drop.
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
  - id: AC-002
    kind: rubric
    producer:
      kind: human
      config:
        reviewer_role: security_engineer
```

```yaml
# specs/tasks/TASK-017.yaml
id: TASK-017
implements: [REQ-001]
depends_on: [TASK-016]
required_gates: [AC-001, AC-002]
```

### Spec-format posture in the Superpowers ecosystem

- **Ships 1–2:** hand-author YAML in toy repos for dogfood. No integration with Superpowers `brainstorming` or `writing-plans`.
- **Ship 3:** **additive sidecar**. Prose specs under `docs/superpowers/specs/*.md` stay exactly as Superpowers produces them. Cairn YAML lives alongside under `specs/requirements/*.yaml` and `specs/tasks/*.yaml`, hand-authored by the agent after the prose spec is approved. The `using-cairn` skill teaches the agent to emit both: prose for human review, YAML for machine verification.
- **Post-Ship 4:** revisit. Maybe YAML becomes canonical; maybe prose+YAML coexist long-term; maybe a skill auto-derives YAML from approved prose. No decision now.

---

## CLI surface

Every command: open DB, one transaction, exit. Every mutation emits an event. Every mutation accepts `--op-id <id>`; if omitted, cairn generates one and prints it in the JSON response for the caller to record.

```
cairn init [--repo-root <path>]
cairn spec validate [--path specs/]
cairn task plan
cairn task list [--status open|claimed|...]
cairn task claim <task_id> --agent <id> --ttl 30m [--op-id <id>]
cairn task heartbeat <claim_id> [--op-id <id>]
cairn task release <claim_id> [--op-id <id>]
cairn task complete <claim_id> [--op-id <id>]
cairn verdict report --gate <gate_id> --run <run_id> \
                     --status pass|fail|inconclusive \
                     --evidence <path> \
                     --producer-hash <hash> \
                     --inputs-hash <hash> \
                     [--score-json <json>] [--op-id <id>]
cairn verdict latest <gate_id>
cairn verdict history <gate_id>
cairn evidence put <path>
cairn evidence verify <sha256>
cairn evidence get <sha256>
cairn memory append --kind decision|rationale|outcome|failure \
                    --body <text> [--entity-kind task --entity-id <id>] \
                    [--tags tag1,tag2] [--op-id <id>]
cairn memory search <query> [--limit 10] [--kind ...]
cairn memory list [--entity-id <id>] [--since <ts>]
cairn events since <timestamp> [--limit 100]
cairn reconcile [--dry-run]
```

All commands output JSON by default. Add `--format human` where it meaningfully helps.

**Removed from prior drafts:** no `cairn replay --as-of`. Replay capability is implicit in `cairn events since <ts>` plus client-side projection. If reconstruction proves common and painful in Ship 4 use, add a thin wrapper later.

---

## Reconciliation rules

`cairn reconcile` runs these, in order, inside a single transaction:

1. For every `claims` row where `expires_at < now` and `released_at IS NULL`, mark released. For every `tasks` row in `claimed|in_progress|gate_pending` with no live claim, revert to `open`.
2. For every `done` task, recompute staleness for each required gate. If any required gate is stale, flip task to `stale`.
3. Probe 5% of evidence rows (random sample, bounded to 100 per run). For any whose file is missing or hash mismatches, mark evidence invalid and flag dependent verdicts.
4. For every `runs` row where `ended_at IS NULL` and the associated `claims.released_at + 10min < now`, mark orphaned (outcome=`orphaned`). The 10-minute grace absorbs clock skew between agents and DB and ensures rule 1's just-released claims are not immediately orphaned; the rule must therefore run after rule 1 within the same transaction.
5. Report any task whose `required_gates` include gate IDs not present in state. (Authoring errors surfaced.)

Each rule that mutates state emits an event (`reconcile_rule_applied`) inside the transaction. See §"Event-log completeness invariant".

Callers: the `reconcile` command. Also called inline by `cairn task claim` before looking for claimable tasks, so expired leases are cleaned opportunistically.

---

## Staleness (binary)

For a verdict V against gate G with current spec S:

- **Fresh:** `V.gate_def_hash == S.gate_def_hash` AND `V.inputs_hash == current_inputs_hash` AND `V.status == pass`.
- **Stale:** anything else.

No soft-stale tier. No `strict_producer_staleness` config knob. The `gate_def_hash` already includes whatever the user chooses to expose to staleness (prompt, temperature, system instruction, producer identity). If vendor version bumps must invalidate verdicts, capture them inside `gate_def_hash` at gate-definition time.

Revisit in Ship 4 if binary turns out to under- or over-invalidate in real use. Soft-stale can be added back as a derived query without schema change.

---

## Concurrency

SQLite in WAL mode. Every mutation is a single `BEGIN IMMEDIATE` transaction. CAS via `UPDATE ... WHERE ... RETURNING`. On `SQLITE_BUSY`, retry with exponential backoff up to 500ms. Multi-process coordination is native to SQLite at single-host scale.

Multi-host is explicitly out of scope. If ever needed, swap backend to Postgres — schema and queries translate cleanly.

---

## Idempotency — why op_log is retained

The subtraction pressure on `op_log` was rejected. Justification:

- **Cost:** one table, one index, one two-line check per mutation (`SELECT result_json FROM op_log WHERE op_id = ?; if hit, return it and skip`).
- **Failure mode without it:** network blip + skill retry → double-claim, double-verdict, double-complete. Silent.
- **Not speculative:** the Superpowers flow has multiple points where a skill might re-dispatch on apparent failure (timeout, dropped connection, agent-reported BLOCKED that actually landed). `op_id` makes retry safe by construction.

Callers supply `op_id` on every mutation; if omitted, cairn generates one and includes it in the JSON response so the caller can record and replay it.

---

## Memory (FTS5) — why FTS5 is retained

The subtraction pressure on FTS5 was rejected. Justification:

- **Cost:** one `CREATE VIRTUAL TABLE ... USING fts5(...)` statement. Built into SQLite, no extra dependency.
- **Value:** substantial over grep even in small corpora — stemming, ranking, multi-term AND queries, phrase matching.
- **Not speculative:** cross-session decision recall is a core cairn purpose. Memory without search degrades to a linear log you hope is short.

Embeddings remain deferred (see §"Explicitly deferred"). If FTS5 proves insufficient after three months, add embeddings as a sidecar.

---

## Event-log completeness invariant (explicit)

Invariant 10 says `cairn events since <ts>` is the single source of truth for "what happened when." To honor it, every mutation type must emit an event **in the same transaction** as the mutation. The required event kinds:

| Event kind                 | Emitted by                              | Payload essentials                              |
| -------------------------- | --------------------------------------- | ----------------------------------------------- |
| `task_planned`             | `cairn task plan`                       | task_id, requirement_id, spec_hash              |
| `task_status_changed`      | any mutation that flips `tasks.status`  | task_id, from, to, reason                       |
| `claim_acquired`           | `cairn task claim`                      | claim_id, task_id, agent_id, expires_at         |
| `claim_heartbeat`          | `cairn task heartbeat`                  | claim_id, new_expires_at                        |
| `claim_released`           | `cairn task release` / reconcile rule 1 | claim_id, reason (voluntary\|expired)           |
| `run_started`              | implicit on claim acquire               | run_id, claim_id, task_id                       |
| `run_ended`                | `cairn task complete` / reconcile       | run_id, outcome                                 |
| `evidence_stored`          | `cairn evidence put`                    | evidence_id, sha256, bytes, content_type        |
| `evidence_invalidated`     | reconcile rule 3                        | evidence_id, reason                             |
| `verdict_bound`            | `cairn verdict report`                  | verdict_id, gate_id, run_id, status, all hashes |
| `memory_appended`          | `cairn memory append`                   | memory_id, kind, entity_kind, entity_id         |
| `reconcile_started`        | `cairn reconcile`                       | reconcile_id                                    |
| `reconcile_rule_applied`   | each reconcile rule that mutated        | reconcile_id, rule_number, affected_entity_ids  |
| `reconcile_ended`          | `cairn reconcile`                       | reconcile_id, stats                             |
| `spec_materialized`        | `cairn task plan` when a spec hash changes | spec_path, old_hash, new_hash                |

**Mandatory Ship 1 CI test.** After running the Ship 1 dogfood flow end-to-end, the test asserts:

```
cairn events since 0 | jq -r '.kind' | sort -u
```

covers every event kind in the table above that applies to the exercised surface. This test lives in Ship 1, not Ship 2 or later. Any new mutation added in later ships adds a new event kind + extends this assertion in the same PR.

---

## Ship plan

Four ships. Single developer. Target: end-to-end dogfood by end of week 4.

### Ship 1 — Core substrate (week 1)

Goal: `cairn init`, spec validation, task lifecycle, verdict reporting, evidence store. Dogfood: manual CLI use only.

Scope:
- Go module, cobra scaffold, CI.
- SQLite schema + migrations (as above).
- `internal/ids` — ULID, op_id validation.
- `internal/repoid` — repo identity via `git rev-parse --git-common-dir`. Worktree coverage in tests.
- `internal/db` — open (WAL), txn helpers, CAS, retry on BUSY.
- `internal/events` — append + query + **completeness-coverage helper** used by the Ship 1 CI test.
- `internal/intent` — loader, schema validation, hashing.
- `internal/evidence` — put (content-address, two-hex sharding, dedupe), verify (reachability + hash), get.
- `internal/task` — claim (CAS with inline expired-lease cleanup), heartbeat, release, complete (binary staleness check before flip to done).
- `internal/verdict` — report (requires evidence verify pass), latest, history, binary staleness.
- CLI commands: `init`, `spec validate`, `task plan`, `task list`, `task claim`, `task heartbeat`, `task release`, `task complete`, `verdict report`, `verdict latest`, `verdict history`, `evidence put`, `evidence verify`, `evidence get`, `events since`.

Done when: Ship 1 dogfood scenario below passes, including the event-log completeness test.

### Ship 2 — Reconcile, memory (week 2)

Goal: staleness detection, memory append + FTS5 search, on-demand reconciliation.

Scope:
- `internal/memory` — append, FTS5 search, list.
- `internal/reconcile` — five rules, idempotent, each emitting events.
- CLI commands: `memory append`, `memory search`, `memory list`, `reconcile`.
- Extend the event-log completeness test to cover the reconcile and memory kinds.
- **Ship 3 target selected and written as spec:** pick one concrete small improvement to cairn itself. Write it as `specs/requirements/REQ-002.yaml` plus 2–4 task YAMLs inside cairn's own `specs/`. Candidate examples (choose one at the end of Ship 2, based on what's actually painful after real Ship 1–2 use — do not prematurely commit):
  - Add a `cairn task tree` command that prints the requirement → task → gate dependency graph.
  - Add `cairn verdict diff <gate>` showing what changed between the last two bindings.
  - Add a `--watch` mode to `cairn events since` for tailing during dogfood sessions.

Done when: Ship 2 dogfood passes + Ship 3 target captured in spec form.

### Ship 3 — Superpowers integration + cairn dogfoods cairn (week 3)

Goal: Superpowers skills (a narrowed set) call cairn at the right moments. End-to-end AI-driven flow works. **The real feature built in Ship 3 is the Ship-2-selected improvement to cairn itself — cairn dogfoods cairn.** This guarantees the repo, the substrate, and the motivation for end-to-end testing all exist, and the first real user of cairn is cairn's own development.

Narrowed skill integration scope (from Q2 decision):

- **New skill in the cairn plugin: `using-cairn`** — teaches agents when and how to invoke cairn, enumerates the CLI, shows the claim → evidence → verdict → complete cycle with worked examples. References `superpowers:subagent-driven-development` and `superpowers:verification-before-completion` by fully-qualified name.
- **Wrap `superpowers:subagent-driven-development`** — cairn ships its own variant (e.g. `cairn:subagent-driven-development-with-verdicts`) that layers `cairn task claim` before dispatch, `cairn evidence put` + `cairn verdict report` after the gate runs, `cairn task complete` at the end. The original Superpowers skill is **not modified**; the `using-cairn` skill tells agents when to prefer cairn's variant.
- **Wrap `superpowers:verification-before-completion`** — cairn provides `cairn:verdict-backed-verification`. Same pattern — enforces the same discipline with hash-verified evidence instead of agent-discipline-alone.
- **Maybe wrap `superpowers:test-driven-development`** — only if a clean insertion point exists where emitting a verdict after RED-GREEN is natural. If it would require behavioral changes to TDD discipline, skip and revisit in Ship 4.
- **Do not touch** `superpowers:brainstorming`, `superpowers:writing-plans`. Prose specs stay as Superpowers produces them; cairn YAML is authored separately as additive sidecar (per §"Spec-format posture").
- **No cairn SessionStart hook** (Q6 decision). Reconcile is called explicitly (by skills or by the user running `cairn reconcile`). Avoids conflicting with Superpowers' existing SessionStart hook.
- **Claude Code only** for Ship 3 skill support (Q7 decision). Cursor / Codex / Gemini / Copilot / OpenCode ports come after cairn proves out.
- **`code-reviewer` agent integration** (Q8 decision): the cairn `using-cairn` skill documents the pattern where the `superpowers:code-reviewer` agent is passed the relevant `gate_id` and shells out to `cairn verdict report` + `cairn evidence put` directly to bind a verdict. No agent wrapping. The cairn plugin does not ship its own reviewer agent in Ship 3.

Done when: Ship 3 dogfood passes — cairn's REQ-002 is implemented in cairn via Superpowers + cairn end-to-end, with full event trail visible via `cairn events since <session-start>`.

### Ship 4 — Use it (week 4)

Goal: use the system on actual work outside cairn itself. Find what breaks. Fix only that.

No pre-planned scope. The point is to surface real pain, not build more features. Likely fix areas: staleness tuning, FTS5 adequacy, spec-format ergonomics, skill integration friction.

---

## Explicitly deferred

- Daemon. Long-running process. HTTP server. Dashboard. Review UI.
- Agent producer (LLM-backed rubric scoring). Human producer via Superpowers review flows covers subjective gates for now.
- Pipeline producer. Composition happens in skills, not in cairn.
- Embeddings in memory. FTS5 only. Reconsider after three months of use.
- Cost estimation. Budget enforcer.
- Multi-host, Postgres, cross-repo aggregation.
- Linear / GitHub / Notion / Slack projection.
- MCP server wrapping the library.
- **Upstream PRs to `obra/superpowers`.** Standalone plugin only (see §"Upstream posture").
- **Soft-stale tier.** Binary staleness for now (see §"Staleness (binary)").
- **Three-tier producer hashing.** Single `producer_hash` for now.
- **`sensitivity` field.** Cut from Ship 1 schema; reintroduce when producer polymorphism arrives.
- **`cairn replay --as-of` command.** Event-log query covers the use case.

---

## Dogfood scenarios

### Ship 1 dogfood

1. In a throwaway test repo, run `cairn init`.
2. Write `specs/requirements/REQ-001.yaml` with a single gate: `kind: test`, producer runs `echo ok` with exit 0.
3. Write `specs/tasks/TASK-001.yaml` requiring AC-001.
4. `cairn spec validate` — passes.
5. `cairn task plan` — materializes requirement, gate, task; emits `task_planned` and `spec_materialized`.
6. `cairn task list` — TASK-001 is `open`.
7. `cairn task claim TASK-001 --agent test-agent --ttl 10m`. Prints claim_id. Emits `claim_acquired`, `run_started`, `task_status_changed (open→claimed)`.
8. Run the gate's command, capture output. `cairn evidence put <output-file>` — prints sha256 and uri. Emits `evidence_stored`.
9. `cairn verdict report --gate AC-001 --run <run_id> --status pass --evidence <output-file> --producer-hash <hash> --inputs-hash <hash>`. Emits `verdict_bound`.
10. `cairn task complete <claim_id>` — task transitions `claimed → done` after all required gates verify fresh + pass. Emits `task_status_changed (claimed→done)`, `run_ended`, `claim_released`.
11. **Event-log completeness test:** `cairn events since 0 | jq -r '.kind' | sort -u` must cover all event kinds applicable to this flow: `task_planned`, `spec_materialized`, `claim_acquired`, `run_started`, `task_status_changed`, `evidence_stored`, `verdict_bound`, `run_ended`, `claim_released`. This assertion is a Ship 1 CI test.
12. Edit `specs/requirements/REQ-001.yaml` (add a scope entry). `cairn task plan` again. `cairn verdict latest AC-001` — now reports stale.

### Ship 2 dogfood

1. Start from Ship 1's final state.
2. `cairn reconcile` — rule 2 flips TASK-001 done → stale based on edited spec. Emits `reconcile_started`, `reconcile_rule_applied`, `task_status_changed (done→stale)`, `reconcile_ended`.
3. `cairn memory append --kind decision --body "chose to hash evidence before binding"` — emits `memory_appended`.
4. `cairn memory search "evidence"` — returns the entry.
5. Claim a fresh task with `--ttl 1s`, do not heartbeat, wait 2 seconds. `cairn reconcile` — rule 1 releases the claim and reverts the task to `open`. Events emitted as above.
6. Extended event-log completeness test now covers: `reconcile_started`, `reconcile_rule_applied`, `reconcile_ended`, `memory_appended`, `claim_released` (reason=expired).
7. Select the Ship 3 target — write it as `specs/requirements/REQ-002.yaml` + tasks.

### Ship 3 dogfood — cairn improves cairn

1. User opens Claude Code with Superpowers + cairn installed, working in the cairn repo.
2. User: "Let's implement REQ-002" (the Ship 2–chosen improvement to cairn).
3. `superpowers:brainstorming` runs unmodified → prose spec at `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`. User approves.
4. Agent (or user) hand-authors the cairn YAML sidecar: `specs/requirements/REQ-002.yaml` + 2–4 `specs/tasks/TASK-00N.yaml` files. `cairn spec validate` passes. `cairn task plan` materializes. (Automatic prose-to-YAML extraction is deferred past Ship 4.)
5. `superpowers:writing-plans` runs unmodified → prose plan at `docs/superpowers/plans/YYYY-MM-DD-<feature>.md`.
6. Agent invokes `cairn:subagent-driven-development-with-verdicts` instead of the Superpowers original. For each task:
   - `cairn memory search "<topic>"` for prior cross-session context.
   - `cairn task claim <id> --agent <sub> --ttl 30m`.
   - Subagent implements under TDD.
   - Run the gate's test command, capture output.
   - `cairn evidence put` on the output file, `cairn verdict report` with the gate_id.
   - Dispatch `superpowers:code-reviewer` with the rubric `gate_id`. The reviewer agent shells out to `cairn verdict report` (binding a human verdict) and `cairn evidence put` (storing the review text as evidence). No agent wrapping.
   - `cairn task complete`.
   - `cairn memory append --kind outcome --body "..."`.
7. `cairn events since <session-start>` shows the full trail. Manual spot-check: every mutation above appears as an event of the right kind.
8. REQ-002 is actually implemented in cairn, via cairn, verified by cairn. The first real user of cairn has been cairn's own development.

---

## Open risks

| Risk                                                      | Mitigation                                                                                                                                                        |
| --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SQLite lock contention under concurrent skill invocations | WAL mode + retry on BUSY up to 500ms. Ship 1 CI runs ≥5 concurrent invocations against the same DB.                                                               |
| Skills forget to call cairn at the right moments          | Discipline encoded in `using-cairn`, `cairn:subagent-driven-development-with-verdicts`, `cairn:verdict-backed-verification`. Reconcile catches forgotten completions. Missing verdicts surface as stale on next check. |
| Repo identity collisions across worktrees                 | Resolved: keyed off `git rev-parse --git-common-dir`. Ship 1 has explicit worktree tests.                                                                         |
| Binary staleness too aggressive or too lax                | Log every staleness flip. Tune in Ship 4 from real use. If binary is insufficient, re-introduce soft-stale as a derived query — no schema change.                 |
| FTS5 search too weak                                      | Ship FTS5. If three months in it's clearly insufficient, add embeddings as a sidecar.                                                                             |
| Ship 3 skill integration slips past week 3                | Acceptable. Ship 3 is the integration ship; slipping it does not block the substrate's existence.                                                                 |
| Event-log completeness regresses                          | CI test in Ship 1 asserts coverage. Any new mutation PR must extend the assertion in the same PR.                                                                 |
| Cairn skills diverge from Superpowers terminology         | `using-cairn` borrows Superpowers phrases verbatim ("your human partner", "Red Flags", "Iron Law"). Do not paraphrase.                                            |
| YAML + prose divergence (sidecar posture)                 | Ship 3 `using-cairn` teaches the agent to write both. If they diverge, the prose is canonical for humans; the YAML is canonical for cairn. Ship 4 may automate.   |
| Ship 2 Ship-3-target selection is premature               | Defer the selection until the end of Ship 2, after 1–2 weeks of real substrate use. Do not commit in Ship 1.                                                      |

---

## What success means

- After **Ship 1**: full verify-and-complete loop works manually on a toy spec. Event-log completeness verified in CI.
- After **Ship 2**: drift detected, memory searchable, reconciliation idempotent. Ship 3 target captured as a real spec.
- After **Ship 3**: cairn improved cairn via Superpowers + cairn, with every claim, verdict, evidence, and memory entry durable and verifiable.
- After **Ship 4**: you know which parts of cairn earn their complexity and which are overbuilt. Revise accordingly.

---

## Before you code (in the new cairn repo)

1. Confirm Ship 1 scope against §"Ship 1 — Core substrate". Nothing more, nothing less.
2. Bootstrap: `go mod init github.com/<you>/cairn`, cobra skeleton under `cmd/cairn/`, CI config, `.gitignore`, `README.md` stub. No code in `internal/` yet.
3. Write the schema migration first. Write a test that opens the DB, applies the migration, inspects `sqlite_master` for every expected table and index.
4. Build bottom-up in this order: `internal/ids` → `internal/repoid` → `internal/db` → `internal/events` (with completeness helper) → `internal/intent` → `internal/evidence` → `internal/task` → `internal/verdict` → CLI glue.
5. The event-log completeness CI test is **mandatory in Ship 1**, not Ship 2 or later.
6. Every CLI mutation command accepts `--op-id` and records it in `op_log`. If omitted, generate one and include it in the JSON response.
7. Every CLI command outputs JSON by default. `--format human` only where it meaningfully helps.

**Do not** start Ship 2 features (memory, reconcile) in Ship 1. **Do not** touch Superpowers skills in Ship 1 or Ship 2. Integration is explicitly Ship 3 territory and only for the narrowed skill list above.

---

## Provenance

This document incorporates:

- The original multi-page cairn plan supplied at session start on 2026-04-17.
- Direct investigation of `obra/superpowers` v5.0.7 (README.md, CLAUDE.md, AGENTS.md, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `hooks/hooks.json`, `hooks/session-start`, all 15 skills under `skills/`, deprecated commands under `commands/`, the `code-reviewer` agent under `agents/`, `.github/PULL_REQUEST_TEMPLATE.md`, a recent Superpowers plan for pattern reference).
- Q1–Q9 decisions from 2026-04-17:
  - Q1: standalone plugin repo.
  - Q2: narrowed Ship 3 — `cairn:subagent-driven-development-with-verdicts`, `cairn:verdict-backed-verification`, `using-cairn`, maybe TDD wrap.
  - Q3: hand-author YAML in Ships 1–2, additive sidecar in Ship 3.
  - Q4: `~/.cairn/<repo-id>/` with `CAIRN_HOME` / XDG / `%USERPROFILE%` resolution; repo identity via `git-common-dir`; blob sharding by first two hex chars.
  - Q5: cairn dogfoods cairn. Concrete REQ-002 chosen at end of Ship 2.
  - Q6: no cairn SessionStart hook.
  - Q7: Claude Code only for Ship 3.
  - Q8: `code-reviewer` agent calls cairn directly; no agent wrapping.
  - Q9: `sensitivity` cut from Ship 1 schema.
- Subtraction outcomes:
  - Accepted cuts: three-tier staleness → binary; producer user/vendor split → single `producer_hash`; `cairn replay --as-of` → cut; `sensitivity` → cut.
  - Rejected cuts: `op_log` kept (retry safety); FTS5 kept (free and strictly better than grep).
- Event-log-completeness invariant elevated to Core Invariant 10, with explicit event-kind table and mandatory Ship 1 CI test.