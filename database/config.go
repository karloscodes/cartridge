package database

import "time"

// Config provides database configuration.
type Config struct {
	// DSN is the database connection string.
	// For SQLite: file path (e.g., "storage/app.db")
	// For PostgreSQL: connection URL or DSN string
	DSN string

	// MaxOpenConns is the maximum number of open connections. Default: 1 for SQLite, 25 for PostgreSQL.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections. Default: 1 for SQLite, 5 for PostgreSQL.
	MaxIdleConns int

	// ConnMaxLifetime is the maximum connection lifetime. Default: 10 minutes.
	ConnMaxLifetime time.Duration

	// SQLite-specific options (ignored for other drivers)
	SQLite SQLiteOptions

	// PostgreSQL-specific options (ignored for other drivers)
	Postgres PostgresOptions
}

// SQLiteOptions contains SQLite-specific configuration.
type SQLiteOptions struct {
	// BusyTimeout in milliseconds. Default: 5000.
	BusyTimeout int

	// EnableWAL enables Write-Ahead Logging. Default: true.
	EnableWAL bool

	// TxImmediate uses immediate transaction locking. Default: true.
	// This prevents SQLITE_BUSY errors in concurrent write scenarios.
	TxImmediate bool
}

// PostgresOptions contains PostgreSQL-specific configuration.
type PostgresOptions struct {
	// SSLMode for connection security. Default: "prefer".
	SSLMode string

	// Timezone for the connection. Default: "UTC".
	Timezone string

	// SearchPath sets the schema search path.
	SearchPath string
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig(dsn string) *Config {
	return &Config{
		DSN:             dsn,
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: 10 * time.Minute,
		SQLite: SQLiteOptions{
			BusyTimeout: 5000,
			EnableWAL:   true,
			TxImmediate: true,
		},
		Postgres: PostgresOptions{
			SSLMode:  "prefer",
			Timezone: "UTC",
		},
	}
}
