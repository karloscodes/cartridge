package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/sqlite"
)

type poolNote struct {
	ID     uint
	Body   string
	Status string `gorm:"default:new"`
}

// newPoolManager opens a database with a read pool and the notes table.
func newPoolManager(t *testing.T, cfg sqlite.Config) (*sqlite.Manager, *gorm.DB) {
	t.Helper()
	if cfg.Path == "" {
		cfg.Path = filepath.Join(t.TempDir(), "pool.db")
	}
	cfg.ReadPool = true
	m := sqlite.NewManager(cfg)
	t.Cleanup(func() { _ = m.Close() })
	db, err := m.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !cfg.ReadOnly {
		if err := db.AutoMigrate(&poolNote{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return m, db
}

func TestReadPool(t *testing.T) {
	t.Run("GORM code reads and writes as before", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		note := poolNote{Body: "hello"}

		createErr := db.Create(&note).Error
		updateErr := db.Model(&note).Update("body", "changed").Error
		var found poolNote
		findErr := db.First(&found, note.ID).Error
		var count int64
		countErr := db.Model(&poolNote{}).Where("body = ?", "changed").Count(&count).Error
		deleteErr := db.Delete(&note).Error

		if err := errors.Join(createErr, updateErr, findErr, countErr, deleteErr); err != nil {
			t.Fatal(err)
		}
		// Status has a database default, so GORM reads it back with the insert.
		if note.ID == 0 || note.Status != "new" || found.Body != "changed" || count != 1 {
			t.Errorf("note = %+v, found = %+v, count = %d", note, found, count)
		}
	})

	t.Run("a transaction reads its own writes, and a rollback leaves nothing", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		stop := errors.New("stop")
		var inside int64

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&poolNote{Body: "draft"}).Error; err != nil {
				return err
			}
			tx.Model(&poolNote{}).Count(&inside)
			return stop
		})

		var after int64
		db.Model(&poolNote{}).Count(&after)
		if !errors.Is(err, stop) || inside != 1 || after != 0 {
			t.Errorf("inside = %d, after = %d, err = %v, want 1, 0, and the error", inside, after, err)
		}
	})

	t.Run("writes at the same time do not fail with a locked database", func(t *testing.T) {
		// One millisecond of busy_timeout: two connections that write at
		// once would fail. One write connection cannot collide with itself.
		_, db := newPoolManager(t, sqlite.Config{MaxOpenConns: 8, BusyTimeout: 1})
		var wg sync.WaitGroup
		errs := make(chan error, 200)

		for i := range 40 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 5 {
					if err := db.Create(&poolNote{Body: "note"}).Error; err != nil {
						errs <- err
					}
					var n int64
					if err := db.Model(&poolNote{}).Where("id > ?", i).Count(&n).Error; err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Fatalf("a statement failed: %v", err)
		}
		var count int64
		db.Model(&poolNote{}).Count(&count)
		if count != 200 {
			t.Errorf("count = %d, want 200", count)
		}
	})

	t.Run("a read does not wait for an open write transaction", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{MaxOpenConns: 1})
		if err := db.Create(&poolNote{Body: "first"}).Error; err != nil {
			t.Fatal(err)
		}
		inWrite, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- m.Write(context.Background(), func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return tx.Create(&poolNote{Body: "second"}).Error
			})
		}()
		<-inWrite

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var count int64
		readErr := db.WithContext(ctx).Model(&poolNote{}).Count(&count).Error
		close(release)

		if readErr != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want the committed row with no wait", count, readErr)
		}
		if err := <-done; err != nil {
			t.Errorf("write: %v", err)
		}
	})

	t.Run("the reader refuses a statement that writes", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{})
		reader := m.Reader()
		if reader == nil {
			t.Fatal("Reader returned nil")
		}

		_, err := reader.Exec("INSERT INTO pool_notes (body) VALUES ('x')")

		if err == nil {
			t.Error("a write on the reader worked, want an error")
		}
	})

	t.Run("a read-only database reads, and a write returns ErrReadOnly", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		writer, db := newPoolManager(t, sqlite.Config{Path: path})
		if err := db.Create(&poolNote{Body: "seed"}).Error; err != nil {
			t.Fatal(err)
		}
		_ = writer.Close()
		_, readOnly := newPoolManager(t, sqlite.Config{Path: path, ReadOnly: true})

		var count int64
		readErr := readOnly.Model(&poolNote{}).Count(&count).Error
		writeErr := readOnly.Create(&poolNote{Body: "x"}).Error

		if readErr != nil || count != 1 || !errors.Is(writeErr, sqlite.ErrReadOnly) {
			t.Errorf("count = %d (%v), write error = %v, want 1 and ErrReadOnly", count, readErr, writeErr)
		}
	})

	t.Run("after Close, the next use opens both pools again", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "kept"}).Error; err != nil {
			t.Fatal(err)
		}

		closeErr := m.Close()
		again, connectErr := m.Connect()

		if closeErr != nil || connectErr != nil {
			t.Fatalf("close: %v, connect: %v", closeErr, connectErr)
		}
		var count int64
		if err := again.Model(&poolNote{}).Count(&count).Error; err != nil || count != 1 {
			t.Errorf("count = %d, err = %v", count, err)
		}
		if err := again.Create(&poolNote{Body: "more"}).Error; err != nil {
			t.Errorf("write after reopen: %v", err)
		}
	})

	t.Run("a database in memory keeps one pool", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: ":memory:", ReadPool: true})
		t.Cleanup(func() { _ = m.Close() })
		db, err := m.Connect()
		if err != nil {
			t.Fatal(err)
		}

		migrateErr := db.AutoMigrate(&poolNote{})
		createErr := db.Create(&poolNote{Body: "x"}).Error

		if migrateErr != nil || createErr != nil || m.Reader() != nil {
			t.Errorf("migrate: %v, create: %v, has a reader: %v", migrateErr, createErr, m.Reader() != nil)
		}
	})

	t.Run("without ReadPool, the manager has no reader", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "one.db")})
		t.Cleanup(func() { _ = m.Close() })

		if m.Reader() != nil {
			t.Error("Reader returned a pool, want nil")
		}
	})
}
