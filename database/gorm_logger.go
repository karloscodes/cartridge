package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// GormLoggerConfig configures the GORM logger adapter.
type GormLoggerConfig struct {
	// SlowThreshold defines when a query is considered slow. Default: 200ms.
	SlowThreshold time.Duration

	// IgnoreRecordNotFoundError suppresses "record not found" errors. Default: true.
	IgnoreRecordNotFoundError bool
}

// GormLogger adapts slog to gorm's logger.Interface.
type GormLogger struct {
	slogger *slog.Logger
	level   logger.LogLevel
	config  *GormLoggerConfig
}

// NewGormLogger creates a gorm-compatible logger backed by slog.
// The log level is derived from the slog handler's level.
func NewGormLogger(slogger *slog.Logger, cfg *GormLoggerConfig) logger.Interface {
	if cfg == nil {
		cfg = &GormLoggerConfig{}
	}

	// Apply defaults
	if cfg.SlowThreshold == 0 {
		cfg.SlowThreshold = 200 * time.Millisecond
	}

	// Default to ignore record not found
	if !cfg.IgnoreRecordNotFoundError {
		cfg.IgnoreRecordNotFoundError = true
	}

	// Determine GORM log level from slog
	gormLevel := logger.Warn
	if slogger.Enabled(context.Background(), slog.LevelDebug) {
		gormLevel = logger.Info
	} else if slogger.Enabled(context.Background(), slog.LevelInfo) {
		gormLevel = logger.Info
	} else if slogger.Enabled(context.Background(), slog.LevelWarn) {
		gormLevel = logger.Warn
	} else {
		gormLevel = logger.Error
	}

	return &GormLogger{
		slogger: slogger,
		level:   gormLevel,
		config:  cfg,
	}
}

func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Info {
		l.slogger.Info(fmt.Sprintf(msg, data...))
	}
}

func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Warn {
		l.slogger.Warn(fmt.Sprintf(msg, data...))
	}
}

func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Error {
		l.slogger.Error(fmt.Sprintf(msg, data...))
	}
}

func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	sql = sanitizeGormSQL(sql)

	switch {
	case err != nil && (l.config.IgnoreRecordNotFoundError && errors.Is(err, gorm.ErrRecordNotFound)):
		return
	case err != nil:
		l.slogger.Error("gorm query failed",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
			slog.String("error", err.Error()),
		)
	case elapsed > l.config.SlowThreshold && l.level >= logger.Warn:
		l.slogger.Warn("gorm slow query",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	case l.level >= logger.Info:
		l.slogger.Debug("gorm query",
			slog.Duration("elapsed", elapsed),
			slog.Int64("rows", rows),
			slog.String("sql", sql),
		)
	}
}

func sanitizeGormSQL(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = regexp.MustCompile(`\s+`).ReplaceAllString(sql, " ")
	if len(sql) > 500 {
		return sql[:500] + "..."
	}
	return sql
}
