// Package postgres provides PostgreSQL implementations of the repository
// interfaces defined in the parent package.
package postgres

import (
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
