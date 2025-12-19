package cartridge

import (
	"log/slog"

	"gorm.io/gorm"
)

// Logger is an alias for *slog.Logger.
// This allows applications to use cartridge.Logger without importing slog directly.
type Logger = *slog.Logger

// Config abstracts runtime configuration access.
// Applications implement this interface to provide environment-specific configuration.
type Config interface {
	// IsDevelopment returns true if running in development mode.
	IsDevelopment() bool

	// IsProduction returns true if running in production mode.
	IsProduction() bool

	// IsTest returns true if running in test mode.
	IsTest() bool

	// GetPort returns the HTTP server port.
	GetPort() string

	// GetPublicDirectory returns the path to public/static assets.
	GetPublicDirectory() string

	// GetAssetsPrefix returns the URL prefix for static assets (e.g., "/assets").
	GetAssetsPrefix() string
}

// DBManager abstracts database connection management.
// Applications implement this interface to provide database access.
type DBManager interface {
	// GetConnection returns a GORM database connection.
	// Returns nil if the connection is unavailable.
	GetConnection() *gorm.DB

	// Connect opens a database connection and returns it.
	// Returns an error if the connection cannot be established.
	Connect() (*gorm.DB, error)
}
