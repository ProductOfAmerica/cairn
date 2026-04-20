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

// WriteAtomic is a convenience shim around WriteAtomicStream for callers that
// already have the bytes in memory. It is preserved for in-memory test
// fixtures; production callers (Put) should prefer WriteAtomicStream so
// arbitrary-size payloads do not require a heap buffer. Observable semantics
// (dedupe = 0 bytes, blob_collision substrate error with the same details
// shape) are identical to the streaming form.
func WriteAtomic(dst string, data []byte) (int, error) {
	n, _, err := WriteAtomicStream(dst, bytes.NewReader(data))
	return int(n), err
}

// WriteAtomicStream copies src to dst atomically, computing its sha256
// during the copy via io.MultiWriter so the user payload is never buffered
// fully into RAM. Dedupe and collision semantics: same-size short-circuit
// on collision, hash-on-equal-size dedupe, and blob_collision substrate
// errors with details that include the existing/new shas (or sizes, on
// the size-mismatch fast path).
//
// Returns bytes written (0 on dedupe), the sha256 of the streamed source
// (always populated when the stream completed; even on dedupe), and any
// error.
func WriteAtomicStream(dst string, src io.Reader) (int64, [sha256.Size]byte, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("mkdir %q: %w", filepath.Dir(dst), err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Belt-and-braces cleanup: if we never reach the rename, remove the
	// temp file. os.Remove on an already-renamed path is a no-op error
	// we swallow.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, hasher), src)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("copy to temp: %w", copyErr)
	}
	if syncErr != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("fsync temp: %w", syncErr)
	}
	if closeErr != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("close temp: %w", closeErr)
	}

	var sum [sha256.Size]byte
	copy(sum[:], hasher.Sum(nil))

	// Dedupe / collision check: size-compare first, hash only if sizes
	// agree (different size cannot collide; identical size and hash dedupes).
	if info, err := os.Stat(dst); err == nil && !info.IsDir() {
		if info.Size() != n {
			return 0, sum, cairnerr.New(cairnerr.CodeSubstrate, "blob_collision",
				fmt.Sprintf("existing blob at %s differs in size", dst)).
				WithDetails(map[string]any{
					"path":          dst,
					"existing_size": info.Size(),
					"new_size":      n,
				})
		}
		// Same size — must hash to disambiguate dedupe vs collision.
		f, oerr := os.Open(dst)
		if oerr != nil {
			return 0, sum, fmt.Errorf("open existing blob: %w", oerr)
		}
		eh := sha256.New()
		_, ecopy := io.Copy(eh, f)
		ecloseErr := f.Close()
		if ecopy != nil {
			return 0, sum, fmt.Errorf("hash existing blob: %w", ecopy)
		}
		if ecloseErr != nil {
			return 0, sum, fmt.Errorf("close existing blob: %w", ecloseErr)
		}
		var existingSum [sha256.Size]byte
		copy(existingSum[:], eh.Sum(nil))
		if bytes.Equal(existingSum[:], sum[:]) {
			return 0, sum, nil // dedupe
		}
		return 0, sum, cairnerr.New(cairnerr.CodeSubstrate, "blob_collision",
			fmt.Sprintf("existing blob at %s does not match new content (possible TOCTOU race or genuine collision)", dst)).
			WithDetails(map[string]any{
				"path":         dst,
				"existing_sha": hex.EncodeToString(existingSum[:]),
				"new_sha":      hex.EncodeToString(sum[:]),
			})
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		return 0, sum, fmt.Errorf("rename temp→final: %w", err)
	}
	cleanup = false
	return n, sum, nil
}
