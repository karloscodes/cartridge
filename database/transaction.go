package database

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// writeMutex helps prevent database locking issues when using mutex-based queuing.
var writeMutex sync.Mutex

// TransactionConfig controls how PerformWrite handles retries and queuing.
type TransactionConfig struct {
	// UseNativeSQLiteQueuing controls the write strategy:
	// - true (default): Use SQLite-native queuing via busy_timeout and _txlock=immediate
	// - false: Use app-level mutex serialization (more conservative)
	UseNativeSQLiteQueuing bool

	// MaxRetries is the maximum number of retry attempts on busy errors.
	MaxRetries int

	// BaseDelay is the initial delay before retry.
	BaseDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration
}

// DefaultTransactionConfig returns sensible defaults for SQLite transactions.
func DefaultTransactionConfig() TransactionConfig {
	return TransactionConfig{
		UseNativeSQLiteQueuing: true, // Rely on SQLite's busy_timeout
		MaxRetries:             10,
		BaseDelay:              100 * time.Millisecond,
		MaxDelay:               5 * time.Second,
	}
}

// PerformWrite executes a write transaction with retry logic for SQLite busy errors.
//
// For SQLite, this handles the common "database is locked" errors by:
// 1. Optionally serializing writes with a mutex (UseNativeSQLiteQueuing = false)
// 2. Retrying with exponential backoff and jitter on busy errors
// 3. Rolling back on failure and committing on success
//
// The native SQLite queuing approach (default) relies on:
// - SQLite's busy_timeout (configured via DSN)
// - _txlock=immediate to prevent lock upgrade deadlocks
// - WAL mode for concurrent readers during writes
func PerformWrite(logger *slog.Logger, db *gorm.DB, f func(tx *gorm.DB) error) error {
	return PerformWriteWithConfig(logger, db, f, DefaultTransactionConfig())
}

// PerformWriteWithConfig executes a write transaction with custom retry configuration.
func PerformWriteWithConfig(logger *slog.Logger, db *gorm.DB, f func(tx *gorm.DB) error, cfg TransactionConfig) error {
	if cfg.UseNativeSQLiteQueuing {
		return performWriteNative(logger, db, f, cfg)
	}
	return performWriteWithMutex(logger, db, f, cfg)
}

// performWriteWithMutex executes a write transaction with app-level mutex serialization.
// This prevents multiple goroutines from attempting writes simultaneously.
//
// IMPORTANT: The mutex is explicitly unlocked before each continue/return to avoid deadlocks.
// Go's defer executes at function exit, not loop iteration end.
func performWriteWithMutex(logger *slog.Logger, db *gorm.DB, f func(tx *gorm.DB) error, cfg TransactionConfig) error {
	var err error
	for i := 0; i < cfg.MaxRetries; i++ {
		if i > 0 {
			delay := calculateRetryDelay(i, cfg.BaseDelay, cfg.MaxDelay)
			logger.Info("Retrying transaction",
				slog.Int("attempt", i+1),
				slog.Duration("delay", delay),
				slog.Any("error", err))
			time.Sleep(delay)
		}

		writeMutex.Lock()

		tx := db.Session(&gorm.Session{
			SkipDefaultTransaction: true,
		}).Begin()

		if tx.Error != nil {
			writeMutex.Unlock()
			logger.Error("Failed to begin transaction", slog.Any("error", tx.Error))
			return fmt.Errorf("failed to begin transaction: %w", tx.Error)
		}

		err = f(tx)
		if err != nil {
			logger.Debug("Write failed", slog.Any("error", err))
			tx.Rollback()
			if !isBusyError(err) {
				writeMutex.Unlock()
				return err
			}
			writeMutex.Unlock()
			continue
		}

		err = tx.Commit().Error
		if err != nil {
			logger.Error("Commit failed", slog.Any("error", err))
			tx.Rollback()
			if isBusyError(err) {
				writeMutex.Unlock()
				continue
			}
			writeMutex.Unlock()
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		writeMutex.Unlock()
		return nil // Success
	}
	return fmt.Errorf("transaction failed after %d retries: %w", cfg.MaxRetries, err)
}

// performWriteNative executes a write transaction relying on SQLite's native queuing.
// No app-level mutex is used - SQLite handles write serialization.
//
// Benefits:
// 1. SQLite's busy_timeout provides native lock waiting
// 2. _txlock=immediate prevents lock upgrade deadlocks
// 3. WAL mode allows concurrent readers during writes
// 4. No goroutine blocking on app-level mutex
func performWriteNative(logger *slog.Logger, db *gorm.DB, f func(tx *gorm.DB) error, cfg TransactionConfig) error {
	var err error
	for i := 0; i < cfg.MaxRetries; i++ {
		if i > 0 {
			delay := calculateRetryDelay(i, cfg.BaseDelay, cfg.MaxDelay)
			logger.Info("Retrying transaction (native mode)",
				slog.Int("attempt", i+1),
				slog.Duration("delay", delay),
				slog.Any("error", err))
			time.Sleep(delay)
		}

		tx := db.Session(&gorm.Session{
			SkipDefaultTransaction: true,
		}).Begin()

		if tx.Error != nil {
			logger.Error("Failed to begin transaction", slog.Any("error", tx.Error))
			return fmt.Errorf("failed to begin transaction: %w", tx.Error)
		}

		err = f(tx)
		if err != nil {
			logger.Debug("Write failed", slog.Any("error", err))
			tx.Rollback()
			if !isBusyError(err) {
				return err
			}
			continue
		}

		err = tx.Commit().Error
		if err != nil {
			logger.Error("Commit failed", slog.Any("error", err))
			tx.Rollback()
			if isBusyError(err) {
				continue
			}
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil // Success
	}
	return fmt.Errorf("transaction failed after %d retries: %w", cfg.MaxRetries, err)
}

// calculateRetryDelay calculates exponential backoff with jitter.
func calculateRetryDelay(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt-1)))
	if delay > maxDelay {
		delay = maxDelay
	}
	// Add 20% jitter
	jitter := time.Duration(rand.Float64() * 0.2 * float64(delay))
	return delay + jitter
}

// isBusyError checks if the error is a database busy/locked error.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "database is locked") ||
		strings.Contains(errMsg, "database is busy") ||
		strings.Contains(errMsg, "locked") ||
		strings.Contains(errMsg, "busy") ||
		strings.Contains(errMsg, "SQL statements in progress")
}
