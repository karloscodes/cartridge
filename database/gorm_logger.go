package database

import (
	"context"
	"fmt"
	"time"

	"github.com/karloscodes/cartridge/logging"
	"gorm.io/gorm/logger"
)

// gormLogger wraps our logging.Logger to implement gorm.Logger interface
type gormLogger struct {
	logger                logging.Logger
	logLevel             logger.LogLevel
	ignoreRecordNotFound bool
	slowThreshold        time.Duration
}

// NewGormLogger creates a new GORM logger that wraps our Logger interface
func NewGormLogger(l logging.Logger) logger.Interface {
	return &gormLogger{
		logger:                l,
		logLevel:             logger.Info,
		ignoreRecordNotFound: true,
		slowThreshold:        200 * time.Millisecond,
	}
}

// LogMode sets the log level
func (gl *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *gl
	newLogger.logLevel = level
	return &newLogger
}

// Info logs info messages
func (gl *gormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if gl.logLevel >= logger.Info {
		gl.logger.Info(fmt.Sprintf(msg, data...))
	}
}

// Warn logs warning messages
func (gl *gormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if gl.logLevel >= logger.Warn {
		gl.logger.Warn(fmt.Sprintf(msg, data...))
	}
}

// Error logs error messages
func (gl *gormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if gl.logLevel >= logger.Error {
		gl.logger.Error(fmt.Sprintf(msg, data...))
	}
}

// Trace logs SQL queries with execution time
func (gl *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if gl.logLevel <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []logging.Field{
		{Key: "duration_ms", Value: float64(elapsed.Nanoseconds()) / 1e6},
		{Key: "rows", Value: rows},
		{Key: "sql", Value: sql},
	}

	switch {
	case err != nil && gl.logLevel >= logger.Error && (!gl.ignoreRecordNotFound || !isRecordNotFoundError(err)):
		fields = append(fields, logging.Field{Key: "error", Value: err.Error()})
		gl.logger.Error("Database query failed", fields...)
	case elapsed > gl.slowThreshold && gl.logLevel >= logger.Warn:
		gl.logger.Warn("Slow SQL query detected", fields...)
	case gl.logLevel >= logger.Info:
		gl.logger.Debug("SQL query executed", fields...)
	}
}

// isRecordNotFoundError checks if the error is a "record not found" error
func isRecordNotFoundError(err error) bool {
	// This is a simplified check - in a real implementation you might want to check for specific error types
	return err.Error() == "record not found"
}
