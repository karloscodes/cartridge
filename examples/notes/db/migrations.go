// Package db holds the schema and the queries of the notes app.
//
// The schema is the SQL files in migrations/. The queries are in
// queries.sql. sqlc reads both and writes db.go, models.go, and
// queries.sql.go: do not edit those three files.
package db

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations are the .sql files that cartridge.NewSQLMigrator runs.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return sub
}
