package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cli"
)

func TestSpecInit_CreatesTemplates(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatalf("SpecInit: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("created: want 2 files, got %d: %v", len(res.Created), res.Created)
	}

	for _, want := range []string{
		filepath.Join(target, "requirements", "REQ-001.yaml.example"),
		filepath.Join(target, "tasks", "TASK-001.yaml.example"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing file: %s", want)
		}
	}
}

func TestSpecInit_Idempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	first, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Created) != 2 {
		t.Fatalf("first call: want 2 created, got %d", len(first.Created))
	}

	second, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Created) != 0 {
		t.Errorf("second call: want 0 created, got %v", second.Created)
	}
	if len(second.Skipped) != 2 {
		t.Errorf("second call: want 2 skipped, got %v", second.Skipped)
	}
	if second.Overwritten {
		t.Errorf("second call should not report overwritten without force")
	}
}

func TestSpecInit_Force(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "specs")

	if _, err := cli.SpecInit(target, false); err != nil {
		t.Fatal(err)
	}
	// Mutate one file so we can detect a real rewrite.
	mutated := filepath.Join(target, "requirements", "REQ-001.yaml.example")
	if err := os.WriteFile(mutated, []byte("# manually edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := cli.SpecInit(target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Overwritten {
		t.Errorf("force should report overwritten=true")
	}
	body, _ := os.ReadFile(mutated)
	if string(body) == "# manually edited\n" {
		t.Errorf("force should have rewritten the manually-edited file")
	}
}

func TestSpecInit_CustomPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "alt", "spec-tree")

	res, err := cli.SpecInit(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("custom path: want 2 created, got %d", len(res.Created))
	}
	for _, p := range []string{
		filepath.Join(target, "requirements", "REQ-001.yaml.example"),
		filepath.Join(target, "tasks", "TASK-001.yaml.example"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing: %s", p)
		}
	}
}
