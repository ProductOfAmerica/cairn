package integration_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBootstrapE2E simulates the §6.1 Ship 3 bootstrap on a fresh repo:
// hand-author REQ + TASK YAML, init the cairn state DB, plan, claim, evidence,
// verdict, complete. Exercises every cairn surface used by Phase 0 + the
// REQ-002 dogfood loop in a single test. Does NOT require the new skills —
// this is a CLI-only smoke test of the bootstrap sequence.
func TestBootstrapE2E(t *testing.T) {
	root := mustEmptyRepo(t)
	cairnHome := t.TempDir()
	specsRoot := filepath.Join(root, "specs")
	reqDir := filepath.Join(specsRoot, "requirements")
	taskDir := filepath.Join(specsRoot, "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	// 1. Hand-author REQ + TASK.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-BOOT.yaml"),
		[]byte(`id: REQ-BOOT
title: bootstrap smoke
why: bootstrap smoke
scope_in: []
scope_out: []
gates:
  - id: AC-BOOT
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
`), 0o644)
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-BOOT.yaml"),
		[]byte(`id: TASK-BOOT
implements: [REQ-BOOT]
depends_on: []
required_gates: [AC-BOOT]
`), 0o644)

	// 2. cairn init (creates state DB).
	runCairnExit(t, root, cairnHome, 0, "init")

	// 3. cairn spec validate — passes.
	env := runCairnExit(t, root, cairnHome, 0, "spec", "validate", "--path", "specs")
	data := env["data"].(map[string]any)
	if errs := data["errors"].([]any); len(errs) != 0 {
		t.Fatalf("validate failed: %v", errs)
	}

	// 4. cairn task plan — materializes.
	runCairnExit(t, root, cairnHome, 0, "task", "plan")

	// 5. cairn task claim TASK-BOOT.
	env = runCairnExit(t, root, cairnHome, 0, "task", "claim", "TASK-BOOT", "--agent", "test-bootstrap", "--ttl", "10m")
	claimID := env["data"].(map[string]any)["claim_id"].(string)
	runID := env["data"].(map[string]any)["run_id"].(string)

	// 6. Capture gate output.
	gateOut := filepath.Join(root, "gate-out.txt")
	_ = os.WriteFile(gateOut, []byte("ok\n"), 0o644)

	// 7. cairn evidence put.
	runCairnExit(t, root, cairnHome, 0, "evidence", "put", gateOut)

	// 8. cairn verdict report (placeholder hashes).
	runCairnExit(t, root, cairnHome, 0, "verdict", "report",
		"--gate", "AC-BOOT",
		"--run", runID,
		"--status", "pass",
		"--evidence", gateOut,
		"--producer-hash", "0000000000000000000000000000000000000000000000000000000000000001",
		"--inputs-hash", "0000000000000000000000000000000000000000000000000000000000000002",
	)

	// 9. cairn task complete.
	runCairnExit(t, root, cairnHome, 0, "task", "complete", claimID)

	// 10. Verify task done.
	doneEnv := runCairnExit(t, root, cairnHome, 0, "task", "list", "--status", "done")
	doneTasks, _ := doneEnv["data"].(map[string]any)["tasks"].([]any)
	if !containsTaskID(doneTasks, "TASK-BOOT") {
		t.Fatalf("task done list missing TASK-BOOT: %+v", doneTasks)
	}
}
