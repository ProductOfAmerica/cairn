# cairn

A verification substrate for AI-coordinated software development.

Cairn is a small Go binary that opens a SQLite database, performs one coordination primitive, and exits. It is invoked by Superpowers skills (or any caller) at specific moments — claim a task, report a verdict, store evidence, query memory, check staleness.

## Status

Ship 1 — core substrate. See `docs/PLAN.md` for the full roadmap.

## Install (from source)

```bash
go install github.com/ProductOfAmerica/cairn/cmd/cairn@latest
```

## Usage

```bash
# Scaffold state for the current git repo
cairn init

# Validate intent specs
cairn spec validate

# Materialize requirements/gates/tasks into state
cairn task plan

# Claim a task with a lease
cairn task claim TASK-001 --agent my-agent --ttl 30m

# Store evidence for a gate
cairn evidence put ./test-output.txt

# Bind a verdict to the evidence
cairn verdict report --gate AC-001 --run <run_id> --status pass \
  --evidence <path> --producer-hash <hash> --inputs-hash <hash>

# Complete the task (checks gates fresh + pass)
cairn task complete <claim_id>

# Inspect the event log
cairn events since 0
```

All commands output JSON by default. Add `--format human` for human-readable output.

## State location

`$CAIRN_HOME` if set, else:
- Linux: `$XDG_DATA_HOME/cairn/` (default `~/.local/share/cairn/`)
- macOS: `~/.cairn/`
- Windows: `%USERPROFILE%\.cairn\`

Per-repo directory is keyed off `git rev-parse --git-common-dir` so worktrees share state.

## License

MIT. See `LICENSE`.
