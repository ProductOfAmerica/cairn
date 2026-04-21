package hook_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli/hook"
)

func TestEnable_UserScopeWritesSettings(t *testing.T) {
	claudeHome := t.TempDir()
	res, err := hook.Enable(hook.ScopeUser, "", claudeHome)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if res.Scope != "user" {
		t.Errorf("scope: %q", res.Scope)
	}
	if !res.Enabled {
		t.Errorf("Enabled=false unexpected")
	}
	if res.AlreadyEnabled {
		t.Errorf("AlreadyEnabled should be false on first install")
	}
	path := filepath.Join(claudeHome, "settings.json")
	if res.SettingsPath != path {
		t.Errorf("SettingsPath=%q want %q", res.SettingsPath, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), hook.CairnHookDriftCommand) {
		t.Errorf("drift command not in settings.json: %s", body)
	}
}

func TestEnable_Idempotent(t *testing.T) {
	claudeHome := t.TempDir()
	first, err := hook.Enable(hook.ScopeUser, "", claudeHome)
	if err != nil {
		t.Fatal(err)
	}
	if first.AlreadyEnabled {
		t.Errorf("first run: AlreadyEnabled=true unexpected")
	}

	second, err := hook.Enable(hook.ScopeUser, "", claudeHome)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyEnabled {
		t.Errorf("second run: AlreadyEnabled should be true")
	}

	// Byte-identical settings.json.
	path := filepath.Join(claudeHome, "settings.json")
	a, _ := os.ReadFile(path)
	// Run a third time — also byte-identical.
	_, _ = hook.Enable(hook.ScopeUser, "", claudeHome)
	b, _ := os.ReadFile(path)
	if string(a) != string(b) {
		t.Errorf("non-idempotent write\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}

func TestEnable_ProjectScope(t *testing.T) {
	cwd := t.TempDir()
	res, err := hook.Enable(hook.ScopeProject, cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".claude", "settings.json")
	if res.SettingsPath != want {
		t.Errorf("SettingsPath: %q want %q", res.SettingsPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("project settings.json not written: %v", err)
	}
}

func TestEnable_LocalScope(t *testing.T) {
	cwd := t.TempDir()
	res, err := hook.Enable(hook.ScopeLocal, cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".claude", "settings.local.json")
	if res.SettingsPath != want {
		t.Errorf("SettingsPath: %q want %q", res.SettingsPath, want)
	}
}

func TestEnable_CLAUDE_HOMEEnvOverridesDefault(t *testing.T) {
	// Explicit arg wins over env, env wins over default.
	override := t.TempDir()
	t.Setenv("CLAUDE_HOME", "/should/not/be/used")
	res, err := hook.Enable(hook.ScopeUser, "", override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.SettingsPath, override) {
		t.Errorf("explicit claudeHome not honored: %q", res.SettingsPath)
	}
}

func TestEnable_CLAUDE_HOMEEnvUsedWhenNoExplicit(t *testing.T) {
	envHome := t.TempDir()
	t.Setenv("CLAUDE_HOME", envHome)
	res, err := hook.Enable(hook.ScopeUser, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.SettingsPath, envHome) {
		t.Errorf("CLAUDE_HOME env not honored: %q", res.SettingsPath)
	}
}

func TestEnable_MalformedSettingsFails(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte("{malformed"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hook.Enable(hook.ScopeUser, "", home)
	if err == nil {
		t.Fatal("malformed file must error")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) || ce.Kind != "hook_settings_parse_failed" {
		t.Errorf("got %v, want hook_settings_parse_failed", err)
	}
}

func TestEnable_PreservesOperatorEntries(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	initial := `{
  "$schema": "https://schema.example.com/cc.json",
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "/usr/local/bin/my-stop"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.Enable(hook.ScopeUser, "", home); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "/usr/local/bin/my-stop") {
		t.Errorf("operator entry dropped: %s", body)
	}
	if !strings.Contains(string(body), `"$schema"`) {
		t.Errorf("$schema dropped: %s", body)
	}
	if !strings.Contains(string(body), hook.CairnHookDriftCommand) {
		t.Errorf("cairn entry not added: %s", body)
	}
}

func TestDisable_RemovesEntriesFlipsConfig(t *testing.T) {
	home := t.TempDir()
	if _, err := hook.Enable(hook.ScopeUser, "", home); err != nil {
		t.Fatal(err)
	}
	res, err := hook.Disable(hook.ScopeUser, "", home)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Disabled {
		t.Errorf("Disabled=false unexpected")
	}
	if res.EntriesRemoved != 1 {
		t.Errorf("EntriesRemoved=%d want 1", res.EntriesRemoved)
	}
	body, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	if strings.Contains(string(body), hook.CairnHookDriftCommand) {
		t.Errorf("cairn entry not stripped: %s", body)
	}
	if !strings.Contains(string(body), `"enabled": false`) {
		t.Errorf("cairn.enabled=false flag missing: %s", body)
	}
}

func TestDisable_NoopOnFreshScope(t *testing.T) {
	home := t.TempDir()
	res, err := hook.Disable(hook.ScopeUser, "", home)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !res.AlreadyDisabled {
		t.Errorf("AlreadyDisabled should be true on bare scope")
	}
	// Must not create settings.json as a side effect.
	if _, err := os.Stat(filepath.Join(home, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("disable on bare scope created file: %v", err)
	}
}

func TestStatus_AllScopes(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	// Enable user only.
	if _, err := hook.Enable(hook.ScopeUser, cwd, home); err != nil {
		t.Fatal(err)
	}
	res, err := hook.Status(nil, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scopes) != 3 {
		t.Fatalf("want 3 scopes, got %d", len(res.Scopes))
	}
	byName := map[string]hook.ScopeStatus{}
	for _, s := range res.Scopes {
		byName[s.Scope] = s
	}
	u := byName["user"]
	if !u.Exists || !u.CairnBlockPresent || !u.Enabled || u.EntryCount != 1 {
		t.Errorf("user scope status: %+v", u)
	}
	if u.Drift != "" {
		t.Errorf("no drift expected on fresh install: %q", u.Drift)
	}
	p := byName["project"]
	if p.Exists {
		t.Errorf("project scope should not exist: %+v", p)
	}
}

func TestStatus_DriftDetected(t *testing.T) {
	home := t.TempDir()
	// Manually write an entry without the cairn config block — partial install drift.
	path := filepath.Join(home, "settings.json")
	body := `{
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "cairn hook check-drift"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := hook.Status([]hook.Scope{hook.ScopeUser}, "", home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scopes) != 1 {
		t.Fatal("scope count")
	}
	u := res.Scopes[0]
	if u.Drift == "" {
		t.Errorf("expected drift (entry without cairn block): %+v", u)
	}
}

func TestStatus_MalformedReportsDrift(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := hook.Status([]hook.Scope{hook.ScopeUser}, "", home)
	if err != nil {
		t.Fatalf("status should tolerate malformed file: %v", err)
	}
	if !strings.HasPrefix(res.Scopes[0].Drift, "parse:") {
		t.Errorf("drift should report parse error: %q", res.Scopes[0].Drift)
	}
}

func TestParseScope(t *testing.T) {
	for _, valid := range []string{"user", "project", "local"} {
		if _, err := hook.ParseScope(valid); err != nil {
			t.Errorf("valid %q rejected: %v", valid, err)
		}
	}
	if _, err := hook.ParseScope("managed"); err == nil {
		t.Error("managed scope should be rejected (CC policy scope, not cairn's territory)")
	}
	if _, err := hook.ParseScope(""); err == nil {
		t.Error("empty scope should be rejected")
	}
}

func TestSettingsPath_MissingCWD(t *testing.T) {
	_, err := hook.SettingsPath(hook.ScopeProject, "", "")
	if err == nil {
		t.Error("project scope without cwd must error")
	}
	_, err = hook.SettingsPath(hook.ScopeLocal, "", "")
	if err == nil {
		t.Error("local scope without cwd must error")
	}
}
