package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// DryRun simulates a real Run without writing state or emitting events.
// Returns a DryRunResult with per-rule would-be mutations for rules 1..4
// and authoring findings for rule 5.
func (o *Orchestrator) DryRun(ctx context.Context, opts Opts) (DryRunResult, error) {
	return o.dryRun(ctx, opts)
}

func (o *Orchestrator) dryRun(ctx context.Context, opts Opts) (DryRunResult, error) {
	result := DryRunResult{DryRun: true}
	now := o.clock.NowMilli()

	// Rule 1: candidate claims/tasks.
	rel, rev, err := dryRule1(ctx, o.db, now)
	if err != nil {
		return result, err
	}
	var m1 []Mutation
	for _, id := range rel {
		m1 = append(m1, Mutation{Rule: 1, EntityID: id, Action: "release", Reason: "expired"})
	}
	for _, id := range rev {
		m1 = append(m1, Mutation{Rule: 1, EntityID: id, Action: "revert_to_open", Reason: "lease_expired"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 1, Mutations: m1})

	// Rule 2: would-flip tasks.
	flips, err := dryRule2(ctx, o.db)
	if err != nil {
		return result, err
	}
	var m2 []Mutation
	for _, id := range flips {
		m2 = append(m2, Mutation{Rule: 2, EntityID: id, Action: "flip_stale", Reason: "spec_drift"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 2, Mutations: m2})

	// Rule 3: probe, then re-stat each candidate (same defense as real run).
	probeOpts := ProbeOpts{Full: opts.EvidenceSampleFull, SamplePct: opts.SamplePct, SampleCap: opts.SampleCap}
	candidates, err := RunEvidenceProbe(ctx, o.db, probeOpts)
	if err != nil {
		return result, err
	}
	var m3 []Mutation
	for _, c := range candidates {
		reason, stillInvalid, err := reStatInvalid(c.URI, c.Sha256)
		if err != nil {
			return result, err
		}
		if stillInvalid {
			m3 = append(m3, Mutation{Rule: 3, EntityID: c.EvidenceID, Action: "invalidate", Reason: reason})
		}
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 3, Mutations: m3})

	// Rule 4: would-orphan runs.
	orphans, err := dryRule4(ctx, o.db, now)
	if err != nil {
		return result, err
	}
	var m4 []Mutation
	for _, id := range orphans {
		m4 = append(m4, Mutation{Rule: 4, EntityID: id, Action: "orphan", Reason: "grace_expired"})
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 4, Mutations: m4})

	// Rule 5: authoring errors via read-only query (no tx required).
	authErrs, err := dryRule5(ctx, o.db)
	if err != nil {
		return result, err
	}
	result.Rules = append(result.Rules, DryRunRule{Rule: 5, AuthoringErrors: authErrs})

	return result, nil
}

func dryRule1(ctx context.Context, h *db.DB, now int64) ([]string, []string, error) {
	var released []string
	rows, err := h.SQL().QueryContext(ctx,
		`SELECT id FROM claims WHERE expires_at < ? AND released_at IS NULL`, now)
	if err != nil {
		return nil, nil, fmt.Errorf("dry rule 1 claims: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		released = append(released, id)
	}
	rows.Close()

	var reverted []string
	rows, err = h.SQL().QueryContext(ctx, `
		SELECT id FROM tasks
		 WHERE status IN ('claimed','in_progress','gate_pending')
		   AND id NOT IN (
		     SELECT task_id FROM claims
		      WHERE released_at IS NULL AND expires_at >= ?)`, now)
	if err != nil {
		return nil, nil, fmt.Errorf("dry rule 1 tasks: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		reverted = append(reverted, id)
	}
	rows.Close()
	return released, reverted, nil
}

func dryRule2(ctx context.Context, h *db.DB) ([]string, error) {
	// Pull done tasks + their required gates, then per-gate check whether
	// the latest verdict is a fresh pass (same logic as verdict.IsFreshPass,
	// but we can't use verdict.Store without a tx; so we inline the query).
	rows, err := h.SQL().QueryContext(ctx,
		`SELECT id, required_gates_json FROM tasks WHERE status='done'`)
	if err != nil {
		return nil, err
	}
	type task struct {
		ID      string
		GateIDs []string
	}
	var tasks []task
	for rows.Next() {
		var t task
		var gatesJSON string
		if err := rows.Scan(&t.ID, &gatesJSON); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(gatesJSON), &t.GateIDs); err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	rows.Close()

	var flips []string
	for _, t := range tasks {
		stale := false
		for _, g := range t.GateIDs {
			var curGateHash string
			if err := h.SQL().QueryRowContext(ctx,
				`SELECT gate_def_hash FROM gates WHERE id=?`, g,
			).Scan(&curGateHash); err != nil {
				stale = true
				break
			}
			var vGateHash, vStatus string
			err := h.SQL().QueryRowContext(ctx,
				`SELECT gate_def_hash, status FROM verdicts
				 WHERE gate_id=?
				 ORDER BY bound_at DESC, sequence DESC LIMIT 1`, g,
			).Scan(&vGateHash, &vStatus)
			if err != nil || vGateHash != curGateHash || vStatus != "pass" {
				stale = true
				break
			}
		}
		if stale {
			flips = append(flips, t.ID)
		}
	}
	return flips, nil
}

func dryRule4(ctx context.Context, h *db.DB, now int64) ([]string, error) {
	rows, err := h.SQL().QueryContext(ctx, `
		SELECT runs.id
		  FROM runs
		  JOIN claims ON claims.id = runs.claim_id
		 WHERE runs.ended_at IS NULL
		   AND claims.released_at IS NOT NULL
		   AND claims.released_at + ? < ?`, orphanGraceMs, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func dryRule5(ctx context.Context, h *db.DB) ([]AuthoringError, error) {
	rows, err := h.SQL().QueryContext(ctx, `
		SELECT tasks.id, j.value
		  FROM tasks, json_each(tasks.required_gates_json) j
		  LEFT JOIN gates ON gates.id = j.value
		 WHERE gates.id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthoringError
	for rows.Next() {
		var e AuthoringError
		if err := rows.Scan(&e.TaskID, &e.MissingGateID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// keep strings imported via dependency.
var _ = strings.Contains
