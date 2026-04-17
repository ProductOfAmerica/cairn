// Package cli owns the JSON envelope, exit-code mapping, and flag helpers.
// Commands under cmd/cairn are thin wrappers that construct an Envelope and
// call WriteEnvelope.
package cli

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// Envelope is the response shape per § 6d of the design spec.
type Envelope struct {
	OpID string
	Kind string
	Data any
	Err  error
}

// WriteEnvelope writes the JSON envelope to w. Never returns an error; any
// marshal failure is swallowed into a last-ditch plain message.
func WriteEnvelope(w io.Writer, e Envelope) {
	out := map[string]any{"kind": e.Kind}
	if e.OpID != "" {
		out["op_id"] = e.OpID
	}
	if e.Err != nil {
		var ce *cairnerr.Err
		if errors.As(e.Err, &ce) {
			errMap := map[string]any{
				"code":    ce.Kind,
				"message": ce.Message,
			}
			if ce.Details != nil {
				errMap["details"] = ce.Details
			}
			out["error"] = errMap
		} else {
			out["error"] = map[string]any{
				"code":    "internal",
				"message": e.Err.Error(),
			}
		}
	} else {
		data := e.Data
		if data == nil {
			data = map[string]any{}
		}
		out["data"] = data
	}
	body, err := json.Marshal(out)
	if err != nil {
		_, _ = io.WriteString(w, `{"kind":"cli.error","error":{"code":"marshal_failed"}}`)
		_, _ = io.WriteString(w, "\n")
		return
	}
	_, _ = w.Write(body)
	_, _ = io.WriteString(w, "\n")
}
