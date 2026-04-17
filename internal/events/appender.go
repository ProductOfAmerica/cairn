// Package events owns the event log. Every mutation emits one or more events
// in the same transaction as the mutation it describes. events.Since queries
// them back; events.Kinds is the coverage helper for the Ship 1 completeness
// test.
package events

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
)

// Record is an event to be appended.
type Record struct {
	Kind       string         // "task_planned", "claim_acquired", ...
	EntityKind string         // "task", "claim", "verdict", ...
	EntityID   string
	Payload    map[string]any // serialized as JSON
	OpID       string         // empty for reads / read-only mutations
}

// Appender interface is what domain stores receive. Declared explicitly so
// mocks or alternate implementations can be swapped.
type Appender interface {
	Append(tx *db.Tx, rec Record) error
}

// appender is the production implementation backed by a clock.
type appender struct {
	clock clock.Clock
}

// NewAppender returns an Appender.
func NewAppender(c clock.Clock) Appender { return &appender{clock: c} }

// Append writes a single event row inside the caller's transaction. The
// op_id is stored as NULL when the caller passes an empty string so the
// column semantics stay honest.
func (a *appender) Append(tx *db.Tx, rec Record) error {
	payload, err := json.Marshal(rec.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = tx.Exec(
		`INSERT INTO events (at, kind, entity_kind, entity_id, payload_json, op_id)
         VALUES (?, ?, ?, ?, ?, NULLIF(?, ''))`,
		a.clock.NowMilli(), rec.Kind, rec.EntityKind, rec.EntityID,
		string(payload), rec.OpID,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// Event is the read-back shape used by Since.
type Event struct {
	ID         int64
	At         int64
	Kind       string
	EntityKind string
	EntityID   string
	Payload    json.RawMessage
	OpID       sql.NullString
}
