package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

// fakeGlobalPluginCache creates the canonical Claude Code plugin cache
// layout for cairn at <claudeHome>/plugins/cache/cairn/cairn/<version>/
// .claude-plugin/plugin.json. See
// https://code.claude.com/docs/en/plugins-reference for the layout.
func fakeGlobalPluginCache(t *testing.T, claudeHome string) {
	t.Helper()
	dir := filepath.Join(claudeHome, "plugins", "cache", "cairn", "cairn", "0.3.0", ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "plugin.json"),
		[]byte(`{"name":"cairn","version":"0.3.0"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestInstallSkills_NoGlobal_InstallsNormally(t *testing.T) {
	claudeHome := t.TempDir() // empty cache — no global install.
	cwd := t.TempDir()

	res, err := cli.InstallSkills(cwd, false, claudeHome)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(res.Written) == 0 {
		t.Errorf("no global: want Written > 0, got 0")
	}
	if res.SkippedReason != "" {
		t.Errorf("no global: SkippedReason should be empty, got %q", res.SkippedReason)
	}
	if len(res.LocalShadow) != 0 {
		t.Errorf("no global: LocalShadow should be empty, got %v", res.LocalShadow)
	}
	// At least the using-cairn SKILL.md should exist on disk.
	if _, err := os.Stat(filepath.Join(cwd, ".claude", "skills", "using-cairn", "SKILL.md")); err != nil {
		t.Errorf("no global: expected using-cairn/SKILL.md on disk, got %v", err)
	}
}

func TestInstallSkills_GlobalPluginDetected_Skips(t *testing.T) {
	claudeHome := t.TempDir()
	cwd := t.TempDir()
	fakeGlobalPluginCache(t, claudeHome)

	res, err := cli.InstallSkills(cwd, false, claudeHome)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(res.Written) != 0 {
		t.Errorf("global detected: Written should be empty, got %v", res.Written)
	}
	if len(res.Skipped) == 0 {
		t.Errorf("global detected: Skipped should enumerate embedded files, got 0")
	}
	if res.SkippedReason != "global_plugin_detected" {
		t.Errorf("SkippedReason: got %q, want global_plugin_detected", res.SkippedReason)
	}
	// No files should be written to disk.
	if _, err := os.Stat(filepath.Join(cwd, ".claude", "skills", "using-cairn", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("global detected: expected no skill files on disk, stat err=%v", err)
	}
}

func TestInstallSkills_GlobalPluginDetected_ForceOverrides(t *testing.T) {
	claudeHome := t.TempDir()
	cwd := t.TempDir()
	fakeGlobalPluginCache(t, claudeHome)

	res, err := cli.InstallSkills(cwd, true, claudeHome)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(res.Written) == 0 {
		t.Errorf("--force: want Written > 0 despite global install, got 0")
	}
	if res.SkippedReason != "" {
		t.Errorf("--force: SkippedReason should be empty, got %q", res.SkippedReason)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".claude", "skills", "using-cairn", "SKILL.md")); err != nil {
		t.Errorf("--force: expected using-cairn/SKILL.md on disk, got %v", err)
	}
}

func TestInstallSkills_LocalShadowReported(t *testing.T) {
	claudeHome := t.TempDir()
	cwd := t.TempDir()
	fakeGlobalPluginCache(t, claudeHome)

	// Pre-create a redundant local shadow at the exact embedded path.
	shadowDir := filepath.Join(cwd, ".claude", "skills", "using-cairn")
	if err := os.MkdirAll(shadowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadowDir, "SKILL.md"), []byte("stale copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := cli.InstallSkills(cwd, false, claudeHome)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if res.SkippedReason != "global_plugin_detected" {
		t.Fatalf("prereq: SkippedReason should be global_plugin_detected, got %q", res.SkippedReason)
	}
	if !slices.Contains(res.LocalShadow, "using-cairn/SKILL.md") {
		t.Errorf("LocalShadow should contain using-cairn/SKILL.md, got %v", res.LocalShadow)
	}
	// The shadow file itself must not have been touched.
	body, err := os.ReadFile(filepath.Join(shadowDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read shadow: %v", err)
	}
	if string(body) != "stale copy" {
		t.Errorf("shadow file was modified: got %q want 'stale copy'", string(body))
	}
}
