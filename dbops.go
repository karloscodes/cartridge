package cartridge

import (
	"fmt"

	"gorm.io/gorm"
)

// DatabaseOperations provides clean database operation methods
type DatabaseOperations struct {
	db     *gorm.DB
	logger Logger
	source string // "http", "cron", "async" etc.
}

// NewDatabaseOperations creates a new database operations helper
func NewDatabaseOperations(db *gorm.DB, logger Logger, source string) *DatabaseOperations {
	return &DatabaseOperations{
		db:     db,
		logger: logger,
		source: source,
	}
}

// Exec executes database commands (INSERT, UPDATE, DELETE) with clean error handling
func (db *DatabaseOperations) Exec(query string, args ...interface{}) *gorm.DB {
	result := db.db.Exec(query, args...)
	if result.Error != nil {
		db.logger.Error("Database execution failed",
			"source", db.source,
			"query", query,
			"error", result.Error)
		panic(fmt.Errorf("database execution failed: %w", result.Error))
	}
	return result
}

// Query executes database SELECT queries and scans into destination
func (db *DatabaseOperations) Query(query string, dest interface{}, args ...interface{}) *gorm.DB {
	result := db.db.Raw(query, args...).Scan(dest)
	if result.Error != nil {
		db.logger.Error("Database query failed",
			"source", db.source,
			"query", query,
			"error", result.Error)
		panic(fmt.Errorf("database query failed: %w", result.Error))
	}
	return result
}

// QuerySafe executes database queries with error return instead of panic (for async operations)
func (db *DatabaseOperations) QuerySafe(query string, dest interface{}, args ...interface{}) (*gorm.DB, error) {
	result := db.db.Raw(query, args...).Scan(dest)
	if result.Error != nil {
		db.logger.Error("Database query failed",
			"source", db.source,
			"query", query,
			"error", result.Error)
		return result, result.Error
	}
	return result, nil
}

// ExecSafe executes database commands with error return instead of panic (for async operations)
func (db *DatabaseOperations) ExecSafe(query string, args ...interface{}) (*gorm.DB, error) {
	result := db.db.Exec(query, args...)
	if result.Error != nil {
		db.logger.Error("Database execution failed",
			"source", db.source,
			"query", query,
			"error", result.Error)
		return result, result.Error
	}
	return result, nil
}
