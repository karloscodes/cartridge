package database

// Driver holds what differs between the databases that Manager opens. The
// postgres and mysql packages have one each.
type Driver interface {
	// Name returns the name of the database, such as "postgres".
	Name() string

	// SQLDriver returns the name of the database/sql driver, such as "pgx".
	SQLDriver() string

	// ConfigureDSN adds the options of cfg to the DSN, so every connection
	// of the pool gets them.
	ConfigureDSN(dsn string, cfg *Config) string
}
