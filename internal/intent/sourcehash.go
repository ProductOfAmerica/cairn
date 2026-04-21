package intent

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
)

// sourceHashHeaderRe matches the `# cairn-derived:` header line cairn
// emits at the top of every derived YAML file. Capture groups:
//
//	1. source-hash (64-char lowercase hex sha256 of the prose source file)
//	2. source-path (whitespace-free path, typically docs/superpowers/...)
//	3. derived-at  (RFC3339 UTC, trailing Z)
//
// Kept byte-identical to the prior single source at
// testdata/skill-tests/verify/main.go, which this library replaces.
var sourceHashHeaderRe = regexp.MustCompile(`^# cairn-derived: source-hash=([a-f0-9]{64}) source-path=(\S+) derived-at=(\S+Z)$`)

// ErrNoSourceHashHeader signals that the first line of a YAML file does
// not match the `# cairn-derived:` header regex. Consumers detect this
// specific case via errors.Is (e.g. to emit "no source-hash header"
// warnings without treating it as a read failure).
var ErrNoSourceHashHeader = errors.New("no # cairn-derived: header")

// SourceHashHeader is the parsed `# cairn-derived:` comment on a derived
// YAML file. Every field is required; a malformed or absent header
// produces an error rather than a zero-value struct.
type SourceHashHeader struct {
	SourceHash string // lowercase hex sha256 of the prose source
	SourcePath string // relative path to the prose source (no whitespace)
	DerivedAt  string // iso8601 UTC stamp (ends in Z)
}

// ParseSourceHashHeader extracts the `# cairn-derived:` header from the
// first line of yamlBytes. The header is produced by the using-cairn
// skill's derivation flow (see skills/using-cairn/source-hash-format.md).
//
// Returns ErrNoSourceHashHeader (via errors.Is) when the first line
// doesn't match, including empty input. Any other error is a read
// failure from the underlying byte reader.
func ParseSourceHashHeader(yamlBytes []byte) (SourceHashHeader, error) {
	br := bufio.NewReader(bytes.NewReader(yamlBytes))
	first, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return SourceHashHeader{}, fmt.Errorf("read first line: %w", err)
	}
	first = trimTrailingNewline(first)
	m := sourceHashHeaderRe.FindStringSubmatch(first)
	if m == nil {
		return SourceHashHeader{}, ErrNoSourceHashHeader
	}
	return SourceHashHeader{
		SourceHash: m[1],
		SourcePath: m[2],
		DerivedAt:  m[3],
	}, nil
}

// ComputeSourceHash returns the lowercase-hex sha256 of the file at
// path. Used to verify the SourceHash field of a SourceHashHeader
// against the current state of the prose source file it references.
//
// Returns fs.PathError (via os.Open) when the file is missing; callers
// can detect missing vs read-error cases via errors.Is(err, os.ErrNotExist).
func ComputeSourceHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// trimTrailingNewline strips \r\n or \n at the end of s. Handles files
// authored on Windows where the first line may end in \r\n even when
// the rest of the file uses \n (git autocrlf mixed-mode output).
func trimTrailingNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}
