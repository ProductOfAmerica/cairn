package cli

import (
	"errors"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// ExitCodeFor maps an error (or nil) to the structured process exit code
// per § 5a of the design spec.
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var ce *cairnerr.Err
	if errors.As(err, &ce) {
		switch ce.Code {
		case cairnerr.CodeBadInput, cairnerr.CodeValidation:
			return 1
		case cairnerr.CodeConflict:
			return 2
		case cairnerr.CodeNotFound:
			return 3
		case cairnerr.CodeSubstrate:
			return 4
		}
	}
	return 4
}
