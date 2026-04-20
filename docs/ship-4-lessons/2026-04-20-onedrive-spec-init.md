# Ship 4 friction — `cairn spec init` silently no-ops in OneDrive-synced workspaces

**Date observed:** 2026-04-20
**Workspace:** `C:\Users\eelwo\OneDrive\Desktop\dreambot-scripts` (Windows, OneDrive-synced)
**cairn version:** v0.4.0 (between commits `8c5a2fb` and `2ffb449`)
**Status:** Workaround known (`--force` on first init). Cairn-side fix not yet specified.

> **Next session:** invoke `superpowers:brainstorming` on this file to scope a small fix in cairn itself. See "For the next session" at the bottom for a copy-paste prompt.

---

## Symptom

Running `cairn init && cairn spec init` in a OneDrive-synced directory completes with exit 0 and a JSON envelope reporting success, but the `.yaml.example` template files **do not actually appear on disk**. The parent `specs/requirements/` and `specs/tasks/` directories are created, but they are empty.

Re-running `cairn spec init --force` writes the files correctly, and they remain visible thereafter. `cairn spec validate` then succeeds.

## Reconstructed timeline (from session `f7fab06a-8e89-4a8a-b739-67f07f70a3c0`, 2026-04-20 UTC)

| Time (UTC) | Event |
|------------|-------|
| 01:57:39 | User: "Set it up for me in `C:\Users\eelwo\OneDrive\Desktop\dreambot-scripts`" |
| 01:58:37 | Ran `cairn init && cairn spec init` in target dir. CLI envelope reported success — `created` list contained both `.yaml.example` paths. |
| 01:58:47 | Ran `cairn spec validate && ls specs/requirements/ specs/tasks/`. Validate succeeded (loader ignores `.yaml.example` per design). |
| 01:59:10 | `ls` output revealed empty subdirectories: `total 0`, only `.` and `..`. Files claimed-created were absent. |
| 01:59:19 | Re-ran with `cairn spec init --force`. Files landed. Subsequent `ls` showed both `.yaml.example` files. |

## Root-cause hypothesis

`internal/cli/spec_init.go:87` (`SpecInit`):

```go
for _, p := range pairs {
    if _, err := os.Stat(p.path); err == nil && !force {
        res.Skipped = append(res.Skipped, p.path)
        continue
    }
    if err := os.WriteFile(p.path, []byte(p.body), 0o644); err != nil {
        return nil, fmt.Errorf("write %s: %w", p.path, err)
    }
    res.Created = append(res.Created, p.path)
    ...
}
```

Without `--force`, SpecInit skips paths where `os.Stat` returns `nil err`. On a OneDrive-synced directory, OneDrive's sync engine can leave reparse-point placeholders (Files-On-Demand stubs) at paths that **previously existed on the OneDrive cloud side** even when the local file system has no actual content. `os.Stat` succeeds against the placeholder; `os.WriteFile` is skipped; the directory listing later shows zero bytes because no real file was ever written by SpecInit and the placeholder isn't materialized.

The `--force` path bypasses the Stat check and unconditionally writes. The write triggers OneDrive to either materialize or replace the placeholder, and the file becomes visible.

**Open question for the brainstorm:** is the failure on first init a fresh placeholder that OneDrive created speculatively (e.g., from a sibling user's earlier upload of `dreambot-scripts/specs/`), or is it a Windows reparse-point semantic that affects more than just OneDrive (e.g., Dropbox, iCloud, BoxDrive)? The current evidence is consistent with a sync-placeholder explanation but is not conclusive — needs a clean-room repro.

## What `cairn spec init` reports vs. what's on disk

The CLI's success envelope is a lie when this happens. Result struct:

```go
type SpecInitResult struct {
    Created     []string `json:"created"`
    Skipped     []string `json:"skipped"`
    Overwritten bool     `json:"overwritten,omitempty"`
}
```

In the failure mode, the path appears in `Created` because `os.WriteFile` returned `nil err` — but the file is never visible. Or it appears in `Skipped` because Stat succeeded against a placeholder. Either way, the user has no way to know from the envelope that the post-condition (file on disk with template content) is unmet.

## Possible cairn-side fixes (for the brainstorm to choose between)

These are options to discuss, not commitments. Subtraction first — pick the smallest fix that prevents the silent failure.

1. **Post-write verify.** After `os.WriteFile`, immediately re-`os.Stat` the path; if size != `len(body)` or the path is a reparse point, return `cairnerr.New(CodeSubstrate, "spec_init_write_unverified", ...)`. Cost: one extra syscall per file. Catches OneDrive placeholder + future similar sync-engine issues.

2. **Stat hardening.** Replace `os.Stat` with a check that distinguishes "real file exists with content" from "placeholder reparse point exists with zero bytes". Use `info.Size() > 0 && info.Mode().IsRegular()` and (Windows) check `syscall.Win32FileAttributeData` for `FILE_ATTRIBUTE_REPARSE_POINT`. Skip only when the file is genuinely real. Cost: Windows-specific code in a previously-portable file.

3. **Atomic write that ignores Stat.** Drop the skip-if-exists semantics entirely; always write to a `.tmp-*` sibling and `os.Rename` to target. If the target exists with identical content, it's a no-op write (rename overwrites atomically). Cost: changes the user contract — `cairn spec init` becomes idempotent-by-overwrite rather than idempotent-by-skip. Test impact: existing `TestSpecInit_Idempotent` in `internal/cli/spec_init_test.go` would still pass (output is still byte-identical).

4. **Detect-and-warn.** If the spec root path is under `*OneDrive*`, `*Dropbox*`, `*iCloud*`, or `*Box*`, print a stderr warning recommending `--force` on first init. Cost: heuristic + maintenance of the sync-tool list. Benefit: zero behavior change for non-affected users.

5. **Documentation only.** Update README + `cairn spec init --help` to call out the OneDrive caveat. Cost: zero code. Benefit: also zero — users still hit the silent failure once, then read the docs.

Recommended starting point for the brainstorm: **option 1 (post-write verify)** is cheapest, catches the failure loudly via the existing `cairnerr.Err` envelope, and doesn't require platform-specific code. Option 3 is more invasive but eliminates the entire skip-vs-write decision branch. Option 4 is a hedge that helps users without changing semantics.

## Scope guardrail

This is one Ship 4 dogfood data point. The brainstorm should **not** expand into a general "harden cairn against all Windows sync providers" effort. Pick the smallest fix that closes this specific silent-failure mode and ship it. Other sync-provider issues become their own future Ship lessons.

The audit-remediation pattern from this week (15-commit branch closing a focused finding set) is the right shape for execution: brainstorm → write spec → write plan → land via subagent-driven-development. Should land in one short branch.