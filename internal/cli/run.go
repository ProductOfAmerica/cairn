package cli

import (
	"fmt"
	"io"
	"os"
)

// Run wraps a command body: invokes fn, writes the envelope, returns the exit code.
// Commands in cmd/cairn use this to stay ≤10 LOC.
func Run(stdout io.Writer, kind, opID string, fn func() (any, error)) int {
	data, err := fn()
	WriteEnvelope(stdout, Envelope{
		OpID: opID,
		Kind: kind,
		Data: data,
		Err:  err,
	})
	return ExitCodeFor(err)
}

// Exit prints the envelope and calls os.Exit with the mapped code. Used by
// top-level cobra commands that don't return errors naturally.
func Exit(kind, opID string, data any, err error) {
	WriteEnvelope(os.Stdout, Envelope{OpID: opID, Kind: kind, Data: data, Err: err})
	if err != nil {
		os.Exit(ExitCodeFor(err))
	}
}

// Logf writes to stderr at WARN+ (or DEBUG when verbose). Used sparingly.
func Logf(verbose bool, format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	_ = verbose
}
