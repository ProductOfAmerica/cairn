package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// Rule1ReleaseExpiredLeases releases expired claims and reverts their tasks
// to 'open' if no live claim remains.
//
// INVARIANT: runs inside BEGIN IMMEDIATE. SQLite holds RESERVED/WRITE lock
// from start-of-tx; no concurrent writer can interleave between the two
// statements below. The NOT IN subquery is race-free under this serialization.
func Rule1ReleaseExpiredLeases(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	now := clk.NowMilli()
	result := RuleResult{Rule: 1}

	// 1) Release expired claims, capture (id, task_id) for event emission.
	rows, err := tx.Query(
		`UPDATE claims SET released_at = ?
		 WHERE expires_at < ? AND released_at IS NULL
		 RETURNING id, task_id`,
		now, now,
	)
	if err != nil {
		return result, fmt.Errorf("release expired claims: %w", err)
	}

	type releasedClaim struct{ ID, TaskID string }
	var released []releasedClaim
	for rows.Next() {
		var rc releasedClaim
		if err := rows.Scan(&rc.ID, &rc.TaskID); err != nil {
			rows.Close()
			return result, err
		}
		released = append(released, rc)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	for _, rc := range released {
		if err := appender.Append(tx, events.Record{
			Kind:       "claim_released",
			EntityKind: "claim",
			EntityID:   rc.ID,
			Payload: map[string]any{
				"task_id": rc.TaskID,
				"reason":  "expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 1, EntityID: rc.ID, Action: "release", Reason: "expired",
		})
	}
	result.Stats.Rule1ClaimsReleased = len(released)

	// 2) Revert tasks whose claim is gone. Track which tasks flip so we can
	// emit per-task events. RETURNING gives us the old status via the
	// UPDATE syntax in SQLite (supported since 3.35 via RETURNING clause on
	// the row's pre-UPDATE values is NOT standard). Instead, we first
	// SELECT, then UPDATE.
	sel, err := tx.Query(
		`SELECT id, status FROM tasks
		 WHERE status IN ('claimed','in_progress','gate_pending')
		   AND id NOT IN (
		     SELECT task_id FROM claims
		      WHERE released_at IS NULL AND expires_at >= ?)`,
		now,
	)
	if err != nil {
		return result, fmt.Errorf("select tasks to revert: %w", err)
	}
	type pending struct{ ID, From string }
	var flips []pending
	for sel.Next() {
		var p pending
		if err := sel.Scan(&p.ID, &p.From); err != nil {
			sel.Close()
			return result, err
		}
		flips = append(flips, p)
	}
	sel.Close()

	for _, p := range flips {
		_, err := tx.Exec(
			`UPDATE tasks SET status='open', updated_at=? WHERE id=?`,
			now, p.ID,
		)
		if err != nil {
			return result, fmt.Errorf("revert task: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "task_status_changed",
			EntityKind: "task",
			EntityID:   p.ID,
			Payload: map[string]any{
				"from":   p.From,
				"to":     "open",
				"reason": "lease_expired",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 1, EntityID: p.ID, Action: "revert_to_open", Reason: "lease_expired",
		})
	}
	result.Stats.Rule1TasksReverted = len(flips)

	// 3) Emit reconcile_rule_applied if anything happened.
	if len(released)+len(flips) > 0 {
		affected := make([]string, 0, len(released)+len(flips))
		for _, rc := range released {
			affected = append(affected, rc.ID)
		}
		for _, p := range flips {
			affected = append(affected, p.ID)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                1,
				"affected_entity_ids": affected,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
