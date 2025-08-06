package cartridge

import (
	"fmt"
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

// NewConfig creates a new configuration instance with values loaded from environment variables
func NewConfig() *Config {
	config := Config{
		Environment:      getEnv("APP_ENV", "development"),
		Port:             getEnv("APP_PORT", "3000"),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		PrivateKey:       getEnv("PRIVATE_KEY", ""),
		Debug:            getEnv("DEBUG", "false") == "true",
		LogLevel:         LogLevel(getEnv("LOG_LEVEL", "info")),
		LogsDirectory:    getEnv("LOGS_DIR", "logs"),
		LogsMaxSizeInMb:  getEnvInt("LOGS_MAX_SIZE_MB", 20),
		LogsMaxBackups:   getEnvInt("LOGS_MAX_BACKUPS", 10),
		LogsMaxAgeInDays: getEnvInt("LOGS_MAX_AGE_DAYS", 30),
		CSRFContextKey:   "csrf",
	}

	// Set default database path if not provided
	if config.DatabaseURL == "" {
		config.DatabaseURL = getDefaultDatabasePath()
	}

	return &config
}

// IsProduction returns true if the environment is production
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// IsDevelopment returns true if the environment is development
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// IsTest returns true if the environment is test
func (c *Config) IsTest() bool {
	return c.Environment == "test"
}

// getEnv gets an environment variable with a fallback value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an environment variable as int with a fallback value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getDefaultDatabasePath returns the default database path
func getDefaultDatabasePath() string {
	dataDir := getDataDirectory()
	return filepath.Join(dataDir, "app.db")
}

// getDataDirectory returns the data directory path
func getDataDirectory() string {
	// Create data directory if it doesn't exist
	dataDir := "data"
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			log.Printf("Failed to create data directory: %v", err)
			return "."
		}
	}
	return dataDir
}

// Validate validates the configuration
func (c *Config) Validate() error {
	var errors []string

	if c.Environment == "" {
		errors = append(errors, "APP_ENV is required")
	}

	if c.Port == "" {
		errors = append(errors, "APP_PORT is required")
	}

	if c.Environment == "production" && c.PrivateKey == "" {
		errors = append(errors, "PRIVATE_KEY is required in production")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(errors, ", "))
	}

	return nil
}

// GetDatabaseDirectory returns the directory containing the database file
func (c *Config) GetDatabaseDirectory() string {
	return filepath.Dir(c.DatabaseURL)
}
