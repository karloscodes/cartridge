// Package mysql is the MySQL driver for database.Manager.
package mysql

import (
	"log/slog"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/database"
)

// Driver implements database.Driver for MySQL.
type Driver struct{}

// NewDriver creates a new MySQL driver.
func NewDriver() *Driver {
	return &Driver{}
}

// Name returns "mysql".
func (d *Driver) Name() string {
	return "mysql"
}

// Open returns a GORM MySQL dialector.
func (d *Driver) Open(dsn string) gorm.Dialector {
	return mysql.Open(dsn)
}

// ConfigureDSN turns parseTime on. Without it the driver returns DATETIME
// columns as bytes, and a time.Time field cannot read them. The driver's
// own defaults already give utf8mb4 and UTC. A DSN that does not parse is
// returned as it is, and the connection reports the error.
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return dsn
	}
	parsed.ParseTime = true
	return parsed.FormatDSN()
}

// AfterConnect has nothing to do: the options are in the DSN.
func (d *Driver) AfterConnect(db *gorm.DB, cfg *database.Config, logger *slog.Logger) error {
	return nil
}

// Close is a no-op for MySQL.
func (d *Driver) Close(db *gorm.DB, logger *slog.Logger) error {
	return nil
}

// SupportsCheckpoint returns false for MySQL.
func (d *Driver) SupportsCheckpoint() bool {
	return false
}

// Checkpoint is a no-op for MySQL.
func (d *Driver) Checkpoint(db *gorm.DB, mode string) error {
	return nil
}

// Ensure Driver implements database.Driver
var _ database.Driver = (*Driver)(nil)
