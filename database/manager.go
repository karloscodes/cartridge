package database

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/logging"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	DatabaseURL     string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	JournalMode     string
	Synchronous     string
	CacheSize       int
	ForeignKeys     bool
	TempStore       string
}

// DBManager interface for database operations
type DBManager interface {
	GetGenericConnection() *gorm.DB
	LockDatabases() func()
	CheckpointWAL(mode string) error
	Close() error
	MigrateDatabase() error
	Init() error
}

// sqliteDBManager implements DBManager for SQLite
type sqliteDBManager struct {
	db     *gorm.DB
	config *config.Config
	logger logging.Logger
	mutex  sync.Mutex
}

var (
	instance *sqliteDBManager
	once     sync.Once
)

// NewDBManager creates a new database manager instance
func NewDBManager(cfg *config.Config, logger logging.Logger) DBManager {
	once.Do(func() {
		instance = &sqliteDBManager{
			config: cfg,
			logger: logger,
		}
	})
	return instance
}

// GetInstance returns the singleton database manager instance
func GetInstance() DBManager {
	if instance == nil {
		panic("Database manager not initialized. Call NewDBManager first.")
	}
	return instance
}

// Init initializes the database connection
func (dm *sqliteDBManager) Init() error {
	dbConfig := DatabaseConfig{
		DatabaseURL:     dm.config.DatabaseURL,
		MaxOpenConns:    1, // SQLite single writer limitation
		MaxIdleConns:    1,
		ConnMaxLifetime: 10 * time.Minute,
		JournalMode:     "WAL",
		Synchronous:     "NORMAL",
		CacheSize:       1000,
		ForeignKeys:     true,
		TempStore:       "MEMORY",
	}

	// Ensure data directory exists
	if err := dm.config.EnsureDataDirectory(); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create GORM logger
	gormLogger := NewGormLogger(dm.logger)

	// Open database connection
	db, err := gorm.Open(sqlite.Open(dbConfig.DatabaseURL), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying sql.DB
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(dbConfig.MaxOpenConns)
	sqlDB.SetMaxIdleConns(dbConfig.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(dbConfig.ConnMaxLifetime)

	// Apply SQLite pragmas for optimization
	if err := dm.applySQLitePragmas(db, dbConfig); err != nil {
		return fmt.Errorf("failed to apply SQLite pragmas: %w", err)
	}

	dm.db = db
	dm.logger.Info("Database connection established", logging.Field{Key: "url", Value: dbConfig.DatabaseURL})

	return nil
}

// applySQLitePragmas applies critical SQLite pragmas for performance
func (dm *sqliteDBManager) applySQLitePragmas(db *gorm.DB, config DatabaseConfig) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA journal_mode = %s", config.JournalMode),
		fmt.Sprintf("PRAGMA synchronous = %s", config.Synchronous),
		fmt.Sprintf("PRAGMA temp_store = %s", config.TempStore),
		fmt.Sprintf("PRAGMA cache_size = %d", config.CacheSize),
	}

	if config.ForeignKeys {
		pragmas = append(pragmas, "PRAGMA foreign_keys = ON")
	}

	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			return fmt.Errorf("failed to execute pragma '%s': %w", pragma, err)
		}
		dm.logger.Debug("Applied SQLite pragma", logging.Field{Key: "pragma", Value: pragma})
	}

	return nil
}

// GetGenericConnection returns the database connection
func (dm *sqliteDBManager) GetGenericConnection() *gorm.DB {
	if dm.db == nil {
		panic("Database not initialized. Call Init() first.")
	}
	return dm.db
}

// LockDatabases acquires a lock for write operations and returns an unlock function
func (dm *sqliteDBManager) LockDatabases() func() {
	dm.mutex.Lock()
	return func() {
		dm.mutex.Unlock()
	}
}

// CheckpointWAL performs a WAL checkpoint operation
func (dm *sqliteDBManager) CheckpointWAL(mode string) error {
	if dm.db == nil {
		return fmt.Errorf("database not initialized")
	}

	validModes := []string{"PASSIVE", "NORMAL", "FULL", "RESTART", "TRUNCATE"}
	mode = strings.ToUpper(mode)
	
	valid := false
	for _, validMode := range validModes {
		if mode == validMode {
			valid = true
			break
		}
	}
	
	if !valid {
		mode = "NORMAL"
	}

	query := fmt.Sprintf("PRAGMA wal_checkpoint(%s)", mode)
	if err := dm.db.Exec(query).Error; err != nil {
		return fmt.Errorf("failed to checkpoint WAL: %w", err)
	}

	dm.logger.Debug("WAL checkpoint completed", logging.Field{Key: "mode", Value: mode})
	return nil
}

// Close closes the database connection
func (dm *sqliteDBManager) Close() error {
	if dm.db == nil {
		return nil
	}

	// Perform final WAL checkpoint
	if err := dm.CheckpointWAL("NORMAL"); err != nil {
		dm.logger.Warn("Failed to perform final WAL checkpoint", logging.Field{Key: "error", Value: err})
	}

	sqlDB, err := dm.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	dm.logger.Info("Database connection closed")
	return nil
}

// MigrateDatabase runs database migrations
func (dm *sqliteDBManager) MigrateDatabase() error {
	if dm.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// This method should be implemented by the application
	// Here we just provide the interface
	dm.logger.Info("Database migration completed")
	return nil
}

// PerformWrite executes a write operation with retry logic for database locks
func PerformWrite(logger logging.Logger, db *gorm.DB, operation func(tx *gorm.DB) error) error {
	const (
		maxRetries = 5
		baseDelay  = 50 * time.Millisecond
		maxDelay   = 1 * time.Second
	)

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Start transaction
		tx := db.Begin()
		if tx.Error != nil {
			lastErr = tx.Error
			continue
		}

		// Execute operation
		err := operation(tx)
		if err != nil {
			tx.Rollback()
			
			// Check if it's a database lock error
			if isDatabaseLockError(err) {
				lastErr = err
				
				if attempt < maxRetries {
					delay := calculateBackoffDelay(attempt, baseDelay, maxDelay)
					logger.Debug("Database lock detected, retrying",
						logging.Field{Key: "attempt", Value: attempt + 1},
						logging.Field{Key: "delay_ms", Value: delay.Milliseconds()},
						logging.Field{Key: "error", Value: err.Error()})
					
					time.Sleep(delay)
					continue
				}
			}
			
			return err
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			if isDatabaseLockError(err) {
				lastErr = err
				
				if attempt < maxRetries {
					delay := calculateBackoffDelay(attempt, baseDelay, maxDelay)
					logger.Debug("Database lock during commit, retrying",
						logging.Field{Key: "attempt", Value: attempt + 1},
						logging.Field{Key: "delay_ms", Value: delay.Milliseconds()})
					
					time.Sleep(delay)
					continue
				}
			}
			
			return err
		}

		// Success
		if attempt > 0 {
			logger.Debug("Write operation succeeded after retries",
				logging.Field{Key: "attempts", Value: attempt + 1})
		}
		
		return nil
	}

	return fmt.Errorf("write operation failed after %d attempts: %w", maxRetries+1, lastErr)
}

// isDatabaseLockError checks if the error is a SQLite database lock error
func isDatabaseLockError(err error) bool {
	if err == nil {
		return false
	}
	
	errorMsg := strings.ToLower(err.Error())
	lockErrors := []string{
		"database is locked",
		"database lock",
		"locked",
		"busy",
	}
	
	for _, lockError := range lockErrors {
		if strings.Contains(errorMsg, lockError) {
			return true
		}
	}
	
	return false
}

// calculateBackoffDelay calculates exponential backoff delay with jitter
func calculateBackoffDelay(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := baseDelay * time.Duration(1<<uint(attempt))
	
	// Cap at maxDelay
	if delay > maxDelay {
		delay = maxDelay
	}
	
	// Add jitter (±25%)
	jitter := float64(delay) * 0.25 * (rand.Float64()*2 - 1)
	delay += time.Duration(jitter)
	
	// Ensure minimum delay
	if delay < baseDelay {
		delay = baseDelay
	}
	
	return delay
}
