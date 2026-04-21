package hook_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli/hook"
)

// repoLayout builds a minimal cairn-ish repo: one prose file under
// docs/, one YAML under specs/ whose source-hash header points at the
// prose file. Returns the tmpdir rooted at cwd.
type repoLayout struct {
	cwd       string
	prose     string
	yaml      string
	proseHash string
}

func makeRepo(t *testing.T, proseBody string) repoLayout {
	t.Helper()
	cwd := t.TempDir()

	proseRel := "docs/superpowers/specs/2026-04-21-design.md"
	prose := filepath.Join(cwd, filepath.FromSlash(proseRel))
	if err := os.MkdirAll(filepath.Dir(prose), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prose, []byte(proseBody), 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte(proseBody))
	hashHex := hex.EncodeToString(sum[:])

	yamlRel := "specs/requirements/REQ-001.yaml"
	yaml := filepath.Join(cwd, filepath.FromSlash(yamlRel))
	if err := os.MkdirAll(filepath.Dir(yaml), 0o755); err != nil {
		t.Fatal(err)
	}
	header := "# cairn-derived: source-hash=" + hashHex +
		" source-path=" + proseRel +
		" derived-at=2026-04-21T09:14:07Z\n"
	body := header + "requirement:\n  id: REQ-001\n"
	if err := os.WriteFile(yaml, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return repoLayout{cwd: cwd, prose: prose, yaml: yaml, proseHash: hashHex}
}

func TestCheckDrift_HeaderMatch_Clean(t *testing.T) {
	r := makeRepo(t, "hello cairn\n")
	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if !res.Clean {
		t.Errorf("want Clean, got warnings=%v", res.Warnings)
	}
	if res.Checked != 1 {
		t.Errorf("Checked=%d want 1", res.Checked)
	}
	if res.Skipped {
		t.Errorf("Skipped=true unexpected")
	}
}

func TestCheckDrift_HeaderMismatch_Warns(t *testing.T) {
	r := makeRepo(t, "hello cairn\n")
	// Mutate prose after YAML has been derived.
	if err := os.WriteFile(r.prose, []byte("hello cairn v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if res.Clean {
		t.Errorf("want dirty (warnings), got clean")
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("warnings: got %d want 1: %v", len(res.Warnings), res.Warnings)
	}
	if !strings.Contains(res.Warnings[0], "drift:") {
		t.Errorf("warning must mention drift: %q", res.Warnings[0])
	}
}

func TestCheckDrift_HeaderMissing_Warns(t *testing.T) {
	r := makeRepo(t, "hello\n")
	// Overwrite YAML with no header.
	if err := os.WriteFile(r.yaml, []byte("requirement:\n  id: REQ-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if res.Clean {
		t.Errorf("want dirty")
	}
	if !strings.Contains(res.Warnings[0], "no source-hash header:") {
		t.Errorf("warning must mention missing header: %q", res.Warnings[0])
	}
}

func TestCheckDrift_SourceFileMissing_Warns(t *testing.T) {
	r := makeRepo(t, "hello\n")
	if err := os.Remove(r.prose); err != nil {
		t.Fatal(err)
	}
	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if res.Clean {
		t.Errorf("want dirty")
	}
	if !strings.Contains(res.Warnings[0], "source file missing:") {
		t.Errorf("warning must mention missing source: %q", res.Warnings[0])
	}
}

func TestCheckDrift_NoSpecsDir_Skips(t *testing.T) {
	cwd := t.TempDir()
	res, err := hook.CheckDrift(cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if !res.Skipped {
		t.Errorf("want Skipped")
	}
	if res.SkipReason != "no_specs_dir" {
		t.Errorf("SkipReason: %q", res.SkipReason)
	}
	if !res.Clean {
		t.Errorf("Skipped runs must report Clean=true")
	}
	if res.Checked != 0 {
		t.Errorf("Checked=%d want 0", res.Checked)
	}
}

func TestCheckDrift_MultipleYAML_SomeDrift(t *testing.T) {
	r := makeRepo(t, "v1\n")
	// Add a second YAML pointing at a different prose file.
	proseRel := "docs/superpowers/plans/2026-04-21-plan.md"
	prose := filepath.Join(r.cwd, filepath.FromSlash(proseRel))
	if err := os.MkdirAll(filepath.Dir(prose), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prose, []byte("plan v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("plan v1\n"))
	hashHex := hex.EncodeToString(sum[:])
	header := "# cairn-derived: source-hash=" + hashHex +
		" source-path=" + proseRel +
		" derived-at=2026-04-21T09:14:07Z\n"
	yaml2 := filepath.Join(r.cwd, "specs", "tasks", "TASK-001-001.yaml")
	if err := os.MkdirAll(filepath.Dir(yaml2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yaml2, []byte(header+"task: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Now drift the plan prose file only.
	if err := os.WriteFile(prose, []byte("plan v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if res.Checked != 2 {
		t.Errorf("Checked=%d want 2", res.Checked)
	}
	if res.Clean {
		t.Errorf("want dirty")
	}
	if len(res.Warnings) != 1 {
		t.Errorf("exactly one drift expected, got %d: %v", len(res.Warnings), res.Warnings)
	}
}

func TestCheckDrift_IgnoresNonYAML(t *testing.T) {
	r := makeRepo(t, "v1\n")
	// Drop a README under specs/ — must not be walked.
	if err := os.WriteFile(filepath.Join(r.cwd, "specs", "README.md"), []byte("stuff"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := hook.CheckDrift(r.cwd)
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if res.Checked != 1 {
		t.Errorf("Checked=%d want 1 (README.md ignored)", res.Checked)
	}
}

func TestWriteDriftWarnings_PrefixAndLineCount(t *testing.T) {
	var buf bytes.Buffer
	hook.WriteDriftWarnings(&buf, hook.CheckDriftResult{
		Warnings: []string{"drift: a", "drift: b"},
	})
	out := buf.String()
	if !strings.Contains(out, "cairn hook check-drift: drift: a") {
		t.Errorf("missing first line: %q", out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("want 2 lines, got %d: %q", strings.Count(out, "\n"), out)
	}
}

func TestIsCairnTracked_SpecsDir(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hook.IsCairnTracked(cwd, t.TempDir()) {
		t.Errorf("specs/ present but IsCairnTracked false")
	}
}

func TestIsCairnTracked_NoSpecsNoState(t *testing.T) {
	cwd := t.TempDir()
	if hook.IsCairnTracked(cwd, t.TempDir()) {
		t.Errorf("bare tmpdir must not be cairn-tracked")
	}
}

func TestGuardPanic_RecoversAndReportsTrue(t *testing.T) {
	recovered := hook.GuardPanic(func() { panic("boom") })
	if !recovered {
		t.Errorf("GuardPanic must report true on panic")
	}
}

func TestGuardPanic_NoPanicReportsFalse(t *testing.T) {
	recovered := hook.GuardPanic(func() {})
	if recovered {
		t.Errorf("GuardPanic must report false when fn returns normally")
	}
}

func TestReadInput_EmptyStdin(t *testing.T) {
	_, err := hook.ReadInput(strings.NewReader(""))
	if err == nil {
		t.Errorf("empty stdin must error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty: %v", err)
	}
}

func TestReadInput_ValidCCShape(t *testing.T) {
	body := `{"session_id":"abc","transcript_path":"/tmp/t.jsonl","cwd":"/repo","hook_event_name":"Stop","stop_hook_active":false}`
	in, err := hook.ReadInput(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if in.CWD != "/repo" || in.HookEventName != "Stop" || in.SessionID != "abc" {
		t.Errorf("fields: %+v", in)
	}
}

func TestReadInput_UnknownFieldsIgnored(t *testing.T) {
	body := `{"cwd":"/x","future_field_cc_adds":"someday","hook_event_name":"Stop"}`
	in, err := hook.ReadInput(strings.NewReader(body))
	if err != nil {
		t.Fatalf("tolerant decode should accept unknown fields: %v", err)
	}
	if in.CWD != "/x" {
		t.Errorf("CWD: %q", in.CWD)
	}
}
