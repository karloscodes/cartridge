package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/karloscodes/cartridge/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newWriteManager(t *testing.T, cfg sqlite.Config) *sqlite.Manager {
	t.Helper()
	cfg.Path = filepath.Join(t.TempDir(), "write.db")
	m := sqlite.NewManager(cfg)
	db, err := m.Connect()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TestModel{}))
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func countRows(t *testing.T, m *sqlite.Manager) int64 {
	t.Helper()
	var n int64
	require.NoError(t, m.GetConnection().Model(&TestModel{}).Count(&n).Error)
	return n
}

func TestWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("commits the transaction", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})

		err := m.Write(ctx, func(tx *gorm.DB) error {
			return tx.Create(&TestModel{Name: "a"}).Error
		})

		require.NoError(t, err)
		assert.Equal(t, int64(1), countRows(t, m))
	})

	t.Run("rolls back on error and frees the turn", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		boom := errors.New("boom")

		err := m.Write(ctx, func(tx *gorm.DB) error {
			if err := tx.Create(&TestModel{Name: "a"}).Error; err != nil {
				return err
			}
			return boom
		})
		next := m.Write(ctx, func(tx *gorm.DB) error {
			return tx.Create(&TestModel{Name: "b"}).Error
		})

		assert.ErrorIs(t, err, boom)
		require.NoError(t, next)
		assert.Equal(t, int64(1), countRows(t, m))
	})

	t.Run("frees the turn after a panic", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{WriteWait: 100 * time.Millisecond})

		func() {
			defer func() { _ = recover() }()
			_ = m.Write(ctx, func(tx *gorm.DB) error { panic("boom") })
		}()
		err := m.Write(ctx, func(tx *gorm.DB) error {
			return tx.Create(&TestModel{Name: "a"}).Error
		})

		require.NoError(t, err)
	})

	t.Run("returns ErrBusy when the wait passes WriteWait", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{WriteWait: 50 * time.Millisecond})
		started, release := make(chan struct{}), make(chan struct{})
		go func() {
			_ = m.Write(ctx, func(tx *gorm.DB) error {
				close(started)
				<-release
				return nil
			})
		}()
		<-started

		begin := time.Now()
		err := m.Write(ctx, func(tx *gorm.DB) error { return nil })
		close(release)

		assert.ErrorIs(t, err, sqlite.ErrBusy)
		assert.Less(t, time.Since(begin), time.Second)
	})

	t.Run("returns the context error when the caller leaves while waiting", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		started, release := make(chan struct{}), make(chan struct{})
		go func() {
			_ = m.Write(ctx, func(tx *gorm.DB) error {
				close(started)
				<-release
				return nil
			})
		}()
		<-started
		gone, cancel := context.WithCancel(ctx)
		cancel()

		err := m.Write(gone, func(tx *gorm.DB) error { return nil })
		close(release)

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, sqlite.ErrBusy)
	})

	t.Run("commits after the caller leaves mid-transaction", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{})
		reqCtx, cancel := context.WithCancel(ctx)

		err := m.Write(reqCtx, func(tx *gorm.DB) error {
			cancel()
			return tx.Create(&TestModel{Name: "a"}).Error
		})

		require.NoError(t, err)
		assert.Equal(t, int64(1), countRows(t, m))
	})

	t.Run("queued writers hold no pool connection, so reads go on", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{MaxOpenConns: 2, MaxIdleConns: 2})
		started, release := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Write(ctx, func(tx *gorm.DB) error {
				close(started)
				<-release
				return tx.Create(&TestModel{Name: "first"}).Error
			})
		}()
		<-started
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = m.Write(ctx, func(tx *gorm.DB) error {
					return tx.Create(&TestModel{Name: "queued"}).Error
				})
			}()
		}
		time.Sleep(20 * time.Millisecond) // let the writers queue

		readCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var n int64
		readErr := m.GetConnection().WithContext(readCtx).Model(&TestModel{}).Count(&n).Error
		close(release)
		wg.Wait()

		require.NoError(t, readErr)
		assert.Equal(t, int64(21), countRows(t, m))
	})

	t.Run("returns ErrBusy when a write outside Write holds the lock", func(t *testing.T) {
		m := newWriteManager(t, sqlite.Config{MaxOpenConns: 2, MaxIdleConns: 2, BusyTimeout: 50})
		sqlDB, err := m.GetConnection().DB()
		require.NoError(t, err)
		holder, err := sqlDB.BeginTx(ctx, nil) // BEGIN IMMEDIATE
		require.NoError(t, err)

		err = m.Write(ctx, func(tx *gorm.DB) error {
			return tx.Create(&TestModel{Name: "a"}).Error
		})
		_ = holder.Rollback()

		assert.ErrorIs(t, err, sqlite.ErrBusy)
	})
}
