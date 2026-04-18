package events

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Since returns events with `at > sinceMilli`, ordered by id ascending.
// Default limit of 100 applies when limit <= 0.
func Since(sqlDB *sql.DB, sinceMilli int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := sqlDB.Query(
		`SELECT id, at, kind, entity_kind, entity_id, payload_json, op_id
         FROM events WHERE at > ? ORDER BY id ASC LIMIT ?`,
		sinceMilli, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		// modernc.org/sqlite returns TEXT columns as string, and json.RawMessage
		// (type []byte) doesn't implement sql.Scanner for string. Scan into a
		// string intermediate then convert.
		var payloadStr string
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.EntityKind,
			&e.EntityID, &payloadStr, &e.OpID); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payloadStr)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Kinds returns the distinct event kinds with `at > sinceMilli` plus the
// number of times each kind appears. Used by the Ship 1 event-log
// completeness test to assert the emitted-kind set covers the expected list.
func Kinds(sqlDB *sql.DB, sinceMilli int64) (map[string]int, error) {
	rows, err := sqlDB.Query(
		`SELECT kind, count(*) FROM events WHERE at > ? GROUP BY kind`,
		sinceMilli,
	)
	if err != nil {
		return nil, fmt.Errorf("query event kinds: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}
