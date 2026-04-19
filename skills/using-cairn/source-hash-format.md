# Source-hash format and derived-comment protocol

## 1. Comment format (exact, first line of every derived YAML)

```yaml
# cairn-derived: source-hash=<sha256 of source prose file content> source-path=<repo-relative path> derived-at=<ISO 8601 UTC>
```

## 2. Regeneration protocol

On skill invocation that may author or consume YAML:
- Read header comment from each YAML file under `specs/`.
- For each YAML: compute sha256 of the file at `source-path`. Compare to `source-hash`.
- Mismatch → regenerate YAML from current prose; overwrite file including new header comment.
- Missing or malformed comment → treat as stale; regenerate.

## 3. Parser regex (single line, strict)

```
^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$
```

Timestamp anchor `Z` enforces UTC suffix.

## 4. Whitespace path constraint (hard)

`source-path` MUST NOT contain whitespace. If a prose spec file path contains a space, the authoring skill errors before emission with the design question:

> Prose spec path `<path>` contains whitespace, which cairn does not support. Rename the file (e.g., `kebab-case.md`) or relocate.

No YAML is written until path is clean.

## 5. Timestamp format

ISO 8601 UTC with `Z` suffix, second precision. Example: `2026-04-18T14:23:00Z`. Timestamp is for human inspection via `git blame`; not load-bearing for staleness detection.
