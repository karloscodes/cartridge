package cartridge

import (
	"log/slog"
	"github.com/karloscodes/cartridge/internal/adapters"
	"github.com/karloscodes/cartridge/internal/logging"
)

// Logger interface for logging operations - public wrapper
// This matches the core.Logger interface but is exported publicly
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	With(args ...interface{}) Logger
}

// LoggerWrapper wraps the internal logger for public API
type LoggerWrapper struct {
	internal *adapters.CoreLoggerAdapter
}

// NewLogger creates a new logger from slog.Logger
func NewLogger(logger *slog.Logger) Logger {
	return &LoggerWrapper{
		internal: adapters.NewCoreLoggerAdapter(logger),
	}
}

// Debug logs a debug message
func (l *LoggerWrapper) Debug(msg string, args ...interface{}) {
	l.internal.Debug(msg, args...)
}

// Info logs an info message
func (l *LoggerWrapper) Info(msg string, args ...interface{}) {
	l.internal.Info(msg, args...)
}

// Warn logs a warning message
func (l *LoggerWrapper) Warn(msg string, args ...interface{}) {
	l.internal.Warn(msg, args...)
}

// Error logs an error message
func (l *LoggerWrapper) Error(msg string, args ...interface{}) {
	l.internal.Error(msg, args...)
}

// With returns a new logger with the given key-value pairs
func (l *LoggerWrapper) With(args ...interface{}) Logger {
	coreLogger := l.internal.With(args...)
	// Cast back to the adapter type we know
	if adapter, ok := coreLogger.(*adapters.CoreLoggerAdapter); ok {
		return &LoggerWrapper{internal: adapter}
	}
	// Fallback - create new adapter (shouldn't happen normally)
	return &LoggerWrapper{internal: l.internal}
}

// LogLevel represents the log level - re-export from internal
type LogLevel = logging.LogLevel

// Log level constants
const (
	LogLevelDebug = logging.LogLevelDebug
	LogLevelInfo  = logging.LogLevelInfo
	LogLevelWarn  = logging.LogLevelWarn
	LogLevelError = logging.LogLevelError
)

// LogConfig holds logging configuration - re-export from internal
type LogConfig = logging.LogConfig

// NewLoggerFromConfig creates a new logger from configuration
func NewLoggerFromConfig(config LogConfig) Logger {
	slogLogger := logging.NewLogger(config)
	return NewLogger(slogLogger)
}