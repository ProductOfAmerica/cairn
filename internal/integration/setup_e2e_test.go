package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupE2E exercises "cairn setup": state bootstrap + embedded
// skill files land under <cwd>/.claude/skills/ so Claude Code
// auto-discovers them without /plugin install.
func TestSetupE2E(t *testing.T) {
	repo := mustEmptyRepo(t)
	cairnHome := t.TempDir()

	// Run 1: fresh project — expect skills to be written.
	env, stderr := runSetup(t, repo, cairnHome)
	if env["kind"] != "setup" {
		t.Fatalf("kind=%v want setup", env["kind"])
	}
	data, _ := env["data"].(map[string]any)
	for _, field := range []string{"repo_id", "state_dir", "db_path", "skills"} {
		if _, ok := data[field]; !ok {
			t.Errorf("data missing %q: %+v", field, data)
		}
	}
	skills, _ := data["skills"].(map[string]any)
	written, _ := skills["written"].([]any)
	if len(written) == 0 {
		t.Fatalf("expected written skill files on first setup, got 0")
	}
	skipped, _ := skills["skipped"].([]any)
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped files on first setup, got %d", len(skipped))
	}

	// Core skill files land at the expected paths.
	for _, rel := range []string{
		".claude/skills/using-cairn/SKILL.md",
		".claude/skills/using-cairn/yaml-authoring.md",
		".claude/skills/using-cairn/hash-placeholders.md",
		".claude/skills/using-cairn/source-hash-format.md",
		".claude/skills/using-cairn/code-reviewer-pattern.md",
		".claude/skills/subagent-driven-development-with-verdicts/SKILL.md",
		".claude/skills/verdict-backed-verification/SKILL.md",
	} {
		full := filepath.Join(repo, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected skill file at %s: %v", rel, err)
		}
	}

	// Stderr guidance mentions the install.
	for _, frag := range []string{
		"cairn state initialized",
		".claude/skills",
		"using-cairn",
		"subagent-driven-development-with-verdicts",
		"verdict-backed-verification",
	} {
		if !strings.Contains(stderr, frag) {
			t.Errorf("stderr missing %q\n---\n%s\n---", frag, stderr)
		}
	}

	// Run 2: re-setup without --force — everything skipped, nothing written.
	env2, stderr2 := runSetup(t, repo, cairnHome)
	skills2, _ := env2["data"].(map[string]any)["skills"].(map[string]any)
	written2, _ := skills2["written"].([]any)
	skipped2, _ := skills2["skipped"].([]any)
	if len(written2) != 0 {
		t.Errorf("re-run should write nothing, wrote %d", len(written2))
	}
	if len(skipped2) == 0 {
		t.Errorf("re-run should skip all, skipped %d", len(skipped2))
	}
	if !strings.Contains(stderr2, "already present") {
		t.Errorf("re-run stderr missing 'already present' hint:\n%s", stderr2)
	}

	// Run 3: --force overwrites.
	env3, _ := runSetup(t, repo, cairnHome, "--force")
	skills3, _ := env3["data"].(map[string]any)["skills"].(map[string]any)
	written3, _ := skills3["written"].([]any)
	if len(written3) == 0 {
		t.Errorf("--force run should write files, wrote 0")
	}
}

// runSetup runs "cairn setup" in dir with CAIRN_HOME set, returns the
// parsed stdout envelope and the captured stderr.
func runSetup(t *testing.T, dir, cairnHome string, extraArgs ...string) (map[string]any, string) {
	t.Helper()
	args := append([]string{"setup"}, extraArgs...)
	cmd := exec.Command(cairnBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("cairn %v: %v\nstderr: %s", args, err, stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &env); err != nil {
		t.Fatalf("stdout not valid JSON: %s\n(err=%v)", stdout.String(), err)
	}
	return env, stderr.String()
}
