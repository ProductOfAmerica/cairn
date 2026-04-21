# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Cairn is a verification substrate for AI coding agents — a single-host Go CLI backed by SQLite that records durable memory, append-only verdicts, content-addressed evidence, and safe leases. It is **not** a daemon, orchestrator, or server. GitNexus guidance and index-freshness rules live in `AGENTS.md`.

Full design lives in `docs/PLAN.md` (decisions are load-bearing; read before making architectural changes). `README.md` is the user-facing quick start.

## Commands

```bash
go build ./...                          # build library + CLI (module: github.com/ProductOfAmerica/cairn)
go test -race ./...                     # full suite (unit + internal/integration e2e)
go test -race ./internal/task/...       # single package
go test -race -run TestName ./internal/x/   # single test
go vet ./...                            # vet
go mod verify                           # verify module graph (CI gate)
make test-skills-verify                 # static checks over skill fixtures (Linux-only in CI)
make test-skills-record                 # prints manual fixture regen procedure (not automated)
go build -o bin/cairn ./cmd/cairn       # build the binary
```

CI matrix: Linux, macOS, Windows on Go 1.25.x. A separate `offline` job proves Invariant 7 by running `go test -race ./...` with `iptables -P OUTPUT DROP` and `GOFLAGS=-mod=readonly GOSUMDB=off GOPROXY=off` — do not introduce runtime network dependencies.

## Architecture

### Library-first CLI

`cmd/cairn/*.go` files are cobra wrappers; every subcommand is a thin shell over `internal/...` Store methods. Each command body calls `cli.Run(stdout, kind, opID, func() (any, error) {...})` which emits a single JSON envelope on stdout and maps the error to an exit code. Keep command bodies ≤ ~30 LOC — logic lives in the store.

- **JSON envelope** (`internal/cli/envelope.go`): `{kind, op_id?, data|error}`. Only format supported in Ship 1; `--format=json` is enforced by `GlobalFlags.RequireJSONFormat`.
- **Exit codes** (`internal/cairnerr`): `bad_input|validation → 1`, `conflict → 2`, `not_found → 3`, `substrate → 4`. `Err.Kind` is a short stable string (e.g. `dep_not_done`); `Err.Code` drives the process exit code. Every user-visible error must be a `*cairnerr.Err` — bare errors collapse to `"internal"`.
- **Idempotency (Invariant 6)**: every mutation accepts `--op-id` (ULID). If omitted, `GlobalFlags.ResolveOpID` generates one. Replays with a seen `op_id` must return the cached result without re-executing. New mutations: add an `op_log` check in the store.

### State location

State lives **outside git**. `cli.ResolveStateRoot` precedence: `--state-root` > `CAIRN_HOME` env > platform default (`$XDG_DATA_HOME/cairn` / `~/.cairn` / `%USERPROFILE%\.cairn`). Repo identity is `sha256(canonical abs path of 'git rev-parse --git-common-dir')` (`internal/repoid`) so worktrees share state. State path: `<state-root>/<repo-id>/state.db` + `<state-root>/<repo-id>/blobs/<sha[:2]>/<sha>`.

### DB layer (`internal/db`)

- `db.Open` sets `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`, `_txlock=immediate` — every `BeginTx` is `BEGIN IMMEDIATE`.
- `db.WithTx` is the **only** mutation lifecycle owner. Stores never call `Commit`/`Rollback`. Retries `SQLITE_BUSY` with exponential backoff up to 500 ms wall time; commit-time BUSY is handled by the C-level `busy_timeout` only (see the long comment in `tx.go` — do not "fix" this).
- `db.WithReadTx` for read-only paths (e.g. `task list`).
- Schema is embedded via `//go:embed schema/*.sql`; migrations run in filename-sorted order in their own txns, tracked in `schema_migrations`. Add new migrations as `NNN_name.sql` — never edit applied files.

### Domain packages (Store pattern)

Each domain owns its tables and exposes a `Store` that takes a `*db.Tx`, an `events.Appender`, an `*ids.Generator`, and a `clock.Clock`:

- `internal/task` — lifecycle: plan → list → claim → heartbeat → release → complete. Claim uses CAS against `(task_id, released_at IS NULL, expires_at > now)` (Invariant 5). `Complete` refuses if any required gate is stale or failing (Invariant 4 is a derived query, not a mutation).
- `internal/verdict` — append-only; staleness is computed by joining verdict → gate → inputs hashes at read time.
- `internal/evidence` — content-addressed blob store. Evidence is hash-verified before verdict binding (Invariant 3).
- `internal/memory` — append-only + FTS5. `fts_error.go` wraps modernc.org/sqlite FTS quirks.
- `internal/reconcile` — five stateless idempotent rules (leases, staleness, evidence probe, orphan runs, authoring errors); one transaction, safe to run concurrently with writers on-demand.
- `internal/intent` — YAML spec loader, JSON-schema validation, JCS canonical hashing (`gate_def_hash`, `spec_hash`).
- `internal/events` — `Appender` writes an event row in the same txn as the mutation. **Invariant 10**: every mutation emits an event; `cairn events since <ts>` is the single source of truth. The e2e completeness test in `internal/integration` asserts every event kind is covered.
- `internal/ids` — ULID generation (injectable via `clock.Clock` for tests) and `ValidateOpID`.
- `internal/clock` — `Wall` for prod; `Fake` for tests. Always inject; never call `time.Now()` inside stores.

### Spec pipeline

YAML under `specs/requirements/` and `specs/tasks/` is **derived from prose**, never hand-edited. The `using-cairn` skill (`skills/using-cairn/`) owns the deterministic prose → YAML protocol; regeneration is byte-identical and a `# cairn-derived:` comment carries the source hash for drift detection. `cairn spec init` scaffolds templates; `cairn spec validate` runs schema + referential checks; `cairn task plan` materializes specs into state.

### Plugin layout

`.claude-plugin/plugin.json` declares three skills in `skills/`: `using-cairn`, `subagent-driven-development-with-verdicts`, `verdict-backed-verification`. Keep skill scope compositional with Superpowers (see `PLAN.md §"Upstream posture"` — cairn does not upstream to `obra/superpowers`).

## Working on this codebase

- **Ship-scoped design.** For each decision ask "does the current Ship actually consume this?" If not, take the cheapest valid implementation and defer. `docs/PLAN.md` is the canonical Ship list.
- **Subtraction first.** New deps, abstractions, or config knobs need to earn their complexity. The dogfood test: would `cairn reconcile` still be stateless if you added this?
- **Go deps land with importing code.** Never `go get` ahead of the first file that imports the dep — `go mod tidy` in CI will strip it. Add the dep and the first import in the same commit.
- **modernc.org/sqlite TEXT scan quirk.** When scanning TEXT columns into `json.RawMessage` / `[]byte`, scan through a `string` intermediate — direct `[]byte` scans fail at runtime with this driver.
- **Commits & PRs.** No Claude/Anthropic attribution in commit messages, PR titles, or PR bodies.
- **CLI behavior changes.** A new mutation kind, error kind, or event kind is a schema change for downstream consumers. Add a fixture under `testdata/e2e/` and an integration test in `internal/integration/`.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **cairn** (1268 symbols, 3907 relationships, 100 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## When Debugging

1. `gitnexus_query({query: "<error or symptom>"})` — find execution flows related to the issue
2. `gitnexus_context({name: "<suspect function>"})` — see all callers, callees, and process participation
3. `READ gitnexus://repo/cairn/process/{processName}` — trace the full execution flow step by step
4. For regressions: `gitnexus_detect_changes({scope: "compare", base_ref: "main"})` — see what your branch changed

## When Refactoring

- **Renaming**: MUST use `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` first. Review the preview — graph edits are safe, text_search edits need manual review. Then run with `dry_run: false`.
- **Extracting/Splitting**: MUST run `gitnexus_context({name: "target"})` to see all incoming/outgoing refs, then `gitnexus_impact({target: "target", direction: "upstream"})` to find all external callers before moving code.
- After any refactor: run `gitnexus_detect_changes({scope: "all"})` to verify only expected files changed.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Tools Quick Reference

| Tool | When to use | Command |
|------|-------------|---------|
| `query` | Find code by concept | `gitnexus_query({query: "auth validation"})` |
| `context` | 360-degree view of one symbol | `gitnexus_context({name: "validateUser"})` |
| `impact` | Blast radius before editing | `gitnexus_impact({target: "X", direction: "upstream"})` |
| `detect_changes` | Pre-commit scope check | `gitnexus_detect_changes({scope: "staged"})` |
| `rename` | Safe multi-file rename | `gitnexus_rename({symbol_name: "old", new_name: "new", dry_run: true})` |
| `cypher` | Custom graph queries | `gitnexus_cypher({query: "MATCH ..."})` |

## Impact Risk Levels

| Depth | Meaning | Action |
|-------|---------|--------|
| d=1 | WILL BREAK — direct callers/importers | MUST update these |
| d=2 | LIKELY AFFECTED — indirect deps | Should test |
| d=3 | MAY NEED TESTING — transitive | Test if critical path |

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/cairn/context` | Codebase overview, check index freshness |
| `gitnexus://repo/cairn/clusters` | All functional areas |
| `gitnexus://repo/cairn/processes` | All execution flows |
| `gitnexus://repo/cairn/process/{name}` | Step-by-step execution trace |

## Self-Check Before Finishing

Before completing any code modification task, verify:
1. `gitnexus_impact` was run for all modified symbols
2. No HIGH/CRITICAL risk warnings were ignored
3. `gitnexus_detect_changes()` confirms changes match expected scope
4. All d=1 (WILL BREAK) dependents were updated

## Keeping the Index Fresh

After committing code changes, the GitNexus index becomes stale. Re-run analyze to update it:

```bash
npx gitnexus analyze
```

If the index previously included embeddings, preserve them by adding `--embeddings`:

```bash
npx gitnexus analyze --embeddings
```

To check whether embeddings exist, inspect `.gitnexus/meta.json` — the `stats.embeddings` field shows the count (0 means no embeddings). **Running analyze without `--embeddings` will delete any previously generated embeddings.**

> Claude Code users: A PostToolUse hook handles this automatically after `git commit` and `git merge`.

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
