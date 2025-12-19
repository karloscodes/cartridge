package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Environment constants.
const (
	Development = "development"
	Production  = "production"
	Test        = "test"
)

// Config provides common configuration for cartridge applications.
// Apps can embed this struct and add their own fields.
type Config struct {
	// AppName is the application name, used for env var prefix and database filename.
	AppName string `mapstructure:"appname"`

	// Environment: development, production, or test.
	Environment string `mapstructure:"environment"`

	// Port for the HTTP server.
	Port string `mapstructure:"port"`

	// Debug enables debug mode.
	Debug bool `mapstructure:"debug"`

	// Logging configuration.
	LogLevel         string `mapstructure:"loglevel"`
	LogsDirectory    string `mapstructure:"logsdirectory"`
	LogsMaxSizeMB    int    `mapstructure:"logsmaxsizeinmb"`
	LogsMaxBackups   int    `mapstructure:"logsmaxbackups"`
	LogsMaxAgeDays   int    `mapstructure:"logsmaxageindays"`

	// Session configuration.
	SessionSecret  string `mapstructure:"sessionsecret"`
	SessionTimeout int    `mapstructure:"sessiontimeoutseconds"`

	// Data and database configuration.
	DataDirectory    string `mapstructure:"datadirectory"`
	DatabaseFilename string `mapstructure:"databasefilename"`
	DatabasePath     string `mapstructure:"-"` // Resolved path, not from env
	MaxOpenConns     int    `mapstructure:"databasemaxopenconns"`
	MaxIdleConns     int    `mapstructure:"databasemaxidleconns"`

	// Internal: the env var prefix (derived from AppName).
	envPrefix string
}

// Load creates a new Config for the given app name.
// It reads from environment variables prefixed with the uppercase app name.
// Example: Load("formlander") reads FORMLANDER_ENV, FORMLANDER_PORT, etc.
func Load(appName string) (*Config, error) {
	v := viper.New()

	// Normalize app name
	appName = strings.ToLower(strings.TrimSpace(appName))
	if appName == "" {
		appName = "app"
	}
	prefix := strings.ToUpper(appName)

	// Read .env file if present
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	_ = v.ReadInConfig()

	// Set defaults
	setDefaults(v, appName)

	// Bind environment variables
	v.SetEnvPrefix(prefix)
	bindEnvVars(v, prefix)

	cfg := &Config{envPrefix: prefix}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// Resolve database path
	cfg.DatabasePath = cfg.resolveDatabasePath()

	// Ensure directories exist
	cfg.ensureDirectories()

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper, appName string) {
	v.SetDefault("appname", appName)
	v.SetDefault("environment", Production)
	v.SetDefault("port", "8080")
	v.SetDefault("debug", false)

	v.SetDefault("loglevel", "error")
	v.SetDefault("logsdirectory", "storage/logs")
	v.SetDefault("logsmaxsizeinmb", 20)
	v.SetDefault("logsmaxbackups", 10)
	v.SetDefault("logsmaxageindays", 30)

	v.SetDefault("sessiontimeoutseconds", 604800) // 1 week

	v.SetDefault("datadirectory", "storage")
	v.SetDefault("databasefilename", appName+".db")
	v.SetDefault("databasemaxopenconns", 0)
	v.SetDefault("databasemaxidleconns", 0)
}

func bindEnvVars(v *viper.Viper, prefix string) {
	// Core env vars: {PREFIX}_ENV, {PREFIX}_PORT, etc.
	v.BindEnv("environment", prefix+"_ENV")
	v.BindEnv("port", prefix+"_PORT")
	v.BindEnv("sessionsecret", prefix+"_SESSION_SECRET")
	v.BindEnv("loglevel", prefix+"_LOG_LEVEL")
	v.BindEnv("datadirectory", prefix+"_DATA_DIR")
	v.BindEnv("debug", prefix+"_DEBUG")
}

func (c *Config) validate() error {
	var problems []string

	// Adjust log level for development
	if c.LogLevel == "" || c.LogLevel == "error" {
		if c.IsDevelopment() || c.IsTest() {
			c.LogLevel = "info"
		}
	}

	// Session secret handling
	if c.IsProduction() {
		if c.SessionSecret == "" {
			problems = append(problems, fmt.Sprintf("%s_SESSION_SECRET is REQUIRED in production", c.envPrefix))
		}
	} else if c.SessionSecret == "" {
		c.SessionSecret = "dev-secret-do-not-use-in-production-f8e3a9c2d1b7e6a4"
		if c.IsDevelopment() {
			log.Printf("info: Using default development secret (set %s_SESSION_SECRET for custom value)", c.envPrefix)
		}
	}

	// Validate environment
	switch c.Environment {
	case Development, Production, Test:
	default:
		problems = append(problems, fmt.Sprintf("invalid %s_ENV value %q", c.envPrefix, c.Environment))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c *Config) resolveDatabasePath() string {
	filename := c.DatabaseFilename
	if filename == "" {
		filename = c.AppName + ".db"
	}

	// Add environment suffix: app.development.db, app.test.db, app.production.db
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if ext == "" {
		ext = ".db"
	}
	filename = fmt.Sprintf("%s.%s%s", base, c.Environment, ext)

	if filepath.IsAbs(filename) {
		return filename
	}
	return filepath.Join(c.DataDirectory, filename)
}

func (c *Config) ensureDirectories() {
	dirs := []string{c.DataDirectory, c.LogsDirectory}
	for _, dir := range dirs {
		if dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				log.Printf("config: failed to create directory %q: %v", dir, err)
			}
		}
	}
}

// Environment checks.

func (c *Config) IsDevelopment() bool { return c.Environment == Development }
func (c *Config) IsProduction() bool  { return c.Environment == Production }
func (c *Config) IsTest() bool        { return c.Environment == Test }

// Cartridge interface implementations.

func (c *Config) GetPort() string            { return c.Port }
func (c *Config) GetPublicDirectory() string { return "web/static" }
func (c *Config) GetAssetsPrefix() string    { return "/assets" }

// LogConfigProvider implementation.

func (c *Config) GetLogLevel() string     { return c.LogLevel }
func (c *Config) GetLogDirectory() string { return c.LogsDirectory }
func (c *Config) GetLogMaxSizeMB() int    { return c.LogsMaxSizeMB }
func (c *Config) GetLogMaxBackups() int   { return c.LogsMaxBackups }
func (c *Config) GetLogMaxAgeDays() int   { return c.LogsMaxAgeDays }
func (c *Config) GetAppName() string      { return c.AppName }

// Database configuration.

func (c *Config) DatabaseDSN() string { return c.DatabasePath }

func (c *Config) GetMaxOpenConns() int {
	if c.MaxOpenConns > 0 {
		return c.MaxOpenConns
	}
	if c.IsProduction() {
		return 10
	}
	return 1
}

func (c *Config) GetMaxIdleConns() int {
	if c.MaxIdleConns > 0 {
		return c.MaxIdleConns
	}
	if c.IsProduction() {
		return 5
	}
	return 1
}

// GetSessionSecret returns the session secret.
func (c *Config) GetSessionSecret() string { return c.SessionSecret }

// GetSessionTimeout returns session timeout in seconds.
func (c *Config) GetSessionTimeout() int { return c.SessionTimeout }
