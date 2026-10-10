package cartridge

import (
	"database/sql"
	"log/slog"
)

// Logger is an alias for *slog.Logger.
// This allows applications to use cartridge.Logger without importing slog directly.
type Logger = *slog.Logger

// Config abstracts runtime configuration access.
// Applications implement this interface to provide environment-specific configuration.
type Config interface {
	// IsDevelopment returns true if running in development mode.
	IsDevelopment() bool

	// IsProduction returns true if running in production mode.
	IsProduction() bool

	// IsTest returns true if running in test mode.
	IsTest() bool

	// GetPort returns the HTTP server port.
	GetPort() string

	// GetPublicDirectory returns the path to public/static assets.
	GetPublicDirectory() string
}

// DBManager owns the connections of one database. It gives two handles,
// one for reads and one for writes, so an app reads and writes the same way
// on every database:
//
//   - SQLite (sqlite.Manager): the reader is a pool of read-only
//     connections, and the writer is the one write connection. SQLite
//     allows one writer, and a write on a read connection is an error.
//   - PostgreSQL and MySQL (database.Manager): both handles are the same
//     pool, because these databases take many writers at once.
//
// Both methods open the database on the first call.
type DBManager interface {
	// Reader returns the pool for queries that only read.
	Reader() (*sql.DB, error)

	// Writer returns the pool for statements that write. For a
	// transaction, use Write or Context.WriteTx.
	Writer() (*sql.DB, error)
}
