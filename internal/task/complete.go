package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// CompleteInput is the caller-supplied data for completing a task.
type CompleteInput struct {
	OpID    string
	ClaimID string
}

// CompleteResult is the outcome of a successful Complete call.
type CompleteResult struct {
	TaskID string `json:"task_id"`
	RunID  string `json:"run_id"`
}

// Complete requires every required gate on the task to have a latest verdict
// that is fresh + status=pass. If any fail, returns CodeConflict. Otherwise
// flips the task to done, ends the run, and releases the claim.
func (s *Store) Complete(in CompleteInput) (CompleteResult, error) {
	cached, hit, err := s.CheckOpLog(in.OpID, "task.complete")
	if err != nil {
		return CompleteResult{}, err
	}
	if hit {
		var r CompleteResult
		_ = json.Unmarshal(cached, &r)
		return r, nil
	}

	var taskID string
	var released sql.NullInt64
	err = s.tx.QueryRow(
		`SELECT task_id, released_at FROM claims WHERE id=?`, in.ClaimID,
	).Scan(&taskID, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return CompleteResult{}, cairnerr.New(cairnerr.CodeNotFound, "claim_not_found",
			fmt.Sprintf("claim %q", in.ClaimID))
	}
	if err != nil {
		return CompleteResult{}, err
	}
	if released.Valid {
		return CompleteResult{}, cairnerr.New(cairnerr.CodeConflict, "claim_released",
			fmt.Sprintf("claim %q already released", in.ClaimID))
	}

	// Load required_gates and current task status.
	var reqJSON, prevStatus string
	err = s.tx.QueryRow(
		`SELECT required_gates_json, status FROM tasks WHERE id=?`, taskID,
	).Scan(&reqJSON, &prevStatus)
	if err != nil {
		return CompleteResult{}, err
	}
	var requiredGates []string
	_ = json.Unmarshal([]byte(reqJSON), &requiredGates)

	// Check each gate's latest verdict using a same-txn sub-Store.
	vStore := verdict.NewStore(s.tx, s.events, ids.NewGenerator(s.clock),
		evidence.NewStore(s.tx, s.events, ids.NewGenerator(s.clock), ""))
	type failure struct {
		GateID string `json:"gate_id"`
		Reason string `json:"reason"`
	}
	var failures []failure
	for _, g := range requiredGates {
		ok, reason, err := vStore.IsFreshPass(g)
		if err != nil {
			return CompleteResult{}, err
		}
		if !ok {
			failures = append(failures, failure{GateID: g, Reason: reason})
		}
	}
	if len(failures) > 0 {
		return CompleteResult{}, cairnerr.New(cairnerr.CodeConflict,
			"gates_not_fresh_pass",
			fmt.Sprintf("%d required gate(s) not fresh+pass", len(failures))).
			WithDetails(map[string]any{"failing": failures})
	}

	now := s.clock.NowMilli()
	if _, err := s.tx.Exec(
		`UPDATE tasks SET status='done', updated_at=? WHERE id=?`, now, taskID,
	); err != nil {
		return CompleteResult{}, err
	}
	var runID string
	_ = s.tx.QueryRow(
		`SELECT id FROM runs WHERE claim_id=? AND ended_at IS NULL`, in.ClaimID,
	).Scan(&runID)
	if runID != "" {
		if _, err := s.tx.Exec(
			`UPDATE runs SET ended_at=?, outcome='done' WHERE id=?`, now, runID,
		); err != nil {
			return CompleteResult{}, err
		}
	}
	if _, err := s.tx.Exec(
		`UPDATE claims SET released_at=? WHERE id=?`, now, in.ClaimID,
	); err != nil {
		return CompleteResult{}, err
	}

	evs := []events.Record{
		{Kind: "task_status_changed", EntityKind: "task", EntityID: taskID,
			Payload: map[string]any{
				"from": prevStatus, "to": "done", "reason": "complete",
			}, OpID: in.OpID},
	}
	if runID != "" {
		evs = append(evs, events.Record{
			Kind: "run_ended", EntityKind: "run", EntityID: runID,
			Payload: map[string]any{"outcome": "done"}, OpID: in.OpID,
		})
	}
	evs = append(evs, events.Record{
		Kind: "claim_released", EntityKind: "claim", EntityID: in.ClaimID,
		Payload: map[string]any{"reason": "voluntary"}, OpID: in.OpID,
	})
	for _, e := range evs {
		if err := s.events.Append(s.tx, e); err != nil {
			return CompleteResult{}, err
		}
	}

	res := CompleteResult{TaskID: taskID, RunID: runID}
	payload, _ := json.Marshal(res)
	if err := s.RecordOpLog(in.OpID, "task.complete", payload); err != nil {
		return CompleteResult{}, err
	}
	return res, nil
}
