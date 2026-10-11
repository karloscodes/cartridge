package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karloscodes/cartridge/query"
	"github.com/karloscodes/cartridge/sqlite"
)

// newWriteManager returns a SQLite database with an empty rows table.
func newWriteManager(t *testing.T, cfg sqlite.Config) *sqlite.Manager {
	t.Helper()
	if cfg.Path == "" {
		cfg.Path = filepath.Join(t.TempDir(), "write.db")
	}
	cfg.Logger = testLogger()
	m := sqlite.NewManager(cfg)
	t.Cleanup(func() { _ = m.Close() })
	writer, err := m.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("CREATE TABLE IF NOT EXISTS rows (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	return m
}

func insertRow(ctx context.Context, name string) func(tx *sql.Tx) error {
	return func(tx *sql.Tx) error {
		_, err := query.Exec(ctx, tx, "INSERT INTO rows (name) VALUES (?)", name)
		return err
	}
}

func countRows(t *testing.T, m *sqlite.Manager) int {
	t.Helper()
	reader, err := m.Reader()
	if err != nil {
		t.Fatal(err)
	}
	count, err := query.One[int](context.Background(), reader, "SELECT COUNT(*) FROM rows")
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// holdWrite starts a Write on m that stays open until the test calls the
// function it returns.
func holdWrite(t *testing.T, m *sqlite.Manager) (release func()) {
	t.Helper()
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		_ = Write(context.Background(), m, func(tx *sql.Tx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	return sync.OnceFunc(func() { close(done) })
}

func TestWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("commits the transaction", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})

		err := Write(ctx, m, insertRow(ctx, "a"))

		if err != nil || countRows(t, m) != 1 {
			t.Errorf("err = %v, rows = %d, want 1 row", err, countRows(t, m))
		}
	})

	t.Run("rolls back when fn fails, and the next write works", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		failed := errors.New("stop")

		err := Write(ctx, m, func(tx *sql.Tx) error {
			if err := insertRow(ctx, "lost")(tx); err != nil {
				return err
			}
			return failed
		})
		next := Write(ctx, m, insertRow(ctx, "kept"))

		if !errors.Is(err, failed) || next != nil || countRows(t, m) != 1 {
			t.Errorf("err = %v, next = %v, rows = %d, want the error and 1 row", err, next, countRows(t, m))
		}
	})

	t.Run("rolls back when fn panics, and the next write works", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{WriteWait: 200 * time.Millisecond})

		func() {
			defer func() { _ = recover() }()
			_ = Write(ctx, m, func(tx *sql.Tx) error {
				_ = insertRow(ctx, "lost")(tx)
				panic("boom")
			})
		}()
		next := Write(ctx, m, insertRow(ctx, "kept"))

		if next != nil || countRows(t, m) != 1 {
			t.Errorf("next = %v, rows = %d, want 1 row", next, countRows(t, m))
		}
	})

	t.Run("runs one write at a time", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		var inFlight, most atomic.Int32
		var wg sync.WaitGroup

		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = Write(ctx, m, func(tx *sql.Tx) error {
					n := inFlight.Add(1)
					if n > most.Load() {
						most.Store(n)
					}
					time.Sleep(5 * time.Millisecond)
					inFlight.Add(-1)
					return nil
				})
			}()
		}
		wg.Wait()

		if most.Load() != 1 {
			t.Errorf("most writes at once = %d, want 1", most.Load())
		}
	})

	t.Run("a write that waits longer than WriteWait returns sqlite.ErrBusy", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{WriteWait: 50 * time.Millisecond})
		release := holdWrite(t, m)
		defer release()

		begin := time.Now()
		err := Write(ctx, m, insertRow(ctx, "a"))

		if !errors.Is(err, sqlite.ErrBusy) || time.Since(begin) > time.Second {
			t.Errorf("err = %v after %v, want sqlite.ErrBusy after about 50ms", err, time.Since(begin))
		}
	})

	t.Run("a caller that leaves while it waits gets its context error", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		release := holdWrite(t, m)
		defer release()
		gone, cancel := context.WithCancel(ctx)
		cancel()

		err := Write(gone, m, insertRow(ctx, "a"))

		if !errors.Is(err, context.Canceled) || errors.Is(err, sqlite.ErrBusy) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("rolls back as a whole when the caller leaves before a statement", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		request, cancel := context.WithCancel(ctx)

		err := Write(request, m, func(tx *sql.Tx) error {
			if err := insertRow(request, "first")(tx); err != nil {
				return err
			}
			cancel()
			return insertRow(request, "second")(tx)
		})

		if !errors.Is(err, context.Canceled) || countRows(t, m) != 0 {
			t.Errorf("err = %v, rows = %d, want context.Canceled and no row", err, countRows(t, m))
		}
	})

	t.Run("commits when the caller leaves after the last statement", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		request, cancel := context.WithCancel(ctx)

		err := Write(request, m, func(tx *sql.Tx) error {
			defer cancel()
			return insertRow(request, "a")(tx)
		})

		if err != nil || countRows(t, m) != 1 {
			t.Errorf("err = %v, rows = %d, want 1 row", err, countRows(t, m))
		}
	})

	t.Run("returns sqlite.ErrBusy when another process holds the lock", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		m := newWriteManager(t, sqlite.Config{Path: path, BusyTimeout: 50})
		other := newWriteManager(t, sqlite.Config{Path: path})
		release := holdWrite(t, other)
		defer release()

		err := Write(ctx, m, insertRow(ctx, "a"))

		if !errors.Is(err, sqlite.ErrBusy) {
			t.Errorf("err = %v, want sqlite.ErrBusy", err)
		}
	})

	t.Run("on a read-only database, returns sqlite.ErrReadOnly and does not run fn", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		_ = newWriteManager(t, sqlite.Config{Path: path}).Close()
		m := sqlite.NewManager(sqlite.Config{Path: path, ReadOnly: true, Logger: testLogger()})
		t.Cleanup(func() { _ = m.Close() })
		ran := false

		err := Write(ctx, m, func(tx *sql.Tx) error {
			ran = true
			return nil
		})

		if !errors.Is(err, sqlite.ErrReadOnly) || ran {
			t.Errorf("err = %v, ran = %v, want sqlite.ErrReadOnly and no run", err, ran)
		}
	})

	t.Run("a JobContext built by hand has no database to write", func(t *testing.T) {
		job := &JobContext{}

		err := job.WriteTx(func(tx *sql.Tx) error { return nil })

		if err == nil {
			t.Error("WriteTx returned nil, want an error")
		}
	})
}
