package postgres

import (
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/database"
)

// Driver implements database.Driver for PostgreSQL.
type Driver struct{}

// NewDriver creates a new PostgreSQL driver.
func NewDriver() *Driver {
	return &Driver{}
}

// Name returns "postgres".
func (d *Driver) Name() string {
	return "postgres"
}

// Open returns a GORM PostgreSQL dialector.
func (d *Driver) Open(dsn string) gorm.Dialector {
	return postgres.Open(dsn)
}

// ConfigureDSN adds PostgreSQL-specific options to the DSN.
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	// If DSN already has query params, append; otherwise add
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}

	var params []string

	if cfg.Postgres.SSLMode != "" {
		params = append(params, fmt.Sprintf("sslmode=%s", cfg.Postgres.SSLMode))
	}
	if cfg.Postgres.Timezone != "" {
		params = append(params, fmt.Sprintf("TimeZone=%s", cfg.Postgres.Timezone))
	}

	if len(params) > 0 {
		dsn += separator + strings.Join(params, "&")
	}

	return dsn
}

// AfterConnect sets up PostgreSQL-specific configuration.
func (d *Driver) AfterConnect(db *gorm.DB, cfg *database.Config, logger *slog.Logger) error {
	// Set search path if specified
	if cfg.Postgres.SearchPath != "" {
		if err := db.Exec(fmt.Sprintf("SET search_path TO %s", cfg.Postgres.SearchPath)).Error; err != nil {
			logger.Error("failed to set search_path", slog.String("search_path", cfg.Postgres.SearchPath), slog.Any("error", err))
			return fmt.Errorf("postgres: set search_path: %w", err)
		}
	}
	return nil
}

// Close is a no-op for PostgreSQL.
func (d *Driver) Close(db *gorm.DB, logger *slog.Logger) error {
	return nil
}

// SupportsCheckpoint returns false for PostgreSQL.
func (d *Driver) SupportsCheckpoint() bool {
	return false
}

// Checkpoint is a no-op for PostgreSQL.
func (d *Driver) Checkpoint(db *gorm.DB, mode string) error {
	return nil
}

// Ensure Driver implements database.Driver
var _ database.Driver = (*Driver)(nil)
