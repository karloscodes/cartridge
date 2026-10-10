package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/karloscodes/cartridge/internal/sqltx"
	"github.com/karloscodes/cartridge/sqlite"
)

// Write runs fn in one write transaction on the writer of m. Use tx for
// every query in fn. When fn returns an error or panics, the transaction
// rolls back.
//
// SQLite has one write connection, so writes run one at a time. A write
// that waits for the connection longer than the manager's WriteWait
// (default 5 seconds) fails with sqlite.ErrBusy. Answer that with 503 and
// Retry-After. Do not call Write inside fn: the second call waits for the
// connection that the first one holds. Run nothing slow in fn, such as an
// HTTP call, because every other write waits for it.
//
// PostgreSQL and MySQL have many write connections, so their writes run at
// the same time, each in its transaction.
//
// Once fn starts, the transaction runs to commit or rollback, also when
// ctx is canceled: a client that goes away does not leave half a write.
func Write(ctx context.Context, m DBManager, fn func(tx *sql.Tx) error) error {
	writer, err := m.Writer()
	if err != nil {
		return err
	}

	wait := defaultWriteWait
	if w, ok := m.(interface{ WriteWait() time.Duration }); ok && w.WriteWait() > 0 {
		wait = w.WriteWait()
	}
	// Only the wait for the connection has a deadline. The transaction
	// itself must not: a deadline on its context would roll it back.
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	conn, err := writer.Conn(waitCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: waited %v for the write connection", sqlite.ErrBusy, wait)
		}
		return err
	}
	defer func() { _ = conn.Close() }()

	err = sqltx.Run(context.WithoutCancel(ctx), conn, fn)
	if sqlite.IsBusyError(err) {
		// Another process held SQLite's lock longer than busy_timeout.
		return fmt.Errorf("%w: %w", sqlite.ErrBusy, err)
	}
	return err
}

// defaultWriteWait is the wait of a manager that names none.
const defaultWriteWait = 5 * time.Second

// WriteTx runs fn in one write transaction for this request. See Write.
//
//	err := ctx.WriteTx(func(tx *sql.Tx) error {
//	    return db.New(tx).CreateNote(ctx.Context(), params)
//	})
func (ctx *Context) WriteTx(fn func(tx *sql.Tx) error) error {
	return Write(ctx.Context(), ctx.DBManager, fn)
}

// DatabaseWriteTx runs fn in one write transaction on the named database.
// See Write and Database. On a database opened with sqlite.Config.ReadOnly,
// it returns sqlite.ErrReadOnly.
func (ctx *Context) DatabaseWriteTx(name string, fn func(tx *sql.Tx) error) error {
	m := ctx.namedDatabase(name)
	if m == nil {
		return fmt.Errorf("cartridge: no database named %q", name)
	}
	return Write(ctx.Context(), m, fn)
}

// WriteTx runs fn in one write transaction for this job. See Write.
func (ctx *JobContext) WriteTx(fn func(tx *sql.Tx) error) error {
	if ctx.dbManager == nil {
		return fmt.Errorf("cartridge: the job has no database manager")
	}
	return Write(ctx.context(), ctx.dbManager, fn)
}

// DatabaseWriteTx runs fn in one write transaction on the named database
// for this job. See Context.DatabaseWriteTx.
func (ctx *JobContext) DatabaseWriteTx(name string, fn func(tx *sql.Tx) error) error {
	m := ctx.databases[name]
	if m == nil {
		return fmt.Errorf("cartridge: no database named %q", name)
	}
	return Write(ctx.context(), m, fn)
}
