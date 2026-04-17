package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
)

// GateDefHash returns the lowercase-hex sha256 of the RFC 8785 JCS
// canonicalization of the gate's canonical JSON form.
//
// Canonical form includes: id, kind, producer.kind, producer.config.
// Excludes file position, comments, whitespace (JCS normalizes these).
func GateDefHash(g Gate) (string, error) {
	canon := map[string]any{
		"id":   g.ID,
		"kind": g.Kind,
		"producer": map[string]any{
			"kind":   g.Producer.Kind,
			"config": normalizeForJSON(g.Producer.Config),
		},
	}
	raw, err := json.Marshal(canon)
	if err != nil {
		return "", fmt.Errorf("marshal gate for jcs: %w", err)
	}
	jcsBytes, err := jcs.Transform(raw)
	if err != nil {
		return "", fmt.Errorf("jcs: %w", err)
	}
	sum := sha256.Sum256(jcsBytes)
	return hex.EncodeToString(sum[:]), nil
}

// normalizeForJSON flattens map[any]any → map[string]any (yaml.v3 artifact)
// so json.Marshal succeeds deterministically.
func normalizeForJSON(v any) any {
	switch x := v.(type) {
	case map[any]any:
		m := map[string]any{}
		for k, val := range x {
			m[fmt.Sprint(k)] = normalizeForJSON(val)
		}
		return m
	case map[string]any:
		m := map[string]any{}
		for k, val := range x {
			m[k] = normalizeForJSON(val)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeForJSON(e)
		}
		return out
	default:
		return v
	}
}
