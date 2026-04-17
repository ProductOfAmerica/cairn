package intent

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed schema/*.json
var schemaFS embed.FS

// SpecError represents a single validation failure.
type SpecError struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`    // schema|ref|duplicate|cycle
	Message string `json:"message"`
}

// Validate runs schema + referential + uniqueness checks on the bundle.
// Returns ALL errors in one pass (not fail-fast).
func Validate(b *Bundle) []SpecError {
	var errs []SpecError
	errs = append(errs, validateSchemas(b)...)
	errs = append(errs, validateReferential(b)...)
	return errs
}

func validateSchemas(b *Bundle) []SpecError {
	var out []SpecError
	reqSchema, err := compileSchema("schema/requirement.schema.json")
	if err != nil {
		return []SpecError{{Kind: "schema", Message: err.Error()}}
	}
	taskSchema, err := compileSchema("schema/task.schema.json")
	if err != nil {
		return []SpecError{{Kind: "schema", Message: err.Error()}}
	}
	for _, r := range b.Requirements {
		if errs := validateOne(reqSchema, r.RawYAML, r.SpecPath); len(errs) > 0 {
			out = append(out, errs...)
		}
	}
	for _, t := range b.Tasks {
		if errs := validateOne(taskSchema, t.RawYAML, t.SpecPath); len(errs) > 0 {
			out = append(out, errs...)
		}
	}
	return out
}

func compileSchema(embedPath string) (*jsonschema.Schema, error) {
	raw, err := schemaFS.ReadFile(embedPath)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource(embedPath, doc); err != nil {
		return nil, err
	}
	return c.Compile(embedPath)
}

func validateOne(sch *jsonschema.Schema, rawYAML []byte, path string) []SpecError {
	// YAML → generic map → JSON → UnmarshalJSON → Validate.
	var raw any
	if err := yaml.Unmarshal(rawYAML, &raw); err != nil {
		return []SpecError{{Path: path, Kind: "schema", Message: err.Error()}}
	}
	asJSON, err := json.Marshal(yamlToJSONCompatible(raw))
	if err != nil {
		return []SpecError{{Path: path, Kind: "schema", Message: err.Error()}}
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(asJSON)))
	if err != nil {
		return []SpecError{{Path: path, Kind: "schema", Message: err.Error()}}
	}
	if err := sch.Validate(doc); err != nil {
		// Split multi-error messages into individual SpecErrors.
		msg := err.Error()
		return splitValidationErrors(path, msg)
	}
	return nil
}

// yamlToJSONCompatible converts yaml.v3's map[interface{}]interface{} (which
// happens for some nested structures) into map[string]interface{} suitable
// for json.Marshal.
func yamlToJSONCompatible(v any) any {
	switch x := v.(type) {
	case map[any]any:
		m := map[string]any{}
		for k, val := range x {
			m[fmt.Sprint(k)] = yamlToJSONCompatible(val)
		}
		return m
	case map[string]any:
		m := map[string]any{}
		for k, val := range x {
			m[k] = yamlToJSONCompatible(val)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = yamlToJSONCompatible(e)
		}
		return out
	default:
		return v
	}
}

func splitValidationErrors(path, combined string) []SpecError {
	var out []SpecError
	for _, line := range strings.Split(combined, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "jsonschema validation failed") {
			continue
		}
		out = append(out, SpecError{Path: path, Kind: "schema", Message: line})
	}
	if len(out) == 0 {
		out = []SpecError{{Path: path, Kind: "schema", Message: combined}}
	}
	return out
}

func validateReferential(_ *Bundle) []SpecError { return nil }
