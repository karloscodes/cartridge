package cartridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

// Database interface for database operations
type Database interface {
	GetGenericConnection() interface{} // Returns *gorm.DB
	LockDatabases() func()
	CheckpointWAL(mode string) error
	Close() error
	MigrateDatabase() error
	Init() error
	Ping() error
}

// SlogGormLogger implements GORM's logger interface using slog
type SlogGormLogger struct {
	slogLogger Logger
	logLevel   logger.LogLevel
}

// NewSlogGormLogger creates a new GORM logger that uses slog
func NewSlogGormLogger(slogLogger Logger, logLevel logger.LogLevel) logger.Interface {
	return &SlogGormLogger{
		slogLogger: slogLogger,
		logLevel:   logLevel,
	}
}

// LogMode sets the log level for GORM operations
func (l *SlogGormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return &SlogGormLogger{
		slogLogger: l.slogLogger,
		logLevel:   level,
	}
}

// Info logs info level messages
func (l *SlogGormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Info {
		l.slogLogger.Info(fmt.Sprintf(msg, data...))
	}
}

// Warn logs warning level messages
func (l *SlogGormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Warn {
		l.slogLogger.Warn(fmt.Sprintf(msg, data...))
	}
}

// Error logs error level messages
func (l *SlogGormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Error {
		l.slogLogger.Error(fmt.Sprintf(msg, data...))
	}
}

// Trace logs SQL queries with execution time and affected rows
func (l *SlogGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	// Clean up SQL formatting
	sql = strings.ReplaceAll(strings.ReplaceAll(sql, "\n", " "), "\t", " ")
	sql = strings.Join(strings.Fields(sql), " ") // Remove extra spaces

	switch {
	case err != nil && l.logLevel >= logger.Error:
		l.slogLogger.Error("Database query failed",
			"error", err,
			"duration", elapsed,
			"sql", sql,
			"rows", rows,
		)
	case elapsed > 200*time.Millisecond && l.logLevel >= logger.Warn:
		l.slogLogger.Warn("Slow database query",
			"duration", elapsed,
			"sql", sql,
			"rows", rows,
		)
	case l.logLevel >= logger.Info:
		l.slogLogger.Debug("Database query",
			"duration", elapsed,
			"sql", sql,
			"rows", rows,
		)
	}
}

// sqliteDatabase implements Database for SQLite
type sqliteDatabase struct {
	db     *gorm.DB
	config *Config
	logger Logger
	mutex  sync.Mutex
}

var (
	dbInstance *sqliteDatabase
	dbOnce     sync.Once
)

// NewDatabase creates a new database manager instance
func NewDatabase(cfg *Config, logger Logger) Database {
	dbOnce.Do(func() {
		dbInstance = &sqliteDatabase{
			config: cfg,
			logger: logger,
		}
	})
	return dbInstance
}

// GetInstance returns the singleton database instance
func GetDatabaseInstance() Database {
	if dbInstance == nil {
		panic("Database manager not initialized. Call NewDatabase first.")
	}
	return dbInstance
}

// Init initializes the database connection
func (dm *sqliteDatabase) Init() error {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if dm.db != nil {
		return nil // Already initialized
	}

	dm.logger.Info("Initializing database connection", "url", dm.config.DatabaseURL)

	// Configure GORM logger with slog
	var gormLogLevel logger.LogLevel
	if dm.config.IsDevelopment() {
		gormLogLevel = logger.Info // Show all SQL queries in development
	} else {
		gormLogLevel = logger.Warn // Only show warnings and errors in production
	}

	gormLogger := NewSlogGormLogger(dm.logger.With("component", "gorm"), gormLogLevel)

	// Open database connection
	db, err := gorm.Open(sqlite.Open(dm.config.DatabaseURL), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		dm.logger.Error("Failed to connect to database", "error", err, "url", dm.config.DatabaseURL)
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	dm.db = db

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		dm.logger.Error("Failed to get underlying sql.DB", "error", err)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Apply SQLite-specific configurations
	dbConfig := DatabaseConfig{
		DatabaseURL:     dm.config.DatabaseURL,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
		JournalMode:     "WAL",
		Synchronous:     "NORMAL",
		CacheSize:       -64000, // 64MB
		ForeignKeys:     true,
		TempStore:       "memory",
	}

	// Set connection pool settings
	sqlDB.SetMaxOpenConns(dbConfig.MaxOpenConns)
	sqlDB.SetMaxIdleConns(dbConfig.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(dbConfig.ConnMaxLifetime)

	if err := dm.applySQLitePragmas(dbConfig); err != nil {
		dm.logger.Error("Failed to apply SQLite pragmas", "error", err)
		return fmt.Errorf("failed to apply SQLite pragmas: %w", err)
	}

	dm.logger.Info("Database connection established successfully", 
		"max_open_conns", dbConfig.MaxOpenConns,
		"max_idle_conns", dbConfig.MaxIdleConns,
		"journal_mode", dbConfig.JournalMode)
	return nil
}

// applySQLitePragmas applies SQLite-specific configuration optimized for production
func (dm *sqliteDatabase) applySQLitePragmas(config DatabaseConfig) error {
	// Core performance and reliability pragmas
	pragmas := []string{
		fmt.Sprintf("PRAGMA journal_mode = %s", config.JournalMode), // WAL mode for better concurrency
		fmt.Sprintf("PRAGMA synchronous = %s", config.Synchronous),  // NORMAL for good performance/safety balance
		fmt.Sprintf("PRAGMA cache_size = %d", config.CacheSize),     // Negative value = KB, positive = pages
		fmt.Sprintf("PRAGMA temp_store = %s", config.TempStore),     // Store temp tables in memory
		
		// Performance optimizations
		"PRAGMA busy_timeout = 30000",        // 30s timeout for concurrent access
		"PRAGMA auto_vacuum = INCREMENTAL",   // Gradual vacuum to prevent large pauses
		"PRAGMA mmap_size = 268435456",       // 256MB memory-mapped I/O
		"PRAGMA page_size = 4096",            // 4KB pages (good balance)
		
		// WAL mode optimizations
		"PRAGMA wal_autocheckpoint = 1000",   // Auto checkpoint every 1000 pages
		"PRAGMA wal_checkpoint(TRUNCATE)",    // Initial WAL cleanup
		
		// Security and reliability
		"PRAGMA secure_delete = OFF",         // Faster deletes (data overwritten anyway in WAL)
		"PRAGMA case_sensitive_like = ON",    // Consistent LIKE behavior
	}

	// Environment-specific pragmas
	if dm.config.IsDevelopment() {
		// Development: More paranoid settings for debugging
		pragmas = append(pragmas, 
			"PRAGMA integrity_check",         // Check DB integrity
			"PRAGMA optimize",               // Analyze tables for query optimization
		)
	} else {
		// Production: Performance-focused
		pragmas = append(pragmas,
			"PRAGMA analysis_limit = 1000", // Limit ANALYZE time
			"PRAGMA optimize",              // Optimize query planner stats
		)
	}

	// Foreign keys (usually enabled)
	if config.ForeignKeys {
		pragmas = append(pragmas, "PRAGMA foreign_keys = ON")
	}

	dm.logger.Info("Applying SQLite pragmas for production optimization", "count", len(pragmas))

	// Apply each pragma with detailed logging
	successCount := 0
	for _, pragma := range pragmas {
		result := dm.db.Exec(pragma)
		if result.Error != nil {
			// Log as warning but continue - some pragmas might not be available
			dm.logger.Warn("Failed to apply pragma",
				"pragma", pragma,
				"error", result.Error)
		} else {
			dm.logger.Debug("Applied pragma successfully", "pragma", pragma)
			successCount++
		}
	}

	dm.logger.Info("SQLite pragmas configuration completed", 
		"total", len(pragmas), 
		"successful", successCount,
		"mode", config.JournalMode,
		"cache_size_kb", -config.CacheSize/1024)
	
	return nil
}

// GetGenericConnection returns the underlying GORM database connection
func (dm *sqliteDatabase) GetGenericConnection() interface{} {
	if dm.db == nil {
		dm.logger.Error("Database not initialized")
		return nil
	}
	return dm.db
}

// LockDatabases locks the database for exclusive access
func (dm *sqliteDatabase) LockDatabases() func() {
	dm.mutex.Lock()
	return func() {
		dm.mutex.Unlock()
	}
}

// CheckpointWAL performs a WAL checkpoint operation
func (dm *sqliteDatabase) CheckpointWAL(mode string) error {
	if dm.db == nil {
		return fmt.Errorf("database not initialized")
	}

	validModes := []string{"PASSIVE", "FULL", "RESTART", "TRUNCATE"}
	mode = strings.ToUpper(mode)
	isValid := false
	for _, validMode := range validModes {
		if mode == validMode {
			isValid = true
			break
		}
	}

	if !isValid {
		dm.logger.Warn("Invalid WAL checkpoint mode, using PASSIVE", "requested_mode", mode)
		mode = "PASSIVE"
	}

	query := fmt.Sprintf("PRAGMA wal_checkpoint(%s)", mode)
	result := dm.db.Exec(query)
	if result.Error != nil {
		dm.logger.Error("WAL checkpoint failed", "error", result.Error, "mode", mode)
		return fmt.Errorf("WAL checkpoint failed: %w", result.Error)
	}

	dm.logger.Debug("WAL checkpoint completed", "mode", mode, "rows_affected", result.RowsAffected)
	return nil
}

// Close closes the database connection
func (dm *sqliteDatabase) Close() error {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if dm.db == nil {
		return nil
	}

	dm.logger.Info("Closing database connection")

	// Perform final WAL checkpoint
	if err := dm.CheckpointWAL("TRUNCATE"); err != nil {
		dm.logger.Warn("Failed to perform final WAL checkpoint", "error", err)
	}

	// Close the connection
	sqlDB, err := dm.db.DB()
	if err != nil {
		dm.logger.Error("Failed to get underlying sql.DB", "error", err)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		dm.logger.Error("Failed to close database", "error", err)
		return fmt.Errorf("failed to close database: %w", err)
	}

	dm.db = nil
	dm.logger.Info("Database connection closed successfully")
	return nil
}

// MigrateDatabase runs database migrations
func (dm *sqliteDatabase) MigrateDatabase() error {
	if dm.db == nil {
		return fmt.Errorf("database not initialized")
	}

	dm.logger.Info("Running database migrations")

	// Add your migrations here
	// Example:
	// if err := dm.db.AutoMigrate(&User{}, &Post{}); err != nil {
	//     dm.logger.Error("Failed to run migrations", "error", err)
	//     return fmt.Errorf("failed to run migrations: %w", err)
	// }

	dm.logger.Info("Database migrations completed successfully")
	return nil
}

// Ping checks database connectivity
func (dm *sqliteDatabase) Ping() error {
	if dm.db == nil {
		return fmt.Errorf("database not initialized")
	}
	
	sqlDB, err := dm.db.DB()
	if err != nil {
		dm.logger.Error("Failed to get underlying sql.DB for ping", "error", err)
		return err
	}
	
	if err := sqlDB.Ping(); err != nil {
		dm.logger.Error("Database ping failed", "error", err)
		return err
	}
	
	dm.logger.Debug("Database ping successful")
	return nil
}
