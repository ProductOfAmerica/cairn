// Package memory owns the memory_entries + memory_fts tables. Entries are
// append-only (enforced by schema triggers); search is FTS5-ranked.
package memory

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// Locked enums. See design spec §2 (Q2, Q3).
var validKinds = map[string]bool{
	"decision":  true,
	"rationale": true,
	"outcome":   true,
	"failure":   true,
}

var validEntityKinds = map[string]bool{
	"requirement": true,
	"task":        true,
	"gate":        true,
	"verdict":     true,
	"run":         true,
	"claim":       true,
	"evidence":    true,
	"memory":      true,
}

// Tag contract (design spec §3): ASCII [a-zA-Z0-9_], 1..64 chars, max 20 per entry.
var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

const (
	maxTagLen   = 64
	maxTagCount = 20
)

// ValidateKind returns nil iff k is one of the four locked memory kinds.
func ValidateKind(k string) error {
	if !validKinds[k] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_kind",
			"kind must be one of decision|rationale|outcome|failure").
			WithDetails(map[string]any{"got": k})
	}
	return nil
}

// ValidateEntityKind returns nil iff k is one of the allowed entity kinds.
// Pass empty string only when no entity is attached; callers that accept
// optional entities should use ValidateEntityPair instead.
func ValidateEntityKind(k string) error {
	if !validEntityKinds[k] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_entity_kind",
			"entity_kind must be one of requirement|task|gate|verdict|run|claim|evidence|memory").
			WithDetails(map[string]any{"got": k})
	}
	return nil
}

// ValidateEntityPair enforces the XOR invariant: both empty, or both present.
// When present, the kind must be valid and id must be non-whitespace.
func ValidateEntityPair(kind, id string) error {
	kindEmpty := kind == ""
	idEmpty := strings.TrimSpace(id) == ""
	if kindEmpty != idEmpty {
		return cairnerr.New(cairnerr.CodeBadInput, "entity_kind_id_mismatch",
			"entity_kind and entity_id must both be set or both omitted").
			WithDetails(map[string]any{"entity_kind": kind, "entity_id": id})
	}
	if kindEmpty {
		return nil
	}
	if !validEntityKinds[kind] {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_entity_kind",
			"entity_kind must be one of requirement|task|gate|verdict|run|claim|evidence|memory").
			WithDetails(map[string]any{"got": kind})
	}
	return nil
}

// ValidateTags enforces the tag format contract. Empty/nil is valid.
func ValidateTags(tags []string) error {
	if len(tags) > maxTagCount {
		return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
			"too many tags").
			WithDetails(map[string]any{"count": len(tags), "max": maxTagCount})
	}
	for _, t := range tags {
		if len(t) == 0 {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag must not be empty")
		}
		if len(t) > maxTagLen {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag too long").
				WithDetails(map[string]any{"tag": t, "max": maxTagLen})
		}
		if !tagPattern.MatchString(t) {
			return cairnerr.New(cairnerr.CodeBadInput, "invalid_tag",
				"tag must match [a-zA-Z0-9_]+").
				WithDetails(map[string]any{"tag": t})
		}
	}
	return nil
}

// TagsText returns the FTS5-indexed form: space-joined tokens.
func TagsText(tags []string) string {
	return strings.Join(tags, " ")
}

// TagsJSON returns the canonical structured form stored in tags_json.
// Always emits a JSON array; nil → "[]".
func TagsJSON(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, _ := json.Marshal(tags)
	return string(b)
}
