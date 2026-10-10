// Package dialect holds the few differences between SQLite, PostgreSQL,
// and MySQL that cartridge's own SQL needs.
package dialect

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Dialect is the SQL dialect of a database.
type Dialect int

const (
	SQLite Dialect = iota
	Postgres
	MySQL
)

// Of returns the dialect of db, from the type of its driver. A driver it
// does not know counts as SQLite.
func Of(db *sql.DB) Dialect {
	driver := strings.ToLower(fmt.Sprintf("%T", db.Driver()))
	switch {
	case strings.Contains(driver, "pgx"), strings.Contains(driver, "stdlib"), strings.Contains(driver, "pq."):
		return Postgres
	case strings.Contains(driver, "mysql"):
		return MySQL
	}
	return SQLite
}

// Rebind turns the "?" placeholders of query into the form of the dialect:
// "$1", "$2" for PostgreSQL. The query must hold no "?" in a string.
func (d Dialect) Rebind(query string) string {
	if d != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Blob is the column type for bytes.
func (d Dialect) Blob() string {
	switch d {
	case Postgres:
		return "BYTEA"
	case MySQL:
		return "LONGBLOB"
	}
	return "BLOB"
}

// Upsert returns the end of an INSERT that replaces the given columns when
// a row with the same key exists.
func (d Dialect) Upsert(key string, columns ...string) string {
	sets := make([]string, len(columns))
	if d == MySQL {
		for i, c := range columns {
			sets[i] = c + " = VALUES(" + c + ")"
		}
		return "ON DUPLICATE KEY UPDATE " + strings.Join(sets, ", ")
	}
	for i, c := range columns {
		sets[i] = c + " = excluded." + c
	}
	return "ON CONFLICT (" + key + ") DO UPDATE SET " + strings.Join(sets, ", ")
}

// IsDuplicate reports whether err says that a row with the same key exists.
func IsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}
