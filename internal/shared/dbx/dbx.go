// Package dbx is the one place that knows how to open a MySQL connection pool and run a
// transaction. Every module's mysql/ package uses this instead of calling database/sql directly,
// so the pool-sizing and transaction-boundary rules live in exactly one place
// (specs/global/06_ENGINEERING_STANDARDS.md §5: "a service never calls db.Begin() directly").
package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver with database/sql; never called directly
)

// Open connects to MySQL using dsn and returns a ready-to-use connection pool.
//
// The underlying *sql.DB is NOT one connection — it's a pool that opens and reuses connections
// as needed. SetMaxOpenConns/SetMaxIdleConns bound that pool; the starting values below match
// specs/global/03_NFR_BASELINE.md §1's stated starting point (10 connections per backend
// instance), to be tuned against real measurement later, not guessed at now.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("dbx: open: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	// sql.Open doesn't actually connect — it just validates the DSN. PingContext is what proves
	// the database is reachable, and we want to know that at startup, not on the first request.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("dbx: ping: %w", err)
	}

	return db, nil
}

// WithTx runs fn inside a database transaction: BEGIN, then fn, then COMMIT if fn returned nil or
// ROLLBACK if it returned an error. This is the ONLY function in the codebase allowed to call
// db.BeginTx — every feature's checkout/status-update/stock-adjust logic goes through this
// (specs/global/06_ENGINEERING_STANDARDS.md §5), which is what makes "does this code path
// correctly roll back on error" a question with one answer instead of one answer per feature.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dbx: begin tx: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("dbx: %w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dbx: commit: %w", err)
	}
	return nil
}
