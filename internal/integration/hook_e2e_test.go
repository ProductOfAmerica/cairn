package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookE2E exercises the full `cairn hook` surface in one test:
// enable → status → check-drift (clean) → mutate prose → check-drift
// (drift) → disable → status → operator-entry preservation. One test
// keeps tmpdir churn down and the narrative readable.
func TestHookE2E(t *testing.T) {
	repo := mustEmptyRepo(t)
	cairnHome := t.TempDir()
	claudeHome := t.TempDir()

	// Seed a cairn-derived YAML under specs/ with its prose counterpart
	// under docs/superpowers/specs. Computed hash is pinned in the YAML
	// header so CheckDrift has something to verify.
	proseRel := "docs/superpowers/specs/2026-04-21-design.md"
	prosePath := filepath.Join(repo, filepath.FromSlash(proseRel))
	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		t.Fatal(err)
	}
	proseBody := []byte("design v1\n")
	if err := os.WriteFile(prosePath, proseBody, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(proseBody)
	hashHex := hex.EncodeToString(sum[:])

	yamlPath := filepath.Join(repo, "specs", "requirements", "REQ-001.yaml")
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	header := "# cairn-derived: source-hash=" + hashHex +
		" source-path=" + proseRel +
		" derived-at=2026-04-21T09:14:07Z\n"
	yamlBody := header + "requirement:\n  id: REQ-001\n"
	if err := os.WriteFile(yamlPath, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 1: cairn hook enable --scope project.
	env := runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "enable", "--scope", "project")
	expectEnvelopeKind(t, env, "hook.enable")
	data, _ := env["data"].(map[string]any)
	if data["scope"] != "project" {
		t.Errorf("scope: %v", data["scope"])
	}
	if enabled, _ := data["enabled"].(bool); !enabled {
		t.Errorf("enabled=false unexpected")
	}
	settingsPath, _ := data["settings_path"].(string)
	if settingsPath == "" {
		t.Errorf("settings_path missing: %+v", data)
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("settings.json not written: %v", err)
	}
	body, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(body), "cairn hook check-drift") {
		t.Errorf("settings.json missing drift command:\n%s", body)
	}

	// Step 2: cairn hook status --scope project → enabled, 1 entry.
	env = runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "status", "--scope", "project")
	expectEnvelopeKind(t, env, "hook.status")
	scopes, _ := env["data"].(map[string]any)["scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("want 1 scope, got %d", len(scopes))
	}
	s0, _ := scopes[0].(map[string]any)
	if s0["scope"] != "project" {
		t.Errorf("scope: %v", s0["scope"])
	}
	if enabled, _ := s0["enabled"].(bool); !enabled {
		t.Errorf("status enabled=false after enable: %+v", s0)
	}
	if count, _ := s0["entry_count"].(float64); count != 1 {
		t.Errorf("entry_count=%v want 1", count)
	}

	// Step 3: simulate CC Stop hook — pipe JSON to `cairn hook check-drift`.
	// Hash matches → exit 0, Clean=true.
	stopInput := fmt.Sprintf(`{"session_id":"s1","cwd":%q,"hook_event_name":"Stop","stop_hook_active":false}`, repo)
	env = runHookCmd(t, repo, cairnHome, claudeHome, []byte(stopInput), 0,
		"hook", "check-drift")
	expectEnvelopeKind(t, env, "hook.check_drift")
	d, _ := env["data"].(map[string]any)
	if clean, _ := d["clean"].(bool); !clean {
		t.Errorf("want clean=true, got: %+v", d)
	}
	if checked, _ := d["checked"].(float64); checked != 1 {
		t.Errorf("checked=%v want 1", checked)
	}

	// Step 4: mutate prose → drift → exit 1.
	if err := os.WriteFile(prosePath, []byte("design v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, code := runHookCmdNoExit(t, repo, cairnHome, claudeHome, []byte(stopInput),
		"hook", "check-drift")
	if code != 1 {
		t.Errorf("drift run exit=%d want 1; env=%+v", code, env)
	}
	d, _ = env["data"].(map[string]any)
	if clean, _ := d["clean"].(bool); clean {
		t.Errorf("want clean=false after prose mutation")
	}
	warnings, _ := d["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("want 1 warning, got %d: %+v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].(string), "drift:") {
		t.Errorf("warning should mention drift: %v", warnings[0])
	}

	// Step 5: cairn hook disable → entries stripped, flag flipped.
	env = runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "disable", "--scope", "project")
	expectEnvelopeKind(t, env, "hook.disable")
	d, _ = env["data"].(map[string]any)
	if disabled, _ := d["disabled"].(bool); !disabled {
		t.Errorf("Disabled=false unexpected")
	}
	body, _ = os.ReadFile(settingsPath)
	if strings.Contains(string(body), "cairn hook check-drift") {
		t.Errorf("drift command not stripped:\n%s", body)
	}
	if !strings.Contains(string(body), `"enabled": false`) {
		t.Errorf("cairn.enabled not flipped to false:\n%s", body)
	}

	// Step 6: status reflects disabled.
	env = runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "status", "--scope", "project")
	scopes, _ = env["data"].(map[string]any)["scopes"].([]any)
	s0, _ = scopes[0].(map[string]any)
	if enabled, _ := s0["enabled"].(bool); enabled {
		t.Errorf("status still enabled=true after disable: %+v", s0)
	}
	if count, _ := s0["entry_count"].(float64); count != 0 {
		t.Errorf("entry_count=%v want 0 after disable", count)
	}

	// Step 7: operator-entry preservation. Pre-populate a non-cairn
	// Stop entry, enable cairn, confirm both survive.
	operatorSettings := fmt.Sprintf(`{
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "%s"}]}
    ]
  }
}`, filepath.ToSlash(filepath.Join(t.TempDir(), "operator-script.sh")))
	if err := os.WriteFile(settingsPath, []byte(operatorSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	env = runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "enable", "--scope", "project")
	expectEnvelopeKind(t, env, "hook.enable")
	body, _ = os.ReadFile(settingsPath)
	if !strings.Contains(string(body), "operator-script.sh") {
		t.Errorf("operator entry swept:\n%s", body)
	}
	if !strings.Contains(string(body), "cairn hook check-drift") {
		t.Errorf("cairn entry not added:\n%s", body)
	}

	// Disable sweeps only cairn; operator survives.
	env = runHookCmd(t, repo, cairnHome, claudeHome, nil, 0,
		"hook", "disable", "--scope", "project")
	body, _ = os.ReadFile(settingsPath)
	if !strings.Contains(string(body), "operator-script.sh") {
		t.Errorf("disable swept operator entry:\n%s", body)
	}
	if strings.Contains(string(body), "cairn hook check-drift") {
		t.Errorf("disable did not strip cairn entry:\n%s", body)
	}
}

// runHookCmd invokes the cairn binary with optional stdin bytes and
// asserts the exit code. Sets CAIRN_HOME + CLAUDE_HOME so tests are
// hermetic (never touching the developer's real ~/.claude).
func runHookCmd(t *testing.T, dir, cairnHome, claudeHome string, stdin []byte, expectedExit int, args ...string) map[string]any {
	t.Helper()
	env, code := runHookCmdNoExit(t, dir, cairnHome, claudeHome, stdin, args...)
	if code != expectedExit {
		t.Fatalf("cairn %v: exit=%d want %d\nenv=%+v", args, code, expectedExit, env)
	}
	return env
}

func runHookCmdNoExit(t *testing.T, dir, cairnHome, claudeHome string, stdin []byte, args ...string) (map[string]any, int) {
	t.Helper()
	cmd := exec.Command(cairnBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CAIRN_HOME="+cairnHome,
		"CLAUDE_HOME="+claudeHome,
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("cairn %v: exec: %v\nstderr: %s", args, err, errb.String())
	}
	stripped := bytes.TrimSpace(out.Bytes())
	if len(stripped) == 0 {
		return nil, code
	}
	var envMap map[string]any
	if err := json.Unmarshal(stripped, &envMap); err != nil {
		t.Fatalf("cairn %v: stdout not JSON: %s\n(err=%v, stderr=%s)", args, out.String(), err, errb.String())
	}
	return envMap, code
}

// TestHookCheckDrift_NonCairnRepoSkipsSilently covers the "operator
// installed hooks at user scope but opened CC in a non-cairn repo"
// case: the hook should emit an envelope with skipped=true and exit 0.
func TestHookCheckDrift_NonCairnRepoSkipsSilently(t *testing.T) {
	repo := t.TempDir() // bare tmpdir — no specs/, not a git repo
	cairnHome := t.TempDir()
	claudeHome := t.TempDir()
	stdin := fmt.Sprintf(`{"cwd":%q,"hook_event_name":"Stop"}`, repo)

	env := runHookCmd(t, repo, cairnHome, claudeHome, []byte(stdin), 0,
		"hook", "check-drift")
	d, _ := env["data"].(map[string]any)
	if skipped, _ := d["skipped"].(bool); !skipped {
		t.Errorf("want skipped=true for non-cairn repo: %+v", d)
	}
}
