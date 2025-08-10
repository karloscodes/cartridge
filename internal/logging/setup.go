package logging

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lmittmann/tint"
)

// LogLevel represents the log level
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// LogConfig holds logging configuration
type LogConfig struct {
	Level         LogLevel
	Directory     string
	UseJSON       bool
	EnableConsole bool
	EnableColors  bool
	AddSource     bool
	Environment   string // "development", "production", "test"
}

// NewLogger creates a new slog.Logger instance with the given configuration
func NewLogger(config LogConfig) *slog.Logger {
	// Convert LogLevel to slog.Level
	var slogLevel slog.Level
	switch config.Level {
	case LogLevelDebug:
		slogLevel = slog.LevelDebug
	case LogLevelInfo:
		slogLevel = slog.LevelInfo
	case LogLevelWarn:
		slogLevel = slog.LevelWarn
	case LogLevelError:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	// Create handler options
	opts := &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: config.AddSource,
	}

	var handler slog.Handler
	isProduction := config.Environment == "production"

	// Production: Log to file only with structured JSON
	if isProduction && config.Directory != "" {
		if err := os.MkdirAll(config.Directory, 0o755); err == nil {
			logFile := filepath.Join(config.Directory, "app.log")
			if file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				// Always use JSON in production for structured logs
				handler = slog.NewJSONHandler(file, opts)
				return slog.New(handler)
			}
		}
		// Fallback to stdout with JSON if file creation fails
		handler = slog.NewJSONHandler(os.Stdout, opts)
		return slog.New(handler)
	}

	// Development/Test: Console with colors, optional file logging
	if config.EnableConsole || config.Directory == "" {
		// Console output with colors in development
		if config.EnableColors {
			handler = tint.NewHandler(os.Stdout, &tint.Options{
				Level:      slogLevel,
				TimeFormat: "15:04:05",
				AddSource:  config.AddSource,
			})
		} else if config.UseJSON {
			handler = slog.NewJSONHandler(os.Stdout, opts)
		} else {
			handler = slog.NewTextHandler(os.Stdout, opts)
		}
		return slog.New(handler)
	}

	// File logging for development (when console is disabled)
	if config.Directory != "" {
		if err := os.MkdirAll(config.Directory, 0o755); err == nil {
			logFile := filepath.Join(config.Directory, "app.log")
			if file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				if config.UseJSON {
					handler = slog.NewJSONHandler(file, opts)
				} else {
					handler = slog.NewTextHandler(file, opts)
				}
				return slog.New(handler)
			}
		}
	}

	// Final fallback
	handler = slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

// Fatal logs a fatal message and exits (helper function since slog doesn't have Fatal)
func Fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}