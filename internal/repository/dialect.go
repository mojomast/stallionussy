package repository

// Dialect identifies which SQL engine backs the repository layer.
//
// StallionUSSY speaks two dialects: PostgreSQL for the normal online
// deployment, and SQLite (via the pure-Go modernc.org/sqlite driver) for
// offline mode, where the server must run with no external dependencies.
type Dialect string

const (
	// DialectPostgres is the default online-mode database.
	DialectPostgres Dialect = "postgres"

	// DialectSQLite is the offline-mode embedded database.
	DialectSQLite Dialect = "sqlite"
)
