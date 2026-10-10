package sqlite

import (
	"database/sql"
	"sync"

	"gorm.io/gorm"
)

// With ReadPool, GORM runs on the one write connection, and its read
// operations move to the pool of read-only connections.
//
// GORM itself says which operations read: Find, First, Take, Scan, Count,
// Pluck, Rows, and Row run its query and row callbacks. Create, Update,
// Delete, and Exec run the others. So the method that the app calls decides
// the pool, not the SQL text. A statement in a transaction stays on the
// transaction, and every transaction is on the write connection.
//
// A write through a read method, such as Raw("UPDATE ... RETURNING
// id").Scan(&id), runs on a read-only connection, and SQLite refuses it.
// Run such a statement in a transaction.
//
// SQLite allows one writer. With one write connection, writes wait for each
// other in Go and never fail with "database is locked" against each other,
// and readers keep their own connections.
//
// The read pool keeps no prepared statements. Measured on two apps, a
// statement cache gave nothing on simple queries and made analytics
// queries 2.5 times slower: SQLite plans a fresh statement with the values
// of its arguments, and a reused one keeps its first plan.

// readPools maps the pools of a manager with ReadPool to its read pool, for
// ReadPoolOf.
var readPools sync.Map // *sql.DB -> *sql.DB

// routeReads registers the callbacks that move a read operation of db from
// writer to reader, and a later write on the same statement back.
func routeReads(db *gorm.DB, writer *sql.DB, reader gorm.ConnPool) error {
	toReader := func(tx *gorm.DB) {
		// Only a statement on the write pool itself moves. One in a
		// transaction, or on a connection of its own, stays where it is.
		if pool, ok := tx.Statement.ConnPool.(*sql.DB); ok && pool == writer {
			tx.Statement.ConnPool = reader
		}
	}
	toWriter := func(tx *gorm.DB) {
		// A statement that read before is reused for a write.
		if tx.Statement.ConnPool == reader {
			tx.Statement.ConnPool = writer
		}
	}

	callbacks := db.Callback()
	const name = "cartridge:read_pool"
	for _, register := range []func() error{
		func() error { return callbacks.Query().Before("*").Register(name, toReader) },
		func() error { return callbacks.Row().Before("*").Register(name, toReader) },
		func() error { return callbacks.Create().Before("*").Register(name, toWriter) },
		func() error { return callbacks.Update().Before("*").Register(name, toWriter) },
		func() error { return callbacks.Delete().Before("*").Register(name, toWriter) },
		func() error { return callbacks.Raw().Before("*").Register(name, toWriter) },
	} {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

// ReadPoolOf returns the pool of read-only connections behind a GORM handle
// of a manager with ReadPool. Use it for code that needs its own connection
// for a read, such as a long report:
//
//	if pool, ok := sqlite.ReadPoolOf(db); ok {
//	    conn, err := pool.Conn(ctx)
//	    ...
//	}
//
// db.DB() is the wrong pool for that: with ReadPool it is the one write
// connection, and holding it stops every write. ok is false without
// ReadPool, and for the handle of a transaction.
func ReadPoolOf(db *gorm.DB) (*sql.DB, bool) {
	if db == nil || db.Statement == nil {
		return nil, false
	}
	pool, ok := db.Statement.ConnPool.(*sql.DB)
	if !ok {
		return nil, false // a transaction, or a connection of its own
	}
	reader, ok := readPools.Load(pool)
	if !ok {
		return nil, false
	}
	return reader.(*sql.DB), true
}
