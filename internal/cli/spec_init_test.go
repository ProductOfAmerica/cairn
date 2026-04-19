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
