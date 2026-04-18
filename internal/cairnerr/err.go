// Package cairnerr provides a structured error type mapped to CLI exit codes.
//
// Code (coarse) maps 1:1 to exit code:
//
//	CodeBadInput, CodeValidation -> 1
//	CodeConflict                 -> 2
//	CodeNotFound                 -> 3
//	CodeSubstrate                -> 4
//
// Kind (fine) is a short string like "dep_not_done", "task_not_claimable",
// "evidence_hash_mismatch". Kind goes to the envelope's `error.code` field;
// Code controls the process exit code.
package cairnerr

import (
	"fmt"
	"strings"
)

// Code is the coarse error category.
type Code string

const (
	CodeBadInput   Code = "bad_input"
	CodeValidation Code = "validation"
	CodeConflict   Code = "conflict"
	CodeNotFound   Code = "not_found"
	CodeSubstrate  Code = "substrate"
)

// Err is cairn's structured error.
type Err struct {
	Code    Code
	Kind    string
	Message string
	Details map[string]any
	Cause   error
}

// New constructs an Err.
func New(code Code, kind, message string) *Err {
	if kind == "" {
		panic("cairnerr.New: kind must not be empty")
	}
	return &Err{Code: code, Kind: kind, Message: message}
}

// WithCause wraps an underlying error.
func (e *Err) WithCause(cause error) *Err {
	e.Cause = cause
	return e
}

// WithDetails attaches structured details.
func (e *Err) WithDetails(d map[string]any) *Err {
	e.Details = d
	return e
}

// Error satisfies the error interface.
func (e *Err) Error() string {
	var b strings.Builder
	b.WriteString(e.Kind)
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

// Unwrap enables errors.Is / errors.As.
func (e *Err) Unwrap() error { return e.Cause }

// Errorf is a convenience that uses fmt.Sprintf for Message.
func Errorf(code Code, kind, format string, args ...any) *Err {
	if kind == "" {
		panic("cairnerr.Errorf: kind must not be empty")
	}
	return New(code, kind, fmt.Sprintf(format, args...))
}
