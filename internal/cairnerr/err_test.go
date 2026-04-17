package cairnerr_test

import (
	"errors"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

func TestErr_MessageAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	e := cairnerr.New(cairnerr.CodeSubstrate, "busy", "db busy after retry budget").
		WithCause(cause)
	if !errors.Is(e, cause) {
		t.Fatalf("errors.Is cause not matched")
	}
	if got := e.Error(); got != "busy: db busy after retry budget: boom" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestErr_AsExtracts(t *testing.T) {
	e := cairnerr.New(cairnerr.CodeConflict, "dep_not_done", "blocked")
	var target *cairnerr.Err
	if !errors.As(e, &target) {
		t.Fatalf("errors.As failed")
	}
	if target.Code != cairnerr.CodeConflict {
		t.Fatalf("code mismatch")
	}
	if target.Kind != "dep_not_done" {
		t.Fatalf("kind mismatch")
	}
}

func TestErr_WithDetails(t *testing.T) {
	e := cairnerr.New(cairnerr.CodeNotFound, "gate_not_found", "no such gate").
		WithDetails(map[string]any{"gate_id": "AC-001"})
	if e.Details["gate_id"] != "AC-001" {
		t.Fatalf("details lost")
	}
}

func TestErr_Error_NoCause(t *testing.T) {
	e := cairnerr.New(cairnerr.CodeValidation, "too_short", "length is 2")
	if got := e.Error(); got != "too_short: length is 2" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestErr_Error_NoMessageNoCause(t *testing.T) {
	e := cairnerr.New(cairnerr.CodeBadInput, "bad_input", "")
	if got := e.Error(); got != "bad_input" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestErrorf_FormatsMessage(t *testing.T) {
	e := cairnerr.Errorf(cairnerr.CodeNotFound, "gate_not_found", "gate %q not present", "AC-001")
	if e.Message != `gate "AC-001" not present` {
		t.Fatalf("unexpected message: %q", e.Message)
	}
	if e.Kind != "gate_not_found" {
		t.Fatalf("kind mismatch")
	}
}

func TestErr_New_EmptyKindPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty kind")
		}
	}()
	_ = cairnerr.New(cairnerr.CodeBadInput, "", "msg")
}
