// Package postgres provides the SQL-backed implementations of the repository
// interfaces defined in the parent package.
//
// Historically this package only spoke PostgreSQL (hence the name). It now
// supports two dialects behind the same *DB handle:
//
//   - PostgreSQL (online mode) — opened with New(connStr)
//   - SQLite via modernc.org/sqlite (offline mode) — opened with NewSQLite(path)
//
// The repository queries themselves are written in the portable subset both
// engines understand ($N placeholders, basic ON CONFLICT, no RETURNING, no
// INTERVAL arithmetic; time comparisons are done against bound parameters
// rather than NOW()). Only the schema migrations differ per dialect — see
// repository.RunMigrationsFor.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/repository"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB connection pool for PostgreSQL or SQLite.
type DB struct {
	db      *sql.DB
	dialect repository.Dialect
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
	return &DB{db: db, dialect: repository.DialectPostgres}, nil
}

// NewSQLite opens (creating if necessary) a SQLite database at the given file
// path using the pure-Go modernc.org/sqlite driver — no CGO, no server, no
// external dependencies. This powers offline mode.
//
// Pass ":memory:" for an ephemeral in-memory database (used by tests).
//
// The pool is pinned to a single connection: SQLite allows one writer at a
// time, and a single shared connection sidesteps SQLITE_BUSY contention
// entirely while also making ":memory:" safe with database/sql pooling.
func NewSQLite(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite connect: empty database path")
	}
	// _time_format=sqlite makes the driver bind time.Time values in SQLite's
	// canonical text format ("2006-01-02 15:04:05.999999999-07:00") so they
	// compare and sort correctly alongside CURRENT_TIMESTAMP defaults.
	dsn := "file:" + path +
		"?_time_format=sqlite" +
		"&_txlock=immediate" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite connect: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	return &DB{db: db, dialect: repository.DialectSQLite}, nil
}

// Dialect reports which SQL dialect this handle speaks.
func (d *DB) Dialect() repository.Dialect { return d.dialect }

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
