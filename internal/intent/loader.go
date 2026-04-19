package intent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load walks root/requirements/*.yaml and root/tasks/*.yaml, parses each,
// and returns the Bundle. Fails on the first YAML parse error; schema +
// referential checks are performed separately by Validate.
func Load(root string) (*Bundle, error) {
	reqs, err := loadYAMLDir(filepath.Join(root, "requirements"), func(b []byte, path string) (any, error) {
		var r Requirement
		if err := yaml.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		r.SpecPath = path
		r.RawYAML = b
		return r, nil
	})
	if err != nil {
		return nil, err
	}

	tasks, err := loadYAMLDir(filepath.Join(root, "tasks"), func(b []byte, path string) (any, error) {
		var t Task
		if err := yaml.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		t.SpecPath = path
		t.RawYAML = b
		return t, nil
	})
	if err != nil {
		return nil, err
	}

	bundle := &Bundle{}
	for _, x := range reqs {
		bundle.Requirements = append(bundle.Requirements, x.(Requirement))
	}
	for _, x := range tasks {
		bundle.Tasks = append(bundle.Tasks, x.(Task))
	}
	return bundle, nil
}

func loadYAMLDir(dir string, parse func([]byte, string) (any, error)) ([]any, error) {
	var out []any
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Ship 3 contract: only files with the literal `.yaml` suffix are loaded.
	// `.yaml.example` files (written by `cairn spec init`) are reference-only
	// scaffolds and MUST be skipped here. The strict-suffix match below
	// satisfies that requirement; do not relax it without updating the
	// renamed-template detector in validate.go.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		v, err := parse(b, path)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
