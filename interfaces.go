package cartridge

import "gorm.io/gorm"

// Logger abstracts logging operations across different logging libraries.
// Both slog and zap can implement this interface via thin adapters.
type Logger interface {
	// Debug logs a debug-level message with optional key-value pairs.
	Debug(msg string, keysAndValues ...any)

	// Info logs an info-level message with optional key-value pairs.
	Info(msg string, keysAndValues ...any)

	// Warn logs a warning-level message with optional key-value pairs.
	Warn(msg string, keysAndValues ...any)

	// Error logs an error-level message with optional key-value pairs.
	Error(msg string, keysAndValues ...any)
}

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
}
