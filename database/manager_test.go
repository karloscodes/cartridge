package database

import (
	"io"
	"log/slog"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockDriver implements Driver for testing without import cycles.
type mockDriver struct{}

func (d *mockDriver) Name() string { return "mock-sqlite" }
func (d *mockDriver) Open(dsn string) gorm.Dialector {
	return sqlite.Open(dsn)
}
func (d *mockDriver) ConfigureDSN(dsn string, cfg *Config) string {
	return dsn
}
func (d *mockDriver) AfterConnect(db *gorm.DB, cfg *Config, logger *slog.Logger) error {
	return nil
}
func (d *mockDriver) Close(db *gorm.DB, logger *slog.Logger) error {
	return nil
}
func (d *mockDriver) SupportsCheckpoint() bool { return false }
func (d *mockDriver) Checkpoint(db *gorm.DB, mode string) error {
	return nil
}

func TestManager_Connect(t *testing.T) {
	driver := &mockDriver{}
	cfg := &Config{
		DSN:          ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	}

	manager := NewManager(driver, cfg, testLogger())

	// Test Connect
	db, err := manager.Connect()
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if db == nil {
		t.Fatal("expected non-nil db")
	}

	// Test GetConnection
	db2 := manager.GetConnection()
	if db2 == nil {
		t.Fatal("expected non-nil db from GetConnection")
	}

	// Test query
	var result int
	if err := db.Raw("SELECT 1").Scan(&result).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}

	// Test Close
	if err := manager.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestManager_Driver(t *testing.T) {
	driver := &mockDriver{}
	manager := NewManager(driver, nil, nil)

	if manager.Driver() != driver {
		t.Error("expected same driver instance")
	}
	if manager.Driver().Name() != "mock-sqlite" {
		t.Errorf("expected mock-sqlite, got %s", manager.Driver().Name())
	}
}

func TestManager_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/tmp/test.db")
	if cfg.DSN != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %s", cfg.DSN)
	}
	if cfg.MaxOpenConns != 1 {
		t.Errorf("expected 1, got %d", cfg.MaxOpenConns)
	}
	if !cfg.SQLite.EnableWAL {
		t.Error("expected EnableWAL to be true by default")
	}
}
