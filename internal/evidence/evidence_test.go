package evidence_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/evidence"
)

func TestBlobPath_ShardsByFirstTwoHex(t *testing.T) {
	p := evidence.BlobPath("/root", "abcdef0123456789")
	want := filepath.ToSlash("/root/ab/abcdef0123456789")
	got := filepath.ToSlash(p)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWriteAtomic_NewFile(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "ab", "abcdef")
	n, err := evidence.WriteAtomic(dst, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes written, got %d", n)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "hello" {
		t.Fatalf("content mismatch: %q", string(b))
	}
}

func TestWriteAtomic_RenameExistsSameContentDedupes(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "ab", "abcdef")
	n, err := evidence.WriteAtomic(dst, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes written, got %d", n)
	}
	// Second write: existing file has same sha as input bytes → dedupe.
	dup, err := evidence.WriteAtomic(dst, []byte("hello"))
	if err != nil {
		t.Fatalf("dedupe write should not error: %v", err)
	}
	if dup != 0 {
		t.Fatalf("dedupe write should return 0 bytes, got %d", dup)
	}
}

func TestWriteAtomic_RenameExistsDifferentContentFails(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "ab", "abcdef")
	// Pre-populate with different content.
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	_ = os.WriteFile(dst, []byte("other content"), 0o644)
	_, err := evidence.WriteAtomic(dst, []byte("hello"))
	if err == nil {
		t.Fatal("expected blob_collision error for mismatched existing content")
	}
}
