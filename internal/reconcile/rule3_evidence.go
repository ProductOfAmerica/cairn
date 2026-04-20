package reconcile

import (
	"context"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// Rule3ApplyEvidenceInvalidations consumes probe-phase candidates, re-stats
// each one inside the tx (design spec §5.5 re-stat invariant), and issues
// UPDATE evidence SET invalidated_at for survivors.
//
// Re-stat invariant: both file presence AND hash match must FAIL to invalidate.
// A file that is now present and hashes cleanly → skip (probe was stale).
//
// Idempotent: `invalidated_at IS NULL` guard in WHERE prevents double-flipping.
func Rule3ApplyEvidenceInvalidations(
	tx *db.Tx,
	appender events.Appender,
	clk clock.Clock,
	reconcileID string,
	candidates []EvidenceCandidate,
) (RuleResult, error) {
	result := RuleResult{Rule: 3}
	if len(candidates) == 0 {
		return result, nil
	}
	now := clk.NowMilli()

	var affected []string
	for _, c := range candidates {
		// Re-stat: confirm the candidate's condition still holds.
		reason, stillInvalid, err := reStatInvalid(c.URI, c.Sha256)
		if err != nil {
			return result, err
		}
		if !stillInvalid {
			// File present + hash matches; probe was stale, skip.
			continue
		}

		// Single-column UPDATE; triggers permit it. Guard on
		// invalidated_at IS NULL keeps this idempotent.
		res, err := tx.Exec(
			`UPDATE evidence SET invalidated_at = ?
			 WHERE id = ? AND invalidated_at IS NULL`,
			now, c.EvidenceID,
		)
		if err != nil {
			return result, fmt.Errorf("invalidate evidence %s: %w", c.EvidenceID, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue // already invalidated by a concurrent/prior run
		}

		if err := appender.Append(tx, events.Record{
			Kind:       "evidence_invalidated",
			EntityKind: "evidence",
			EntityID:   c.EvidenceID,
			Payload: map[string]any{
				"reason": reason,
				"sha256": c.Sha256,
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 3, EntityID: c.EvidenceID, Action: "invalidate", Reason: reason,
		})
		affected = append(affected, c.EvidenceID)
	}
	result.Stats.Rule3EvidenceInvalid = len(affected)

	if len(affected) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                3,
				"affected_entity_ids": affected,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}

// reStatInvalid returns (reason, true, nil) if the blob at uri is still
// missing or still hash-mismatched vs expected. Returns ("", false, nil) if
// the blob is present AND matches — probe was stale. Returns ("", false, err)
// if an I/O error prevents re-stat from deciding — caller must abort.
//
// Uses context.Background(): re-stat happens inside the mutation tx where
// candidates are already pre-filtered/small; cancellable hashing matters in
// the probe phase (RunEvidenceProbe), not here.
func reStatInvalid(uri, expected string) (string, bool, error) {
	r, ok, err := checkBlob(context.Background(), uri, expected)
	if err != nil {
		return "", false, err
	}
	if ok {
		return "", false, nil
	}
	return r, true, nil
}
