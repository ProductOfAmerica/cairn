package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// runCLIInDir runs the cairn binary from dir with the given args, using a
// fresh CAIRN_HOME for each call, and returns the combined stdout+stderr as a
// string. Failures from the binary (non-zero exits) are not fatal here;
// callers may inspect the envelope's error field.
func runCLIInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(cairnBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+home)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	_ = cmd.Run() // non-zero exits are not fatal; callers check the envelope
	combined := bytes.TrimSpace(out.Bytes())
	if len(combined) == 0 {
		// return stderr so tests can fail with useful output
		return errb.String()
	}
	return string(combined)
}

// parseEnvelope unmarshals the JSON envelope string returned by runCLIInDir.
func parseEnvelope(t *testing.T, out string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parseEnvelope: invalid JSON: %s\n(err=%v)", out, err)
	}
	return env
}

// repoRoot walks up from the integration test directory to find the repo root
// (the directory containing go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repoRoot: runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repoRoot: go.mod not found")
		}
		dir = parent
	}
}

func TestSpecValidateEnvelopeEmpty(t *testing.T) {
	root := t.TempDir()
	specsRoot := filepath.Join(root, "specs")
	if err := os.MkdirAll(specsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// no requirements/, no tasks/

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)

	if env["error"] != nil {
		t.Fatalf("expected no error envelope, got: %v", env["error"])
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong shape: %v", env["data"])
	}
	errs, ok := data["errors"].([]any)
	if !ok {
		t.Fatalf("errors missing or wrong shape: %v", data["errors"])
	}
	if len(errs) != 0 {
		t.Fatalf("errors should be empty, got: %v", errs)
	}
	scanned, ok := data["specs_scanned"].(map[string]any)
	if !ok {
		t.Fatalf("specs_scanned missing: %v", data["specs_scanned"])
	}
	if scanned["requirements"] != float64(0) {
		t.Errorf("requirements: want 0, got %v", scanned["requirements"])
	}
	if scanned["tasks"] != float64(0) {
		t.Errorf("tasks: want 0, got %v", scanned["tasks"])
	}
}

func TestSpecValidateEnvelopePopulated(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "specs", "requirements")
	taskDir := filepath.Join(root, "specs", "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	for i := 1; i <= 3; i++ {
		_ = os.WriteFile(
			filepath.Join(reqDir, fmt.Sprintf("REQ-00%d.yaml", i)),
			[]byte(fmt.Sprintf(`id: REQ-00%d
title: x
gates:
  - id: AC-%d
    kind: test
    producer: {kind: executable}
`, i, i)), 0o644)
	}
	for i := 1; i <= 5; i++ {
		_ = os.WriteFile(
			filepath.Join(taskDir, fmt.Sprintf("TASK-00%d.yaml", i)),
			[]byte(fmt.Sprintf("id: TASK-00%d\nimplements: [REQ-001]\nrequired_gates: [AC-1]\n", i)), 0o644)
	}

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	scanned := data["specs_scanned"].(map[string]any)
	if scanned["requirements"] != float64(3) {
		t.Errorf("requirements: want 3, got %v", scanned["requirements"])
	}
	if scanned["tasks"] != float64(5) {
		t.Errorf("tasks: want 5, got %v", scanned["tasks"])
	}
}

func TestSpecValidateEnvelopeMixedValidInvalid(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "specs", "requirements")
	_ = os.MkdirAll(reqDir, 0o755)

	// Two valid requirements + one with a schema error (missing id).
	for _, name := range []string{"REQ-001.yaml", "REQ-002.yaml"} {
		_ = os.WriteFile(filepath.Join(reqDir, name),
			[]byte(`id: REQ-OK
title: ok
gates:
  - id: AC-1
    kind: test
    producer: {kind: executable}
`), 0o644)
	}
	// Bad file: missing id.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-BAD.yaml"),
		[]byte("title: missing-id\ngates:\n  - id: AC-X\n    kind: test\n    producer: {kind: executable}\n"),
		0o644)

	out := runCLIInDir(t, root, "spec", "validate", "--path", "specs")
	env := parseEnvelope(t, out)
	data := env["data"].(map[string]any)
	errs := data["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected at least one error, got none")
	}
	scanned := data["specs_scanned"].(map[string]any)
	// All three loaded — counts attempts, not passes.
	if scanned["requirements"] != float64(3) {
		t.Errorf("requirements scanned: want 3, got %v", scanned["requirements"])
	}
}
