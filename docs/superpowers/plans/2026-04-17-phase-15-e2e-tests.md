# Phase 15 — End-to-End Regression Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated end-to-end regression suite under `internal/integration/e2e_test.go` that exercises five whole-system scenarios against the real `cairn` binary, with fixture data under `testdata/e2e/`.

**Architecture:** Each test creates a fresh throwaway git repo, writes a spec under `specs/`, and drives the compiled `cairn` binary via `exec.Command` (same pattern as `TestShip1DogfoodEventCoverage`). Tests assert on JSON envelope shape, exit code, emitted event kinds, and derived state (e.g. `verdict latest` freshness). Fixtures that exceed a few lines live under `testdata/e2e/<scenario>/` and are copied into the test's tempdir. Shared helpers (`runCairn`, `mustDogfoodRepo` equivalent) are reused or lightly adapted from existing integration tests.

**Tech Stack:**
- Go 1.25 stdlib `testing` + `exec.Command` + `encoding/json`.
- Existing `internal/integration/main_test.go` binary builder.
- Existing `internal/integration/dogfood_test.go` `runCairn` helper (imported — same package).

---

## File Structure

```
internal/integration/
  e2e_test.go                           # 5 test functions, in-package with existing tests.
  e2e_helpers_test.go                   # Shared helpers specific to e2e scenarios.

testdata/e2e/                           # Small YAML fixtures copied into test tempdirs.
  spec-edit-stale/
    requirements/REQ-001.yaml
    requirements/REQ-001-edited.yaml    # The edited-producer variant applied mid-test.
    tasks/TASK-001.yaml
  dep-blocking/
    requirements/REQ-001.yaml
    tasks/TASK-dep.yaml                 # Dependency target, never completed.
    tasks/TASK-main.yaml                # depends_on: [TASK-dep].
  lease-expiry/
    requirements/REQ-001.yaml
    tasks/TASK-001.yaml
  op-id-replay/
    requirements/REQ-001.yaml
    tasks/TASK-001.yaml
  evidence-hash-mismatch/
    requirements/REQ-001.yaml
    tasks/TASK-001.yaml
```

All five scenarios reuse a minimal REQ-001 with one gate AC-001 (`kind: test`, producer: executable running `echo ok`). Only the spec-edit-stale scenario has a second YAML that replaces the first mid-test.

**Helpers on `e2e_helpers_test.go`:**
- `copyFixture(t *testing.T, dst string, fixtureName string)` — copies `testdata/e2e/<fixtureName>/**` into `dst`.
- `runCairnExit(t, dir, cairnHome, expectedExit int, args ...)` — wrapper around `runCairn` that asserts the exit code.
- `expectEnvelopeKind(t, env, kind)` — checks envelope `kind` field.
- `expectErrorKind(t, env, kind)` — checks envelope `error.code` field.

Existing `runCairn` and `mustDogfoodRepo` (in `dogfood_test.go`) stay unchanged; e2e tests build on them.

---

## Task 1: Test harness helpers + fixtures for spec-edit-stale

**Files:**
- Create: `internal/integration/e2e_helpers_test.go`
- Create: `testdata/e2e/spec-edit-stale/requirements/REQ-001.yaml`
- Create: `testdata/e2e/spec-edit-stale/requirements/REQ-001-edited.yaml`
- Create: `testdata/e2e/spec-edit-stale/tasks/TASK-001.yaml`

- [ ] **Step 1: Write the helpers file**

Create `internal/integration/e2e_helpers_test.go`:
```go
package integration_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyFixture copies every file under testdata/e2e/<fixtureName>/ into dst,
// preserving relative paths. The test's tempdir is the typical dst.
func copyFixture(t *testing.T, dst, fixtureName string) {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "e2e", fixtureName)
	// The test runs from internal/integration; the testdata root lives two
	// levels up in the repo root.
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %q not found: %v", fixtureName, err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyFixture %q: %v", fixtureName, err)
	}
}

// runCairnExit is runCairn plus an exit-code assertion. Useful when the
// test cares both about the exit code and the envelope payload.
func runCairnExit(t *testing.T, dir, cairnHome string, expectedExit int, args ...string) map[string]any {
	t.Helper()
	env, code := runCairn(t, dir, cairnHome, args...)
	if code != expectedExit {
		t.Fatalf("cairn %v: exit=%d want %d\nenv=%+v", args, code, expectedExit, env)
	}
	return env
}

// expectEnvelopeKind fails the test if env.kind != want.
func expectEnvelopeKind(t *testing.T, env map[string]any, want string) {
	t.Helper()
	got, _ := env["kind"].(string)
	if got != want {
		t.Fatalf("envelope kind=%q want %q", got, want)
	}
}

// expectErrorKind fails the test if env.error.code != want.
func expectErrorKind(t *testing.T, env map[string]any, want string) {
	t.Helper()
	e, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no error, got %+v", env)
	}
	got, _ := e["code"].(string)
	if got != want {
		t.Fatalf("error.code=%q want %q", got, want)
	}
}

// stringsContainsAll returns true iff every substring is present in s.
// Not currently used but kept for future scenario diagnostics.
func stringsContainsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Write the spec-edit-stale fixture**

Create `testdata/e2e/spec-edit-stale/requirements/REQ-001.yaml`:
```yaml
id: REQ-001
title: spec-edit-stale scenario
why: verify gate_def_hash drift flips verdict fresh=false
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
```

Create `testdata/e2e/spec-edit-stale/requirements/REQ-001-edited.yaml`:
```yaml
id: REQ-001
title: spec-edit-stale scenario (edited)
why: verify gate_def_hash drift flips verdict fresh=false
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, changed]
        pass_on_exit_code: 0
```

Create `testdata/e2e/spec-edit-stale/tasks/TASK-001.yaml`:
```yaml
id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
```

- [ ] **Step 3: Build verifies helpers compile**

Run: `go build ./internal/integration/...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/e2e_helpers_test.go testdata/e2e/spec-edit-stale/
git commit -m "test(e2e): helpers + spec-edit-stale fixture"
```

---

## Task 2: E2E test — spec-edit-stale

**Files:**
- Create: `internal/integration/e2e_test.go`

- [ ] **Step 1: Write the test**

Create `internal/integration/e2e_test.go`:
```go
package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E_SpecEditStale(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "spec-edit-stale")
	// Rename the fixture so the first plan sees REQ-001.yaml (not -edited).
	origSpec := filepath.Join(repo, "specs", "requirements", "REQ-001.yaml")
	editedSpec := filepath.Join(repo, "specs", "requirements", "REQ-001-edited.yaml")
	// Move fixture dir into place: the fixture's specs/ root.
	_ = os.Rename(filepath.Join(repo, "requirements"), filepath.Join(repo, "specs", "requirements"))
	_ = os.Rename(filepath.Join(repo, "tasks"), filepath.Join(repo, "specs", "tasks"))
	_ = origSpec
	_ = editedSpec

	cairnHome := t.TempDir()

	// Initial plan + claim + verdict + complete.
	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	runID := env["data"].(map[string]any)["run_id"].(string)
	claimID := env["data"].(map[string]any)["claim_id"].(string)

	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

	// Sanity: latest is fresh.
	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); !fresh {
		t.Fatal("expected fresh=true before spec edit")
	}

	// Replace the spec with the edited variant (changes producer.config.command).
	data, err := os.ReadFile(filepath.Join(repo, "specs", "requirements", "REQ-001-edited.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(repo, "specs", "requirements", "REQ-001-edited.yaml"))

	// Re-plan picks up new gate_def_hash.
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Now verdict latest must be fresh=false.
	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); fresh {
		t.Fatalf("expected fresh=false after gate edit, got env=%+v", env)
	}
}
```

- [ ] **Step 2: Add a `mustEmptyRepo` helper**

Append to `internal/integration/e2e_helpers_test.go`:
```go
// mustEmptyRepo creates a throwaway git repo with no spec files. Callers
// supply the spec themselves (typically via copyFixture).
func mustEmptyRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run := func(args ...string) {
		c := osExecCommand(args[0], args[1:]...)
		c.Dir = d
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "commit", "--allow-empty", "-q", "-m", "bootstrap")
	return d
}

// osExecCommand is an indirection so tests can mock exec if needed.
var osExecCommand = execCommand
```

Also add at top of the file the import + declaration:
```go
import (
	"os/exec"
	// ... keep others
)

// execCommand is the real exec.Command; osExecCommand points at it by default.
var execCommand = exec.Command
```

Actually this indirection is not needed — just inline `exec.Command` in `mustEmptyRepo`. Remove the `osExecCommand` / `execCommand` vars and call `exec.Command` directly:

```go
func mustEmptyRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run := func(args ...string) {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = d
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "commit", "--allow-empty", "-q", "-m", "bootstrap")
	return d
}
```

Add `"os/exec"` to the existing imports in `e2e_helpers_test.go`.

- [ ] **Step 3: Fix the fixture-copy logic**

The test code as written has a bug: `copyFixture` extracts to `repo` directly, but the fixture's internal layout is `requirements/…` and `tasks/…`. We need to copy into `repo/specs/…` so the cairn CLI's `--path specs` default works.

Option A: change `copyFixture` to accept a nested prefix.
Option B: reshape the fixture directories to include `specs/` at the top level.

Pick **Option B** — cleaner. Restructure fixtures so each scenario's root is the `specs/` tree:

Move the fixture files created in Task 1:
- `testdata/e2e/spec-edit-stale/requirements/REQ-001.yaml` → `testdata/e2e/spec-edit-stale/specs/requirements/REQ-001.yaml`
- `testdata/e2e/spec-edit-stale/requirements/REQ-001-edited.yaml` → `testdata/e2e/spec-edit-stale/specs/requirements/REQ-001-edited.yaml`
- `testdata/e2e/spec-edit-stale/tasks/TASK-001.yaml` → `testdata/e2e/spec-edit-stale/specs/tasks/TASK-001.yaml`

Now `copyFixture(t, repo, "spec-edit-stale")` produces `repo/specs/requirements/…` directly. Remove the post-copy `os.Rename` lines from the test. Updated test body (replace the fixture/rename dance at the top of `TestE2E_SpecEditStale`):

```go
func TestE2E_SpecEditStale(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "spec-edit-stale")

	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	runID := env["data"].(map[string]any)["run_id"].(string)
	claimID := env["data"].(map[string]any)["claim_id"].(string)

	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); !fresh {
		t.Fatal("expected fresh=true before spec edit")
	}

	data, err := os.ReadFile(filepath.Join(repo, "specs", "requirements", "REQ-001-edited.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(repo, "specs", "requirements", "REQ-001-edited.yaml"))

	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	env = runCairnExit(t, repo, cairnHome, 0, "verdict", "latest", "AC-001")
	if fresh, _ := env["data"].(map[string]any)["fresh"].(bool); fresh {
		t.Fatalf("expected fresh=false after gate edit, got env=%+v", env)
	}
}
```

- [ ] **Step 4: Run the test**

Run: `go test -race -v ./internal/integration/... -run TestE2E_SpecEditStale`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/integration/e2e_test.go internal/integration/e2e_helpers_test.go testdata/e2e/spec-edit-stale/
git commit -m "test(e2e): spec-edit flips verdict fresh=false"
```

---

## Task 3: E2E test — dep-blocking

**Files:**
- Create: `testdata/e2e/dep-blocking/specs/requirements/REQ-001.yaml`
- Create: `testdata/e2e/dep-blocking/specs/tasks/TASK-dep.yaml`
- Create: `testdata/e2e/dep-blocking/specs/tasks/TASK-main.yaml`
- Modify: `internal/integration/e2e_test.go`

- [ ] **Step 1: Create fixtures**

Create `testdata/e2e/dep-blocking/specs/requirements/REQ-001.yaml`:
```yaml
id: REQ-001
title: dep-blocking scenario
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
```

Create `testdata/e2e/dep-blocking/specs/tasks/TASK-dep.yaml`:
```yaml
id: TASK-dep
implements: [REQ-001]
required_gates: [AC-001]
```

Create `testdata/e2e/dep-blocking/specs/tasks/TASK-main.yaml`:
```yaml
id: TASK-main
implements: [REQ-001]
depends_on: [TASK-dep]
required_gates: [AC-001]
```

- [ ] **Step 2: Append the test to e2e_test.go**

```go
func TestE2E_DepBlocking(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "dep-blocking")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Attempt to claim TASK-main while TASK-dep is still open. Should get
	// exit 2 with error.code="dep_not_done" and details listing TASK-dep.
	env, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-main",
		"--agent", "e2e", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "dep_not_done")

	// Details should mention TASK-dep with status=open.
	e, _ := env["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	blocking, _ := details["blocking"].([]any)
	if len(blocking) != 1 {
		t.Fatalf("expected 1 blocking dep, got %d", len(blocking))
	}
	b := blocking[0].(map[string]any)
	if b["id"] != "TASK-dep" {
		t.Errorf("blocking[0].id=%v, want TASK-dep", b["id"])
	}
	if b["status"] != "open" {
		t.Errorf("blocking[0].status=%v, want open", b["status"])
	}

	// Complete TASK-dep (claim → evidence → verdict → complete).
	env = runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-dep",
		"--agent", "e2e", "--ttl", "30m")
	depRun := env["data"].(map[string]any)["run_id"].(string)
	depClaim := env["data"].(map[string]any)["claim_id"].(string)

	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", depRun, "--status", "pass",
		"--evidence", out,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", depClaim)

	// Now TASK-main should claim cleanly.
	runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-main",
		"--agent", "e2e", "--ttl", "30m")
}
```

- [ ] **Step 3: Run the test**

Run: `go test -race -v ./internal/integration/... -run TestE2E_DepBlocking`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/e2e_test.go testdata/e2e/dep-blocking/
git commit -m "test(e2e): dep-blocking refuses claim until dep done"
```

---

## Task 4: E2E test — lease-expiry

**Files:**
- Create: `testdata/e2e/lease-expiry/specs/requirements/REQ-001.yaml`
- Create: `testdata/e2e/lease-expiry/specs/tasks/TASK-001.yaml`
- Modify: `internal/integration/e2e_test.go`

- [ ] **Step 1: Create fixtures**

Create `testdata/e2e/lease-expiry/specs/requirements/REQ-001.yaml`:
```yaml
id: REQ-001
title: lease-expiry scenario
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
```

Create `testdata/e2e/lease-expiry/specs/tasks/TASK-001.yaml`:
```yaml
id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
```

- [ ] **Step 2: Append the test**

Append to `internal/integration/e2e_test.go`:

```go
func TestE2E_LeaseExpiry(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "lease-expiry")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Claim with a very short TTL (1s).
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "agent-A", "--ttl", "1s")
	firstClaim := env["data"].(map[string]any)["claim_id"].(string)
	_ = firstClaim

	// Immediately attempting a second claim must fail — lease still live.
	_, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "agent-B", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2 on contested claim, got %d", code)
	}

	// Wait for the lease to expire. Add a margin for OS timer resolution.
	time.Sleep(1500 * time.Millisecond)

	// Second claim should now succeed — inline rule-1 cleanup releases the
	// expired claim and reverts the task to open before the new CAS.
	env = runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "agent-B", "--ttl", "30m")
	secondClaim := env["data"].(map[string]any)["claim_id"].(string)
	if secondClaim == "" {
		t.Fatal("second claim did not return a claim_id")
	}

	// Events should show the expired-lease cleanup: claim_released (expired)
	// and a task_status_changed entry with reason=lease_expired.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	sawExpired := false
	sawLeaseExpiredTransition := false
	for _, raw := range evs {
		e := raw.(map[string]any)
		kind, _ := e["Kind"].(string)
		payloadRaw := e["Payload"]
		var payloadStr string
		switch p := payloadRaw.(type) {
		case string:
			payloadStr = p
		case []byte:
			payloadStr = string(p)
		}
		if kind == "claim_released" && strings.Contains(payloadStr, `"reason":"expired"`) {
			sawExpired = true
		}
		if kind == "task_status_changed" && strings.Contains(payloadStr, `"reason":"lease_expired"`) {
			sawLeaseExpiredTransition = true
		}
	}
	if !sawExpired {
		t.Error("expected claim_released{reason:expired} event, not found")
	}
	if !sawLeaseExpiredTransition {
		t.Error("expected task_status_changed{reason:lease_expired} event, not found")
	}
}
```

Add `"time"` to the imports of `e2e_test.go` if it is not already present.

- [ ] **Step 3: Run the test**

Run: `go test -race -v ./internal/integration/... -run TestE2E_LeaseExpiry`
Expected: PASS. Total run time ~2 seconds due to the 1.5s sleep.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/e2e_test.go testdata/e2e/lease-expiry/
git commit -m "test(e2e): lease-expiry releases stale claim on re-claim"
```

---

## Task 5: E2E test — op_id-idempotency across subprocesses

**Files:**
- Create: `testdata/e2e/op-id-replay/specs/requirements/REQ-001.yaml`
- Create: `testdata/e2e/op-id-replay/specs/tasks/TASK-001.yaml`
- Modify: `internal/integration/e2e_test.go`

- [ ] **Step 1: Create fixtures**

Create `testdata/e2e/op-id-replay/specs/requirements/REQ-001.yaml`:
```yaml
id: REQ-001
title: op-id-replay scenario
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
```

Create `testdata/e2e/op-id-replay/specs/tasks/TASK-001.yaml`:
```yaml
id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
```

- [ ] **Step 2: Append the test**

```go
func TestE2E_OpIDReplay(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "op-id-replay")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0Q"
	args := []string{"--op-id", opID, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m"}

	// First subprocess invocation.
	env := runCairnExit(t, repo, cairnHome, 0, args...)
	firstClaim := env["data"].(map[string]any)["claim_id"].(string)
	firstRun := env["data"].(map[string]any)["run_id"].(string)

	// Second subprocess invocation with the SAME op_id — must return the
	// cached result verbatim.
	env = runCairnExit(t, repo, cairnHome, 0, args...)
	secondClaim := env["data"].(map[string]any)["claim_id"].(string)
	secondRun := env["data"].(map[string]any)["run_id"].(string)
	if firstClaim != secondClaim {
		t.Fatalf("claim_id differs on replay: first=%s second=%s", firstClaim, secondClaim)
	}
	if firstRun != secondRun {
		t.Fatalf("run_id differs on replay: first=%s second=%s", firstRun, secondRun)
	}

	// Event-log must show exactly one claim_acquired — no duplicate side-effect.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	count := 0
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "claim_acquired" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 claim_acquired event, got %d", count)
	}

	// Third invocation with a DIFFERENT op_id + same task should now fail
	// with CodeConflict because the task is already claimed (not because of
	// op_id mismatch).
	env, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
		"--agent", "e2e", "--ttl", "30m")
	if code != 2 {
		t.Fatalf("expected exit 2 on already-claimed, got %d", code)
	}
	expectErrorKind(t, env, "task_not_claimable")

	// Fourth invocation reusing the first op_id but with a DIFFERENT command
	// (task.heartbeat) must error with op_id_kind_mismatch. claim_id must be
	// the one returned in step 1.
	env, code = runCairn(t, repo, cairnHome, "--op-id", opID,
		"task", "heartbeat", firstClaim)
	if code != 2 {
		t.Fatalf("expected exit 2 on op_id_kind_mismatch, got %d env=%+v", code, env)
	}
	expectErrorKind(t, env, "op_id_kind_mismatch")
}
```

- [ ] **Step 3: Run the test**

Run: `go test -race -v ./internal/integration/... -run TestE2E_OpIDReplay`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/e2e_test.go testdata/e2e/op-id-replay/
git commit -m "test(e2e): op_id replay returns cached result; kind mismatch rejected"
```

---

## Task 6: E2E test — evidence-hash-mismatch

**Files:**
- Create: `testdata/e2e/evidence-hash-mismatch/specs/requirements/REQ-001.yaml`
- Create: `testdata/e2e/evidence-hash-mismatch/specs/tasks/TASK-001.yaml`
- Modify: `internal/integration/e2e_test.go`

- [ ] **Step 1: Create fixtures**

Create `testdata/e2e/evidence-hash-mismatch/specs/requirements/REQ-001.yaml`:
```yaml
id: REQ-001
title: evidence-hash-mismatch scenario
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
```

Create `testdata/e2e/evidence-hash-mismatch/specs/tasks/TASK-001.yaml`:
```yaml
id: TASK-001
implements: [REQ-001]
required_gates: [AC-001]
```

- [ ] **Step 2: Append the test**

```go
func TestE2E_EvidenceHashMismatch(t *testing.T) {
	repo := mustEmptyRepo(t)
	copyFixture(t, repo, "evidence-hash-mismatch")
	cairnHome := t.TempDir()

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Put legitimate evidence.
	out := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(out, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := runCairnExit(t, repo, cairnHome, 0, "evidence", "put", out)
	sha := env["data"].(map[string]any)["sha256"].(string)

	// Corrupt the blob on disk directly. Find the blob under
	// <cairnHome>/<repoId>/blobs/<sha[:2]>/<sha> and overwrite.
	// We don't know the repoId, so walk cairnHome.
	var blobPath string
	err := filepath.WalkDir(cairnHome, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == sha {
			blobPath = path
		}
		return nil
	})
	if err != nil || blobPath == "" {
		t.Fatalf("could not locate blob for sha=%s under %s: %v", sha, cairnHome, err)
	}
	if err := os.WriteFile(blobPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("could not tamper blob: %v", err)
	}

	// Evidence verify should now fail with CodeSubstrate + error.code
	// "hash_mismatch". Exit 4.
	env, code := runCairn(t, repo, cairnHome, "evidence", "verify", sha)
	if code != 4 {
		t.Fatalf("expected exit 4, got %d; env=%+v", code, env)
	}
	expectErrorKind(t, env, "hash_mismatch")

	// An evidence_invalidated event must now be present.
	env = runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs := env["data"].(map[string]any)["events"].([]any)
	sawInvalidated := false
	for _, raw := range evs {
		if raw.(map[string]any)["Kind"] == "evidence_invalidated" {
			sawInvalidated = true
		}
	}
	if !sawInvalidated {
		t.Error("expected evidence_invalidated event after tamper, not found")
	}
}
```

Add `"io/fs"` to the imports of `e2e_test.go` if not already present.

- [ ] **Step 3: Run the test**

Run: `go test -race -v ./internal/integration/... -run TestE2E_EvidenceHashMismatch`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/e2e_test.go testdata/e2e/evidence-hash-mismatch/
git commit -m "test(e2e): evidence-hash-mismatch fails verify with exit 4"
```

---

## Task 7: Full-suite green + summary commit

**Files:**
- No new files. Verification only.

- [ ] **Step 1: Run every e2e test together**

Run:
```bash
go test -race -v ./internal/integration/... -run TestE2E_
```
Expected: 5 tests PASS. Total wall time under 10 seconds (lease-expiry sleeps 1.5s; the rest run in subseconds).

- [ ] **Step 2: Run the full repo suite**

Run:
```bash
go test -race ./...
```
Expected: every package green, including `internal/integration` with both the existing dogfood/concurrent/replay tests AND the new e2e tests.

- [ ] **Step 3: Verify nothing leaked into source files**

Run:
```bash
git status --short
```
Expected: nothing outside `internal/integration/e2e*.go` and `testdata/e2e/` from this plan. No stray source modifications.

- [ ] **Step 4: If all green, Phase 15 is complete**

No final commit needed — each task already committed its own slice. The branch now has 5 new green e2e tests on top of Ship 1's existing test suite.

---

## Plan Self-Review

### Spec coverage

The user asked for five scenarios:

| Scenario | Task |
|---|---|
| spec-edit-stale | Task 2 |
| dep-blocking | Task 3 |
| lease-expiry | Task 4 |
| op_id-idempotency-across-subprocess | Task 5 |
| evidence-hash-mismatch | Task 6 |

All five have a dedicated task. Task 1 is the shared helpers + the first fixture. Task 7 is the final verification pass. Seven tasks total.

### Placeholder scan

None. Every task has complete code. No "TBD" / "TODO" / "fill in" anywhere.

### Type consistency

All five tests use:
- `runCairnExit(t, dir, cairnHome, expectedExit int, args ...)` — defined in Task 1.
- `mustEmptyRepo(t) string` — defined in Task 2.
- `copyFixture(t, dst, fixtureName)` — defined in Task 1.
- `expectErrorKind(t, env, kind)` — defined in Task 1.
- Existing `runCairn(t, dir, cairnHome, args...) (map[string]any, int)` from `dogfood_test.go`.

Names are consistent. The `stringsContainsAll` helper defined in Task 1 is declared for future use but not invoked by these five tests — acceptable since it's tiny.

### Scope

Single coherent sub-project: end-to-end regression tests. Not decomposable.

### One gap worth flagging

The existing `TestShip1Dogfood_SpecEditFlipsStale` in `dogfood_test.go` overlaps Task 2's `TestE2E_SpecEditStale`. The existing `TestReplay_OpIDReturnsCachedResult` in `replay_test.go` overlaps Task 5's `TestE2E_OpIDReplay`. These overlaps are acceptable — the e2e suite is explicitly about collecting regression scenarios under one naming convention (`TestE2E_*`) with fixture-based specs. Removing the overlap would require consolidating tests, which is out of scope for this plan.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-17-phase-15-e2e-tests.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Uses `superpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
