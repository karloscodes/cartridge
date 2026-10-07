package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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

	// Host is the address the HTTP server binds. Empty binds every
	// interface in production and 127.0.0.1 in development and test.
	Host string `mapstructure:"host"`

	// Port for the HTTP server.
	Port string `mapstructure:"port"`

	// Debug enables debug mode.
	Debug bool `mapstructure:"debug"`

	// Logging configuration.
	LogLevel       string `mapstructure:"loglevel"`
	LogsDirectory  string `mapstructure:"logsdirectory"`
	LogsMaxSizeMB  int    `mapstructure:"logsmaxsizeinmb"`
	LogsMaxBackups int    `mapstructure:"logsmaxbackups"`
	LogsMaxAgeDays int    `mapstructure:"logsmaxageindays"`

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

// envVars maps the keys that read a prefixed env var to its suffix, for
// example "port" reads {PREFIX}_PORT.
var envVars = map[string]string{
	"environment":   "_ENV",
	"host":          "_HOST",
	"port":          "_PORT",
	"sessionsecret": "_SESSION_SECRET",
	"loglevel":      "_LOG_LEVEL",
	"datadirectory": "_DATA_DIR",
	"debug":         "_DEBUG",
}

// Load creates a new Config for the given app name.
// It reads from environment variables prefixed with the uppercase app name.
// Example: Load("formlander") reads FORMLANDER_ENV, FORMLANDER_PORT, etc.
//
// A .env file in the working directory can set any field by its key, in
// any case, for example PORT=3000 or SESSIONTIMEOUTSECONDS=60. A prefixed
// env var wins over the .env file. An empty env var counts as unset.
func Load(appName string) (*Config, error) {
	// Normalize app name
	appName = strings.ToLower(strings.TrimSpace(appName))
	if appName == "" {
		appName = "app"
	}
	prefix := strings.ToUpper(appName)

	cfg := defaults(appName)
	cfg.envPrefix = prefix

	values := map[string]string{}
	for key, val := range readDotEnv(".env") {
		values[strings.ToLower(key)] = val
	}
	for key, suffix := range envVars {
		if val := os.Getenv(prefix + suffix); val != "" {
			values[key] = val
		}
	}
	if err := cfg.set(values); err != nil {
		return nil, err
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

func defaults(appName string) *Config {
	return &Config{
		AppName:          appName,
		Environment:      Production,
		Port:             "8080",
		LogLevel:         "error",
		LogsDirectory:    "storage/logs",
		LogsMaxSizeMB:    20,
		LogsMaxBackups:   10,
		LogsMaxAgeDays:   30,
		SessionTimeout:   604800, // 1 week
		DataDirectory:    "storage",
		DatabaseFilename: appName + ".db",
	}
}

// set writes values to the fields named by their mapstructure tags. An
// empty int is 0 and an empty bool is false.
func (c *Config) set(values map[string]string) error {
	v := reflect.ValueOf(c).Elem()
	for _, field := range reflect.VisibleFields(v.Type()) {
		key := field.Tag.Get("mapstructure")
		raw, ok := values[key]
		if !ok || key == "-" {
			continue
		}
		f := v.FieldByIndex(field.Index)
		switch f.Kind() {
		case reflect.String:
			f.SetString(raw)
		case reflect.Bool:
			b := false
			if raw != "" {
				var err error
				if b, err = strconv.ParseBool(raw); err != nil {
					return fmt.Errorf("config: %s: %q is not a bool", key, raw)
				}
			}
			f.SetBool(b)
		case reflect.Int:
			var n int64
			if raw != "" {
				var err error
				if n, err = strconv.ParseInt(raw, 0, 64); err != nil {
					return fmt.Errorf("config: %s: %q is not an integer", key, raw)
				}
			}
			f.SetInt(n)
		}
	}
	return nil
}

func (c *Config) validate() error {
	var problems []string

	// Adjust log level for development
	if c.LogLevel == "" || c.LogLevel == "error" {
		if c.IsDevelopment() || c.IsTest() {
			c.LogLevel = "info"
		}
	}

	// Session secret handling — fall back to PRIVATE_KEY (set by matcha)
	if c.SessionSecret == "" {
		if pk := os.Getenv("PRIVATE_KEY"); pk != "" {
			c.SessionSecret = pk
		}
	}
	if c.IsProduction() {
		if c.SessionSecret == "" {
			problems = append(problems, fmt.Sprintf("%s_SESSION_SECRET is REQUIRED in production (or set PRIVATE_KEY)", c.envPrefix))
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

func (c *Config) GetHost() string            { return c.Host }
func (c *Config) GetPort() string            { return c.Port }
func (c *Config) GetPublicDirectory() string { return "web/static" }

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
