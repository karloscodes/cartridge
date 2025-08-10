package adapters

import (
	"log/slog"
	"github.com/karloscodes/cartridge/internal/core"
	"github.com/karloscodes/cartridge/internal/middleware"
)

// LoggerAdapter wraps slog.Logger to implement various Logger interfaces
type LoggerAdapter struct {
	logger *slog.Logger
}

// NewLoggerAdapter creates a new logger adapter
func NewLoggerAdapter(logger *slog.Logger) *LoggerAdapter {
	return &LoggerAdapter{logger: logger}
}

// Debug logs a debug message
func (l *LoggerAdapter) Debug(msg string, args ...interface{}) {
	l.logger.Debug(msg, args...)
}

// Info logs an info message
func (l *LoggerAdapter) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, args...)
}

// Warn logs a warning message
func (l *LoggerAdapter) Warn(msg string, args ...interface{}) {
	l.logger.Warn(msg, args...)
}

// Error logs an error message
func (l *LoggerAdapter) Error(msg string, args ...interface{}) {
	l.logger.Error(msg, args...)
}

// With returns a new logger with the given key-value pairs
func (l *LoggerAdapter) With(args ...interface{}) core.Logger {
	return NewCoreLoggerAdapter(l.logger.With(args...))
}

// CoreLoggerAdapter implements core.Logger
type CoreLoggerAdapter struct {
	*LoggerAdapter
}

// NewCoreLoggerAdapter creates a core logger adapter
func NewCoreLoggerAdapter(logger *slog.Logger) *CoreLoggerAdapter {
	return &CoreLoggerAdapter{
		LoggerAdapter: NewLoggerAdapter(logger),
	}
}

// With returns a new logger with the given key-value pairs
func (l *CoreLoggerAdapter) With(args ...interface{}) core.Logger {
	return NewCoreLoggerAdapter(l.logger.With(args...))
}

// MiddlewareLoggerAdapter implements middleware.Logger
type MiddlewareLoggerAdapter struct {
	*LoggerAdapter
}

// NewMiddlewareLoggerAdapter creates a middleware logger adapter
func NewMiddlewareLoggerAdapter(logger *slog.Logger) *MiddlewareLoggerAdapter {
	return &MiddlewareLoggerAdapter{
		LoggerAdapter: NewLoggerAdapter(logger),
	}
}

// With returns a new logger with the given key-value pairs
func (l *MiddlewareLoggerAdapter) With(args ...interface{}) middleware.Logger {
	return NewMiddlewareLoggerAdapter(l.logger.With(args...))
}

// GetSlogLogger returns the underlying slog.Logger for interoperability
func (l *LoggerAdapter) GetSlogLogger() *slog.Logger {
	return l.logger
}

// GetSlogLogger returns the underlying slog.Logger for interoperability
func (l *CoreLoggerAdapter) GetSlogLogger() *slog.Logger {
	return l.logger
}

// GetSlogLogger returns the underlying slog.Logger for interoperability
func (l *MiddlewareLoggerAdapter) GetSlogLogger() *slog.Logger {
	return l.logger
}

