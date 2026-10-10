package sqlite

import (
	"errors"
	"strings"
)

// ErrBusy means a write did not get the write connection in time, or
// another process held SQLite's lock too long. Answer it with 503 and
// Retry-After, so the client sends it again later.
var ErrBusy = errors.New("sqlite: database busy")

// IsBusyError reports whether err is SQLite lock contention.
//
// It matches the driver's full messages for SQLITE_BUSY and SQLITE_LOCKED
// (sqlite3_errstr), never a bare "busy" or "locked": those words also appear
// in data, such as a domain named busybee.com. The driver's error code would
// be exact, but sqlite3.Error exists only under cgo.
func IsBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "database schema is locked")
}
