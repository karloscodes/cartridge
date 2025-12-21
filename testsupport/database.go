package testsupport

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDBOptions configures test database creation.
type TestDBOptions struct {
	// Models to auto-migrate
	Models []any

	// Enable SQL logging (default: silent)
	Verbose bool
}

// SetupTestDB creates an in-memory SQLite database for testing.
// It applies WAL mode pragmas and optionally migrates provided models.
func SetupTestDB(t *testing.T, opts ...TestDBOptions) *gorm.DB {
	t.Helper()

	var options TestDBOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	logMode := logger.Silent
	if options.Verbose {
		logMode = logger.Info
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
	})
	if err != nil {
		t.Fatalf("testsupport: failed to open test database: %v", err)
	}

	// Apply SQLite pragmas for test consistency
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
	}
	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			t.Fatalf("testsupport: failed to apply pragma %s: %v", pragma, err)
		}
	}

	// Auto-migrate models if provided
	if len(options.Models) > 0 {
		if err := db.AutoMigrate(options.Models...); err != nil {
			t.Fatalf("testsupport: failed to migrate models: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

// TestDBManager implements cartridge.DBManager for testing.
type TestDBManager struct {
	db *gorm.DB
}

// NewTestDBManager creates a DBManager that wraps a test database.
func NewTestDBManager(db *gorm.DB) *TestDBManager {
	return &TestDBManager{db: db}
}

// GetConnection returns the test database connection.
func (m *TestDBManager) GetConnection() *gorm.DB {
	return m.db
}

// Connect returns the test database connection.
func (m *TestDBManager) Connect() (*gorm.DB, error) {
	return m.db, nil
}
