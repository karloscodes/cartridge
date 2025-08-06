package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LogLevel represents the logging level
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Config holds all application configuration
type Config struct {
	Environment string `envconfig:"APP_ENV" default:"development"`
	Port        string `envconfig:"APP_PORT" default:"3000"`
	DatabaseURL string `envconfig:"DATABASE_URL"`
	PrivateKey  string `envconfig:"PRIVATE_KEY"`
	Debug       bool   `envconfig:"DEBUG"`

	// Logging
	LogLevel         LogLevel `envconfig:"LOG_LEVEL" default:"info"`
	LogsDirectory    string   `envconfig:"LOGS_DIR" default:"logs"`
	LogsMaxSizeInMb  int      `envconfig:"LOGS_MAX_SIZE_MB" default:"20"`
	LogsMaxBackups   int      `envconfig:"LOGS_MAX_BACKUPS" default:"10"`
	LogsMaxAgeInDays int      `envconfig:"LOGS_MAX_AGE_DAYS" default:"30"`

	// Security
	CSRFContextKey string `default:"csrf"`
}

// New creates a new configuration instance with values loaded from environment variables
func New() *Config {
	config := Config{
		Environment:      getEnv("APP_ENV", "development"),
		Port:            getEnv("APP_PORT", "3000"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		PrivateKey:      getEnv("PRIVATE_KEY", ""),
		Debug:           getEnv("DEBUG", "false") == "true",
		LogLevel:        LogLevel(getEnv("LOG_LEVEL", "info")),
		LogsDirectory:   getEnv("LOGS_DIR", "logs"),
		LogsMaxSizeInMb: getEnvInt("LOGS_MAX_SIZE_MB", 20),
		LogsMaxBackups:  getEnvInt("LOGS_MAX_BACKUPS", 10),
		LogsMaxAgeInDays: getEnvInt("LOGS_MAX_AGE_DAYS", 30),
		CSRFContextKey:  "csrf",
	}

	// Set defaults if not provided
	if config.DatabaseURL == "" {
		config.DatabaseURL = getDefaultDatabaseURL(config.Environment)
	}

	if config.PrivateKey == "" {
		config.PrivateKey = getDefaultSecretKey(config.Environment)
	}

	// Ensure logs directory exists
	if err := os.MkdirAll(config.LogsDirectory, 0755); err != nil {
		log.Printf("Warning: Could not create logs directory: %v", err)
	}

	return &config
}

// IsDevelopment returns true if the environment is development
func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.Environment) == "development"
}

// IsProduction returns true if the environment is production
func (c *Config) IsProduction() bool {
	return strings.ToLower(c.Environment) == "production"
}

// IsTest returns true if the environment is test
func (c *Config) IsTest() bool {
	return strings.ToLower(c.Environment) == "test"
}

// getDefaultDatabaseURL returns a default database URL based on environment
func getDefaultDatabaseURL(env string) string {
	switch strings.ToLower(env) {
	case "test":
		return ":memory:"
	case "production":
		return "data/production.db"
	default:
		return "data/development.db"
	}
}

// getDefaultSecretKey returns a default secret key based on environment
func getDefaultSecretKey(env string) string {
	switch strings.ToLower(env) {
	case "test":
		return "test-secret-key-32-characters-long"
	case "production":
		log.Println("WARNING: Using default secret key in production! Set PRIVATE_KEY environment variable.")
		return "change-me-in-production-32-chars"
	default:
		return "development-secret-key-32-chars"
	}
}

// EnsureDataDirectory creates the data directory if it doesn't exist
func (c *Config) EnsureDataDirectory() error {
	if c.DatabaseURL == ":memory:" {
		return nil
	}

	dir := filepath.Dir(c.DatabaseURL)
	if dir == "." {
		return nil
	}

	return os.MkdirAll(dir, 0755)
}

// GetDatabaseDirectory returns the directory containing the database file
func (c *Config) GetDatabaseDirectory() string {
	if c.DatabaseURL == ":memory:" {
		return ""
	}
	return filepath.Dir(c.DatabaseURL)
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an environment variable as an integer or returns a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
