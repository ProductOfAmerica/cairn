# OneDrive `spec init` silent no-op fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `os.Stat`-based skip in `SpecInit` with a content-check + post-write verify, type all failure modes as `*cairnerr.Err`, and close the Ship 4 OneDrive silent-failure mode end-to-end.

**Architecture:** One library function (`internal/cli/spec_init.go`) is rewritten around an `os.ReadFile + bytes.Equal` skip proxy and a post-write reread; three typed error kinds replace the generic `init_failed` wrapper; tests land at both unit and integration layers. No schema change, no new event kinds, no new envelope fields.

**Tech Stack:** Go 1.25.x, cobra CLI, modernc.org/sqlite (unchanged), existing `internal/cairnerr` + `internal/cli` envelope. Symlink-based Unix tests exercise the "bytes didn't land" branch the wild failure referenced.

**Spec:** `docs/superpowers/specs/2026-04-20-onedrive-spec-init-fix-design.md` (v3, post-adversarial-review).

**Friction writeup:** `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md`.

**Target branch:** `fix/onedrive-spec-init`.

---

## File structure

**Implementation files:**
- `internal/cli/spec_init.go` — rewrite `SpecInit` with content-check + post-write verify; add `TemplatesForTest` helper. The existing `requirementTemplate` / `taskTemplate` constants stay unchanged (marker comments are load-bearing for `intent.Validate`).
- `cmd/cairn/spec.go` — drop the `init_failed` wrapper; pass `SpecInit`'s typed error through to `cli.Run` verbatim.

**Test files:**
- `internal/cli/spec_init_test.go` — five new unit tests added; existing four (`CreatesTemplates`, `Idempotent`, `Force`, `CustomPath`) unchanged.
- `internal/integration/spec_init_e2e_test.go` — three new integration tests added; existing `TestSpecInitE2E` unchanged.

**Docs:**
- `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md` — update the Status line at the top to reference the fix commit/PR once landed.

**Not touched:** `internal/cairnerr`, `internal/cli/envelope.go`, `internal/cli/run.go`, `internal/cli/exitcode.go`, schema files, any `internal/` domain under task/verdict/evidence/memory/reconcile/intent/events/ids/clock.

---

## Task 1: Create feature branch

**Files:** none (branch setup).

- [ ] **Step 1: Create and check out the feature branch**

```bash
git checkout -b fix/onedrive-spec-init
```

Expected: `Switched to a new branch 'fix/onedrive-spec-init'`.

- [ ] **Step 2: Confirm clean working tree except the already-committed spec**

```bash
git status
```

Expected: either clean, or only the new spec/plan docs staged. If anything else is modified, pause and figure out why before proceeding.

---

## Task 2: Export template constants for cross-package byte-compare

The canonical template constants currently live as unexported package-level strings in `internal/cli/spec_init.go`. Integration tests in `internal/integration/` need to assert that disk content matches the canonical body byte-for-byte; without an export, they'd have to duplicate the 2 KB of template literal (drift hazard). A small exported helper solves this cleanly without widening the package API.

**Files:**
- Modify: `internal/cli/spec_init.go` (add helper near the bottom of the file, just before the closing of the package).

- [ ] **Step 1: Add `TemplatesForTest` helper at the end of `internal/cli/spec_init.go`**

Append to `internal/cli/spec_init.go` (after the `SpecInit` function, before EOF):

```go
// TemplatesForTest returns the canonical scaffolding bodies for
// REQ-001.yaml.example and TASK-001.yaml.example. Exposed so that
// integration tests in internal/integration can byte-compare
// disk content against the expected template body without
// duplicating the literal. The returned strings MUST be treated
// as read-only; callers must not mutate the underlying backing
// arrays.
func TemplatesForTest() (requirement, task string) {
	return requirementTemplate, taskTemplate
}
```

- [ ] **Step 2: Verify the build still passes**

```bash
go build ./...
```

Expected: no output, zero exit code.

- [ ] **Step 3: Confirm all existing tests still pass**

```bash
go test -race ./internal/cli/... ./internal/integration/...
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/spec_init.go
git commit -m "feat(cli): export TemplatesForTest helper for e2e byte-compare"
```

---

## Task 3: Replace `os.Stat`-skip with `os.ReadFile` content-check

This is the core of the fix. A pre-existing zero-byte file at the template path is the direct vehicle of the OneDrive silent failure. Skip only when the on-disk content is byte-identical to the canonical template; every other outcome (absent, read-error, mismatch, force) falls through to the write path.

**Files:**
- Create (new test): add `TestSpecInit_OverwritesZeroBytePlaceholder` to `internal/cli/spec_init_test.go`.
- Modify: `internal/cli/spec_init.go` (rewrite the per-pair loop body).

- [ ] **Step 1: Add `bytes` import to `internal/cli/spec_init.go`**

The file currently imports only `fmt`, `os`, `path/filepath`. The rewritten per-pair loop needs `bytes` for `bytes.Equal`. Later tasks (5, 6, 7) will add `github.com/ProductOfAmerica/cairn/internal/cairnerr`, `crypto/sha256`, and `encoding/hex` as they convert each error path. Keep imports minimal at each step.

Change the import block at the top of `internal/cli/spec_init.go` to:

```go
import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)
```

`fmt` stays — it's still used by the `MkdirAll` and `WriteFile` wrappers until Tasks 5 and 6 type them.

- [ ] **Step 2: Write the failing test `TestSpecInit_OverwritesZeroBytePlaceholder`**

Append to `internal/cli/spec_init_test.go`:

```go
func TestSpecInit_OverwritesZeroBytePlaceholder(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	taskPath := filepath.Join(target, "tasks", "TASK-001.yaml.example")
	// Pre-existing zero-byte predecessors at both template paths —
	// this is the synthetic reproduction of the OneDrive silent-failure mode.
	if err := os.WriteFile(reqPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatalf("SpecInit: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created: want 2, got %d: %v", len(res.Created), res.Created)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("skipped: want 0, got %v", res.Skipped)
	}

	wantReq, wantTask := cli.TemplatesForTest()
	if b, _ := os.ReadFile(reqPath); string(b) != wantReq {
		t.Errorf("req content: not byte-equal to canonical template")
	}
	if b, _ := os.ReadFile(taskPath); string(b) != wantTask {
		t.Errorf("task content: not byte-equal to canonical template")
	}
}
```

- [ ] **Step 3: Run the new test — verify it fails**

```bash
go test -race -run TestSpecInit_OverwritesZeroBytePlaceholder ./internal/cli/...
```

Expected: FAIL. Current `SpecInit` takes the `os.Stat` success branch → both paths go to `Skipped` → `len(res.Created) != 2` assertion fires.

- [ ] **Step 4: Rewrite the per-pair loop in `SpecInit`**

Replace the existing per-pair `for _, p := range pairs { ... }` loop in `internal/cli/spec_init.go` (currently lines 86–98) with:

```go
	for _, p := range pairs {
		existing, readErr := os.ReadFile(p.path)
		if readErr == nil && !force && bytes.Equal(existing, []byte(p.body)) {
			res.Skipped = append(res.Skipped, p.path)
			continue
		}
		// All other paths (absent, read-error, content-mismatch, force=true) write.
		if err := os.WriteFile(p.path, []byte(p.body), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", p.path, err)
		}
		res.Created = append(res.Created, p.path)
		if force {
			res.Overwritten = true
		}
	}
```

Note: we are **intentionally not** distinguishing read error types (e.g. `errors.Is(readErr, fs.ErrNotExist)`). Any read outcome that isn't "bytes already match" falls through to the write path — that is the correctness property the spec's "Why no `spec_init_read_failed`" section argues for.

- [ ] **Step 5: Run the new test — verify it passes**

```bash
go test -race -run TestSpecInit_OverwritesZeroBytePlaceholder ./internal/cli/...
```

Expected: PASS.

- [ ] **Step 6: Run the full `internal/cli` suite — verify nothing regressed**

```bash
go test -race ./internal/cli/...
```

Expected: all tests PASS — specifically `TestSpecInit_CreatesTemplates`, `TestSpecInit_Idempotent`, `TestSpecInit_Force`, `TestSpecInit_CustomPath`. `Idempotent` is the critical check: second call must still report `Created=0, Skipped=2` because the content now matches.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/spec_init.go internal/cli/spec_init_test.go
git commit -m "fix(cli): spec init now uses content-check skip instead of os.Stat

Replaces the os.Stat-success skip proxy in SpecInit with an
os.ReadFile + bytes.Equal byte-compare against the canonical
template. A zero-byte predecessor (OneDrive Files-On-Demand
placeholder, crash residue, : > redirect) no longer fools the
command into reporting success with empty files on disk.

Skipped now means 'content already byte-identical to the
canonical template'. Absent / read-error / mismatch / force
all fall through to the write path."
```

---

## Task 4: Verify wrong-content case works with the same impl

The fix for mismatching content (user-edited file, corrupt predecessor) shares the write path with the zero-byte case. Adding an explicit test guards against future regressions and documents the behavior change.

**Files:**
- Modify: `internal/cli/spec_init_test.go` (append new test).

- [ ] **Step 1: Write `TestSpecInit_OverwritesWrongContent`**

Append to `internal/cli/spec_init_test.go`:

```go
func TestSpecInit_OverwritesWrongContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	// Pre-existing file with non-template content (simulates corrupted
	// predecessor or a manually-edited template).
	if err := os.WriteFile(reqPath, []byte("# bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatalf("SpecInit: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created: want 2, got %d: %v", len(res.Created), res.Created)
	}

	wantReq, _ := cli.TemplatesForTest()
	if b, _ := os.ReadFile(reqPath); string(b) != wantReq {
		t.Errorf("req content: should have been restored to canonical template, got %q", string(b))
	}
}
```

- [ ] **Step 2: Run the test — should already pass on the Task 3 impl**

```bash
go test -race -run TestSpecInit_OverwritesWrongContent ./internal/cli/...
```

Expected: PASS. No implementation change needed — the mismatch branch is the same as the zero-byte branch under content-check.

- [ ] **Step 3: Commit**

```bash
git add internal/cli/spec_init_test.go
git commit -m "test(cli): cover spec init wrong-content overwrite case"
```

---

## Task 5: Type the mkdir error as `spec_init_mkdir_failed`

The current `MkdirAll` path returns a bare `fmt.Errorf`. Once the `init_failed` wrapper in `cmd/cairn/spec.go` is retired (Task 8), that bare error would collapse to `code:"internal"` in the envelope. Type it now.

**Files:**
- Modify: `internal/cli/spec_init_test.go` (add test).
- Modify: `internal/cli/spec_init.go` (type the error).

- [ ] **Step 1: Add `errors` and `github.com/ProductOfAmerica/cairn/internal/cairnerr` imports to `internal/cli/spec_init_test.go`**

Change the import block at the top of `internal/cli/spec_init_test.go` to:

```go
import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
)
```

- [ ] **Step 2: Write the failing test `TestSpecInit_MkdirFailedReturnsCairnErr`**

Append to `internal/cli/spec_init_test.go`:

```go
func TestSpecInit_MkdirFailedReturnsCairnErr(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")
	// Create <target>/ itself as a directory.
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create a FILE at <target>/requirements, blocking MkdirAll.
	// This is portable: MkdirAll returns ENOTDIR / equivalent on both
	// Unix and Windows when an existing path component is a file.
	blocker := filepath.Join(target, "requirements")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := cli.SpecInit(target, false)
	if err == nil {
		t.Fatal("SpecInit should have errored when MkdirAll is blocked")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("error should be *cairnerr.Err, got %T: %v", err, err)
	}
	if ce.Kind != "spec_init_mkdir_failed" {
		t.Errorf("kind: got %q, want spec_init_mkdir_failed", ce.Kind)
	}
	if ce.Code != cairnerr.CodeSubstrate {
		t.Errorf("code: got %q, want %q", ce.Code, cairnerr.CodeSubstrate)
	}
	if p, ok := ce.Details["path"].(string); !ok || p != blocker {
		t.Errorf("details.path: got %v, want %q", ce.Details["path"], blocker)
	}
	if _, ok := ce.Details["cause"].(string); !ok {
		t.Errorf("details.cause: missing or not string")
	}
}
```

- [ ] **Step 3: Run the new test — verify it fails**

```bash
go test -race -run TestSpecInit_MkdirFailedReturnsCairnErr ./internal/cli/...
```

Expected: FAIL at `errors.As` — current impl returns a `*fmt.wrapError`, not `*cairnerr.Err`.

- [ ] **Step 4: Replace the `MkdirAll` wrapper in `SpecInit`**

In `internal/cli/spec_init.go`, replace the current mkdir loop (lines 74–77):

```go
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
```

with:

```go
	for _, sub := range []string{"requirements", "tasks"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, cairnerr.New(cairnerr.CodeSubstrate,
				"spec_init_mkdir_failed",
				"create spec subdirectory failed").
				WithCause(err).
				WithDetails(map[string]any{
					"path":  dir,
					"cause": err.Error(),
				})
		}
	}
```

- [ ] **Step 5: Run the test — verify it passes**

```bash
go test -race -run TestSpecInit_MkdirFailedReturnsCairnErr ./internal/cli/...
```

Expected: PASS.

- [ ] **Step 6: Run the full `internal/cli` suite**

```bash
go test -race ./internal/cli/...
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/spec_init.go internal/cli/spec_init_test.go
git commit -m "fix(cli): type spec_init_mkdir_failed as *cairnerr.Err

Replaces the bare fmt.Errorf wrap on os.MkdirAll with a typed
*cairnerr.Err carrying kind=spec_init_mkdir_failed, code=substrate,
and details.path / details.cause. Prepares Task 8 to retire the
init_failed wrapper in cmd/cairn/spec.go without re-leaking an
'internal' envelope on directory-creation failure."
```

---

## Task 6: Type the write error as `spec_init_write_failed` (Unix-gated test)

Same treatment for the `os.WriteFile` error path. Test is Unix-gated because `0o444` read-only mode is a no-op on Windows.

**Files:**
- Modify: `internal/cli/spec_init_test.go` (add test).
- Modify: `internal/cli/spec_init.go` (type the error).

- [ ] **Step 1: Add `runtime` import to `internal/cli/spec_init_test.go`**

Change the import block at the top of `internal/cli/spec_init_test.go` to:

```go
import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
)
```

- [ ] **Step 2: Write `TestSpecInit_WriteFailedReturnsCairnErr`**

Append to `internal/cli/spec_init_test.go`:

```go
func TestSpecInit_WriteFailedReturnsCairnErr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o444 is a no-op on Windows; write_failed branch not reliably triggerable without FS injection")
	}
	root := t.TempDir()
	target := filepath.Join(root, "specs")
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Pre-create the REQ template file with mismatching content AND
	// read-only mode. ReadFile succeeds (read is allowed by 0o444),
	// bytes.Equal fails (mismatching content), WriteFile then fails
	// with EACCES.
	reqPath := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	if err := os.WriteFile(reqPath, []byte("# bogus\n"), 0o444); err != nil {
		t.Fatal(err)
	}

	_, err := cli.SpecInit(target, false)
	if err == nil {
		t.Fatal("SpecInit should have errored on read-only target")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("error should be *cairnerr.Err, got %T: %v", err, err)
	}
	if ce.Kind != "spec_init_write_failed" {
		t.Errorf("kind: got %q, want spec_init_write_failed", ce.Kind)
	}
	if ce.Code != cairnerr.CodeSubstrate {
		t.Errorf("code: got %q, want %q", ce.Code, cairnerr.CodeSubstrate)
	}
	if p, ok := ce.Details["path"].(string); !ok || p != reqPath {
		t.Errorf("details.path: got %v, want %q", ce.Details["path"], reqPath)
	}
	if _, ok := ce.Details["cause"].(string); !ok {
		t.Errorf("details.cause: missing or not string")
	}
}
```

- [ ] **Step 3: Run the new test — verify it fails (Unix)**

```bash
go test -race -run TestSpecInit_WriteFailedReturnsCairnErr ./internal/cli/...
```

Expected (Unix): FAIL at `errors.As` — current impl still wraps `WriteFile` errors with `fmt.Errorf`.
Expected (Windows): SKIP.

- [ ] **Step 4: Replace the `os.WriteFile` error wrap in `SpecInit`**

In `internal/cli/spec_init.go`, inside the per-pair loop, replace:

```go
		if err := os.WriteFile(p.path, []byte(p.body), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", p.path, err)
		}
```

with:

```go
		if err := os.WriteFile(p.path, []byte(p.body), 0o644); err != nil {
			return nil, cairnerr.New(cairnerr.CodeSubstrate,
				"spec_init_write_failed",
				"write template failed").
				WithCause(err).
				WithDetails(map[string]any{
					"path":  p.path,
					"cause": err.Error(),
				})
		}
```

- [ ] **Step 5: Run the test — verify it passes**

```bash
go test -race -run TestSpecInit_WriteFailedReturnsCairnErr ./internal/cli/...
```

Expected: PASS on Unix, SKIP on Windows.

- [ ] **Step 6: Run the full `internal/cli` suite**

```bash
go test -race ./internal/cli/...
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/spec_init.go internal/cli/spec_init_test.go
git commit -m "fix(cli): type spec_init_write_failed as *cairnerr.Err"
```

---

## Task 7: Add post-write verify with `spec_init_write_unverified` (Unix-gated test)

The final piece of the fix. Re-read the just-written file; if the read errors or the bytes don't match the canonical body, return `spec_init_write_unverified` with hash details on content mismatch. A symlink to `/dev/null` is the canonical Unix reproducer of the real-world "WriteFile returns nil but bytes didn't persist" failure mode from the Ship 4 writeup.

**Files:**
- Modify: `internal/cli/spec_init_test.go` (add test).
- Modify: `internal/cli/spec_init.go` (add post-write verify + hash imports).

- [ ] **Step 1: Write `TestSpecInit_WriteUnverifiedReturnsCairnErr`**

Append to `internal/cli/spec_init_test.go`:

```go
func TestSpecInit_WriteUnverifiedReturnsCairnErr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink to /dev/null requires Unix-like FS")
	}
	root := t.TempDir()
	target := filepath.Join(root, "specs")
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(target, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	// Symlink -> /dev/null. os.ReadFile returns empty bytes (differ
	// from template), os.WriteFile follows the symlink and writes to
	// /dev/null (bytes discarded), verify-read returns empty, bytes
	// don't match -> spec_init_write_unverified.
	if err := os.Symlink("/dev/null", reqPath); err != nil {
		t.Fatal(err)
	}

	_, err := cli.SpecInit(target, false)
	if err == nil {
		t.Fatal("SpecInit should have errored on symlink-to-/dev/null")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("error should be *cairnerr.Err, got %T: %v", err, err)
	}
	if ce.Kind != "spec_init_write_unverified" {
		t.Errorf("kind: got %q, want spec_init_write_unverified", ce.Kind)
	}
	if ce.Code != cairnerr.CodeSubstrate {
		t.Errorf("code: got %q, want %q", ce.Code, cairnerr.CodeSubstrate)
	}
	if p, ok := ce.Details["path"].(string); !ok || p != reqPath {
		t.Errorf("details.path: got %v, want %q", ce.Details["path"], reqPath)
	}
	expSize, ok := ce.Details["expected_size"].(int)
	if !ok || expSize == 0 {
		t.Errorf("details.expected_size: want non-zero int, got %v (type %T)",
			ce.Details["expected_size"], ce.Details["expected_size"])
	}
	gotSize, ok := ce.Details["got_size"].(int)
	if !ok || gotSize != 0 {
		t.Errorf("details.got_size: want 0, got %v (type %T)",
			ce.Details["got_size"], ce.Details["got_size"])
	}
	if _, ok := ce.Details["expected_sha256"].(string); !ok {
		t.Errorf("details.expected_sha256: missing or not string")
	}
	if _, ok := ce.Details["got_sha256"].(string); !ok {
		t.Errorf("details.got_sha256: missing or not string")
	}
}
```

- [ ] **Step 2: Run the new test — verify it fails (Unix)**

```bash
go test -race -run TestSpecInit_WriteUnverifiedReturnsCairnErr ./internal/cli/...
```

Expected (Unix): FAIL. Current impl writes to the symlink and then returns `Created` without verifying — test expects an error.
Expected (Windows): SKIP.

- [ ] **Step 3: Add `crypto/sha256` and `encoding/hex` imports to `internal/cli/spec_init.go`**

Change the import block at the top of `internal/cli/spec_init.go` to:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)
```

- [ ] **Step 4: Add the post-write verify branch in `SpecInit`**

In `internal/cli/spec_init.go`, inside the per-pair loop, after the `os.WriteFile` block added in Task 6 and before `res.Created = append(...)`, insert:

```go
		verify, verifyErr := os.ReadFile(p.path)
		if verifyErr != nil {
			return nil, cairnerr.New(cairnerr.CodeSubstrate,
				"spec_init_write_unverified",
				"template write did not persist on disk; inspect filesystem or sync-engine state").
				WithCause(verifyErr).
				WithDetails(map[string]any{
					"path":          p.path,
					"expected_size": len(p.body),
					"cause":         verifyErr.Error(),
				})
		}
		if !bytes.Equal(verify, []byte(p.body)) {
			wantSum := sha256.Sum256([]byte(p.body))
			gotSum := sha256.Sum256(verify)
			return nil, cairnerr.New(cairnerr.CodeSubstrate,
				"spec_init_write_unverified",
				"template write did not persist on disk; inspect filesystem or sync-engine state").
				WithDetails(map[string]any{
					"path":            p.path,
					"expected_size":   len(p.body),
					"got_size":        len(verify),
					"expected_sha256": hex.EncodeToString(wantSum[:]),
					"got_sha256":      hex.EncodeToString(gotSum[:]),
				})
		}
```

- [ ] **Step 5: Drop the now-unused `fmt` import**

After Tasks 5 and 6 converted the `MkdirAll` and `WriteFile` wrappers to `cairnerr.New`, `internal/cli/spec_init.go` has no more `fmt.Errorf` calls. Confirm:

```bash
grep -n "fmt\." internal/cli/spec_init.go
```

Expected: zero matches. Remove `"fmt"` from the import block. Final imports become:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)
```

If `grep` shows any `fmt.` match, something went wrong in Tasks 5/6 — stop and reconcile before proceeding.

- [ ] **Step 6: Run the new test — verify it passes**

```bash
go test -race -run TestSpecInit_WriteUnverifiedReturnsCairnErr ./internal/cli/...
```

Expected: PASS on Unix, SKIP on Windows.

- [ ] **Step 7: Run the full `internal/cli` suite**

```bash
go test -race ./internal/cli/...
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/spec_init.go internal/cli/spec_init_test.go
git commit -m "fix(cli): add post-write verify with spec_init_write_unverified

After every successful os.WriteFile, SpecInit now re-reads the
target path and compares bytes against the canonical template.
A read error produces spec_init_write_unverified with
details.cause; a byte mismatch produces spec_init_write_unverified
with details.expected_size / got_size / expected_sha256 /
got_sha256.

This catches the exact real-world failure recorded in the Ship 4
friction writeup — where os.WriteFile returned nil but the bytes
never landed on disk (symlink to /dev/null is the canonical
reproducer)."
```

---

## Task 8: Retire the `init_failed` wrapper in `cmd/cairn/spec.go`

All user-visible errors from `SpecInit` are now typed `*cairnerr.Err`. The wrapper that turned generic errors into `init_failed` is dead weight and obscures the new specific kinds.

**Files:**
- Modify: `cmd/cairn/spec.go` (simplify the `spec init` RunE).

- [ ] **Step 1: Confirm no test asserts against the literal `"init_failed"`**

```bash
go test -race ./... 2>&1 | tee /tmp/pre-init-failed-removal.log
grep -r "init_failed" internal/ cmd/ 2>&1 | tee /tmp/init-failed-refs.log
```

Expected: the only non-test reference to `init_failed` is the line in `cmd/cairn/spec.go` itself. The `docs/superpowers/plans/2026-04-19-ship-3-superpowers-integration.md` historical reference is outside code scope and not part of this grep. If any *test* file asserts `"init_failed"`, stop and investigate before proceeding.

- [ ] **Step 2: Simplify the `spec init` RunE**

In `cmd/cairn/spec.go`, replace the `initCmd.RunE` body (currently lines 86–94):

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "spec.init", "", func() (any, error) {
				res, err := cli.SpecInit(initPath, initForce)
				if err != nil {
					return nil, cairnerr.New(cairnerr.CodeSubstrate, "init_failed", err.Error()).WithCause(err)
				}
				return res, nil
			}))
			return nil
		},
```

with:

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(cli.Run(cmd.OutOrStdout(), "spec.init", "", func() (any, error) {
				return cli.SpecInit(initPath, initForce)
			}))
			return nil
		},
```

- [ ] **Step 3: Check the `cairnerr` import is still needed**

`cmd/cairn/spec.go` still uses `cairnerr.New(... "load_failed" ...)` in the `spec validate` RunE. Keep the import. Verify:

```bash
grep -n "cairnerr\." cmd/cairn/spec.go
```

Expected: at least two matches (the `load_failed` construction and the `ExitCodeFor` call). If not, remove the import — otherwise keep it.

- [ ] **Step 4: Build + run full suite**

```bash
go build ./...
go test -race ./...
```

Expected: build PASS. All tests PASS — including `TestSpecInit_MkdirFailedReturnsCairnErr` / `WriteFailedReturnsCairnErr` / `WriteUnverifiedReturnsCairnErr`, which now receive the typed error straight through `cli.Run` without the `init_failed` middleman.

- [ ] **Step 5: Commit**

```bash
git add cmd/cairn/spec.go
git commit -m "refactor(cmd): retire init_failed wrapper in spec init

SpecInit now returns typed *cairnerr.Err for every failure mode
(spec_init_mkdir_failed, spec_init_write_failed,
spec_init_write_unverified). The generic init_failed wrapper
collapsed all of them into one kind string, obscuring
diagnostics for envelope consumers.

Breaking-change note: external scripts parsing envelope
error.code for 'init_failed' must update to match the three
new specific kinds. No in-repo test or consumer asserted
against the old literal."
```

---

## Task 9: Add `TestSpecInitE2E_OverwritesPlaceholder` integration test

This is the canonical reproduction of the Ship 4 friction. End-to-end: pre-create zero-byte placeholders, run the `cairn` binary, assert the envelope reports success and files on disk are byte-equal to the canonical template.

**Files:**
- Modify: `internal/integration/spec_init_e2e_test.go` (append new test).

- [ ] **Step 1: Add `github.com/ProductOfAmerica/cairn/internal/cli` import (if not already present)**

Check the import block at the top of `internal/integration/spec_init_e2e_test.go`. The existing file imports `"os"`, `"path/filepath"`, `"strings"`, `"testing"`. Add `cli` import:

```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)
```

- [ ] **Step 2: Write `TestSpecInitE2E_OverwritesPlaceholder`**

Append to `internal/integration/spec_init_e2e_test.go`:

```go
func TestSpecInitE2E_OverwritesPlaceholder(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, "specs", sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := filepath.Join(root, "specs", "requirements", "REQ-001.yaml.example")
	taskPath := filepath.Join(root, "specs", "tasks", "TASK-001.yaml.example")
	// The synthetic repro of the OneDrive silent-failure mode.
	if err := os.WriteFile(reqPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	out := runCLIInDir(t, root, "spec", "init", "--path", "specs")
	env := parseEnvelope(t, out)

	if env["error"] != nil {
		t.Fatalf("unexpected error envelope: %v", env["error"])
	}
	if kind, _ := env["kind"].(string); kind != "spec.init" {
		t.Fatalf("envelope kind: got %q, want spec.init", kind)
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope missing data: %+v", env)
	}
	created, _ := data["created"].([]any)
	if len(created) != 2 {
		t.Fatalf("created: want 2, got %d: %v", len(created), created)
	}
	skipped, _ := data["skipped"].([]any)
	if len(skipped) != 0 {
		t.Errorf("skipped: want 0, got %v", skipped)
	}

	// Byte-for-byte equality to the canonical templates (marker-substring
	// check alone is insufficient — the postcondition is byte-equality).
	wantReq, wantTask := cli.TemplatesForTest()
	if b, err := os.ReadFile(reqPath); err != nil {
		t.Fatalf("read req: %v", err)
	} else if string(b) != wantReq {
		t.Errorf("req content: not byte-equal to canonical template")
	}
	if b, err := os.ReadFile(taskPath); err != nil {
		t.Fatalf("read task: %v", err)
	} else if string(b) != wantTask {
		t.Errorf("task content: not byte-equal to canonical template")
	}
}
```

- [ ] **Step 3: Run the new test**

```bash
go test -race -run TestSpecInitE2E_OverwritesPlaceholder ./internal/integration/...
```

Expected: PASS. The binary compiled in prior tasks already has the fix; this test is the end-to-end validation.

- [ ] **Step 4: Run the full integration suite**

```bash
go test -race ./internal/integration/...
```

Expected: all tests PASS — specifically `TestSpecInitE2E` (the existing fresh-init test) must still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/integration/spec_init_e2e_test.go
git commit -m "test(integration): e2e coverage for spec init placeholder overwrite

Reproduces the exact Ship 4 friction repro (pre-existing
zero-byte files at both template paths) and asserts the
binary now writes canonical template content and reports
them in the Created list."
```

---

## Task 10: Add `TestSpecInitE2E_MkdirFailedEnvelope` integration test

Exercises the full `cli.Run` envelope-emission pipeline for a typed `*cairnerr.Err` returned by `SpecInit`. Uses `exec.Command` directly because `runCLIInDir` doesn't expose the exit code.

**Files:**
- Modify: `internal/integration/spec_init_e2e_test.go` (append new test).

- [ ] **Step 1: Add `bytes`, `os/exec` imports**

Update the import block at the top of `internal/integration/spec_init_e2e_test.go`:

```go
import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)
```

- [ ] **Step 2: Write `TestSpecInitE2E_MkdirFailedEnvelope`**

Append to `internal/integration/spec_init_e2e_test.go`:

```go
func TestSpecInitE2E_MkdirFailedEnvelope(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// File blocker at <root>/specs/requirements — forces MkdirAll to ENOTDIR.
	blocker := filepath.Join(root, "specs", "requirements")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	cmd := exec.Command(cairnBinary, "spec", "init", "--path", "specs")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+home)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	_ = cmd.Run() // non-zero exit expected

	env := parseEnvelope(t, string(bytes.TrimSpace(outBuf.Bytes())))
	errMap, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got: %+v", env)
	}
	if got, _ := errMap["code"].(string); got != "spec_init_mkdir_failed" {
		t.Errorf("error.code: got %q, want spec_init_mkdir_failed", got)
	}
	details, _ := errMap["details"].(map[string]any)
	if p, _ := details["path"].(string); p == "" {
		t.Errorf("details.path: missing")
	}
	if _, ok := details["cause"].(string); !ok {
		t.Errorf("details.cause: missing or not string")
	}
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Errorf("exit code: got %d, want 4 (substrate)", code)
	}
}
```

- [ ] **Step 3: Run the new test**

```bash
go test -race -run TestSpecInitE2E_MkdirFailedEnvelope ./internal/integration/...
```

Expected: PASS.

- [ ] **Step 4: Run the full integration suite**

```bash
go test -race ./internal/integration/...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integration/spec_init_e2e_test.go
git commit -m "test(integration): e2e envelope for spec_init_mkdir_failed

Verifies that a directory-creation failure in SpecInit flows
through cli.Run as a typed *cairnerr.Err, producing exit code 4
and error.code=spec_init_mkdir_failed in the JSON envelope."
```

---

## Task 11: Add `TestSpecInitE2E_WriteUnverifiedEnvelope` integration test (Unix-gated)

The end-to-end reproduction of the real-world "bytes didn't land" failure. Symlink-to-`/dev/null` on Unix; skipped on Windows.

**Files:**
- Modify: `internal/integration/spec_init_e2e_test.go` (append new test).

- [ ] **Step 1: Add `runtime` import**

Update the import block at the top of `internal/integration/spec_init_e2e_test.go`:

```go
import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)
```

- [ ] **Step 2: Write `TestSpecInitE2E_WriteUnverifiedEnvelope`**

Append to `internal/integration/spec_init_e2e_test.go`:

```go
func TestSpecInitE2E_WriteUnverifiedEnvelope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink to /dev/null requires Unix-like FS")
	}
	root := t.TempDir()
	for _, sub := range []string{"requirements", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, "specs", sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reqPath := filepath.Join(root, "specs", "requirements", "REQ-001.yaml.example")
	if err := os.Symlink("/dev/null", reqPath); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	cmd := exec.Command(cairnBinary, "spec", "init", "--path", "specs")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+home)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	_ = cmd.Run() // non-zero exit expected

	env := parseEnvelope(t, string(bytes.TrimSpace(outBuf.Bytes())))
	errMap, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got: %+v", env)
	}
	if got, _ := errMap["code"].(string); got != "spec_init_write_unverified" {
		t.Errorf("error.code: got %q, want spec_init_write_unverified", got)
	}
	details, _ := errMap["details"].(map[string]any)
	// JSON numbers unmarshal as float64.
	if got, _ := details["got_size"].(float64); got != 0 {
		t.Errorf("details.got_size: got %v, want 0", details["got_size"])
	}
	if exp, _ := details["expected_size"].(float64); exp == 0 {
		t.Errorf("details.expected_size: want non-zero, got %v", details["expected_size"])
	}
	if s, _ := details["expected_sha256"].(string); s == "" {
		t.Errorf("details.expected_sha256: missing")
	}
	if s, _ := details["got_sha256"].(string); s == "" {
		t.Errorf("details.got_sha256: missing")
	}
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Errorf("exit code: got %d, want 4 (substrate)", code)
	}
}
```

- [ ] **Step 3: Run the new test**

```bash
go test -race -run TestSpecInitE2E_WriteUnverifiedEnvelope ./internal/integration/...
```

Expected: PASS on Unix, SKIP on Windows.

- [ ] **Step 4: Run the full integration suite one more time**

```bash
go test -race ./internal/integration/...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integration/spec_init_e2e_test.go
git commit -m "test(integration): e2e envelope for spec_init_write_unverified

Reproduces the real-world 'WriteFile succeeded but bytes didn't
land' failure via symlink to /dev/null. Verifies the envelope
carries exit code 4, error.code=spec_init_write_unverified, and
details.{expected_size,got_size,expected_sha256,got_sha256}.

Unix-only (symlinks require Unix-like FS semantics)."
```

---

## Task 12: Full repo test + vet sweep

**Files:** none.

- [ ] **Step 1: Run the full race-enabled suite**

```bash
go test -race ./...
```

Expected: all tests PASS across every package. If any unrelated test fails, pause — the fix should not have touched anything outside `internal/cli` + `cmd/cairn/spec.go` + `internal/integration/spec_init_e2e_test.go`.

- [ ] **Step 2: Run `go vet`**

```bash
go vet ./...
```

Expected: no output.

- [ ] **Step 3: Run `go mod verify`**

```bash
go mod verify
```

Expected: `all modules verified`.

- [ ] **Step 4: Build the final binary**

```bash
go build -o bin/cairn ./cmd/cairn
```

Expected: produces `bin/cairn` (or `bin/cairn.exe` on Windows) with no errors.

- [ ] **Step 5: Sanity-check the fix against the synthetic repro from the Ship 4 writeup**

```bash
rm -rf /tmp/cairn-repro && mkdir -p /tmp/cairn-repro/specs/requirements /tmp/cairn-repro/specs/tasks
: > /tmp/cairn-repro/specs/requirements/REQ-001.yaml.example
: > /tmp/cairn-repro/specs/tasks/TASK-001.yaml.example
cd /tmp/cairn-repro && CAIRN_HOME=/tmp/cairn-repro-state "$OLDPWD/bin/cairn" spec init --format=json
wc -c specs/requirements/REQ-001.yaml.example specs/tasks/TASK-001.yaml.example
```

Expected: envelope reports `"created":["...REQ-001.yaml.example","...TASK-001.yaml.example"]`, `wc -c` shows non-zero bytes for both files (1120-ish for REQ, 505-ish for TASK). The synthetic reproduction of the Ship 4 friction no longer reproduces.

---

## Task 13: Update the Ship 4 friction writeup Status line

Close the loop on the friction doc so future readers see the fix reference.

**Files:**
- Modify: `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md` (Status line).

- [ ] **Step 1: Capture the current commit SHA for the fix**

```bash
git log --oneline -n 12 fix/onedrive-spec-init
```

Note the SHAs of Tasks 3, 5, 6, 7, 8 (the impl-changing commits). The branch tip will become the reference once the PR merges.

- [ ] **Step 2: Edit the Status line in the friction writeup**

In `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md`, replace line 5 which currently reads:

```
**Status:** Workaround known (`--force` on first init). Cairn-side fix not yet specified.
```

with:

```
**Status:** Fix landed on branch `fix/onedrive-spec-init` (2026-04-20). See `docs/superpowers/specs/2026-04-20-onedrive-spec-init-fix-design.md` for the design and `docs/superpowers/plans/2026-04-20-onedrive-spec-init-fix.md` for the implementation plan. Content-check skip + post-write verify replaces the `os.Stat` skip; three typed `*cairnerr.Err` kinds (`spec_init_mkdir_failed`, `spec_init_write_failed`, `spec_init_write_unverified`) replace the generic `init_failed` wrapper. `--force` is no longer required for first init over a zero-byte predecessor.
```

Also delete lines 7–8 (the "Next session" block), which are now obsolete:

```
> **Next session:** invoke `superpowers:brainstorming` on this file to scope a small fix in cairn itself. See "For the next session" at the bottom for a copy-paste prompt.

---
```

Leave the `---` separator if present on its own line for reading structure; otherwise skip. Section headings below ("Symptom", "Reconstructed timeline", etc.) stay unchanged — they're the historical record.

- [ ] **Step 3: Commit**

```bash
git add docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md
git commit -m "docs(ship-4): mark OneDrive spec-init friction as fixed"
```

---

## Task 14: GitNexus impact + detect-changes sanity (per CLAUDE.md)

CLAUDE.md requires `gitnexus_impact` before editing symbols and `gitnexus_detect_changes` before committing. Run these as a final pre-push check; catch any surprise changes.

**Files:** none (read-only analysis).

- [ ] **Step 1: Detect changes since `master`**

Use the `gitnexus_detect_changes` MCP tool (from the Claude Code session driving this plan):

```
gitnexus_detect_changes({scope: "compare", base_ref: "master"})
```

Expected: only symbols under `internal/cli/SpecInit`, `internal/cli/TemplatesForTest`, `cmd/cairn/newSpecCmd`, and the test files listed in "File structure" at the top of this plan. No unexpected changes elsewhere.

- [ ] **Step 2: If changes outside the expected set are reported, pause and investigate**

Nothing in this plan should have modified, for example, `internal/task/`, `internal/verdict/`, `internal/evidence/`, `internal/db/`, `internal/intent/`, or any schema file. If a detect-changes result includes them, something is wrong — do not proceed.

- [ ] **Step 3: Re-run the GitNexus index if it has drifted during the work**

```bash
npx gitnexus analyze
```

Expected: index rebuilt cleanly. This step is optional; Claude Code's `PostToolUse` hook handles it automatically after `git commit`.

---

## Task 15: Push branch and open PR

Final handoff task.

**Files:** none.

- [ ] **Step 1: Push the branch**

```bash
git push -u origin fix/onedrive-spec-init
```

Expected: branch pushed, tracking set.

- [ ] **Step 2: Open a PR with a descriptive body**

```bash
gh pr create --title "fix(cli): close OneDrive silent no-op in 'cairn spec init'" --body "$(cat <<'EOF'
## Summary

- Replaces the `os.Stat`-success skip proxy in `SpecInit` with an `os.ReadFile + bytes.Equal` byte-compare against the canonical template. A zero-byte predecessor (OneDrive Files-On-Demand placeholder, crash residue, `: >` redirect) no longer fools the command into reporting success with empty files on disk.
- Adds post-write verify (reread + byte-compare + SHA-256 both sides on mismatch) so the envelope stops lying when `os.WriteFile` returns `nil` but the bytes didn't persist.
- Retires the generic `init_failed` error kind in favor of three specific typed kinds: `spec_init_mkdir_failed`, `spec_init_write_failed`, `spec_init_write_unverified`.

## Spec and plan

- Design: `docs/superpowers/specs/2026-04-20-onedrive-spec-init-fix-design.md`
- Plan:   `docs/superpowers/plans/2026-04-20-onedrive-spec-init-fix.md`
- Friction writeup: `docs/ship-4-lessons/2026-04-20-onedrive-spec-init.md`

## Breaking changes (release-notes items)

- `init_failed` error kind retired. External scripts parsing envelope `error.code` for `init_failed` must update to match the three new specific kinds.
- `Created` semantic widened: previously meant "path did not exist, we created it"; now means "we wrote bytes to this path" (also covers overwriting a placeholder or corrupted predecessor).
- `Skipped` semantic narrowed: previously meant "path existed, we left it alone"; now means "path existed with content byte-identical to the canonical template."
- `--force` role compressed: non-force now also overwrites on content mismatch. `--force` only retains the "rewrite known-correct file anyway" case.
- Manually-edited `.yaml.example` files are no longer preserved across `spec init` calls (they get restored to canonical template). The template header always said "DO NOT EDIT THIS FILE"; the previous silent-preserve behavior was incidental.
- Non-regular predecessors (symlinks, special files) are now followed on read and write. Hardening against this is explicitly deferred (see spec "Out of scope").

## Test plan

- [ ] `go test -race ./...` passes on Linux, macOS, Windows.
- [ ] `go vet ./...` clean.
- [ ] `go mod verify` passes.
- [ ] New unit tests: `OverwritesZeroBytePlaceholder`, `OverwritesWrongContent`, `MkdirFailedReturnsCairnErr`, `WriteFailedReturnsCairnErr` (Unix), `WriteUnverifiedReturnsCairnErr` (Unix).
- [ ] New integration tests: `OverwritesPlaceholder`, `MkdirFailedEnvelope`, `WriteUnverifiedEnvelope` (Unix).
- [ ] Manual sanity check via the Ship 4 synthetic repro in `/tmp/cairn-repro` — envelope reports Created, wc -c shows non-zero.

## Waiver on CLAUDE.md "fixture per new error kind" convention

`testdata/e2e/` fixtures not added. Input state for each new-kind test is "no files" or "one file at a known path" — a fixture directory would contain either nothing or a single empty file. Spec details the waiver in its Test-plan section. All three new kinds get integration-test coverage.
EOF
)"
```

Expected: PR created. Capture the URL.

- [ ] **Step 3: Paste the PR URL into this plan's execution log**

(Done by the executing agent / user, not part of the plan itself.)

---

## After-plan: cairn dogfood via using-cairn

> This section is a **handoff**, not a task. Per the `using-cairn` skill's invocation rule: *"This skill MUST be invoked explicitly by the orchestrating session after each of `superpowers:brainstorming` and `superpowers:writing-plans` commits."*

After this plan is committed (which happens as part of the `writing-plans` skill's output), the orchestrating session must:

1. Invoke the `using-cairn` skill (hub + `yaml-authoring.md` spoke).
2. Derive `specs/requirements/REQ-003.yaml` from `docs/superpowers/specs/2026-04-20-onedrive-spec-init-fix-design.md` (next free REQ number; current repo has REQ-002).
3. Derive `specs/tasks/TASK-003-NNN.yaml` files from this plan (`docs/superpowers/plans/2026-04-20-onedrive-spec-init-fix.md`). One TASK YAML per top-level Task section above; task dependencies follow the linear order (`TASK-003-NNN` depends on `TASK-003-(NNN-1)`).
4. Verify derivation determinism: rerun the derivation, confirm byte-identical output.
5. Run `cairn spec validate --path specs` — expect no errors, `requirements` count = 2 (REQ-002, REQ-003 — the `.example` is excluded), `tasks` count = pre-existing TASK-002-NNN plus the new TASK-003-NNN set.
6. Commit the derived YAML with message `chore(specs): derive REQ-003 / TASK-003-NNN yaml from prose`.
7. (Optional) `cairn task plan --path specs` materializes the task records into the state DB, enabling subagent-driven-development-with-verdicts for plan execution.

---

## Self-review notes

A fresh pass over this plan against the spec (`docs/superpowers/specs/2026-04-20-onedrive-spec-init-fix-design.md`) confirms:

- Every new error kind in the spec has a unit test **and** an integration test in the plan. `spec_init_write_failed` is Unix-only at the unit layer and has no integration test — consistent with the spec's explicit rationale ("adding an e2e test for it would duplicate the unit test without exercising a different `cli.Run` path").
- Every spec bullet in "Changes to the user contract" appears in the PR body's "Breaking changes" list (Task 15). Hash details, symlink follow-through, partial-write ordering, path encoding — all covered.
- Every new kind carries `details.cause` when a cause exists (spec's plumbing fix around `envelope.go`).
- The template constants stay byte-identical (Task 2 only *exports* a helper; the `requirementTemplate` / `taskTemplate` literals are not touched). `intent.Validate`'s `renamed_template` detector still fires on the marker comment.
- Tasks are bite-sized (2–5 min each) and pure TDD (red → green → commit per behavior change).
- No placeholders. Every code step has the full code literal; every run step has the exact command.
