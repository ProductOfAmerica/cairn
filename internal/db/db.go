// Package db owns the SQLite state database. It exports only transaction
// primitives; domain packages build their own SQL via the Store pattern.
package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB is a wrapper around a database/sql handle with cairn-specific setup.
type DB struct {
	sqlDB *sql.DB
}

// Open returns a DB rooted at path. Creates the file if missing.
//
// The DSN sets _txlock=immediate so every BeginTx issues BEGIN IMMEDIATE,
// avoiding deferred-to-immediate upgrade deadlocks when two callers both
// read and then try to write. busy_timeout=5000 gives the driver an
// in-process retry before outer (cairn-level) retry kicks in.
func Open(path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}
	// Setup pragmas. synchronous=NORMAL is safe with WAL and skips the
	// per-txn fsync tax.
	setup := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, s := range setup {
		if _, err := d.Exec(s); err != nil {
			_ = d.Close()
			return nil, fmt.Errorf("pragma %q: %w", s, err)
		}
	}
	// Apply migrations (stub in Task 5.1; real impl in Task 5.2).
	if err := migrate(d); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{sqlDB: d}, nil
}

// Close releases the handle.
func (d *DB) Close() error { return d.sqlDB.Close() }

// SQL exposes the raw *sql.DB for read-only usage outside a transaction.
// Prefer WithTx for mutations (coming in Task 5.3).
func (d *DB) SQL() *sql.DB { return d.sqlDB }

// WithReadTx runs fn inside a read-only transaction with deferred locking.
// This allows readers to proceed without blocking writers in WAL mode.
func (d *DB) WithReadTx(ctx context.Context, fn func(tx *Tx) error) error {
	tx, err := d.sqlDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read tx: %w", err)
	}
	if err := fn(&Tx{sqlTx: tx}); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return err
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit read tx: %w", err)
	}
	return nil
}
