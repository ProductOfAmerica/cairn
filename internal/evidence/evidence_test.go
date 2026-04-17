package evidence_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// openDB creates a temp-file SQLite database for tests.
func openDB(t *testing.T) *db.DB {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "cairn-test-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	path := f.Name()
	f.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

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

func TestPut_StoresBlobAndRow(t *testing.T) {
	d := openDB(t)
	blobRoot := t.TempDir()
	clk := clock.NewFake(1_000_000)

	// Write a temp file to put.
	f, err := os.CreateTemp(t.TempDir(), "evidence-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("test evidence content")
	if _, err := f.Write(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	path := f.Name()

	var res evidence.PutResult
	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		r, err := store.Put("op-001", path, "")
		res = r
		return err
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if res.ContentType != "application/octet-stream" {
		t.Errorf("default content-type: got %q want %q", res.ContentType, "application/octet-stream")
	}
	if res.Bytes != int64(len(content)) {
		t.Errorf("bytes: got %d want %d", res.Bytes, len(content))
	}
	if res.SHA256 == "" {
		t.Error("sha256 is empty")
	}
	if res.ID == "" {
		t.Error("id is empty")
	}
	if res.Dedupe {
		t.Error("first put should not be dedupe")
	}

	// Blob must exist on disk.
	blobPath := evidence.BlobPath(blobRoot, res.SHA256)
	if _, err := os.Stat(blobPath); err != nil {
		t.Errorf("blob not on disk at %s: %v", blobPath, err)
	}

	// Second put of same content in a new txn: dedupe.
	var res2 evidence.PutResult
	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		r, err := store.Put("op-002", path, "text/plain")
		res2 = r
		return err
	})
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if !res2.Dedupe {
		t.Error("second put of same content should be dedupe")
	}
	if res2.ID != res.ID {
		t.Errorf("dedupe should return same evidence_id: got %q want %q", res2.ID, res.ID)
	}
	if res2.SHA256 != res.SHA256 {
		t.Errorf("sha256 mismatch on dedupe: got %q want %q", res2.SHA256, res.SHA256)
	}
}

func TestVerify_HashMatch(t *testing.T) {
	d := openDB(t)
	blobRoot := t.TempDir()
	clk := clock.NewFake(1_000_000)

	f, err := os.CreateTemp(t.TempDir(), "evidence-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("verify-me")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var sha string
	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		r, err := store.Put("op-v1", f.Name(), "")
		sha = r.SHA256
		return err
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		return store.Verify(sha)
	})
	if err != nil {
		t.Errorf("Verify after Put should succeed, got: %v", err)
	}
}

func TestVerify_DetectsCorruption(t *testing.T) {
	d := openDB(t)
	blobRoot := t.TempDir()
	clk := clock.NewFake(1_000_000)

	f, err := os.CreateTemp(t.TempDir(), "evidence-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("original content")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var res evidence.PutResult
	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		r, err := store.Put("op-c1", f.Name(), "")
		res = r
		return err
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Tamper with the blob directly.
	blobPath := evidence.BlobPath(blobRoot, res.SHA256)
	if err := os.WriteFile(blobPath, []byte("tampered content!"), 0o600); err != nil {
		t.Fatalf("tamper blob: %v", err)
	}

	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		store := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot)
		return store.Verify(res.SHA256)
	})
	if err == nil {
		t.Fatal("Verify should detect corruption and return an error")
	}
}
