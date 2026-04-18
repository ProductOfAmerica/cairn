// Package reconcile owns `cairn reconcile`. See design spec §5 for the
// contract: probe phase runs OUTSIDE any tx (filesystem I/O only), mutation
// phase runs inside one BEGIN IMMEDIATE. Do not merge the two.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// EvidenceCandidate is a probe finding: one evidence row whose blob is
// missing or whose on-disk content no longer matches evidence.sha256.
type EvidenceCandidate struct {
	EvidenceID string
	Sha256     string
	URI        string
	Reason     string // "missing" | "hash_mismatch"
}

// ProbeOpts controls probe behavior.
type ProbeOpts struct {
	Full      bool    // --evidence-sample-full
	SamplePct float64 // default 0.05
	SampleCap int     // default 100
}

func (o *ProbeOpts) pct() float64 {
	if o.SamplePct > 0 {
		return o.SamplePct
	}
	return 0.05
}

func (o *ProbeOpts) cap() int {
	if o.SampleCap > 0 {
		return o.SampleCap
	}
	return 100
}

// SampleSize computes how many rows the probe will scan for the given opts.
// Full → total row count. Otherwise min(cap, ceil(total * pct)).
// Exposed for tests and for populating the reconcile_ended stats payload.
func SampleSize(h *db.DB, opts ProbeOpts) (int, error) {
	var total int
	if err := h.SQL().QueryRow(`SELECT COUNT(*) FROM evidence`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count evidence: %w", err)
	}
	if opts.Full {
		return total, nil
	}
	n := int(math.Ceil(float64(total) * opts.pct()))
	if n > opts.cap() {
		n = opts.cap()
	}
	return n, nil
}

// RunEvidenceProbe scans evidence rows outside any tx and returns candidates
// for invalidation. Reads evidence rows (read-only SQL) and hashes blob files
// on disk. Does not touch tx-held state.
//
// Candidates ONLY. The mutation phase re-stats each candidate inside the tx
// before writing (see rule 3 implementation for the re-stat defense).
func RunEvidenceProbe(ctx context.Context, h *db.DB, opts ProbeOpts) ([]EvidenceCandidate, error) {
	limit, err := SampleSize(h, opts)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		return nil, nil
	}

	query := `SELECT id, sha256, uri FROM evidence WHERE invalidated_at IS NULL`
	var args []any
	if !opts.Full {
		query += ` ORDER BY RANDOM() LIMIT ?`
		args = append(args, limit)
	}

	rows, err := h.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sample query: %w", err)
	}
	defer rows.Close()

	var out []EvidenceCandidate
	for rows.Next() {
		var c EvidenceCandidate
		if err := rows.Scan(&c.EvidenceID, &c.Sha256, &c.URI); err != nil {
			return nil, err
		}
		reason, ok, err := checkBlob(c.URI, c.Sha256)
		if err != nil {
			return nil, err
		}
		if ok {
			continue
		}
		c.Reason = reason
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// checkBlob returns ("", true, nil) if the file at uri is present and its
// sha256 matches expected. Returns ("missing", false, nil) if the file is
// absent. Returns ("", false, err) on any other I/O error (permissions, FD
// exhaustion, read failure mid-stream) — the caller must abort rather than
// invalidate otherwise-good evidence on a transient OS condition.
func checkBlob(uri, expected string) (string, bool, error) {
	f, err := os.Open(uri)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "missing", false, nil
		}
		return "", false, fmt.Errorf("open blob %q: %w", uri, err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", false, fmt.Errorf("hash blob %q: %w", uri, err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got == expected {
		return "", true, nil
	}
	return "hash_mismatch", false, nil
}
