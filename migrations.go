package cartridge

import (
	"gorm.io/gorm"
)

// Migrator defines how to run database migrations.
type Migrator interface {
	// Migrate runs database migrations.
	Migrate(db *gorm.DB) error
}

// AutoMigrator uses GORM's AutoMigrate for simple migration needs.
type AutoMigrator struct {
	models []any
}

// NewAutoMigrator creates a migrator that auto-migrates the provided models.
func NewAutoMigrator(models ...any) *AutoMigrator {
	return &AutoMigrator{models: models}
}

// Migrate runs GORM AutoMigrate on all registered models.
func (m *AutoMigrator) Migrate(db *gorm.DB) error {
	if len(m.models) == 0 {
		return nil
	}
	return db.AutoMigrate(m.models...)
}

// RunMigrations is a helper to run migrations on an application's database.
// It connects to the database, runs the migrator, and returns any error.
func RunMigrations(dbManager DBManager, migrator Migrator) error {
	db, err := dbManager.Connect()
	if err != nil {
		return err
	}
	return migrator.Migrate(db)
}
