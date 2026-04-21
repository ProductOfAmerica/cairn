package hook_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli/hook"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadSettings_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	s, err := hook.LoadSettings(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("absent file must not error: %v", err)
	}
	if _, present := s.Cairn(); present {
		t.Errorf("cairn block should be absent on a fresh file")
	}
	if s.CairnEntryCount() != 0 {
		t.Errorf("entry count: %d want 0", s.CairnEntryCount())
	}
}

func TestLoadSettings_EmptyFile(t *testing.T) {
	path := writeSettings(t, "")
	if _, err := hook.LoadSettings(path); err != nil {
		t.Fatalf("empty file should not error: %v", err)
	}
}

func TestLoadSettings_Malformed(t *testing.T) {
	path := writeSettings(t, "{invalid json")
	_, err := hook.LoadSettings(path)
	if err == nil {
		t.Fatal("malformed json must error")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("want *cairnerr.Err, got %T: %v", err, err)
	}
	if ce.Kind != "hook_settings_parse_failed" {
		t.Errorf("kind: %q", ce.Kind)
	}
}

func TestLoadSettings_MalformedHooksBlock(t *testing.T) {
	// hooks must be an object; passing a string triggers type mismatch.
	path := writeSettings(t, `{"hooks": "not an object"}`)
	_, err := hook.LoadSettings(path)
	if err == nil {
		t.Fatal("malformed hooks block must error")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) || ce.Kind != "hook_settings_parse_failed" {
		t.Errorf("got %v, want hook_settings_parse_failed", err)
	}
}

func TestLoadSettings_CairnVersionMismatch(t *testing.T) {
	path := writeSettings(t, `{"cairn": {"version": 99, "enabled": true}}`)
	_, err := hook.LoadSettings(path)
	if err == nil {
		t.Fatal("unknown cairn.version must error")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) || ce.Kind != "hook_settings_parse_failed" {
		t.Errorf("got %v, want hook_settings_parse_failed", err)
	}
	if !strings.Contains(ce.Message, "version 99") {
		t.Errorf("message should name the unknown version: %q", ce.Message)
	}
}

func TestAddCairnHook_FreshFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	body := readBack(t, path)
	if !strings.Contains(body, `"cairn hook check-drift"`) {
		t.Errorf("saved body missing cairn command: %s", body)
	}
	if !strings.Contains(body, `"version": 1`) {
		t.Errorf("saved body missing cairn.version: %s", body)
	}
	if !strings.Contains(body, `"enabled": true`) {
		t.Errorf("saved body missing enabled=true: %s", body)
	}
}

func TestAddCairnHook_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, _ := hook.LoadSettings(path)
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	first := readBack(t, path)

	// Round-trip: load → add → save. Must be byte-identical.
	s2, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	s2.AddCairnHook()
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	second := readBack(t, path)
	if first != second {
		t.Errorf("non-idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	// Count should be exactly 1 handler regardless of how many times
	// enable ran.
	s3, _ := hook.LoadSettings(path)
	if n := s3.CairnEntryCount(); n != 1 {
		t.Errorf("CairnEntryCount: %d want 1 (idempotency)", n)
	}
}

func TestAddCairnHook_PreservesOperatorStopEntry(t *testing.T) {
	// Operator has their own Stop hook. cairn's add must preserve it
	// alongside.
	initial := `{
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "/usr/local/bin/my-stop-script"}]}
    ]
  }
}`
	path := writeSettings(t, initial)
	s, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	body := readBack(t, path)
	if !strings.Contains(body, "/usr/local/bin/my-stop-script") {
		t.Errorf("operator hook was stripped: %s", body)
	}
	if !strings.Contains(body, "cairn hook check-drift") {
		t.Errorf("cairn hook not added: %s", body)
	}
}

func TestRemoveCairnHooks_PreservesOperatorEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// Seed with operator entry, then add cairn.
	initial := `{
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "/usr/local/bin/operator-tool"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := hook.LoadSettings(path)
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Now disable.
	s2, _ := hook.LoadSettings(path)
	s2.RemoveCairnHooks()
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	body := readBack(t, path)
	if strings.Contains(body, "cairn hook check-drift") {
		t.Errorf("cairn handler not stripped: %s", body)
	}
	if !strings.Contains(body, "/usr/local/bin/operator-tool") {
		t.Errorf("operator handler was swept: %s", body)
	}
	// cairn config block flipped to enabled=false, still present.
	s3, _ := hook.LoadSettings(path)
	c, present := s3.Cairn()
	if !present {
		t.Errorf("cairn block should still be present (flipped, not deleted)")
	}
	if c.Enabled {
		t.Errorf("cairn.enabled should be false after disable")
	}
}

func TestRemoveCairnHooks_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, _ := hook.LoadSettings(path)
	s.RemoveCairnHooks() // disable on never-enabled settings
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// Should be a fresh, empty-ish file — nothing to remove.
	s2, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.CairnEntryCount() != 0 {
		t.Errorf("count: %d want 0", s2.CairnEntryCount())
	}
	if _, present := s2.Cairn(); present {
		t.Errorf("cairn block should not be created by a bare disable")
	}
}

func TestSave_PreservesTopLevelOrderAndUnknownKeys(t *testing.T) {
	// Include $schema, theme, and a custom nested unknown. Cairn must
	// not reorder them vs hooks/cairn, and values must pass through
	// byte-identical.
	initial := `{
  "$schema": "https://example.com/schema.json",
  "theme": "dark",
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "/bin/foo"}]}
    ]
  },
  "someNestedUnknown": {"a": 1, "b": [true, false, null]}
}`
	path := writeSettings(t, initial)
	s, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	body := readBack(t, path)

	// Decode top level and verify the key order.
	var keys []string
	dec := json.NewDecoder(strings.NewReader(body))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("top level open: %v %v", tok, err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	// Expected: original keys in order, cairn appended at end.
	wantPrefix := []string{"$schema", "theme", "hooks", "someNestedUnknown"}
	if len(keys) < len(wantPrefix) {
		t.Fatalf("keys: %v", keys)
	}
	for i, want := range wantPrefix {
		if keys[i] != want {
			t.Errorf("keys[%d]=%q want %q (full=%v)", i, keys[i], want, keys)
		}
	}
	if keys[len(keys)-1] != "cairn" {
		t.Errorf("cairn should be appended last, got keys=%v", keys)
	}

	// $schema and theme values byte-identical.
	if !strings.Contains(body, `"https://example.com/schema.json"`) {
		t.Errorf("schema value lost: %s", body)
	}
	if !strings.Contains(body, `"dark"`) {
		t.Errorf("theme value lost: %s", body)
	}
}

func TestSave_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "settings.json")
	s, _ := hook.LoadSettings(path)
	s.AddCairnHook()
	if err := s.Save(); err != nil {
		t.Fatalf("save should create parents: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestCairnEntryCount_MultipleEvents(t *testing.T) {
	// Manually construct a settings.json with cairn in Stop AND
	// PostToolUse. CairnEntryCount must sum across events.
	initial := `{
  "hooks": {
    "Stop": [
      {"matcher": ".*", "hooks": [{"type": "command", "command": "cairn hook check-drift"}]}
    ],
    "PostToolUse": [
      {"matcher": "Edit|Write", "hooks": [
        {"type": "command", "command": "cairn hook check-custom"},
        {"type": "command", "command": "/bin/operator-thing"}
      ]}
    ]
  }
}`
	path := writeSettings(t, initial)
	s, err := hook.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.CairnEntryCount(); n != 2 {
		t.Errorf("CairnEntryCount: %d want 2", n)
	}
	s.RemoveCairnHooks()
	if n := s.CairnEntryCount(); n != 0 {
		t.Errorf("after remove: %d want 0", n)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	body := readBack(t, path)
	if !strings.Contains(body, "/bin/operator-thing") {
		t.Errorf("operator handler swept: %s", body)
	}
	if strings.Contains(body, "cairn hook check-") {
		t.Errorf("cairn handler survived: %s", body)
	}
}
