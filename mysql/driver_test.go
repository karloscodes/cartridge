package mysql_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/cache"
	"github.com/karloscodes/cartridge/database"
	"github.com/karloscodes/cartridge/mysql"
)

func TestConfigureDSN(t *testing.T) {
	configure := func(dsn string) string {
		return mysql.NewDriver().ConfigureDSN(dsn, database.DefaultConfig(dsn))
	}

	t.Run("turns parseTime on", func(t *testing.T) {
		got := configure("app:secret@tcp(db.internal:3306)/shop")

		if want := "app:secret@tcp(db.internal:3306)/shop?parseTime=true"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("keeps the parameters of the DSN", func(t *testing.T) {
		got := configure("app@tcp(db.internal:3306)/shop?timeout=5s")

		if want := "app@tcp(db.internal:3306)/shop?parseTime=true&timeout=5s"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("returns a DSN that does not parse as it is", func(t *testing.T) {
		if got := configure("not a dsn"); got != "not a dsn" {
			t.Errorf("got %q", got)
		}
	})
}

type note struct {
	ID        uint
	Body      string
	CreatedAt time.Time
}

// newManager connects to the server in CARTRIDGE_MYSQL_DSN, in a new
// database that the test owns. Without the env var, the test is skipped.
func newManager(t *testing.T) *database.Manager {
	t.Helper()
	dsn := os.Getenv("CARTRIDGE_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CARTRIDGE_MYSQL_DSN is not set")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	name := fmt.Sprintf("t_%d", time.Now().UnixNano())

	admin := database.NewManager(mysql.NewDriver(), database.DefaultConfig(dsn), logger)
	adminDB, err := admin.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec("DROP DATABASE " + name).Error
		_ = admin.Close()
	})

	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse CARTRIDGE_MYSQL_DSN: %v", err)
	}
	parsed.DBName = name
	cfg := database.DefaultConfig(parsed.FormatDSN())
	cfg.MaxOpenConns = 2
	cfg.MaxIdleConns = 2
	m := database.NewManager(mysql.NewDriver(), cfg, logger)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func connect(t *testing.T, m *database.Manager) *gorm.DB {
	t.Helper()
	db, err := m.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db
}

func TestMySQL(t *testing.T) {
	t.Run("stores and reads a row, with its time and four-byte characters", func(t *testing.T) {
		m := newManager(t)
		db := connect(t, m)
		if err := db.AutoMigrate(&note{}); err != nil {
			t.Fatal(err)
		}
		created := time.Date(2026, 10, 10, 12, 30, 0, 0, time.UTC)

		err := db.Create(&note{Body: "Hello 👋", CreatedAt: created}).Error

		var got note
		if err != nil || db.First(&got).Error != nil {
			t.Fatalf("create: %v, read: %+v", err, got)
		}
		if got.Body != "Hello 👋" || !got.CreatedAt.Equal(created) || got.CreatedAt.Location() != time.UTC {
			t.Errorf("got %q at %v, want the same text at %v in UTC", got.Body, got.CreatedAt, created)
		}
	})

	t.Run("Write commits its transaction", func(t *testing.T) {
		m := newManager(t)
		db := connect(t, m)
		if err := db.AutoMigrate(&note{}); err != nil {
			t.Fatal(err)
		}

		err := cartridge.Write(context.Background(), m, func(tx *gorm.DB) error {
			return tx.Create(&note{Body: "kept"}).Error
		})

		var count int64
		db.Model(&note{}).Count(&count)
		if err != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want 1 row", count, err)
		}
	})

	t.Run("Write rolls back when fn fails", func(t *testing.T) {
		m := newManager(t)
		db := connect(t, m)
		if err := db.AutoMigrate(&note{}); err != nil {
			t.Fatal(err)
		}
		failed := errors.New("stop")

		err := cartridge.Write(context.Background(), m, func(tx *gorm.DB) error {
			if err := tx.Create(&note{Body: "lost"}).Error; err != nil {
				return err
			}
			return failed
		})

		var count int64
		db.Model(&note{}).Count(&count)
		if !errors.Is(err, failed) || count != 0 {
			t.Errorf("count = %d, err = %v, want no row and the error", count, err)
		}
	})

	t.Run("the database cache store keeps, expires, and deletes entries", func(t *testing.T) {
		m := newManager(t)
		store, err := cache.NewDatabaseStore(connect(t, m))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		ctx := context.Background()
		if err := store.Write(ctx, "user:1", []byte("Ada")); err != nil {
			t.Fatal(err)
		}
		if err := store.WriteWithTTL(ctx, "user:2", []byte("old"), -time.Minute); err != nil {
			t.Fatal(err)
		}

		value, found := store.Read(ctx, "user:1")
		_, expiredFound := store.Read(ctx, "user:2")
		deleted, err := store.DeleteByPrefix(ctx, "user:")

		if !found || string(value) != "Ada" || expiredFound {
			t.Errorf("read %q (found %v), expired found %v", value, found, expiredFound)
		}
		if err != nil || deleted < 1 || store.Exist(ctx, "user:1") {
			t.Errorf("deleted %d, err %v", deleted, err)
		}
	})
}
