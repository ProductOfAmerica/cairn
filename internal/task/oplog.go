package task

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
)

// CheckOpLog returns the cached result for (opID, kind) if present. Returns
// (nil, false, nil) on miss. Returns an error if the op_id exists under a
// different kind.
func (s *Store) CheckOpLog(opID, kind string) ([]byte, bool, error) {
	var storedKind string
	var result []byte
	err := s.tx.QueryRow(
		"SELECT kind, result_json FROM op_log WHERE op_id=?", opID,
	).Scan(&storedKind, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedKind != kind {
		return nil, false, cairnerr.New(cairnerr.CodeConflict, "op_id_kind_mismatch",
			fmt.Sprintf("op_id %s was previously recorded with kind=%q, now invoked as %q",
				opID, storedKind, kind))
	}
	return result, true, nil
}

// RecordOpLog writes the op_log row in the current transaction.
func (s *Store) RecordOpLog(opID, kind string, result []byte) error {
	_, err := s.tx.Exec(
		`INSERT INTO op_log (op_id, kind, first_seen_at, result_json)
		 VALUES (?, ?, ?, ?)`,
		opID, kind, s.clock.NowMilli(), string(result),
	)
	return err
}
