package postgres

import (
	"log/slog"
	"net/url"
	"regexp"
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

// ConfigureDSN adds the SSL mode, the time zone, and the search path to the
// DSN, so every connection of the pool gets them. It reads both DSN forms:
// a URL ("postgres://host/db") and keywords ("host=... dbname=..."). An
// option that the DSN sets itself stays as it is.
func (d *Driver) ConfigureDSN(dsn string, cfg *database.Config) string {
	options := [][2]string{
		{"sslmode", cfg.Postgres.SSLMode},
		{"TimeZone", cfg.Postgres.Timezone},
		{"search_path", cfg.Postgres.SearchPath},
	}
	isURL := strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
	for _, option := range options {
		name, value := option[0], option[1]
		if value == "" || dsnSets(dsn, name, isURL) {
			continue
		}
		if !isURL {
			dsn += " " + name + "=" + keywordValue(value)
			continue
		}
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		dsn += separator + name + "=" + url.QueryEscape(value)
	}
	return dsn
}

// dsnSets reports whether the DSN has the parameter, in any case.
func dsnSets(dsn, name string, isURL bool) bool {
	if !isURL {
		return regexp.MustCompile(`(?i)(^|\s)` + regexp.QuoteMeta(name) + `\s*=`).MatchString(dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return false
	}
	for key := range u.Query() {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

// keywordValue quotes a value of a keyword DSN when it needs quotes.
func keywordValue(value string) string {
	if !strings.ContainsAny(value, " '\\") {
		return value
	}
	return "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(value) + "'"
}

// AfterConnect has nothing to do: the options are in the DSN. A SET sent
// here would reach only one connection of the pool.
func (d *Driver) AfterConnect(db *gorm.DB, cfg *database.Config, logger *slog.Logger) error {
	return nil
}

// Close is a no-op for PostgreSQL.
func (d *Driver) Close(db *gorm.DB, logger *slog.Logger) error {
	return nil
}

// ConcurrentWrites returns true: PostgreSQL takes writes from many
// connections at once, so cartridge.Write does not queue them.
func (d *Driver) ConcurrentWrites() bool {
	return true
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
