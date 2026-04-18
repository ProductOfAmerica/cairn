package reconcile

import (
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// Rule5CollectAuthoringErrors scans tasks.required_gates_json for gate IDs
// that do not exist in the gates table. Read-only: emits zero events.
// Findings are surfaced in the reconcile_ended payload and the JSON response.
func Rule5CollectAuthoringErrors(tx *db.Tx) ([]AuthoringError, error) {
	rows, err := tx.Query(
		`SELECT tasks.id, j.value
		 FROM tasks, json_each(tasks.required_gates_json) j
		 LEFT JOIN gates ON gates.id = j.value
		 WHERE gates.id IS NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("select authoring errors: %w", err)
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
