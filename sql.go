package cartridge

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

// SQL returns the *sql.DB of the main database, for queries without GORM.
// It is the same connection pool that DB uses. Read rows into structs with
// the query package:
//
//	notes, err := query.All[Note](ctx.Context(), ctx.SQL(), "SELECT id, body FROM notes WHERE user_id = ?", userID)
//
// With sqlite.Config.ReadPool, SQL returns the pool of read-only
// connections: write through WriteSQL. Like DB, it panics when the
// database connection fails.
func (ctx *Context) SQL() *sql.DB {
	if m, ok := ctx.DBManager.(interface{ Reader() *sql.DB }); ok {
		if reader := m.Reader(); reader != nil {
			return reader
		}
	}
	db, err := ctx.DB().DB()
	if err != nil {
		panic("cartridge: database connection failed: " + err.Error())
	}
	return db
}

// DataVersion returns a token that changes after every commit to the main
// database. Put it in a cache key or an ETag to keep a value until the data
// changes:
//
//	version, err := ctx.DataVersion()
//	stats, err := cache.Fetch(ctx.Context(), store, "stats:"+siteID+":"+version, time.Hour, load)
//
// The token is good for the life of the process, so use it with a cache in
// memory. It needs a SQLite database file. See sqlite.Manager.DataVersion.
func (ctx *Context) DataVersion() (string, error) {
	m, ok := ctx.DBManager.(interface {
		DataVersion(context.Context) (string, error)
	})
	if !ok {
		return "", fmt.Errorf("cartridge: the database manager has no data version")
	}
	return m.DataVersion(ctx.Context())
}

// WriteSQL runs fn in one write transaction for this request, like WriteTx,
// with a *sql.Tx. It uses the same write queue. See Write.
func (ctx *Context) WriteSQL(fn func(tx *sql.Tx) error) error {
	return WriteSQL(ctx.Context(), ctx.DBManager, fn)
}

// WriteSQL runs fn in one write transaction with a *sql.Tx. It follows the
// rules of Write: one write at a time on SQLite, and ErrBusy when the wait
// is too long.
func WriteSQL(ctx context.Context, m DBManager, fn func(tx *sql.Tx) error) error {
	return Write(ctx, m, withSQLTx(fn))
}

// SQL returns the *sql.DB of the job's database, for queries without GORM.
func (ctx *JobContext) SQL() (*sql.DB, error) {
	if ctx.DB == nil {
		return nil, fmt.Errorf("cartridge: the job has no database")
	}
	return ctx.DB.DB()
}

// WriteSQL runs fn in one write transaction for this job, with a *sql.Tx.
// See Write.
func (ctx *JobContext) WriteSQL(fn func(tx *sql.Tx) error) error {
	return ctx.WriteTx(withSQLTx(fn))
}

// withSQLTx gives fn the *sql.Tx that a GORM transaction runs on.
func withSQLTx(fn func(tx *sql.Tx) error) func(tx *gorm.DB) error {
	return func(tx *gorm.DB) error {
		sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
		if !ok {
			return fmt.Errorf("cartridge: the transaction runs on a %T, not a *sql.Tx", tx.Statement.ConnPool)
		}
		return fn(sqlTx)
	}
}
