# `cairn spec init` silent no-op on placeholder files — design

> Date: 2026-04-20
> Ship: 4 (friction fix)
> Status: Draft — awaiting user review. Incorporates adversarial
> review (Codex) findings from the same date.
> Friction writeup: `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md`

## Problem

`cairn spec init` in a OneDrive-synced workspace returns a success envelope
but the `.yaml.example` templates never land on disk. Reproduced on
2026-04-20 both in the wild (`C:\Users\eelwo\OneDrive\Desktop\dreambot-scripts`,
session `f7fab06a`) and synthetically in `/tmp/cairn-repro`:

```
$ : > specs/requirements/REQ-001.yaml.example      # zero-byte predecessor
$ : > specs/tasks/TASK-001.yaml.example
$ cairn spec init --format=json
{"data":{"created":[],"skipped":["specs\\requirements\\REQ-001.yaml.example",
  "specs\\tasks\\TASK-001.yaml.example"]},"kind":"spec.init"}
$ wc -c specs/requirements/REQ-001.yaml.example
0 specs/requirements/REQ-001.yaml.example
```

The envelope reports success with a `Skipped` list. The files remain empty.
Post-condition (template body on disk) is unmet. The CLI lies.

## Root cause

`internal/cli/spec_init.go:87` gates the write on `os.Stat` success:

```go
if _, err := os.Stat(p.path); err == nil && !force {
    res.Skipped = append(res.Skipped, p.path)
    continue
}
```

`os.Stat` is a **proxy** for the real question the command is trying to
answer, which is "does the file already have the canonical template
content?" The proxy is correct only in the fresh-init repeat case and
wrong in every other case it has to handle:

| Path state on disk | `os.Stat` decision | Correct behavior |
|---|---|---|
| absent | write | write ✓ |
| fresh unedited template | skip | skip ✓ |
| zero-byte file / sync placeholder | skip | **write** (the bug) |
| Windows Files-On-Demand placeholder (cloud-size, reparse tag) | skip | **write** |
| edited content | skip | write (template says DO NOT EDIT) |
| wrong content from partial write / corruption | skip | write |

OneDrive placeholders are just one vehicle. Any zero-byte or wrong-content
predecessor triggers the silent-failure path.

## Fix — replace the proxy, verify the post-condition

The command's invariant is post-condition-about-content, so the check is
about content. Skip only on exact byte-match; every other outcome
(absent, read-error, content-mismatch, force) falls through to the
write-then-verify path. A read error is not terminal — an unreadable
OneDrive placeholder still needs to be overwritten to land the template,
and the write+verify pair will surface any substrate problem loudly.

```
for each sub in {requirements, tasks}:
    mkdirErr := os.MkdirAll(<root>/<sub>, 0o755)
    if mkdirErr != nil:
        return cairnerr.New(CodeSubstrate, "spec_init_mkdir_failed", ...).
            WithDetails({path: <root>/<sub>, cause: mkdirErr.Error()})

for each (path, body) in pairs:
    existing, readErr := os.ReadFile(path)
    if readErr == nil && !force && bytes.Equal(existing, body):
        res.Skipped += path
        continue
    // All other paths (absent, read-error, content-mismatch, force=true) write.
    writeErr := os.WriteFile(path, body, 0o644)
    if writeErr != nil:
        return cairnerr.New(CodeSubstrate, "spec_init_write_failed", ...).
            WithDetails({path, cause: writeErr.Error()})
    verify, verifyErr := os.ReadFile(path)
    if verifyErr != nil:
        return cairnerr.New(CodeSubstrate, "spec_init_write_unverified", ...).
            WithDetails({path, expected_size: len(body), cause: verifyErr.Error()})
    if !bytes.Equal(verify, body):
        return cairnerr.New(CodeSubstrate, "spec_init_write_unverified", ...).
            WithDetails({path, expected_size: len(body), got_size: len(verify),
                         expected_sha256: hex(sha256(body)),
                         got_sha256: hex(sha256(verify))})
    res.Created += path
    if force: res.Overwritten = true
```

Four things fall out of the switch to content-check:

1. **Zero-byte and reparse-point placeholders** fail `bytes.Equal` → write.
2. **Unreadable predecessors** (OneDrive offline placeholder, permission
   denial, disk error) also fall through to the write path. If the write
   can still succeed, great — the template lands; if not, we surface
   `spec_init_write_failed` instead of bailing pre-write on a read
   error that wasn't the operator's actual question.
3. **Post-write verify** catches the OneDrive failure mode the writeup
   calls out verbatim: `os.WriteFile` returns `nil` but the bytes
   didn't persist (e.g. symlink-to-`/dev/null`, cloud-provider
   discard). The envelope becomes honest: success always means
   template body is on disk.
4. **`Skipped` becomes truthful**: a path lands there iff the file's
   content is already byte-identical to the canonical template.

`cause` is duplicated into `details` as a string because `envelope.go`
only serializes `code`, `message`, `details` — the `*cairnerr.Err`
`Cause` chain reaches `Err.Error()` (logs, stderr) but never the JSON
envelope. Operators parsing the envelope need `details.cause` to see
the underlying filesystem error.

## Why not a stat-hardening heuristic

Evaluated and rejected: check `info.Size() > 0 && info.Mode().IsRegular()`
before skipping (writeup's option 2, minus Windows-specific reparse
syscalls).

- Windows Files-On-Demand placeholders can report `size > 0` and a regular
  mode via `GetFileAttributesEx`. The heuristic skips them; silent failure
  persists.
- Adding `FILE_ATTRIBUTE_REPARSE_POINT` detection drags platform-specific
  code into a previously portable file and still only covers the sync
  engines we've tested.
- Any stat-based heuristic is a shortcut trading correctness for fewer
  bytes-read. For 1 KB templates the trade buys nothing.

## Why not "always write, drop `Skipped`"

Evaluated and rejected: `os.Rename`-into-place on every call; remove the
skip decision entirely (writeup's option 3).

- `Skipped` is a load-bearing **idempotency signal** for developer
  ergonomics and scripts. Removing it is an envelope schema change;
  CLAUDE.md's "a new kind is a schema change for downstream consumers"
  rule cuts harder against removals than additions.
- `--force` becomes dead weight under always-write.
- The subtraction algorithm says question requirements first. `Skipped`
  earns its keep; the `os.Stat`-based implementation does not.

## Changes to the user contract

Call every item below out in release notes. The envelope *shape* is
unchanged; the envelope *semantics* have shifted and the `--force`
flag's narrow role has compressed.

- **Envelope shape:** unchanged. Same `created`, `skipped`, `overwritten`
  fields. Existing consumers parsing the JSON envelope will still find
  those keys in the same positions.
- **`Created` semantic drift.** `Created` previously meant "path did
  not exist; we created it." It now means "we wrote bytes to this
  path" — which also covers overwriting an existing file whose
  content didn't match the canonical template (zero-byte placeholder,
  sync-placeholder with cloud metadata, corrupted predecessor,
  manually-edited template). The envelope shape is identical but the
  meaning of a path appearing in `Created` has widened.
- **`Skipped` narrowed.** `Skipped` previously meant "path existed; we
  left it alone." It now means "path existed with content byte-
  identical to the canonical template." A file that previously landed
  in `Skipped` because it existed (even with wrong content) will now
  land in `Created`.
- **`--force` role has compressed.** Previously, `--force` was the only
  way to get `spec init` to overwrite existing templates. Non-force
  now also overwrites when content doesn't match. `--force` still
  overwrites when content *does* match (its only remaining use is
  "rewrite known-correct file anyway"). This is a semantic shift even
  though the flag itself is not renamed or removed. Not a remediation
  for `spec_init_write_unverified` — the verify branch runs regardless
  of `--force`.
- **Manually-edited `.yaml.example` files are no longer preserved
  across `spec init` calls.** This is the visible consequence of the
  above. The template header says `DO NOT EDIT THIS FILE` and
  `intent.Validate` fires `renamed_template` on any attempt to
  promote `.yaml.example` to `.yaml`; the system already treats
  scaffolding templates as read-only. The previous
  silent-preserve-on-edit behavior was incidental, not load-bearing.
- **Partial-write ordering is unchanged but now more visible.** Each
  `(requirements, tasks)` pair is written sequentially; a failure on
  the second path after the first has already succeeded leaves the
  filesystem in a half-written state. The envelope returns only the
  error for the failed path and drops the `data` field (per
  `envelope.go` — when `Err != nil`, `data` is omitted). Operators
  should rerun `spec init` after resolving the underlying substrate
  failure; the content-check on the first path will now Skip it.
- **Path encoding unchanged.** `Created` / `Skipped` / `details.path`
  carry OS-native `filepath.Join` output (backslashes on Windows,
  forward slashes elsewhere), matching the pre-fix envelope. This is
  inconsistent with `internal/cli/install_skills.go` which normalizes
  JSON paths to POSIX, but unifying the convention is out of scope
  for this Ship 4 fix.
- **Non-regular predecessors (symlinks, special files) behavior
  change.** Previously, an existing symlink at a template path was
  skipped. Under the new design, `os.ReadFile` follows the symlink
  and byte-compares its target; a mismatch triggers `os.WriteFile`,
  which also follows the symlink and writes to its target. This can
  overwrite files outside the spec tree if an operator has
  deliberately placed a symlink at a template path. Such
  configurations are anti-patterns — no cairn workflow creates or
  relies on them — but the behavior change must be called out.
  See Out of scope for why this is not hardened in this PR.
- **`spec init --help`:** unchanged textually. The docs already say
  "idempotent" without promising which proxy is used.

## New error kinds

Three kinds, all `CodeSubstrate` (exit 4). The current `MkdirAll`
failure path in `spec_init.go:75` uses bare `fmt.Errorf` and today only
escapes its `"internal"` fate because the `cmd/cairn/spec.go` wrapper
catches it as `init_failed`. Retiring that wrapper without typing the
mkdir path would re-leak an `internal` envelope — so a typed mkdir kind
lands in this PR.

Concrete message strings:

| Kind | When it fires | Message | Details |
|---|---|---|---|
| `spec_init_mkdir_failed` | `os.MkdirAll` on `<root>/requirements` or `<root>/tasks` returns an error | `create spec subdirectory failed` | `{path, cause}` |
| `spec_init_write_failed` | `os.WriteFile` on the template path returns an error (after read-compare has already decided to write) | `write template failed` | `{path, cause}` |
| `spec_init_write_unverified` | Post-write `os.ReadFile` errors **or** reread bytes don't match the canonical template body | `template write did not persist on disk; inspect filesystem or sync-engine state` | Content-mismatch: `{path, expected_size, got_size, expected_sha256, got_sha256}`. Reread-error: `{path, expected_size, cause}` |

Every `details` map carries `path` as a string (OS-native from
`filepath.Join`). `cause` is a plain string copy of the underlying
`error.Error()` value — NOT a nested object. This is because
`envelope.go:29` only serializes `code`, `message`, `details`; the
`*cairnerr.Err.Cause` field does not reach the envelope. Operators
parsing JSON envelopes must read `details.cause` to see the underlying
filesystem error. `SpecInit` additionally attaches the underlying
error via `WithCause` on the `*cairnerr.Err` so `Err.Error()` logs,
stderr, and `errors.Is`/`errors.As` all work — it's just the JSON
envelope that can't carry it.

**Why no `spec_init_read_failed`.** The earlier draft included a fourth
kind that fired when `os.ReadFile` returned a non-`fs.ErrNotExist`
error. Evaluation during adversarial review showed this was actively
wrong: an unreadable OneDrive placeholder (offline sync-provider,
permission transient) would bail on the read without ever attempting
to write the template — the exact opposite of the fix's goal. Under
the current design, any read outcome that is not "bytes already
match" falls through to the write path. If write also fails, the
user sees `spec_init_write_failed`; if write succeeds but verify
fails, the user sees `spec_init_write_unverified`. No information is
lost and the offline-placeholder case is now handled.

### Hash details on content-mismatch verify failure

The content-mismatch branch of `spec_init_write_unverified` includes
SHA-256 hashes of both the expected body (template constant) and the
bytes read back from disk. Size-only details do not help when sizes
happen to match — the hash pair identifies exactly which bytes are on
disk versus what was written. Hash computation is ~10 µs for 1 KB
templates; negligible cost.

**Breaking-change note on `init_failed`.** The existing `init_failed`
error kind is retired in favor of the three above. A repo-wide grep
confirms no test or consumer asserts against the literal `"init_failed"`
string — it is defined in exactly one place (`cmd/cairn/spec.go:90`) and
referenced in one historical plan doc. External users parsing envelope
`error.code` and special-casing `init_failed` must update to the new
kinds; the release notes for this fix must call it out.

### On the `write_unverified` message

The earlier draft of this spec suggested `"rerun with --force after
resolving filesystem issue"`. That guidance is false: under the
content-check design, `--force` still executes the write-then-verify
path. If the filesystem is still broken, `--force` produces the same
error. The message now points the operator at the substrate itself
(filesystem / sync engine) without promising a CLI-level fix.

## Test plan

### Unit (`internal/cli/spec_init_test.go`)

Existing tests — all four keep passing unchanged:
- `TestSpecInit_CreatesTemplates` (fresh dir, 2 Created)
- `TestSpecInit_Idempotent` (second call, 0 Created / 2 Skipped) —
  still passes because after a legitimate first write the content
  matches byte-for-byte.
- `TestSpecInit_Force` (force=true rewrites a manually-edited file)
- `TestSpecInit_CustomPath` (custom `--path` target)

New unit tests:

- `TestSpecInit_OverwritesZeroBytePlaceholder` — pre-create two
  zero-byte files at the target paths, call `SpecInit(target, false)`,
  assert `Created=[req, task]`, `Skipped=[]`, files on disk contain
  template body and match the template constants byte-for-byte.
- `TestSpecInit_OverwritesWrongContent` — pre-write arbitrary content
  (`# bogus`), call `SpecInit(target, false)`, assert Created and
  restored content byte-for-byte.
- `TestSpecInit_MkdirFailedReturnsCairnErr` — pre-create a *file* at
  `<root>/requirements` (blocking `os.MkdirAll`; portable — MkdirAll
  returns `ENOTDIR`/equivalent on both Unix and Windows when an
  existing component on the path is a file). Assert returned error
  is `*cairnerr.Err` with `Kind == "spec_init_mkdir_failed"` and
  `details.path` set.
- `TestSpecInit_WriteFailedReturnsCairnErr` (Unix only, `runtime.GOOS`
  gate) — pre-create the target template file with content that
  differs from the template (triggers write path) AND mode `0o444`
  (read-only, blocks WriteFile). Assert `*cairnerr.Err` with
  `Kind == "spec_init_write_failed"`.
- `TestSpecInit_WriteUnverifiedReturnsCairnErr` (Unix only) — at the
  target template path, create a symlink to `/dev/null`. `os.ReadFile`
  returns empty bytes (differ from template), `os.WriteFile` succeeds
  (writes to /dev/null, bytes discarded), verify-read returns empty.
  Assert `*cairnerr.Err` with `Kind == "spec_init_write_unverified"`,
  `details.expected_size > 0`, `details.got_size == 0`, and both
  hash fields populated. This is the closest reliable reproduction of
  the real-world "bytes didn't land" failure mode.

Four existing unit tests remain unchanged; five new tests above. Nine
unit tests total after this PR.

### Explicit CLAUDE.md waiver

CLAUDE.md's "Working on this codebase" section states: *"A new mutation
kind, error kind, or event kind is a schema change for downstream
consumers. Add a fixture under `testdata/e2e/` and an integration test
in `internal/integration/`."* This PR introduces three new error kinds.

All three kinds get integration-test coverage (see below). None get a
`testdata/e2e/` fixture directory. The input state for every test is
either "no files" or "one placeholder file at a known path" — a
fixture directory would contain either nothing or a single empty file
named `REQ-001.yaml.example`, which carries no more signal than a
line of test setup. Consistent with the existing
`spec_init_e2e_test.go` pattern, which is also fixture-less and has
been accepted into main (commit `8390ee8` and earlier).

If Ship 5+ introduces a genuinely multi-file spec-init input state, a
fixture directory should be added at that point.

### Integration (`internal/integration/spec_init_e2e_test.go`)

Existing `TestSpecInitE2E` remains unchanged.

New integration tests:

- `TestSpecInitE2E_OverwritesPlaceholder` — reproduces the exact
  `/tmp/cairn-repro` and in-the-wild OneDrive failure.
  1. Fresh tempdir.
  2. `mkdir -p specs/{requirements,tasks}` and pre-create both paths as
     zero-byte files.
  3. Run `cairn spec init --path specs`.
  4. Parse envelope. Assert `kind == "spec.init"`, no `error` field,
     `data.created` has both paths, `data.skipped` is empty.
  5. Read both files from disk. Assert they are **byte-equal** to
     the canonical `requirementTemplate` and `taskTemplate` constants
     exported (via test helper) from `internal/cli/spec_init.go`. Size
     and marker-substring checks are insufficient — the postcondition
     is byte-equality.

- `TestSpecInitE2E_MkdirFailedEnvelope` — pre-create a *file* at
  `<tempdir>/specs/requirements` (blocks MkdirAll). Run
  `cairn spec init --path specs`. Assert exit code 4 and
  `error.code == "spec_init_mkdir_failed"`. `details.path` should
  contain the blocked path.

- `TestSpecInitE2E_WriteUnverifiedEnvelope` (Unix only, via
  `runtime.GOOS` gate). The canonical wild-failure reproducer.
  1. Fresh tempdir.
  2. `mkdir -p specs/{requirements,tasks}`.
  3. Create a symlink at `specs/requirements/REQ-001.yaml.example` →
     `/dev/null`.
  4. Run `cairn spec init --path specs`.
  5. Parse envelope. Assert exit code 4 and
     `error.code == "spec_init_write_unverified"`. `details.path`
     should contain the symlink path; `details.expected_size` should
     equal `len(requirementTemplate)`; `details.got_size` should be 0;
     `details.expected_sha256` and `details.got_sha256` should both
     be non-empty strings.

Three integration tests: the primary placeholder-overwrite success
path, `spec_init_mkdir_failed`, and `spec_init_write_unverified`.
`spec_init_write_failed` has unit coverage (Unix-only, `0o444` mode);
adding an e2e test for it would duplicate the unit test without
exercising a different `cli.Run` path — all three new error kinds
share the same envelope-rendering pipeline, and exercising two of them
e2e is sufficient to verify that pipeline works for typed errors
emitted by `SpecInit`.

## Out of scope

- Detecting other sync providers (Dropbox, iCloud, Box). Behavior is
  correct for them by construction — any zero-byte / wrong-content
  predecessor now gets overwritten, regardless of why it got there.
- Documentation of OneDrive interaction. The fix removes the failure
  mode; nothing for users to work around.
- Hardening any other cairn command against sync placeholders. Future
  Ship lessons if they surface.
- **Atomic temp+rename writes.** Evaluated during adversarial review
  and deferred. Rationale: the content-check + post-write verify
  already closes the Ship 4 friction (silent success on placeholder).
  Crash-during-write producing a partially-visible file is a distinct
  failure mode that self-heals on next `spec init` via the same
  read-compare path. Going to temp+rename adds platform-specific
  cleanup semantics without closing an observed friction. Will become
  its own Ship lesson if it ever surfaces.
- **Symlink / non-regular-file hardening.** Under the new design, a
  symlink at a template path is followed on both read and write — a
  mismatching symlink target gets overwritten through the link.
  Adding `os.Lstat` + "refuse non-regular predecessors" protection
  would require either a new error kind (`spec_init_unsafe_predecessor`)
  or platform-specific `O_NOFOLLOW` (Unix) / reparse-point detection
  (Windows). No cairn workflow creates symlinks at template paths
  and no friction report references this. Documented in release notes
  (see "Changes to the user contract"); deferred until an operator
  actually hits it. The Unix test
  `TestSpecInitE2E_WriteUnverifiedEnvelope` uses a symlink to
  `/dev/null` to exercise the `write_unverified` branch — that is a
  lab trigger, not a real-world configuration.
- **Partial-write rollback.** When the second template fails after the
  first has already been overwritten, we don't roll back the first.
  Operators rerun `spec init`, which will now correctly Skip the
  already-landed first file and retry the second. Explicit rollback
  would require staging both writes and committing atomically; too
  much machinery for a 2-template CLI scaffold. Documented in
  "Changes to the user contract".
- **Path encoding normalization** (OS-native `\` vs POSIX `/`).
  `SpecInit` emits `filepath.Join` output, matching its pre-fix
  behavior and the broader cairn convention. Unifying with
  `install_skills.go` (which normalizes to POSIX) is a separate
  refactor — this PR preserves the pre-existing behavior and does not
  introduce a new inconsistency.
- **`fs.FS` injection seam** for unit-testing error branches. The
  new Unix-gated unit tests already cover write_failed and
  write_unverified via real-filesystem tricks (`0o444` mode, symlink
  to `/dev/null`). No need for an injection seam; the seam would cost
  more LOC than the two tests.
- Helper-function extraction (`writeTemplate(path, body, force)`).
  Evaluated and dropped as structure-for-structure's-sake; the outer
  loop remains small enough to read inline.
- The deferred audit Wave 4 items (L1, L3, M15, etc.).

## Files touched

Implementation:
- `internal/cli/spec_init.go` — type the `MkdirAll` error path as
  `spec_init_mkdir_failed`; swap `os.Stat`-skip for
  `os.ReadFile + bytes.Equal` content-check; add post-write verify
  (reread + byte-compare + hash both sides on mismatch); return
  `*cairnerr.Err` directly for every user-visible failure.
  Templates remain package-level constants; export them (or add a
  `TemplatesForTest()` helper in a `_test.go` file in the same
  package) so integration tests can byte-compare disk content
  against the canonical body.
- `cmd/cairn/spec.go` — drop the `init_failed` wrapper; `SpecInit` now
  returns typed errors the envelope writer can render verbatim.

Tests:
- `internal/cli/spec_init_test.go` — add five unit tests
  (`OverwritesZeroBytePlaceholder`, `OverwritesWrongContent`,
  `MkdirFailedReturnsCairnErr`, `WriteFailedReturnsCairnErr`
  Unix-only, `WriteUnverifiedReturnsCairnErr` Unix-only); existing
  four (`CreatesTemplates`, `Idempotent`, `Force`, `CustomPath`)
  unchanged.
- `internal/integration/spec_init_e2e_test.go` — add three
  integration tests (`OverwritesPlaceholder`, `MkdirFailedEnvelope`,
  `WriteUnverifiedEnvelope` Unix-only); existing `TestSpecInitE2E`
  unchanged.

No schema migration. No new event kinds. No new envelope fields. No
`testdata/e2e/` fixture (see waiver in Test plan).

## References

- Friction writeup: `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md`
- Ship 3 spec-init design: `docs/superpowers/specs/2026-04-19-req-002-spec-validate-spec-init-design.md`
- Audit remediation branch (reference cadence): commit `301ee78`
