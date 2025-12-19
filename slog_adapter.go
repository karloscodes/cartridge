package cartridge

import "log/slog"

// SlogAdapter wraps slog.Logger to implement the cartridge Logger interface.
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a Logger adapter from an slog.Logger.
func NewSlogAdapter(logger *slog.Logger) Logger {
	return &SlogAdapter{logger: logger}
}

// Debug logs a debug-level message.
func (a *SlogAdapter) Debug(msg string, keysAndValues ...any) {
	a.logger.Debug(msg, keysAndValues...)
}

// Info logs an info-level message.
func (a *SlogAdapter) Info(msg string, keysAndValues ...any) {
	a.logger.Info(msg, keysAndValues...)
}

// Warn logs a warning-level message.
func (a *SlogAdapter) Warn(msg string, keysAndValues ...any) {
	a.logger.Warn(msg, keysAndValues...)
}

// Error logs an error-level message.
func (a *SlogAdapter) Error(msg string, keysAndValues ...any) {
	a.logger.Error(msg, keysAndValues...)
}
