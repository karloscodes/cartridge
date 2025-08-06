package cartridge

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Logger is a simple alias for slog.Logger to provide direct access
type Logger = *slog.Logger

// LogConfig holds logging configuration
type LogConfig struct {
	Level         LogLevel
	Directory     string
	UseJSON       bool
	EnableConsole bool
	AddSource     bool
}

// NewLogger creates a new slog.Logger instance with the given configuration
func NewLogger(config LogConfig) Logger {
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
	var writer *os.File

	// Setup file writer if directory is specified
	if config.Directory != "" {
		if err := os.MkdirAll(config.Directory, 0o755); err != nil {
			// Fall back to stdout
			writer = os.Stdout
		} else {
			logFile := filepath.Join(config.Directory, "app.log")
			file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				writer = os.Stdout
			} else {
				writer = file
			}
		}
	} else {
		writer = os.Stdout
	}

	// Create appropriate handler
	if config.UseJSON {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}

	return slog.New(handler)
}

// Fatal logs a fatal message and exits (helper function since slog doesn't have Fatal)
func Fatal(logger Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}