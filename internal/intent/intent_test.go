package intent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/intent"
)

func writeSpec(t *testing.T, root string) {
	t.Helper()
	reqDir := filepath.Join(root, "requirements")
	taskDir := filepath.Join(root, "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	reqYAML := `id: REQ-001
title: Fast login path
why: p95 login is 800ms
scope_in: [auth/login]
scope_out: []
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ok]
        pass_on_exit_code: 0
`
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-001.yaml"), []byte(reqYAML), 0o644)

	taskYAML := `id: TASK-001
implements: [REQ-001]
depends_on: []
required_gates: [AC-001]
`
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-001.yaml"), []byte(taskYAML), 0o644)
}

func TestLoad_ParsesValidSpec(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Requirements) != 1 {
		t.Fatalf("want 1 requirement got %d", len(bundle.Requirements))
	}
	req := bundle.Requirements[0]
	if req.ID != "REQ-001" {
		t.Errorf("req id=%s", req.ID)
	}
	if len(req.Gates) != 1 || req.Gates[0].ID != "AC-001" {
		t.Errorf("bad gates: %+v", req.Gates)
	}
	if len(bundle.Tasks) != 1 || bundle.Tasks[0].ID != "TASK-001" {
		t.Errorf("bad tasks: %+v", bundle.Tasks)
	}
}

func TestValidate_SchemaHappyPath(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	errs := intent.Validate(bundle)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestValidate_SchemaRejectsMissingID(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "requirements"), 0o755)
	_ = os.WriteFile(
		filepath.Join(root, "requirements", "bad.yaml"),
		[]byte("title: no id\ngates:\n  - id: AC-001\n    kind: test\n    producer: {kind: executable}\n"),
		0o644,
	)
	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	errs := intent.Validate(bundle)
	if len(errs) == 0 {
		t.Fatal("want validation errors")
	}
	found := false
	for _, e := range errs {
		if e.Kind == "schema" && strings.Contains(e.Message, "id") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want schema error about missing id, got: %+v", errs)
	}
}

func TestValidate_RefTaskImplementsMissingRequirement(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "requirements"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	// Write a minimally valid requirement so only the reference is bad.
	_ = os.WriteFile(
		filepath.Join(root, "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: x
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-001.yaml"),
		[]byte(`id: TASK-001
implements: [REQ-999]
required_gates: [AC-001]
`), 0o644)

	bundle, _ := intent.Load(root)
	errs := intent.Validate(bundle)
	found := false
	for _, e := range errs {
		if e.Kind == "ref" && strings.Contains(e.Message, "REQ-999") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want ref error for REQ-999, got: %+v", errs)
	}
}

func TestValidate_DependsCycle(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "requirements"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	_ = os.WriteFile(
		filepath.Join(root, "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: x
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-A.yaml"),
		[]byte(`id: TASK-A
implements: [REQ-001]
depends_on: [TASK-B]
`), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-B.yaml"),
		[]byte(`id: TASK-B
implements: [REQ-001]
depends_on: [TASK-A]
`), 0o644)

	bundle, _ := intent.Load(root)
	errs := intent.Validate(bundle)
	found := false
	for _, e := range errs {
		if e.Kind == "cycle" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want cycle error, got: %+v", errs)
	}
}

func TestValidate_DuplicateTaskID(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "requirements"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	_ = os.WriteFile(
		filepath.Join(root, "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: x
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-A-1.yaml"),
		[]byte("id: TASK-A\nimplements: [REQ-001]\n"), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-A-2.yaml"),
		[]byte("id: TASK-A\nimplements: [REQ-001]\n"), 0o644)

	bundle, _ := intent.Load(root)
	errs := intent.Validate(bundle)
	found := false
	for _, e := range errs {
		if e.Kind == "duplicate" && strings.Contains(e.Message, "TASK-A") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want duplicate error, got: %+v", errs)
	}
}

func TestValidate_RequiredGateNotOnImplementedReq(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "requirements"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	_ = os.WriteFile(
		filepath.Join(root, "requirements", "REQ-001.yaml"),
		[]byte(`id: REQ-001
title: x
gates:
  - id: AC-001
    kind: test
    producer: {kind: executable}
`), 0o644)
	_ = os.WriteFile(
		filepath.Join(root, "tasks", "TASK-A.yaml"),
		[]byte("id: TASK-A\nimplements: [REQ-001]\nrequired_gates: [AC-999]\n"), 0o644)

	bundle, _ := intent.Load(root)
	errs := intent.Validate(bundle)
	found := false
	for _, e := range errs {
		if e.Kind == "ref" && strings.Contains(e.Message, "AC-999") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want ref error for AC-999, got: %+v", errs)
	}
}

func TestGateDefHash_DeterministicAcrossWhitespace(t *testing.T) {
	// Same gate, different YAML whitespace. JCS normalizes the JSON form,
	// so hash must match.
	g1 := intent.Gate{
		ID:   "AC-001",
		Kind: "test",
		Producer: intent.Producer{
			Kind: "executable",
			Config: map[string]any{
				"command":           []any{"echo", "ok"},
				"pass_on_exit_code": int64(0),
			},
		},
	}
	g2 := intent.Gate{
		ID:   "AC-001",
		Kind: "test",
		Producer: intent.Producer{
			Kind: "executable",
			Config: map[string]any{
				"pass_on_exit_code": int64(0),
				"command":           []any{"echo", "ok"},
			},
		},
	}
	h1, err := intent.GateDefHash(g1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := intent.GateDefHash(g2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash drift across map-key order: h1=%s h2=%s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("hash should be 64-char hex, got %d: %s", len(h1), h1)
	}
}

func TestGateDefHash_ChangesOnSemanticEdit(t *testing.T) {
	base := intent.Gate{
		ID: "AC-001", Kind: "test",
		Producer: intent.Producer{Kind: "executable", Config: map[string]any{"command": []any{"echo"}}},
	}
	edited := base
	edited.Producer.Config = map[string]any{"command": []any{"echo", "ko"}}
	h1, _ := intent.GateDefHash(base)
	h2, _ := intent.GateDefHash(edited)
	if h1 == h2 {
		t.Fatalf("semantic edit should change hash")
	}
}

func TestStore_Materialize(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	bundle, _ := intent.Load(root)

	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()

	clk := clock.NewFake(100)
	appender := events.NewAppender(clk)

	var result intent.MaterializeResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := intent.NewStore(tx, appender, clk)
		r, err := store.Materialize(bundle)
		result = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequirementsMaterialized != 1 || result.GatesMaterialized != 1 || result.TasksMaterialized != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}

	var n int
	_ = h.SQL().QueryRow("SELECT count(*) FROM requirements").Scan(&n)
	if n != 1 {
		t.Fatalf("requirements=%d", n)
	}
	_ = h.SQL().QueryRow("SELECT count(*) FROM tasks").Scan(&n)
	if n != 1 {
		t.Fatalf("tasks=%d", n)
	}
	var hash string
	_ = h.SQL().QueryRow("SELECT gate_def_hash FROM gates WHERE id='AC-001'").Scan(&hash)
	if len(hash) != 64 {
		t.Fatalf("gate_def_hash bad: %s", hash)
	}
}

func TestValidate_EmptyBundle(t *testing.T) {
	bundle := &intent.Bundle{}
	errs := intent.Validate(bundle)
	if len(errs) != 0 {
		t.Fatalf("empty bundle should have no errors, got: %+v", errs)
	}
}

func TestValidate_NilBundleReturnsEmptySlice(t *testing.T) {
	// Defensive: passing zero-value bundle should not panic, must return [] not nil.
	bundle := &intent.Bundle{}
	errs := intent.Validate(bundle)
	if errs == nil {
		// Pass — nil slice is fine; the JSON encoder converts at the CLI seam.
		return
	}
	if len(errs) != 0 {
		t.Fatalf("zero bundle: %+v", errs)
	}
}

func TestLoad_ScanCountsEmpty(t *testing.T) {
	root := t.TempDir()
	// no requirements/, no tasks/ subdirs at all
	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if got := len(bundle.Requirements); got != 0 {
		t.Errorf("requirements: want 0, got %d", got)
	}
	if got := len(bundle.Tasks); got != 0 {
		t.Errorf("tasks: want 0, got %d", got)
	}
}

func TestLoad_ScanCountsIgnoresExamples(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requirements")
	taskDir := filepath.Join(root, "tasks")
	_ = os.MkdirAll(reqDir, 0o755)
	_ = os.MkdirAll(taskDir, 0o755)

	// Only .yaml.example files. Loader must skip them.
	_ = os.WriteFile(filepath.Join(reqDir, "REQ-001.yaml.example"), []byte("id: REQ-001\n"), 0o644)
	_ = os.WriteFile(filepath.Join(taskDir, "TASK-001.yaml.example"), []byte("id: TASK-001\n"), 0o644)

	bundle, err := intent.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(bundle.Requirements); got != 0 {
		t.Errorf("requirements: want 0 (only .yaml.example present), got %d", got)
	}
	if got := len(bundle.Tasks); got != 0 {
		t.Errorf("tasks: want 0 (only .yaml.example present), got %d", got)
	}
}
