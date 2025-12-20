package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// Driver defines the interface for database-specific operations.
// Implementations provide connection setup and configuration for different databases.
type Driver interface {
	// Name returns the driver name (e.g., "sqlite", "postgres").
	Name() string

	// Open returns a GORM dialector for this database.
	Open(dsn string) gorm.Dialector

	// ConfigureDSN modifies the DSN with driver-specific options.
	// For SQLite: adds _txlock=immediate
	// For PostgreSQL: adds sslmode, timezone, etc.
	ConfigureDSN(dsn string, cfg *Config) string

	// AfterConnect runs driver-specific setup after connection is established.
	// For SQLite: applies pragmas (WAL, busy_timeout, etc.)
	// For PostgreSQL: might set search_path, timezone, etc.
	AfterConnect(db *gorm.DB, cfg *Config, logger *slog.Logger) error

	// Close performs driver-specific cleanup before closing.
	// For SQLite: WAL checkpoint
	// For PostgreSQL: might be a no-op
	Close(db *gorm.DB, logger *slog.Logger) error

	// SupportsCheckpoint returns true if the driver supports WAL checkpointing.
	SupportsCheckpoint() bool

	// Checkpoint performs a WAL checkpoint (SQLite-specific, no-op for others).
	Checkpoint(db *gorm.DB, mode string) error
}
