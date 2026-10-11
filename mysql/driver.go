// Package mysql is the MySQL driver for database.Manager.
package mysql

import (
	mysqldriver "github.com/go-sql-driver/mysql" // also registers the "mysql" driver

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

// SQLDriver returns "mysql", the database/sql driver of go-sql-driver/mysql.
func (d *Driver) SQLDriver() string {
	return "mysql"
}

// ConfigureDSN turns parseTime on. Without it the driver returns DATETIME
// columns as bytes, and a time.Time cannot read them. The driver's own
// defaults already give utf8mb4 and UTC. A DSN that does not parse is
// returned as it is, and the connection reports the error.
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return dsn
	}
	parsed.ParseTime = true
	return parsed.FormatDSN()
}

// Ensure Driver implements database.Driver
var _ database.Driver = (*Driver)(nil)
