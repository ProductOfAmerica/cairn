package reconcile

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// Rule2FlipStaleTasks iterates over `done` tasks and flips any whose
// required gates include a drifted/missing/non-passing latest verdict to
// `status='stale'`. Reuses verdict.Store.IsFreshPass (Ship 1, tested) for
// the per-gate check.
//
// Implementation is a Go loop over tasks × gates. Design spec §5.4 flags
// this as a Ship 4 optimization candidate if rule_2_latency_ms exceeds
// 100ms in dogfood.
func Rule2FlipStaleTasks(tx *db.Tx, appender events.Appender, clk clock.Clock, reconcileID string) (RuleResult, error) {
	start := time.Now()
	result := RuleResult{Rule: 2}
	now := clk.NowMilli()

	// Pull all done tasks + their required_gates arrays.
	rows, err := tx.Query(`SELECT id, required_gates_json FROM tasks WHERE status='done'`)
	if err != nil {
		return result, fmt.Errorf("select done tasks: %w", err)
	}

	type doneTask struct {
		ID      string
		GateIDs []string
	}
	var tasks []doneTask
	for rows.Next() {
		var t doneTask
		var gatesJSON string
		if err := rows.Scan(&t.ID, &gatesJSON); err != nil {
			rows.Close()
			return result, err
		}
		if err := json.Unmarshal([]byte(gatesJSON), &t.GateIDs); err != nil {
			rows.Close()
			return result, fmt.Errorf("unmarshal required_gates_json for %s: %w", t.ID, err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	// verdict.Store is constructed with a nil evidence store because rule 2
	// only calls IsFreshPass/Latest (reads). No evidence.Verify is invoked
	// in these paths. The generator is only used by Report, also unused here.
	vstore := verdict.NewStore(tx, appender, (*ids.Generator)(nil), nil, clk)

	var flippedIDs []string
	for _, t := range tasks {
		stale := false
		for _, gateID := range t.GateIDs {
			fresh, _, err := vstore.IsFreshPass(gateID)
			if err != nil {
				return result, fmt.Errorf("IsFreshPass %s/%s: %w", t.ID, gateID, err)
			}
			if !fresh {
				stale = true
				break
			}
		}
		if !stale {
			continue
		}

		if _, err := tx.Exec(
			`UPDATE tasks SET status='stale', updated_at=? WHERE id=? AND status='done'`,
			now, t.ID,
		); err != nil {
			return result, fmt.Errorf("flip stale: %w", err)
		}
		if err := appender.Append(tx, events.Record{
			Kind:       "task_status_changed",
			EntityKind: "task",
			EntityID:   t.ID,
			Payload: map[string]any{
				"from":   "done",
				"to":     "stale",
				"reason": "spec_drift",
			},
		}); err != nil {
			return result, err
		}
		result.Mutations = append(result.Mutations, Mutation{
			Rule: 2, EntityID: t.ID, Action: "flip_stale", Reason: "spec_drift",
		})
		flippedIDs = append(flippedIDs, t.ID)
	}

	result.Stats.Rule2TasksFlippedStale = len(flippedIDs)
	result.Stats.Rule2LatencyMs = time.Since(start).Milliseconds()

	if len(flippedIDs) > 0 {
		if err := appender.Append(tx, events.Record{
			Kind:       "reconcile_rule_applied",
			EntityKind: "reconcile",
			EntityID:   reconcileID,
			Payload: map[string]any{
				"rule":                2,
				"affected_entity_ids": flippedIDs,
			},
		}); err != nil {
			return result, err
		}
	}

	return result, nil
}
