package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSetupE2E exercises the "cairn setup" command end-to-end: JSON
// envelope on stdout, human-readable hint block on stderr, state DB
// created idempotently.
func TestSetupE2E(t *testing.T) {
	repo := mustEmptyRepo(t)
	cairnHome := t.TempDir()

	cmd := exec.Command(cairnBinary, "setup")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("cairn setup: %v\nstderr: %s", err, stderr.String())
	}

	// stdout: single-line JSON envelope, kind=setup, data has repo_id /
	// state_dir / db_path.
	var env map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &env); err != nil {
		t.Fatalf("stdout not valid JSON: %s\n(err=%v)", stdout.String(), err)
	}
	if env["kind"] != "setup" {
		t.Fatalf("kind=%v want setup", env["kind"])
	}
	data, _ := env["data"].(map[string]any)
	for _, field := range []string{"repo_id", "state_dir", "db_path"} {
		if _, ok := data[field]; !ok {
			t.Errorf("data missing %q: %+v", field, data)
		}
	}

	// stderr: harness-install guidance. Spot-check key fragments so
	// minor wording tweaks don't force test churn, but drift from the
	// intended shape is caught.
	errText := stderr.String()
	for _, frag := range []string{
		"cairn state initialized",
		"Claude Code",
		"/plugin",
		"github.com/ProductOfAmerica/cairn",
		"Other harnesses",
		"cairn spec validate",
	} {
		if !strings.Contains(errText, frag) {
			t.Errorf("stderr missing %q\n---\n%s\n---", frag, errText)
		}
	}

	// Idempotency: second run leaves DB intact and still prints hints.
	cmd2 := exec.Command(cairnBinary, "setup")
	cmd2.Dir = repo
	cmd2.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
	var stderr2 bytes.Buffer
	cmd2.Stderr = &stderr2
	if err := cmd2.Run(); err != nil {
		t.Fatalf("second cairn setup: %v\nstderr: %s", err, stderr2.String())
	}
	if !strings.Contains(stderr2.String(), "cairn state initialized") {
		t.Errorf("second run did not print hints:\n%s", stderr2.String())
	}
}
