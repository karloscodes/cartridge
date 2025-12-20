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

// ConfigureDSN adds SQLite-specific options to the DSN.
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	if cfg.SQLite.TxImmediate {
		dsn += "?_txlock=immediate"
	}
	return dsn
}

// AfterConnect applies SQLite pragmas.
func (d *Driver) AfterConnect(db *gorm.DB, cfg *database.Config, logger *slog.Logger) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", cfg.SQLite.BusyTimeout),
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = MEMORY",
	}

	if cfg.SQLite.EnableWAL {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}

	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			logger.Error("failed to apply pragma", slog.String("pragma", pragma), slog.Any("error", err))
			return fmt.Errorf("sqlite: apply pragma %s: %w", pragma, err)
		}
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
