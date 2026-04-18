package task

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// ClaimInput is what the CLI passes into Claim.
type ClaimInput struct {
	OpID    string
	TaskID  string
	AgentID string
	TTLMs   int64
}

// ClaimResult echoes the claim to the caller.
type ClaimResult struct {
	ClaimID   string `json:"claim_id"`
	RunID     string `json:"run_id"`
	TaskID    string `json:"task_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// Claim acquires a lease on a task. Runs inline rule-1 cleanup first, then
// checks deps, then CAS-flips the task to claimed, then inserts claim + run.
func (s *Store) Claim(in ClaimInput) (ClaimResult, error) {
	// Idempotency check.
	cached, hit, err := s.CheckOpLog(in.OpID, "task.claim")
	if err != nil {
		return ClaimResult{}, err
	}
	if hit {
		var r ClaimResult
		_ = json.Unmarshal(cached, &r)
		return r, nil
	}

	now := s.clock.NowMilli()

	// Rule 1 inline: expire stale leases, revert tasks whose only claim expired.
	if err := s.expireStaleLeases(now); err != nil {
		return ClaimResult{}, err
	}

	// Dep check — INSIDE THE SAME TXN (TOCTOU-safe).
	if err := s.checkDepsDone(in.TaskID); err != nil {
		return ClaimResult{}, err
	}

	// CAS flip to claimed.
	res, err := s.tx.Exec(
		`UPDATE tasks SET status='claimed', updated_at=? WHERE id=? AND status='open'`,
		now, in.TaskID,
	)
	if err != nil {
		return ClaimResult{}, err
	}
	changed, _ := res.RowsAffected()
	if changed == 0 {
		// Determine current status for a helpful error.
		var status string
		err := s.tx.QueryRow("SELECT status FROM tasks WHERE id=?", in.TaskID).Scan(&status)
		if err == sql.ErrNoRows {
			return ClaimResult{}, cairnerr.New(cairnerr.CodeNotFound, "task_not_found",
				fmt.Sprintf("task %q", in.TaskID))
		}
		return ClaimResult{}, cairnerr.New(cairnerr.CodeConflict, "task_not_claimable",
			fmt.Sprintf("task %q status=%s", in.TaskID, status)).
			WithDetails(map[string]any{"current_status": status})
	}

	claimID := s.ids.ULID()
	runID := s.ids.ULID()
	expiresAt := now + in.TTLMs

	if _, err := s.tx.Exec(
		`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		claimID, in.TaskID, in.AgentID, now, expiresAt, in.OpID,
	); err != nil {
		return ClaimResult{}, err
	}
	if _, err := s.tx.Exec(
		`INSERT INTO runs (id, task_id, claim_id, started_at) VALUES (?, ?, ?, ?)`,
		runID, in.TaskID, claimID, now,
	); err != nil {
		return ClaimResult{}, err
	}

	// Emit events (ordered).
	evs := []events.Record{
		{Kind: "claim_acquired", EntityKind: "claim", EntityID: claimID,
			Payload: map[string]any{
				"task_id": in.TaskID, "agent_id": in.AgentID,
				"expires_at": expiresAt,
			}, OpID: in.OpID},
		{Kind: "run_started", EntityKind: "run", EntityID: runID,
			Payload: map[string]any{"claim_id": claimID, "task_id": in.TaskID},
			OpID:    in.OpID},
		{Kind: "task_status_changed", EntityKind: "task", EntityID: in.TaskID,
			Payload: map[string]any{
				"from": "open", "to": "claimed", "reason": "claim",
			}, OpID: in.OpID},
	}
	for _, e := range evs {
		if err := s.events.Append(s.tx, e); err != nil {
			return ClaimResult{}, err
		}
	}

	result := ClaimResult{
		ClaimID: claimID, RunID: runID, TaskID: in.TaskID, ExpiresAt: expiresAt,
	}
	payload, _ := json.Marshal(result)
	if err := s.RecordOpLog(in.OpID, "task.claim", payload); err != nil {
		return ClaimResult{}, err
	}
	return result, nil
}

// expireStaleLeases flips expired-but-not-released claims to released and
// reverts any task whose only live claim just expired.
func (s *Store) expireStaleLeases(now int64) error {
	// Find expired live claims.
	rows, err := s.tx.Query(
		`SELECT id, task_id FROM claims WHERE expires_at < ? AND released_at IS NULL`,
		now,
	)
	if err != nil {
		return err
	}
	type expiring struct{ claimID, taskID string }
	var ex []expiring
	for rows.Next() {
		var e expiring
		_ = rows.Scan(&e.claimID, &e.taskID)
		ex = append(ex, e)
	}
	rows.Close()

	for _, e := range ex {
		if _, err := s.tx.Exec(
			`UPDATE claims SET released_at=? WHERE id=?`,
			now, e.claimID,
		); err != nil {
			return err
		}
		if err := s.events.Append(s.tx, events.Record{
			Kind: "claim_released", EntityKind: "claim", EntityID: e.claimID,
			Payload: map[string]any{"reason": "expired"},
		}); err != nil {
			return err
		}
		// If no other live claims, revert the task.
		var liveCount int
		_ = s.tx.QueryRow(
			`SELECT count(*) FROM claims WHERE task_id=? AND released_at IS NULL`,
			e.taskID,
		).Scan(&liveCount)
		if liveCount == 0 {
			var prev string
			_ = s.tx.QueryRow(`SELECT status FROM tasks WHERE id=?`, e.taskID).Scan(&prev)
			if prev == "claimed" || prev == "in_progress" || prev == "gate_pending" {
				if _, err := s.tx.Exec(
					`UPDATE tasks SET status='open', updated_at=? WHERE id=?`,
					now, e.taskID,
				); err != nil {
					return err
				}
				if err := s.events.Append(s.tx, events.Record{
					Kind: "task_status_changed", EntityKind: "task", EntityID: e.taskID,
					Payload: map[string]any{
						"from": prev, "to": "open", "reason": "lease_expired",
					},
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Store) checkDepsDone(taskID string) error {
	var depsJSON string
	err := s.tx.QueryRow(
		`SELECT depends_on_json FROM tasks WHERE id=?`, taskID,
	).Scan(&depsJSON)
	if err == sql.ErrNoRows {
		return cairnerr.New(cairnerr.CodeNotFound, "task_not_found",
			fmt.Sprintf("task %q", taskID))
	}
	if err != nil {
		return err
	}
	var deps []string
	_ = json.Unmarshal([]byte(depsJSON), &deps)
	if len(deps) == 0 {
		return nil
	}

	placeholders := ""
	args := []any{}
	for i, d := range deps {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, d)
	}
	rows, err := s.tx.Query(
		"SELECT id, status FROM tasks WHERE id IN ("+placeholders+") AND status != 'done'",
		args...,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	var blocking []map[string]any
	for rows.Next() {
		var id, status string
		_ = rows.Scan(&id, &status)
		blocking = append(blocking, map[string]any{"id": id, "status": status})
	}
	if len(blocking) > 0 {
		return cairnerr.New(cairnerr.CodeConflict, "dep_not_done",
			fmt.Sprintf("task %q blocked by %d dependency(ies)", taskID, len(blocking))).
			WithDetails(map[string]any{"blocking": blocking})
	}
	return nil
}
