package cartridge

import "go.uber.org/zap"

// ZapAdapter wraps zap.Logger to implement the cartridge Logger interface.
type ZapAdapter struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
}

// NewZapAdapter creates a Logger adapter from a zap.Logger.
func NewZapAdapter(logger *zap.Logger) Logger {
	return &ZapAdapter{
		logger: logger,
		sugar:  logger.Sugar(),
	}
}

// Debug logs a debug-level message.
func (a *ZapAdapter) Debug(msg string, keysAndValues ...any) {
	a.sugar.Debugw(msg, keysAndValues...)
}

// Info logs an info-level message.
func (a *ZapAdapter) Info(msg string, keysAndValues ...any) {
	a.sugar.Infow(msg, keysAndValues...)
}

// Warn logs a warning-level message.
func (a *ZapAdapter) Warn(msg string, keysAndValues ...any) {
	a.sugar.Warnw(msg, keysAndValues...)
}

// Error logs an error-level message.
func (a *ZapAdapter) Error(msg string, keysAndValues ...any) {
	a.sugar.Errorw(msg, keysAndValues...)
}

// Underlying returns the wrapped *zap.Logger.
// Use this when you need the concrete logger type.
func (a *ZapAdapter) Underlying() *zap.Logger {
	return a.logger
}
