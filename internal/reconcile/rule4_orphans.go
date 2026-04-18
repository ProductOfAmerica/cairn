package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// orphanGraceMs is the fixed 10-minute grace window from the claim's
// released_at before rule 4 marks the associated run orphaned.
// Design spec §5.6 / Q6.
const orphanGraceMs = 10 * 60 * 1000

// Rule4OrphanExpiredRuns sweeps in-progress runs whose claim was released
// more than orphanGraceMs ago. Sets runs.ended_at = now, outcome='orphaned',
// emits run_ended events.
//
// INVARIANT: must run AFTER rule 1 in the same tx. Rule 1 populates
// claims.released_at on expired leases with now; rule 4's 10-min grace
// correctly misses those (absorbs clock skew, gives agents a chance to
// finish a run whose heartbeat just failed).
func Rule4OrphanExpiredRuns(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	result := RuleResult{Rule: 4}
	now := clk.NowMilli()

	// SELECT candidate runs first so we can emit events per-row.
	rows, err := tx.Query(
		`SELECT runs.id, runs.task_id
		 FROM runs
		 JOIN claims ON claims.id = runs.claim_id
		 WHERE runs.ended_at IS NULL
		   AND claims.released_at IS NOT NULL
		   AND claims.released_at + ? < ?`,
		orphanGraceMs, now,
	)
	if err != nil {
		return result, fmt.Errorf("select orphan candidates: %w", err)
	}
	type row struct{ ID, TaskID string }
	var picked []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.TaskID); err != nil {
			rows.Close()
			return result, err
		}
		picked = append(picked, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	var ids []string
	for _, r := range picked {
		if _, err := tx.Exec(
			`UPDATE runs SET ended_at = ?, outcome = 'orphaned' WHERE id = ?`,
			now, r.ID,
		); err != nil {
			return result, fmt.Errorf("orphan run: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "run_ended",
			EntityKind: "run",
			EntityID:   r.ID,
			Payload: map[string]any{
				"task_id": r.TaskID,
				"outcome": "orphaned",
				"reason":  "grace_expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 4, EntityID: r.ID, Action: "orphan", Reason: "grace_expired",
		})
		ids = append(ids, r.ID)
	}
	result.Stats.Rule4RunsOrphaned = len(ids)

	if len(ids) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                4,
				"affected_entity_ids": ids,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
