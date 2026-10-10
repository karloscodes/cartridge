package cartridge

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

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

// SQLMigrator runs the .sql files of a folder, in the order of their names,
// each one once.
type SQLMigrator struct {
	fsys fs.FS
}

// NewSQLMigrator creates a migrator for the .sql files at the top of fsys,
// for a schema that the app writes in SQL, without GORM:
//
//	//go:embed migrations/*.sql
//	var migrations embed.FS
//
//	sub, _ := fs.Sub(migrations, "migrations")
//	err := app.MigrateDatabase(cartridge.NewSQLMigrator(sub))
//
// Name the files so that they sort in order, for example
// "0001_create_notes.sql". A name holds letters, digits, ".", "_", and "-".
// Each file runs in one transaction, and the table schema_migrations
// records it, so it never runs again. Never change a file that ran: add a
// new one. There are no down migrations.
//
// A file can hold several statements on SQLite and PostgreSQL. MySQL takes
// one statement per file, and it cannot undo a CREATE or ALTER when a later
// statement fails.
func NewSQLMigrator(fsys fs.FS) *SQLMigrator {
	return &SQLMigrator{fsys: fsys}
}

// Migrate runs the files that did not run before.
func (m *SQLMigrator) Migrate(db *gorm.DB) error {
	names, err := fs.Glob(m.fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("cartridge: list migrations: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("cartridge: no .sql files to migrate; check the folder")
	}
	slices.Sort(names)
	for _, name := range names {
		if !validMigrationName(name) {
			return fmt.Errorf("cartridge: migration name %q: use letters, digits, \".\", \"_\", and \"-\"", name)
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY)"); err != nil {
		return fmt.Errorf("cartridge: create schema_migrations: %w", err)
	}
	rows, err := sqlDB.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("cartridge: read schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			_ = rows.Close()
			return err
		}
		applied[version] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, name := range names {
		if applied[name] {
			continue
		}
		content, err := fs.ReadFile(m.fsys, name)
		if err != nil {
			return fmt.Errorf("cartridge: migration %s: %w", name, err)
		}
		tx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("cartridge: migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, string(content)); err == nil {
			// The name holds only safe characters, see validMigrationName.
			// Placeholders differ between databases, so it goes in the text.
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ('"+name+"')")
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("cartridge: migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("cartridge: migration %s: %w", name, err)
		}
	}
	return nil
}

// validMigrationName reports whether name holds only letters, digits, ".",
// "_", and "-". Such a name is safe inside an SQL string.
func validMigrationName(name string) bool {
	return name != "" && strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") == ""
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
