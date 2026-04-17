package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tx is the only type domain stores use to issue SQL. It is intentionally
// narrow; stores own entity knowledge, not db.
type Tx struct {
	sqlTx *sql.Tx
}

// Exec executes a statement within the transaction.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.sqlTx.Exec(query, args...)
}

// Query runs a read statement within the transaction.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.sqlTx.Query(query, args...)
}

// QueryRow runs a single-row read within the transaction.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.sqlTx.QueryRow(query, args...)
}

// retryBudget is the total time WithTx will spend retrying BUSY errors.
// Exported through a var so test fixtures can shrink it for faster coverage.
var retryBudget = 500 * time.Millisecond

// WithTx runs fn inside a BEGIN IMMEDIATE transaction. The BEGIN IMMEDIATE
// behavior is provided by the DSN (_txlock=immediate set in db.Open), so
// every BeginTx opens an immediate-write transaction.
//
// On SQLITE_BUSY at begin OR during fn, WithTx retries with exponential
// backoff capped at 500ms total wall time.
//
// Commit-time BUSY is NOT retried at the Go layer. Rationale: once a
// driver Commit fails, database/sql has already atomically set the tx's
// done flag and released the connection — a second *sql.Tx.Commit() would
// return sql.ErrTxDone without touching the driver. Instead, commit-time
// contention is handled at the SQLite C layer via busy_timeout=5000 (set
// in db.Open's DSN), which spins up to 5 seconds before returning BUSY.
// By the time BUSY propagates to Go, waiting longer would exceed both
// retry budgets; we return the error and let the caller decide.
//
// Stores never call Commit or Rollback; WithTx is the sole txn lifecycle owner.
func (d *DB) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	deadline := time.Now().Add(retryBudget)
	backoff := 10 * time.Millisecond

	for {
		tx, err := d.sqlDB.BeginTx(ctx, nil)
		if err != nil {
			if isBusy(err) && time.Now().Before(deadline) {
				if !sleepOrExhaust(deadline, &backoff) {
					return err
				}
				continue
			}
			return fmt.Errorf("begin: %w", err)
		}
		if err := fn(&Tx{sqlTx: tx}); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
			if isBusy(err) && time.Now().Before(deadline) {
				if !sleepOrExhaust(deadline, &backoff) {
					return err
				}
				continue
			}
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		return nil
	}
}

// sleepOrExhaust sleeps for the lesser of backoff and remaining budget.
// It advances *backoff via capBackoff and returns false if the budget is
// already exhausted (no sleep performed).
func sleepOrExhaust(deadline time.Time, backoff *time.Duration) bool {
	remaining := time.Until(deadline)
	sleepFor := *backoff
	if remaining < sleepFor {
		sleepFor = remaining
	}
	if sleepFor <= 0 {
		return false
	}
	time.Sleep(sleepFor)
	*backoff = capBackoff(*backoff * 2)
	return true
}

// isBusy reports whether err is a SQLite BUSY/locked error. Matches by
// message substring because modernc.org/sqlite doesn't expose the error
// code via a stable sentinel we can compare against.
func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "database is locked")
}

// capBackoff enforces a per-step maximum so exponential backoff doesn't
// burn the whole remaining budget on a single sleep.
func capBackoff(d time.Duration) time.Duration {
	if d > 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return d
}
