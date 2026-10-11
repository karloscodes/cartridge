package cartridge

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// Migrate runs the .sql files at the top of fsys on db, in the order of
// their names, each one once. NewApp calls it for WithMigrations.
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
func Migrate(db *sql.DB, fsys fs.FS) error {
	names, err := fs.Glob(fsys, "*.sql")
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

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(255) PRIMARY KEY)"); err != nil {
		return fmt.Errorf("cartridge: create schema_migrations: %w", err)
	}
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
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
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("cartridge: migration %s: %w", name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
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
