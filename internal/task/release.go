package task

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

type ReleaseInput struct {
	OpID    string
	ClaimID string
}

// Release marks the claim released, ends any active run, and flips the task
// back to open if no other live claim exists for it.
func (s *Store) Release(in ReleaseInput) error {
	if _, hit, err := s.CheckOpLog(in.OpID, "task.release"); err != nil {
		return err
	} else if hit {
		return nil
	}

	var taskID string
	var released sql.NullInt64
	err := s.tx.QueryRow(
		`SELECT task_id, released_at FROM claims WHERE id=?`, in.ClaimID,
	).Scan(&taskID, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return cairnerr.New(cairnerr.CodeNotFound, "claim_not_found",
			fmt.Sprintf("claim %q", in.ClaimID))
	}
	if err != nil {
		return err
	}
	if released.Valid {
		return cairnerr.New(cairnerr.CodeConflict, "claim_already_released",
			fmt.Sprintf("claim %q already released", in.ClaimID))
	}
	now := s.clock.NowMilli()
	if _, err := s.tx.Exec(
		`UPDATE claims SET released_at=? WHERE id=?`, now, in.ClaimID,
	); err != nil {
		return err
	}
	// End any active run.
	var runID string
	err = s.tx.QueryRow(
		`SELECT id FROM runs WHERE claim_id=? AND ended_at IS NULL`, in.ClaimID,
	).Scan(&runID)
	if err == nil {
		if _, err := s.tx.Exec(
			`UPDATE runs SET ended_at=?, outcome='orphaned' WHERE id=?`, now, runID,
		); err != nil {
			return err
		}
		_ = s.events.Append(s.tx, events.Record{
			Kind: "run_ended", EntityKind: "run", EntityID: runID,
			Payload: map[string]any{"outcome": "orphaned"}, OpID: in.OpID,
		})
	}
	// Event for claim release.
	if err := s.events.Append(s.tx, events.Record{
		Kind: "claim_released", EntityKind: "claim", EntityID: in.ClaimID,
		Payload: map[string]any{"reason": "voluntary"}, OpID: in.OpID,
	}); err != nil {
		return err
	}
	// Flip task if no other live claim.
	var liveCount int
	_ = s.tx.QueryRow(
		`SELECT count(*) FROM claims WHERE task_id=? AND released_at IS NULL`,
		taskID,
	).Scan(&liveCount)
	if liveCount == 0 {
		var prev string
		_ = s.tx.QueryRow(`SELECT status FROM tasks WHERE id=?`, taskID).Scan(&prev)
		if prev != "done" && prev != "failed" && prev != "stale" && prev != "open" {
			if _, err := s.tx.Exec(
				`UPDATE tasks SET status='open', updated_at=? WHERE id=?`, now, taskID,
			); err != nil {
				return err
			}
			_ = s.events.Append(s.tx, events.Record{
				Kind: "task_status_changed", EntityKind: "task", EntityID: taskID,
				Payload: map[string]any{
					"from": prev, "to": "open", "reason": "release",
				}, OpID: in.OpID,
			})
		}
	}

	if err := s.RecordOpLog(in.OpID, "task.release", []byte(`{}`)); err != nil {
		return err
	}
	return nil
}
