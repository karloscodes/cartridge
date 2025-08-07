package cartridge

import (
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
)

// BinaryMigration represents a programmatic migration function
type BinaryMigration struct {
	Version     uint
	Description string
	Up          func(*gorm.DB) error
	Down        func(*gorm.DB) error
}

// MigrationManager handles database migrations using golang-migrate under the hood
type MigrationManager struct {
	db               *gorm.DB
	logger           Logger
	migrate          *migrate.Migrate
	binaryMigrations []BinaryMigration
	usingBinary      bool
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db *gorm.DB, logger Logger) *MigrationManager {
	return &MigrationManager{
		db:               db,
		logger:           logger,
		binaryMigrations: []BinaryMigration{},
		usingBinary:      false,
	}
}

// AddBinaryMigration adds a programmatic migration to be executed in memory
func (mm *MigrationManager) AddBinaryMigration(version uint, description string, up, down func(*gorm.DB) error) {
	migration := BinaryMigration{
		Version:     version,
		Description: description,
		Up:          up,
		Down:        down,
	}
	
	mm.binaryMigrations = append(mm.binaryMigrations, migration)
	mm.usingBinary = true
	
	mm.logger.Debug("Binary migration added", "version", version, "description", description)
}

// LoadFromFS loads migrations from embedded filesystem using golang-migrate
// Expects files like: 001_create_users.up.sql, 001_create_users.down.sql
func (mm *MigrationManager) LoadFromFS(fsys embed.FS, dir string) error {
	if mm.usingBinary {
		return fmt.Errorf("cannot mix SQL file migrations with binary migrations")
	}
	mm.logger.Info("Loading migrations from embedded filesystem", "dir", dir)
	
	// Get the underlying sql.DB from GORM
	sqlDB, err := mm.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB from GORM: %w", err)
	}
	
	// Create source from embedded filesystem
	sourceDriver, err := iofs.New(fsys, dir)
	if err != nil {
		return fmt.Errorf("failed to create iofs source driver: %w", err)
	}
	
	// Create database driver
	databaseDriver, err := sqlite3.WithInstance(sqlDB, &sqlite3.Config{})
	if err != nil {
		return fmt.Errorf("failed to create sqlite3 database driver: %w", err)
	}
	
	// Create migrate instance
	migrator, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite3", databaseDriver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	
	mm.migrate = migrator
	
	// Count migrations for logging
	version, dirty, err := mm.migrate.Version()
	if err != nil && err != migrate.ErrNilVersion {
		mm.logger.Warn("Could not determine migration version", "error", err)
	} else if err == migrate.ErrNilVersion {
		mm.logger.Info("No migrations applied yet")
	} else {
		mm.logger.Info("Current migration state", "version", version, "dirty", dirty)
	}
	
	mm.logger.Info("Migration manager ready")
	return nil
}

// Up runs all pending migrations
func (mm *MigrationManager) Up() error {
	if mm.usingBinary {
		return mm.runBinaryMigrationsUp()
	}
	
	if mm.migrate == nil {
		return fmt.Errorf("migration manager not initialized - call LoadFromFS first")
	}
	
	mm.logger.Info("Running database migrations")
	
	// Get current version before migration
	beforeVersion, _, err := mm.migrate.Version()
	if err != nil && err != migrate.ErrNilVersion {
		mm.logger.Warn("Could not get version before migration", "error", err)
		beforeVersion = 0
	} else if err == migrate.ErrNilVersion {
		beforeVersion = 0
	}
	
	// Run migrations
	err = mm.migrate.Up()
	if err != nil {
		if err == migrate.ErrNoChange {
			mm.logger.Info("No pending migrations")
			return nil
		}
		mm.logger.Error("Migration failed", "error", err)
		return fmt.Errorf("migration failed: %w", err)
	}
	
	// Get version after migration
	afterVersion, dirty, err := mm.migrate.Version()
	if err != nil {
		mm.logger.Warn("Could not get version after migration", "error", err)
	} else {
		applied := int(afterVersion) - int(beforeVersion)
		if applied < 0 {
			applied = int(afterVersion) // In case beforeVersion was 0
		}
		mm.logger.Info("Database migrations completed", 
			"applied", applied, 
			"current_version", afterVersion, 
			"dirty", dirty)
	}
	
	return nil
}

// Down rolls back the latest migration
func (mm *MigrationManager) Down() error {
	if mm.usingBinary {
		return mm.runBinaryMigrationsDown()
	}
	
	if mm.migrate == nil {
		return fmt.Errorf("migration manager not initialized - call LoadFromFS first")
	}
	
	mm.logger.Info("Rolling back latest migration")
	
	// Get current version
	version, _, err := mm.migrate.Version()
	if err != nil {
		if err == migrate.ErrNilVersion {
			mm.logger.Info("No migrations to rollback")
			return nil
		}
		return fmt.Errorf("failed to get current version: %w", err)
	}
	
	// Rollback one step
	err = mm.migrate.Steps(-1)
	if err != nil {
		if err == migrate.ErrNoChange {
			mm.logger.Info("No migrations to rollback")
			return nil
		}
		mm.logger.Error("Rollback failed", "error", err)
		return fmt.Errorf("rollback failed: %w", err)
	}
	
	mm.logger.Info("Rollback completed", "from_version", version)
	return nil
}

// Status shows migration status
func (mm *MigrationManager) Status() error {
	if mm.usingBinary {
		return mm.showBinaryMigrationStatus()
	}
	
	if mm.migrate == nil {
		return fmt.Errorf("migration manager not initialized - call LoadFromFS first")
	}
	
	version, dirty, err := mm.migrate.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration status: %w", err)
	}
	
	mm.logger.Info("Migration Status")
	mm.logger.Info("================")
	
	if err == migrate.ErrNilVersion {
		mm.logger.Info("No migrations applied")
	} else {
		mm.logger.Info("Current migration state", 
			"version", version, 
			"dirty", dirty)
		
		if dirty {
			mm.logger.Warn("Database is in dirty state - manual intervention may be required")
		}
	}
	
	return nil
}

// Force sets the migration version without running migrations (for fixing dirty state)
func (mm *MigrationManager) Force(version int) error {
	if mm.migrate == nil {
		return fmt.Errorf("migration manager not initialized - call LoadFromFS first")
	}
	
	mm.logger.Warn("Forcing migration version", "version", version)
	
	err := mm.migrate.Force(version)
	if err != nil {
		mm.logger.Error("Force migration failed", "error", err, "version", version)
		return fmt.Errorf("force migration failed: %w", err)
	}
	
	mm.logger.Info("Migration version forced", "version", version)
	return nil
}

// Close closes the migration manager
func (mm *MigrationManager) Close() error {
	if mm.migrate == nil {
		return nil
	}
	
	sourceErr, databaseErr := mm.migrate.Close()
	if sourceErr != nil || databaseErr != nil {
		var errors []string
		if sourceErr != nil {
			errors = append(errors, fmt.Sprintf("source: %v", sourceErr))
		}
		if databaseErr != nil {
			errors = append(errors, fmt.Sprintf("database: %v", databaseErr))
		}
		return fmt.Errorf("failed to close migrate: %s", strings.Join(errors, ", "))
	}
	
	return nil
}

// AddMigration manually adds a migration (programmatic migrations, not recommended for production)
// This is kept for backward compatibility but using SQL files is preferred
func (mm *MigrationManager) AddMigration(version int, name, upSQL, downSQL string) {
	mm.logger.Warn("AddMigration is deprecated - use embedded SQL files instead", 
		"version", version, "name", name)
}

// Legacy methods for backward compatibility (these log warnings and delegate to new methods)

// Migrate is an alias for Up() - kept for backward compatibility
func (mm *MigrationManager) Migrate() error {
	return mm.Up()
}

// Rollback is an alias for Down() - kept for backward compatibility  
func (mm *MigrationManager) Rollback() error {
	return mm.Down()
}

// Binary migration implementation methods

// Simple migration record for binary migrations
type SimpleMigrationRecord struct {
	Version   uint      `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"autoCreateTime"`
}

func (SimpleMigrationRecord) TableName() string {
	return "schema_migrations"
}

// runBinaryMigrationsUp executes binary migrations in order
func (mm *MigrationManager) runBinaryMigrationsUp() error {
	mm.logger.Info("Running binary migrations")
	
	// Ensure migrations table exists
	if err := mm.db.AutoMigrate(&SimpleMigrationRecord{}); err != nil {
		return fmt.Errorf("failed to create migration tracking table: %w", err)
	}
	
	// Get applied migrations
	var applied []SimpleMigrationRecord
	if err := mm.db.Find(&applied).Error; err != nil {
		return fmt.Errorf("failed to fetch applied migrations: %w", err)
	}
	
	appliedSet := make(map[uint]bool)
	for _, record := range applied {
		appliedSet[record.Version] = true
	}
	
	// Sort migrations by version
	migrations := mm.binaryMigrations
	for i := 0; i < len(migrations)-1; i++ {
		for j := i + 1; j < len(migrations); j++ {
			if migrations[i].Version > migrations[j].Version {
				migrations[i], migrations[j] = migrations[j], migrations[i]
			}
		}
	}
	
	pendingCount := 0
	for _, migration := range migrations {
		if appliedSet[migration.Version] {
			continue
		}
		
		mm.logger.Info("Running migration", 
			"version", migration.Version, 
			"description", migration.Description)
		
		// Run migration in transaction
		if err := mm.db.Transaction(func(tx *gorm.DB) error {
			// Execute the migration
			if err := migration.Up(tx); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}
			
			// Record the migration
			record := SimpleMigrationRecord{
				Version:   migration.Version,
				AppliedAt: time.Now(),
			}
			
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("failed to record migration: %w", err)
			}
			
			return nil
		}); err != nil {
			mm.logger.Error("Migration failed", 
				"version", migration.Version, 
				"description", migration.Description, 
				"error", err)
			return err
		}
		
		mm.logger.Info("Migration completed", 
			"version", migration.Version, 
			"description", migration.Description)
		pendingCount++
	}
	
	if pendingCount == 0 {
		mm.logger.Info("No pending migrations")
	} else {
		mm.logger.Info("Binary migrations completed", "applied", pendingCount)
	}
	
	return nil
}

// runBinaryMigrationsDown rolls back the latest migration
func (mm *MigrationManager) runBinaryMigrationsDown() error {
	mm.logger.Info("Rolling back latest binary migration")
	
	// Get the latest applied migration
	var latest SimpleMigrationRecord
	if err := mm.db.Order("version DESC").First(&latest).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			mm.logger.Info("No migrations to rollback")
			return nil
		}
		return fmt.Errorf("failed to find latest migration: %w", err)
	}
	
	// Find the migration definition
	var migration *BinaryMigration
	for i := range mm.binaryMigrations {
		if mm.binaryMigrations[i].Version == latest.Version {
			migration = &mm.binaryMigrations[i]
			break
		}
	}
	
	if migration == nil {
		return fmt.Errorf("migration definition not found for version %d", latest.Version)
	}
	
	if migration.Down == nil {
		return fmt.Errorf("no rollback defined for migration %d", latest.Version)
	}
	
	mm.logger.Info("Rolling back migration", 
		"version", migration.Version, 
		"description", migration.Description)
	
	// Rollback in transaction
	if err := mm.db.Transaction(func(tx *gorm.DB) error {
		// Execute rollback
		if err := migration.Down(tx); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		
		// Remove migration record
		if err := tx.Delete(&latest).Error; err != nil {
			return fmt.Errorf("failed to remove migration record: %w", err)
		}
		
		return nil
	}); err != nil {
		mm.logger.Error("Rollback failed", 
			"version", migration.Version, 
			"error", err)
		return err
	}
	
	mm.logger.Info("Rollback completed", 
		"version", migration.Version, 
		"description", migration.Description)
	
	return nil
}

// showBinaryMigrationStatus shows status for binary migrations
func (mm *MigrationManager) showBinaryMigrationStatus() error {
	// Get applied migrations
	var applied []SimpleMigrationRecord
	if err := mm.db.Find(&applied).Error; err != nil {
		return fmt.Errorf("failed to fetch applied migrations: %w", err)
	}
	
	appliedSet := make(map[uint]bool)
	for _, record := range applied {
		appliedSet[record.Version] = true
	}
	
	mm.logger.Info("Binary Migration Status")
	mm.logger.Info("=======================")
	
	// Sort migrations by version  
	migrations := mm.binaryMigrations
	for i := 0; i < len(migrations)-1; i++ {
		for j := i + 1; j < len(migrations); j++ {
			if migrations[i].Version > migrations[j].Version {
				migrations[i], migrations[j] = migrations[j], migrations[i]
			}
		}
	}
	
	appliedCount := 0
	pendingCount := 0
	
	for _, migration := range migrations {
		status := "✗ PENDING"
		if appliedSet[migration.Version] {
			status = "✓ APPLIED"
			appliedCount++
		} else {
			pendingCount++
		}
		
		mm.logger.Info(status, 
			"version", migration.Version, 
			"description", migration.Description)
	}
	
	mm.logger.Info("Summary", 
		"applied", appliedCount, 
		"pending", pendingCount, 
		"total", len(migrations))
	
	return nil
}