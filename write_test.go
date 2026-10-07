package cartridge

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/sqlite"
)

type writeRow struct {
	ID   uint
	Name string
}

func TestWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("uses the write queue of a sqlite.Manager", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "q.db"), WriteWait: 50 * time.Millisecond})
		defer func() { _ = m.Close() }()
		db, _ := m.Connect()
		_ = db.AutoMigrate(&writeRow{})
		started, release := make(chan struct{}), make(chan struct{})
		go func() {
			_ = m.Write(ctx, func(tx *gorm.DB) error {
				close(started)
				<-release
				return nil
			})
		}()
		<-started

		err := Write(ctx, m, func(tx *gorm.DB) error { return tx.Create(&writeRow{Name: "a"}).Error })
		close(release)

		if !errors.Is(err, sqlite.ErrBusy) {
			t.Fatalf("err = %v, want sqlite.ErrBusy", err)
		}
	})

	t.Run("runs a plain transaction with another manager", func(t *testing.T) {
		db, _ := gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&writeRow{})

		err := Write(ctx, &mockDBManager{db: db}, func(tx *gorm.DB) error {
			return tx.Create(&writeRow{Name: "a"}).Error
		})

		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		var n int64
		db.Model(&writeRow{}).Count(&n)
		if n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
	})

	t.Run("runs one write at a time with another manager", func(t *testing.T) {
		db, _ := gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{})
		m := &mockDBManager{db: db}
		var inFlight, most atomic.Int32
		var wg sync.WaitGroup

		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = Write(ctx, m, func(tx *gorm.DB) error {
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

	t.Run("works in a JobContext built by hand", func(t *testing.T) {
		db, _ := gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&writeRow{})
		job := &JobContext{DB: db}

		err := job.WriteTx(func(tx *gorm.DB) error { return tx.Create(&writeRow{Name: "a"}).Error })

		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	})
}
