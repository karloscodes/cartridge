package cartridge

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testModel is a simple model for testing AutoMigrator.
type testModel struct {
	ID   uint   `gorm:"primarykey"`
	Name string `gorm:"size:100"`
}

func TestAutoMigrator(t *testing.T) {
	t.Run("NewAutoMigrator creates migrator with models", func(t *testing.T) {
		migrator := NewAutoMigrator(&testModel{})
		if migrator == nil {
			t.Error("expected non-nil migrator")
		}
		if len(migrator.models) != 1 {
			t.Errorf("expected 1 model, got %d", len(migrator.models))
		}
	})

	t.Run("Migrate runs successfully with models", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("failed to open test database: %v", err)
		}

		migrator := NewAutoMigrator(&testModel{})
		err = migrator.Migrate(db)
		if err != nil {
			t.Errorf("Migrate failed: %v", err)
		}

		// Verify table was created
		if !db.Migrator().HasTable(&testModel{}) {
			t.Error("expected table to be created")
		}
	})

	t.Run("Migrate does nothing with no models", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("failed to open test database: %v", err)
		}

		migrator := NewAutoMigrator()
		err = migrator.Migrate(db)
		if err != nil {
			t.Errorf("Migrate with no models should not fail: %v", err)
		}
	})
}

// mockDBManager implements DBManager for testing.
type mockDBManager struct {
	db  *gorm.DB
	err error
}

func (m *mockDBManager) Connect() (*gorm.DB, error) {
	return m.db, m.err
}

func (m *mockDBManager) GetConnection() *gorm.DB {
	return m.db
}

func TestRunMigrations(t *testing.T) {
	t.Run("succeeds with valid migrator", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("failed to open test database: %v", err)
		}

		manager := &mockDBManager{db: db}
		migrator := NewAutoMigrator(&testModel{})

		err = RunMigrations(manager, migrator)
		if err != nil {
			t.Errorf("RunMigrations failed: %v", err)
		}
	})

	t.Run("returns error when connection fails", func(t *testing.T) {
		manager := &mockDBManager{err: gorm.ErrInvalidDB}
		migrator := NewAutoMigrator(&testModel{})

		err := RunMigrations(manager, migrator)
		if err == nil {
			t.Error("expected error when connection fails")
		}
	})
}
