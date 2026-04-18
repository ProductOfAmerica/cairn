// Package evidence owns the content-addressed blob store and the evidence
// table. Blobs live at <state-root>/<repo-id>/blobs/<sha[:2]>/<sha>.
package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// BlobPath returns the on-disk location for a blob with the given sha.
// The sha must be lowercase hex (64 chars); BlobPath does not validate.
func BlobPath(blobRoot, sha string) string {
	if len(sha) < 2 {
		return filepath.Join(blobRoot, "__bad__", sha)
	}
	return filepath.Join(blobRoot, sha[:2], sha)
}

// WriteAtomic writes data to dst via a temp file + rename. On Windows,
// os.Rename fails if dst exists, so we pre-check: if dst is present and
// its sha256 matches data's sha256, this is a dedupe (no error). If dst
// is present with different content, returns a blob_collision error.
//
// Returns the number of bytes written (0 on dedupe).
func WriteAtomic(dst string, data []byte) (int, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, fmt.Errorf("mkdir %q: %w", filepath.Dir(dst), err)
	}
	srcSum := sha256.Sum256(data)

	if info, err := os.Stat(dst); err == nil && !info.IsDir() {
		// Destination exists — check content identity.
		existing, err := os.ReadFile(dst)
		if err != nil {
			return 0, fmt.Errorf("read existing blob: %w", err)
		}
		existingSum := sha256.Sum256(existing)
		if bytes.Equal(existingSum[:], srcSum[:]) {
			return 0, nil // dedupe
		}
		return 0, cairnerr.New(cairnerr.CodeSubstrate, "blob_collision",
			fmt.Sprintf("existing blob at %s has different content", dst)).
			WithDetails(map[string]any{
				"path":         dst,
				"existing_sha": hex.EncodeToString(existingSum[:]),
				"new_sha":      hex.EncodeToString(srcSum[:]),
			})
	}

	// Fresh write.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// On error path, try to clean up the temp file.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return 0, fmt.Errorf("rename temp→final: %w", err)
	}
	return len(data), nil
}
