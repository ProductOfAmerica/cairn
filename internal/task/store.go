// Package task owns tasks, claims, and runs tables.
package task

import (
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// Store is the task package's transaction-bound entry point.
type Store struct {
	tx     *db.Tx
	events events.Appender
	ids    *ids.Generator
	clock  clock.Clock
}

// NewStore binds a transaction.
func NewStore(tx *db.Tx, a events.Appender, g *ids.Generator, c clock.Clock) *Store {
	return &Store{tx: tx, events: a, ids: g, clock: c}
}
