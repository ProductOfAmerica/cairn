package evidence

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// Store owns the evidence table and blob store access. It is bound to an
// externally-managed transaction; the caller opens the txn and Store runs
// inside it.
type Store struct {
	tx       *db.Tx
	events   events.Appender
	ids      *ids.Generator
	blobRoot string
	clock    clock.Clock
}

// NewStore returns a Store bound to the given transaction.
func NewStore(tx *db.Tx, a events.Appender, g *ids.Generator, blobRoot string, c clock.Clock) *Store {
	return &Store{tx: tx, events: a, ids: g, blobRoot: blobRoot, clock: c}
}

// PutResult is the return value from Put.
type PutResult struct {
	ID          string `json:"evidence_id"`
	SHA256      string `json:"sha256"`
	URI         string `json:"uri"`
	Bytes       int64  `json:"bytes"`
	ContentType string `json:"content_type"`
	Dedupe      bool   `json:"dedupe"`
}

// GetResult is the return value from Get.
type GetResult struct {
	ID          string `json:"evidence_id"`
	SHA256      string `json:"sha256"`
	URI         string `json:"uri"`
	Bytes       int64  `json:"bytes"`
	ContentType string `json:"content_type"`
	CreatedAt   int64  `json:"created_at"`
}

// Put reads the file at path, sha256-hashes it, writes it atomically to the
// blob store, inserts (or deduplicates) the evidence row, emits an
// evidence_stored event, and returns a PutResult.
//
// If contentType is empty, it defaults to "application/octet-stream".
func (s *Store) Put(opID, path, contentType string) (PutResult, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Pass 1: stream-hash to compute sha + byteCount + blobPath. We can't
	// pick the blob path until we know the sha (BlobPath shards by sha[:2]),
	// so two passes over the source file are unavoidable. The first pass
	// only allocates a few KB of sha256 state.
	src, err := os.Open(path)
	if err != nil {
		return PutResult{}, fmt.Errorf("open file %q: %w", path, err)
	}
	hasher := sha256.New()
	byteCount, copyErr := io.Copy(hasher, src)
	closeErr := src.Close()
	if copyErr != nil {
		return PutResult{}, fmt.Errorf("hash file %q: %w", path, copyErr)
	}
	if closeErr != nil {
		return PutResult{}, fmt.Errorf("close file %q: %w", path, closeErr)
	}
	sha := hex.EncodeToString(hasher.Sum(nil))
	blobPath := BlobPath(s.blobRoot, sha)

	// Pass 2: stream-write to the dedupe/atomic blob path. WriteAtomicStream
	// returns the sha256 it observed during the copy; we cross-check it
	// against pass 1's sha so that if the user's source file mutated
	// between passes we refuse the write with source_mutated rather than
	// silently storing different bytes at a path derived from pass 1's sha.
	src2, err := os.Open(path)
	if err != nil {
		return PutResult{}, fmt.Errorf("open file %q: %w", path, err)
	}
	n, sum, werr := WriteAtomicStream(blobPath, src2)
	closeErr2 := src2.Close()
	if werr != nil {
		return PutResult{}, fmt.Errorf("write blob: %w", werr)
	}
	if closeErr2 != nil {
		return PutResult{}, fmt.Errorf("close file %q: %w", path, closeErr2)
	}
	if mutErr := crossCheckPutShas(path, sha, sum, byteCount, n); mutErr != nil {
		// TOCTOU: source file changed between pass 1 (hash) and pass 2
		// (write). The bytes on disk now hash to a different sha than the
		// path derived from pass 1's sha. Refuse the write.
		// TODO: integration test for source_mutated requires a deterministic
		// mid-write mutation harness that does not currently exist; the
		// substrate error shape is exercised by unit tests in this package.
		return PutResult{}, mutErr
	}

	// URI is the blob path absolute on the host.
	uri := blobPath

	// Check for existing evidence row (dedupe).
	var existingID string
	err = s.tx.QueryRow(
		`SELECT id FROM evidence WHERE sha256 = ?`, sha,
	).Scan(&existingID)

	var result PutResult
	if err == nil {
		// Row already exists — full dedupe.
		result = PutResult{
			ID:          existingID,
			SHA256:      sha,
			Bytes:       byteCount,
			ContentType: contentType,
			Dedupe:      true,
		}
	} else if err == sql.ErrNoRows {
		// Insert new row.
		newID := s.ids.ULID()
		createdAt := s.clock.NowMilli()
		_, insErr := s.tx.Exec(
			`INSERT INTO evidence (id, sha256, uri, bytes, content_type, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(sha256) DO NOTHING`,
			newID, sha, uri, byteCount, contentType, createdAt,
		)
		if insErr != nil {
			return PutResult{}, fmt.Errorf("insert evidence: %w", insErr)
		}

		// Check if insert landed or was lost to a race-window conflict.
		var finalID string
		if scanErr := s.tx.QueryRow(
			`SELECT id FROM evidence WHERE sha256 = ?`, sha,
		).Scan(&finalID); scanErr != nil {
			return PutResult{}, fmt.Errorf("re-fetch evidence id: %w", scanErr)
		}

		dedupe := finalID != newID
		result = PutResult{
			ID:          finalID,
			SHA256:      sha,
			Bytes:       byteCount,
			ContentType: contentType,
			Dedupe:      dedupe,
		}
	} else {
		return PutResult{}, fmt.Errorf("query evidence: %w", err)
	}

	// Emit evidence_stored event regardless of dedupe.
	if err := s.events.Append(s.tx, events.Record{
		Kind:       "evidence_stored",
		EntityKind: "evidence",
		EntityID:   result.ID,
		OpID:       opID,
		Payload: map[string]any{
			"sha256":       sha,
			"bytes":        byteCount,
			"content_type": contentType,
			"dedupe":       result.Dedupe,
		},
	}); err != nil {
		return PutResult{}, err
	}

	return result, nil
}

// Verify reads the blob for the given sha256, recomputes its hash, and
// returns nil if it matches. On mismatch it emits evidence_invalidated and
// returns a CodeSubstrate error.
//
// If the evidence row has been invalidated (invalidated_at IS NOT NULL),
// Verify returns a CodeValidation error with kind "evidence_invalidated"
// BEFORE touching the blob. This blocks verdict reports from citing stale
// evidence that a reconcile has already flagged.
func (s *Store) Verify(sha string) error {
	var evidenceID, uri string
	var invalidatedAt sql.NullInt64
	if err := s.tx.QueryRow(
		`SELECT id, uri, invalidated_at FROM evidence WHERE sha256 = ?`, sha,
	).Scan(&evidenceID, &uri, &invalidatedAt); err != nil {
		if err == sql.ErrNoRows {
			return cairnerr.New(cairnerr.CodeNotFound, "not_stored",
				fmt.Sprintf("no evidence row for sha256 %s", sha))
		}
		return fmt.Errorf("query evidence for verify: %w", err)
	}

	if invalidatedAt.Valid {
		return cairnerr.New(cairnerr.CodeValidation, "evidence_invalidated",
			"evidence was invalidated by a prior reconcile").
			WithDetails(map[string]any{
				"evidence_id":    evidenceID,
				"invalidated_at": invalidatedAt.Int64,
			})
	}

	f, err := os.Open(uri)
	if err != nil {
		return fmt.Errorf("open blob %q: %w", uri, err)
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("hash blob %q: %w", uri, err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got == sha {
		return nil
	}

	// Corruption detected — emit event then return error.
	// Best-effort event emission: the substrate error returned below is
	// what callers act on. If the events table insert fails here, we still
	// surface the hash mismatch — the corruption fact is what matters.
	// Invariant 10 carries an explicit exception for this site (see docs/PLAN.md).
	_ = s.events.Append(s.tx, events.Record{
		Kind:       "evidence_invalidated",
		EntityKind: "evidence",
		EntityID:   evidenceID,
		Payload: map[string]any{
			"sha256_expected": sha,
			"sha256_actual":   got,
		},
	})
	return cairnerr.New(cairnerr.CodeSubstrate, "evidence_hash_mismatch",
		fmt.Sprintf("blob sha256 mismatch: expected %s got %s", sha, got))
}

// crossCheckPutShas compares the sha computed in Put's pass-1 (used to
// derive blobPath and stored on the evidence row) against the sha returned
// by WriteAtomicStream during pass-2. Returns nil if they match. On
// mismatch returns a CodeSubstrate "source_mutated" error whose Details
// carry both shas and both byte counts so operators can diagnose the race.
//
// Factored out of Put for direct unit testing — racing the file system to
// reproduce a real TOCTOU mid-write is not portable across platforms, so
// the helper boundary is the deterministic test seam.
func crossCheckPutShas(path, pass1Sha string, pass2Sum [sha256.Size]byte, pass1Bytes, pass2Bytes int64) error {
	pass2Sha := hex.EncodeToString(pass2Sum[:])
	if pass2Sha == pass1Sha {
		return nil
	}
	return cairnerr.New(cairnerr.CodeSubstrate, "source_mutated",
		fmt.Sprintf("source file %q mutated between hash and write passes", path)).
		WithDetails(map[string]any{
			"path":        path,
			"pass1_sha":   pass1Sha,
			"pass2_sha":   pass2Sha,
			"pass1_bytes": pass1Bytes,
			"pass2_bytes": pass2Bytes,
		})
}

// Get returns the evidence row for the given sha256.
func (s *Store) Get(sha string) (GetResult, error) {
	var res GetResult
	err := s.tx.QueryRow(
		`SELECT id, sha256, uri, bytes, content_type, created_at
		 FROM evidence WHERE sha256 = ?`, sha,
	).Scan(&res.ID, &res.SHA256, &res.URI, &res.Bytes, &res.ContentType, &res.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return GetResult{}, cairnerr.New(cairnerr.CodeNotFound, "evidence_not_found",
				fmt.Sprintf("no evidence row for sha256 %s", sha))
		}
		return GetResult{}, fmt.Errorf("get evidence: %w", err)
	}
	return res, nil
}
