package sqlite

import (
	"fmt"
	"log/slog"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/database"
)

// Driver implements database.Driver for SQLite.
type Driver struct{}

// NewDriver creates a new SQLite driver.
func NewDriver() *Driver {
	return &Driver{}
}

// Name returns "sqlite".
func (d *Driver) Name() string {
	return "sqlite"
}

// Open returns a GORM SQLite dialector.
func (d *Driver) Open(dsn string) gorm.Dialector {
	return sqlite.Open(dsn)
}

// ConfigureDSN adds the connection settings to the DSN, so the driver applies
// them to every pooled connection (see buildDSN).
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	return buildDSN(dsn, cfg.SQLite.BusyTimeout, cfg.SQLite.EnableWAL, cfg.SQLite.TxImmediate)
}

// AfterConnect applies the one setting with no DSN form. temp_store is only a
// hint for temporary tables and sorts.
func (d *Driver) AfterConnect(db *gorm.DB, cfg *database.Config, logger *slog.Logger) error {
	if err := db.Exec("PRAGMA temp_store = MEMORY").Error; err != nil {
		logger.Error("failed to apply pragma", slog.String("pragma", "temp_store"), slog.Any("error", err))
		return fmt.Errorf("sqlite: apply pragma temp_store: %w", err)
	}
	return nil
}

// Close performs a passive WAL checkpoint before closing.
func (d *Driver) Close(db *gorm.DB, logger *slog.Logger) error {
	logger.Info("performing WAL checkpoint before close")
	return d.Checkpoint(db, "PASSIVE")
}

// SupportsCheckpoint returns true for SQLite.
func (d *Driver) SupportsCheckpoint() bool {
	return true
}

// Checkpoint performs a WAL checkpoint.
// Modes: PASSIVE, FULL, RESTART, TRUNCATE
func (d *Driver) Checkpoint(db *gorm.DB, mode string) error {
	return db.Exec("PRAGMA wal_checkpoint(" + mode + ");").Error
}

// Ensure Driver implements database.Driver
var _ database.Driver = (*Driver)(nil)
