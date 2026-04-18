package memory_test

import (
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func TestValidateKind(t *testing.T) {
	valid := []string{"decision", "rationale", "outcome", "failure"}
	for _, k := range valid {
		if err := memory.ValidateKind(k); err != nil {
			t.Errorf("ValidateKind(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{"", "DECISION", "unknown", " decision", "decision "}
	for _, k := range invalid {
		if err := memory.ValidateKind(k); err == nil {
			t.Errorf("ValidateKind(%q) = nil, want error", k)
		}
	}
}

func TestValidateEntityKind(t *testing.T) {
	valid := []string{"requirement", "task", "gate", "verdict", "run", "claim", "evidence", "memory"}
	for _, k := range valid {
		if err := memory.ValidateEntityKind(k); err != nil {
			t.Errorf("ValidateEntityKind(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{"", "Task", "unknown"}
	for _, k := range invalid {
		if err := memory.ValidateEntityKind(k); err == nil {
			t.Errorf("ValidateEntityKind(%q) = nil, want error", k)
		}
	}
}

func TestValidateEntityPair(t *testing.T) {
	// Both empty → ok (no entity).
	if err := memory.ValidateEntityPair("", ""); err != nil {
		t.Errorf("empty pair: %v", err)
	}
	// Both present → ok if kind valid.
	if err := memory.ValidateEntityPair("task", "TASK-001"); err != nil {
		t.Errorf("valid pair: %v", err)
	}
	// Kind but no id → error.
	if err := memory.ValidateEntityPair("task", ""); err == nil {
		t.Error("kind-without-id should fail")
	}
	// Id but no kind → error.
	if err := memory.ValidateEntityPair("", "TASK-001"); err == nil {
		t.Error("id-without-kind should fail")
	}
	// Invalid kind → error.
	if err := memory.ValidateEntityPair("Task", "TASK-001"); err == nil {
		t.Error("invalid kind should fail")
	}
	// Whitespace-only id → error.
	if err := memory.ValidateEntityPair("task", "   "); err == nil {
		t.Error("whitespace id should fail")
	}
}

func TestValidateTags(t *testing.T) {
	// Happy path.
	if err := memory.ValidateTags([]string{"foo", "bar_baz", "A1"}); err != nil {
		t.Errorf("happy: %v", err)
	}
	// Empty slice → ok.
	if err := memory.ValidateTags(nil); err != nil {
		t.Errorf("nil slice: %v", err)
	}
	if err := memory.ValidateTags([]string{}); err != nil {
		t.Errorf("empty slice: %v", err)
	}

	// Bad cases.
	bad := [][]string{
		{""},                  // empty tag
		{"foo-bar"},           // hyphen
		{"foo.bar"},           // dot
		{"foo bar"},           // space
		{"foo!"},              // symbol
		{strings.Repeat("a", 65)}, // too long (65)
	}
	for _, tags := range bad {
		if err := memory.ValidateTags(tags); err == nil {
			t.Errorf("ValidateTags(%v) = nil, want error", tags)
		}
	}

	// Too many (>20).
	many := make([]string, 21)
	for i := range many {
		many[i] = "t"
	}
	if err := memory.ValidateTags(many); err == nil {
		t.Error("21 tags should fail")
	}

	// Boundary: 64-char tag must pass.
	if err := memory.ValidateTags([]string{strings.Repeat("a", 64)}); err != nil {
		t.Errorf("64-char tag should pass: %v", err)
	}

	// Boundary: exactly 20 tags must pass.
	exactly20 := make([]string, 20)
	for i := range exactly20 {
		exactly20[i] = "t"
	}
	if err := memory.ValidateTags(exactly20); err != nil {
		t.Errorf("20 tags should pass: %v", err)
	}
}

func TestTagsText(t *testing.T) {
	got := memory.TagsText([]string{"foo", "bar"})
	if got != "foo bar" {
		t.Errorf("TagsText = %q, want %q", got, "foo bar")
	}
	if memory.TagsText(nil) != "" {
		t.Error("nil → empty")
	}
}

func TestTagsJSON(t *testing.T) {
	got := memory.TagsJSON([]string{"foo", "bar"})
	if got != `["foo","bar"]` {
		t.Errorf("TagsJSON = %q", got)
	}
	if memory.TagsJSON(nil) != `[]` {
		t.Error("nil → []")
	}
}
