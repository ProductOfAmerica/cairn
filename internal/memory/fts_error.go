package memory

import (
	"regexp"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// ftsNearPattern extracts the position marker out of `fts5: syntax error near "..."`
// if the driver emits one. Not all FTS5 errors include it; callers tolerate miss.
var ftsNearPattern = regexp.MustCompile(`near\s+"([^"]*)"`)

// TranslateFTSError wraps a SQLite/FTS5 error as a cairnerr.Err with a
// sanitized message. Returns nil if err is nil. The raw error is preserved
// as Cause for Unwrap but NOT serialized to the envelope.
//
// Per design spec §4.6: the envelope message must not contain "sqlite",
// "fts5:", or `near "` substrings.
func TranslateFTSError(err error) error {
	if err == nil {
		return nil
	}
	msg := "query syntax invalid; see FTS5 query syntax docs"
	details := map[string]any{}

	raw := err.Error()
	if m := ftsNearPattern.FindStringSubmatch(raw); len(m) == 2 {
		// Record the offending fragment as a detail for debugging. Kept as
		// a structured field; the envelope's top-level Message stays clean.
		details["near"] = m[1]
	}

	// Heuristic: differentiate "syntax" from other classes. Kept minimal.
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "syntax"):
		msg = "query syntax invalid"
	case strings.Contains(low, "no such column"):
		msg = "unknown FTS5 column in query"
	}

	return cairnerr.New(cairnerr.CodeBadInput, "invalid_fts_query", msg).
		WithDetails(details).
		WithCause(err)
}
