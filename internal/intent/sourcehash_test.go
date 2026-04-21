package intent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	hashA64        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	validHeaderLF  = "# cairn-derived: source-hash=" + hashA64 + " source-path=docs/superpowers/specs/2026-04-21-design.md derived-at=2026-04-21T09:14:07Z\n"
	validBodyAfter = "requirement:\n  id: REQ-001\n"
)

func TestParseSourceHashHeader_Valid(t *testing.T) {
	h, err := ParseSourceHashHeader([]byte(validHeaderLF + validBodyAfter))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.SourceHash != hashA64 {
		t.Errorf("SourceHash: got %q want %q", h.SourceHash, hashA64)
	}
	if h.SourcePath != "docs/superpowers/specs/2026-04-21-design.md" {
		t.Errorf("SourcePath: got %q", h.SourcePath)
	}
	if h.DerivedAt != "2026-04-21T09:14:07Z" {
		t.Errorf("DerivedAt: got %q", h.DerivedAt)
	}
}

func TestParseSourceHashHeader_CRLF(t *testing.T) {
	body := strings.Replace(validHeaderLF, "\n", "\r\n", 1) + validBodyAfter
	if _, err := ParseSourceHashHeader([]byte(body)); err != nil {
		t.Errorf("CRLF header should parse: %v", err)
	}
}

func TestParseSourceHashHeader_SingleLineNoTrailingNewline(t *testing.T) {
	body := strings.TrimSuffix(validHeaderLF, "\n")
	if _, err := ParseSourceHashHeader([]byte(body)); err != nil {
		t.Errorf("header without trailing newline should parse: %v", err)
	}
}

func TestParseSourceHashHeader_MissingHeader(t *testing.T) {
	_, err := ParseSourceHashHeader([]byte(validBodyAfter))
	if !errors.Is(err, ErrNoSourceHashHeader) {
		t.Errorf("want ErrNoSourceHashHeader, got %v", err)
	}
}

func TestParseSourceHashHeader_ShortHash(t *testing.T) {
	// 63 hex chars instead of 64 — regex mandates exactly 64.
	body := "# cairn-derived: source-hash=" + strings.Repeat("a", 63) + " source-path=docs/x.md derived-at=2026-04-21T09:14:07Z\n"
	_, err := ParseSourceHashHeader([]byte(body))
	if !errors.Is(err, ErrNoSourceHashHeader) {
		t.Errorf("short hash must not parse: %v", err)
	}
}

func TestParseSourceHashHeader_WhitespaceInSourcePath(t *testing.T) {
	// source-path uses \S+ so a space breaks the match.
	body := "# cairn-derived: source-hash=" + hashA64 + " source-path=docs/bad path.md derived-at=2026-04-21T09:14:07Z\n"
	_, err := ParseSourceHashHeader([]byte(body))
	if !errors.Is(err, ErrNoSourceHashHeader) {
		t.Errorf("whitespace in source-path must not parse: %v", err)
	}
}

func TestParseSourceHashHeader_MissingZ(t *testing.T) {
	body := "# cairn-derived: source-hash=" + hashA64 + " source-path=docs/x.md derived-at=2026-04-21T09:14:07\n"
	_, err := ParseSourceHashHeader([]byte(body))
	if !errors.Is(err, ErrNoSourceHashHeader) {
		t.Errorf("derived-at must end in Z: %v", err)
	}
}

func TestParseSourceHashHeader_Empty(t *testing.T) {
	_, err := ParseSourceHashHeader([]byte(""))
	if !errors.Is(err, ErrNoSourceHashHeader) {
		t.Errorf("empty input must report ErrNoSourceHashHeader, got %v", err)
	}
}

// sha256("hello\n") = 5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03
const helloNewlineSha = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"

func TestComputeSourceHash_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prose.md")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ComputeSourceHash(path)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if got != helloNewlineSha {
		t.Errorf("got %q want %q", got, helloNewlineSha)
	}
}

func TestComputeSourceHash_Missing(t *testing.T) {
	_, err := ComputeSourceHash(filepath.Join(t.TempDir(), "nope.md"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want os.ErrNotExist, got %v", err)
	}
}

func TestComputeSourceHash_Drift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prose.md")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := ComputeSourceHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := ComputeSourceHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Errorf("drift: expected different hashes, got %q twice", h1)
	}
}

func TestComputeSourceHash_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	// Write 1 MiB of deterministic content. Catches any off-by-one
	// in the io.Copy-based hasher vs a single ReadFile-and-sum approach.
	var b strings.Builder
	for b.Len() < 1<<20 {
		b.WriteString("cairn\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := ComputeSourceHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 {
		t.Errorf("hash length: got %d want 64", len(h))
	}
}
