package database

import "time"

// Config provides the configuration of a PostgreSQL or MySQL database.
// SQLite has its own manager: sqlite.Manager.
type Config struct {
	// DSN is the connection string: a URL or a keyword DSN for PostgreSQL,
	// "user:password@tcp(host:3306)/name" for MySQL.
	DSN string

	// MaxOpenConns is the maximum number of open connections. Default: 10.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections. Default: 5.
	MaxIdleConns int

	// ConnMaxLifetime is the maximum connection lifetime. Default: 10 minutes.
	ConnMaxLifetime time.Duration

	// PostgreSQL-specific options (ignored for other drivers)
	Postgres PostgresOptions
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
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 10 * time.Minute,
		Postgres: PostgresOptions{
			SSLMode:  "prefer",
			Timezone: "UTC",
		},
	}
}
