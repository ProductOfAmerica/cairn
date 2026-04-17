package task

import (
	"encoding/json"
	"fmt"
)

// TaskRow is the read shape for List.
type TaskRow struct {
	ID            string   `json:"id"`
	RequirementID string   `json:"requirement_id"`
	SpecPath      string   `json:"spec_path"`
	SpecHash      string   `json:"spec_hash"`
	DependsOn     []string `json:"depends_on"`
	RequiredGates []string `json:"required_gates"`
	Status        string   `json:"status"`
	CreatedAt     int64    `json:"created_at"`
	UpdatedAt     int64    `json:"updated_at"`
}

// List returns tasks, optionally filtered by status (empty = all statuses).
func (s *Store) List(status string) ([]TaskRow, error) {
	q := `SELECT id, requirement_id, spec_path, spec_hash,
	             depends_on_json, required_gates_json, status,
	             created_at, updated_at
	      FROM tasks`
	args := []any{}
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY id"
	rows, err := s.tx.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var out []TaskRow
	for rows.Next() {
		var r TaskRow
		var deps, gates string
		if err := rows.Scan(&r.ID, &r.RequirementID, &r.SpecPath, &r.SpecHash,
			&deps, &gates, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.DependsOn = parseJSONStringArray(deps)
		r.RequiredGates = parseJSONStringArray(gates)
		out = append(out, r)
	}
	return out, rows.Err()
}

func parseJSONStringArray(s string) []string {
	// Use encoding/json. Kept local to avoid sprawling imports in list.go.
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	_ = jsonUnmarshal([]byte(s), &out)
	return out
}

var jsonUnmarshal = json.Unmarshal
