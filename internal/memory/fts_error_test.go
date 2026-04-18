package memory_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/cli"
	"github.com/ProductOfAmerica/cairn/internal/memory"
)

func TestTranslateFTSError_WrapsSQLiteSyntax(t *testing.T) {
	raw := errors.New(`fts5: syntax error near "AND AND"`)
	err := memory.TranslateFTSError(raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("not cairnerr.Err: %T", err)
	}
	if ce.Kind != "invalid_fts_query" {
		t.Errorf("kind = %q, want invalid_fts_query", ce.Kind)
	}
	if ce.Code != cairnerr.CodeBadInput {
		t.Errorf("code = %q, want bad_input", ce.Code)
	}

	// Envelope-visible message must be sanitized.
	for _, leak := range []string{"sqlite", "fts5:", "near \""} {
		if strings.Contains(strings.ToLower(ce.Message), strings.ToLower(leak)) {
			t.Errorf("message leaks %q: %s", leak, ce.Message)
		}
	}
	// Raw underlying error is preserved via Unwrap for debug/trace.
	if !errors.Is(errors.Unwrap(ce), raw) {
		t.Error("raw SQLite error not preserved via Unwrap")
	}
}

func TestTranslateFTSError_NilPassthrough(t *testing.T) {
	if got := memory.TranslateFTSError(nil); got != nil {
		t.Errorf("nil → %v, want nil", got)
	}
}

func TestTranslateFTSError_UnrecognizedWraps(t *testing.T) {
	// Unexpected errors (not FTS syntax) still get wrapped but without
	// leaking raw text; caller can Unwrap for diagnostics.
	raw := errors.New("disk is on fire")
	err := memory.TranslateFTSError(raw)
	var ce *cairnerr.Err
	if !errors.As(err, &ce) {
		t.Fatalf("not cairnerr.Err: %T", err)
	}
	if ce.Kind != "invalid_fts_query" {
		t.Errorf("kind = %q", ce.Kind)
	}
	if strings.Contains(ce.Message, "disk") {
		t.Errorf("leaked raw message: %s", ce.Message)
	}
}

func TestTranslateFTSError_EnvelopeDoesNotLeak(t *testing.T) {
	// Defensive end-to-end check: prove that sanitization holds through the
	// actual JSON envelope path that users see via stdout. The envelope uses
	// ce.Kind + ce.Message explicitly (not err.Error()), so raw SQLite text
	// attached as Cause stays behind in `errors.Unwrap` for debug/trace
	// without leaking to the envelope. Per design spec §4.6.
	raw := errors.New(`fts5: syntax error near "AND AND" ; sqlite constraint`)
	wrapped := memory.TranslateFTSError(raw)

	var buf bytes.Buffer
	cli.WriteEnvelope(&buf, cli.Envelope{
		Kind: "memory.search",
		Err:  wrapped,
	})
	got := buf.String()

	for _, leak := range []string{"sqlite", "fts5:", `near "`} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(leak)) {
			t.Errorf("envelope leaks %q: %s", leak, got)
		}
	}

	// Kind and message must be present in sanitized form.
	if !strings.Contains(got, `"code":"invalid_fts_query"`) {
		t.Errorf("envelope missing sanitized kind: %s", got)
	}
}
