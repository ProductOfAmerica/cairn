package intent

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// Store owns the requirements + gates + tasks tables (for materialization).
type Store struct {
	tx     *db.Tx
	events events.Appender
	clock  clock.Clock
}

// NewStore returns a Store bound to a transaction.
func NewStore(tx *db.Tx, a events.Appender, c clock.Clock) *Store {
	return &Store{tx: tx, events: a, clock: c}
}

// MaterializeResult summarizes what Materialize did.
type MaterializeResult struct {
	RequirementsMaterialized int `json:"requirements_materialized"`
	GatesMaterialized        int `json:"gates_materialized"`
	TasksMaterialized        int `json:"tasks_materialized"`
}

// Materialize upserts the bundle into state.
// Emits spec_materialized per requirement whose spec_hash changed (or is new),
// and task_planned per newly inserted task.
func (s *Store) Materialize(b *Bundle) (MaterializeResult, error) {
	var r MaterializeResult
	now := s.clock.NowMilli()

	for _, req := range b.Requirements {
		specHash := sha256Hex(req.RawYAML)
		var existingHash string
		err := s.tx.QueryRow(
			"SELECT spec_hash FROM requirements WHERE id=?", req.ID,
		).Scan(&existingHash)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, fmt.Errorf("query requirement %s: %w", req.ID, err)
		}
		if err == nil {
			// Row exists; update only if hash changed.
			if existingHash != specHash {
				if _, execErr := s.tx.Exec(
					`UPDATE requirements SET spec_path=?, spec_hash=?, updated_at=? WHERE id=?`,
					req.SpecPath, specHash, now, req.ID,
				); execErr != nil {
					return r, execErr
				}
				if appErr := s.events.Append(s.tx, events.Record{
					Kind: "spec_materialized", EntityKind: "requirement", EntityID: req.ID,
					Payload: map[string]any{
						"spec_path": req.SpecPath,
						"old_hash":  existingHash,
						"new_hash":  specHash,
					},
				}); appErr != nil {
					return r, appErr
				}
			}
		} else {
			// sql.ErrNoRows — insert new row.
			if _, execErr := s.tx.Exec(
				`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?)`,
				req.ID, req.SpecPath, specHash, now, now,
			); execErr != nil {
				return r, execErr
			}
			if appErr := s.events.Append(s.tx, events.Record{
				Kind: "spec_materialized", EntityKind: "requirement", EntityID: req.ID,
				Payload: map[string]any{
					"spec_path": req.SpecPath,
					"old_hash":  "",
					"new_hash":  specHash,
				},
			}); appErr != nil {
				return r, appErr
			}
		}
		r.RequirementsMaterialized++

		// Upsert gates for this requirement.
		for _, g := range req.Gates {
			gateHash, err := GateDefHash(g)
			if err != nil {
				return r, fmt.Errorf("hash gate %s: %w", g.ID, err)
			}
			defJSON, _ := json.Marshal(map[string]any{
				"id":   g.ID,
				"kind": g.Kind,
				"producer": map[string]any{
					"kind":   g.Producer.Kind,
					"config": normalizeForJSON(g.Producer.Config),
				},
			})
			producerJSON, _ := json.Marshal(normalizeForJSON(g.Producer.Config))
			if _, execErr := s.tx.Exec(
				`INSERT INTO gates (id, requirement_id, kind, definition_json,
				     gate_def_hash, producer_kind, producer_config)
				 VALUES (?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				     kind=excluded.kind,
				     definition_json=excluded.definition_json,
				     gate_def_hash=excluded.gate_def_hash,
				     producer_kind=excluded.producer_kind,
				     producer_config=excluded.producer_config`,
				g.ID, req.ID, g.Kind, string(defJSON),
				gateHash, g.Producer.Kind, string(producerJSON),
			); execErr != nil {
				return r, execErr
			}
			r.GatesMaterialized++
		}
	}

	for _, t := range b.Tasks {
		specHash := sha256Hex(t.RawYAML)
		dependsJSON, _ := json.Marshal(t.DependsOn)
		requiredJSON, _ := json.Marshal(t.RequiredGates)
		var existingStatus string
		err := s.tx.QueryRow("SELECT status FROM tasks WHERE id=?", t.ID).Scan(&existingStatus)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return r, fmt.Errorf("query task %s: %w", t.ID, err)
		}
		if err == nil {
			// Row exists; update but preserve status.
			if _, execErr := s.tx.Exec(
				`UPDATE tasks SET
				     requirement_id=?, spec_path=?, spec_hash=?,
				     depends_on_json=?, required_gates_json=?, updated_at=?
				 WHERE id=?`,
				firstOrEmpty(t.Implements), t.SpecPath, specHash,
				string(dependsJSON), string(requiredJSON), now, t.ID,
			); execErr != nil {
				return r, execErr
			}
		} else {
			// sql.ErrNoRows — insert new task.
			if _, execErr := s.tx.Exec(
				`INSERT INTO tasks (
				     id, requirement_id, spec_path, spec_hash,
				     depends_on_json, required_gates_json, status,
				     created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, 'open', ?, ?)`,
				t.ID, firstOrEmpty(t.Implements), t.SpecPath, specHash,
				string(dependsJSON), string(requiredJSON), now, now,
			); execErr != nil {
				return r, execErr
			}
			if appErr := s.events.Append(s.tx, events.Record{
				Kind: "task_planned", EntityKind: "task", EntityID: t.ID,
				Payload: map[string]any{
					"requirement_id": firstOrEmpty(t.Implements),
					"spec_hash":      specHash,
				},
			}); appErr != nil {
				return r, appErr
			}
		}
		r.TasksMaterialized++
	}
	return r, nil
}

func firstOrEmpty(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
