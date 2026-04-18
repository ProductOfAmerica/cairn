package ids

import (
	"fmt"
	"regexp"
)

// opIDPattern matches a ULID in Crockford-base32: 26 chars, uppercase,
// digits + consonants (Crockford excludes I, L, O, U).
var opIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// ValidateOpID returns an error if s is not a valid op_id (ULID-formatted).
func ValidateOpID(s string) error {
	if !opIDPattern.MatchString(s) {
		return fmt.Errorf("op_id must be a 26-char uppercase Crockford-base32 ULID, got %q", s)
	}
	return nil
}
