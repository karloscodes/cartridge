package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrBusy means a write did not get its turn in time. Answer it with 503 and
// Retry-After, so the client sends it again later.
var ErrBusy = errors.New("sqlite: database busy")

// Write runs fn in one transaction, one write at a time.
//
// SQLite allows one writer. Writers wait for their turn in a Go queue, in
// arrival order, and hold no pool connection while they wait. Readers keep
// the rest of the pool. A writer that waits longer than Config.WriteWait gets
// ErrBusy, so overload turns into 503s instead of a growing queue.
//
// The deadline covers only the wait. Once fn starts, the transaction runs to
// commit or rollback, even when ctx is canceled. fn must use tx for every
// query, and must not call Write again: the second call waits for the first,
// which never ends, and fails with ErrBusy after WriteWait.
//
// Write does not retry. Run nothing slow in fn, such as an HTTP call or an
// email, because every other writer waits for it.
//
// On a ReadOnly manager, Write returns ErrReadOnly and does not run fn.
func (m *Manager) Write(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if m.cfg.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, m.cfg.Path)
	}
	db, err := m.Connect()
	if err != nil {
		return err
	}

	wait := time.NewTimer(m.cfg.WriteWait)
	defer wait.Stop()
	select {
	case m.writeTurn <- struct{}{}:
	case <-wait.C:
		return fmt.Errorf("%w: waited %v for a write turn", ErrBusy, m.cfg.WriteWait)
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-m.writeTurn }()

	err = db.WithContext(context.WithoutCancel(ctx)).Transaction(fn)
	if IsBusyError(err) {
		// Another process, or a write outside Write, held the lock
		// longer than busy_timeout.
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return err
}
