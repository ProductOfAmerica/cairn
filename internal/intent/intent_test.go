package intent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
