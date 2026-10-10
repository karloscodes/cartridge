package cartridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/sqlite"
)

// WriteQueue is a DBManager that runs writes one at a time.
// sqlite.Manager implements it.
type WriteQueue interface {
	Write(ctx context.Context, fn func(tx *gorm.DB) error) error
}

// Write runs fn in one write transaction, one write at a time per manager.
//
// Writes wait for their turn in order and fail with sqlite.ErrBusy when the
// wait is too long. Answer that with 503 and Retry-After. A WriteQueue
// (sqlite.Manager) keeps its own turn and WriteWait. Other managers, such as
// a test manager, get a turn here, so writes behave the same in tests.
func Write(ctx context.Context, m DBManager, fn func(tx *gorm.DB) error) error {
	if q, ok := m.(WriteQueue); ok {
		return q.Write(ctx, fn)
	}
	db := m.GetConnection()
	if db == nil {
		return fmt.Errorf("cartridge: database connection failed")
	}

	turn := writeTurnFor(m)
	wait := time.NewTimer(fallbackWriteWait)
	defer wait.Stop()
	select {
	case turn <- struct{}{}:
	case <-wait.C:
		return fmt.Errorf("%w: waited %v for a write turn", sqlite.ErrBusy, fallbackWriteWait)
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-turn }()

	return transaction(context.WithoutCancel(ctx), db, fn)
}

// fallbackWriteWait matches the sqlite.Manager default WriteWait.
const fallbackWriteWait = 5 * time.Second

// writeTurns holds one write turn per manager that is not a WriteQueue.
var writeTurns sync.Map

func writeTurnFor(m DBManager) chan struct{} {
	turn, _ := writeTurns.LoadOrStore(m, make(chan struct{}, 1))
	return turn.(chan struct{})
}

// transaction runs fn in a plain transaction. A SQLite lock error becomes
// sqlite.ErrBusy, as it does in a WriteQueue.
func transaction(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	err := db.WithContext(ctx).Transaction(fn)
	if sqlite.IsBusyError(err) {
		return fmt.Errorf("%w: %w", sqlite.ErrBusy, err)
	}
	return err
}

// WriteTx runs fn in one write transaction for this request. See Write.
func (ctx *Context) WriteTx(fn func(tx *gorm.DB) error) error {
	return Write(ctx.Context(), ctx.DBManager, fn)
}

// DatabaseWriteTx runs fn in one write transaction on the named database.
// See Write and Database. On a database opened with sqlite.Config.ReadOnly,
// it returns sqlite.ErrReadOnly.
func (ctx *Context) DatabaseWriteTx(name string, fn func(tx *gorm.DB) error) error {
	m := ctx.namedDatabase(name)
	if m == nil {
		return fmt.Errorf("cartridge: no database named %q", name)
	}
	return Write(ctx.Context(), m, fn)
}

// WriteTx runs fn in one write transaction for this job. See Write.
func (ctx *JobContext) WriteTx(fn func(tx *gorm.DB) error) error {
	if ctx.dbManager == nil {
		return transaction(ctx.context(), ctx.DB, fn)
	}
	return Write(ctx.context(), ctx.dbManager, fn)
}

// DatabaseWriteTx runs fn in one write transaction on the named database
// for this job. See Context.DatabaseWriteTx.
func (ctx *JobContext) DatabaseWriteTx(name string, fn func(tx *gorm.DB) error) error {
	m := ctx.databases[name]
	if m == nil {
		return fmt.Errorf("cartridge: no database named %q", name)
	}
	return Write(ctx.context(), m, fn)
}
