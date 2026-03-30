// Package postgres provides PostgreSQL implementations of the repository
// interfaces defined in the parent package.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// DB wraps a *sql.DB connection pool for PostgreSQL.
type DB struct {
	db *sql.DB
}

// New opens a PostgreSQL connection, verifies it with a ping, and configures
// sensible pool defaults.
func New(connStr string) (*DB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return &DB{db: db}, nil
}

// Close releases the underlying database connection pool.
func (d *DB) Close() error { return d.db.Close() }

// GetDB returns the raw *sql.DB for advanced use cases (migrations, etc.).
func (d *DB) GetDB() *sql.DB { return d.db }

// WithTx executes fn inside a database transaction. If fn returns a non-nil
// error the transaction is rolled back; otherwise it is committed. The
// deferred Rollback is a no-op after a successful Commit.
//
// Callers pass a closure that receives a *sql.Tx and can execute arbitrary
// SQL statements atomically. This is the primary mechanism for ensuring
// multi-step mutations (trade acceptance, auction settlement, poker payouts)
// are all-or-nothing.
func (d *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op if already committed

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
