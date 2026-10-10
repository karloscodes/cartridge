package postgres

import (
	"net/url"
	"regexp"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

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

// SQLDriver returns "pgx", the database/sql driver of jackc/pgx.
func (d *Driver) SQLDriver() string {
	return "pgx"
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

// Ensure Driver implements database.Driver
var _ database.Driver = (*Driver)(nil)
