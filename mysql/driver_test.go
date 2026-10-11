package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/cache"
	"github.com/karloscodes/cartridge/database"
	"github.com/karloscodes/cartridge/mysql"
	"github.com/karloscodes/cartridge/query"
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
	ID        int64
	Body      string
	CreatedAt time.Time
}

const createNotes = "CREATE TABLE notes (id BIGINT AUTO_INCREMENT PRIMARY KEY, body VARCHAR(255) NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3))"

// newManagerFor connects to the server in CARTRIDGE_MYSQL_DSN, in a new
// database that the test owns. Without the env var, the test is skipped.
func newManagerFor(t *testing.T) *database.Manager {
	t.Helper()
	dsn := os.Getenv("CARTRIDGE_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CARTRIDGE_MYSQL_DSN is not set")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	name := fmt.Sprintf("t_%d", time.Now().UnixNano())

	admin := database.NewManager(mysql.NewDriver(), database.DefaultConfig(dsn), logger)
	adminDB, err := admin.Writer()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec("DROP DATABASE " + name)
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

// pool returns the connection pool of m. PostgreSQL and MySQL have one pool
// for reads and writes.
func pool(t *testing.T, m *database.Manager) *sql.DB {
	t.Helper()
	db, err := m.Writer()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db
}

// notesTable returns the pool of m, with an empty notes table.
func notesTable(t *testing.T, m *database.Manager) *sql.DB {
	t.Helper()
	db := pool(t, m)
	if _, err := db.Exec(createNotes); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	return db
}

func TestMySQL(t *testing.T) {
	t.Run("stores and reads a row, with its time and four-byte characters", func(t *testing.T) {
		db := notesTable(t, newManagerFor(t))
		ctx := context.Background()
		created := time.Date(2026, 10, 10, 12, 30, 0, 0, time.UTC)

		_, err := db.ExecContext(ctx, "INSERT INTO notes (body, created_at) VALUES (?, ?)", "Hello 👋", created)

		got, readErr := query.One[note](ctx, db, "SELECT id, body, created_at FROM notes")
		if err != nil || readErr != nil {
			t.Fatalf("write: %v, read: %v", err, readErr)
		}
		if got.Body != "Hello 👋" || !got.CreatedAt.Equal(created) || got.CreatedAt.Location() != time.UTC {
			t.Errorf("got %q at %v, want the same text at %v in UTC", got.Body, got.CreatedAt, created)
		}
	})

	t.Run("Write commits its transaction", func(t *testing.T) {
		m := newManagerFor(t)
		db := notesTable(t, m)
		ctx := context.Background()

		err := cartridge.Write(ctx, m, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO notes (body) VALUES (?)", "kept")
			return err
		})

		count, countErr := query.One[int](ctx, db, "SELECT COUNT(*) FROM notes")
		if err != nil || countErr != nil || count != 1 {
			t.Errorf("count = %d (%v), err = %v, want 1 row", count, countErr, err)
		}
	})

	t.Run("Write rolls back when fn fails", func(t *testing.T) {
		m := newManagerFor(t)
		db := notesTable(t, m)
		ctx := context.Background()
		failed := errors.New("stop")

		err := cartridge.Write(ctx, m, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO notes (body) VALUES (?)", "lost"); err != nil {
				return err
			}
			return failed
		})

		count, countErr := query.One[int](ctx, db, "SELECT COUNT(*) FROM notes")
		if !errors.Is(err, failed) || countErr != nil || count != 0 {
			t.Errorf("count = %d (%v), err = %v, want no row and the error", count, countErr, err)
		}
	})

	t.Run("the database cache store keeps, expires, and deletes entries", func(t *testing.T) {
		db := pool(t, newManagerFor(t))
		store, err := cache.NewDatabaseStore(db, db)
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
		fetched, fetchErr := cache.Fetch(ctx, store, "count", time.Minute, func() (int, error) { return 3, nil })
		again, againErr := cache.Fetch(ctx, store, "count", time.Minute, func() (int, error) { return 0, errors.New("not called") })
		if fetchErr != nil || againErr != nil || fetched != 3 || again != 3 {
			t.Errorf("Fetch gave %d then %d, errors %v %v", fetched, again, fetchErr, againErr)
		}
	})

	t.Run("writes run at the same time", func(t *testing.T) {
		m := newManagerFor(t)
		notesTable(t, m)
		ctx := context.Background()
		firstStarted, secondDone := make(chan struct{}), make(chan error, 1)
		go func() {
			<-firstStarted
			secondDone <- cartridge.Write(ctx, m, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT INTO notes (body) VALUES (?)", "second")
				return err
			})
		}()

		// The first write stays open until the second one is done.
		var second error
		err := cartridge.Write(ctx, m, func(tx *sql.Tx) error {
			close(firstStarted)
			select {
			case second = <-secondDone:
				return nil
			case <-time.After(3 * time.Second):
				return errors.New("the second write waited for the first")
			}
		})

		if err != nil || second != nil {
			t.Errorf("first: %v, second: %v", err, second)
		}
	})

	t.Run("SQL files migrate the schema, and Write and query use it", func(t *testing.T) {
		m := newManagerFor(t)
		db := pool(t, m)
		ctx := context.Background()
		files := fstest.MapFS{
			// MySQL takes one statement per file.
			"0001_create_items.sql": {Data: []byte("CREATE TABLE items (id BIGINT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL)")},
			"0002_seed_items.sql":   {Data: []byte("INSERT INTO items (name) VALUES ('seed')")},
			"0003_add_done.sql":     {Data: []byte("ALTER TABLE items ADD COLUMN done BOOLEAN NOT NULL DEFAULT FALSE")},
		}
		if err := cartridge.Migrate(db, files); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		if err := cartridge.Migrate(db, files); err != nil {
			t.Fatalf("migrate again: %v", err)
		}

		err := cartridge.Write(ctx, m, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO items (name) VALUES (?)", "written")
			return err
		})

		names, queryErr := query.All[string](ctx, db, "SELECT name FROM items WHERE done = FALSE ORDER BY id")
		if err != nil || queryErr != nil || len(names) != 2 || names[0] != "seed" || names[1] != "written" {
			t.Errorf("names = %v, write: %v, query: %v", names, err, queryErr)
		}
	})

	t.Run("two cron schedulers on the database run each tick once", func(t *testing.T) {
		m := newManagerFor(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		var runs atomic.Int32
		job := func(*cartridge.JobContext) error { runs.Add(1); return nil }
		start := func() *cartridge.CronScheduler {
			scheduler := cartridge.NewCronScheduler(logger, m)
			if err := scheduler.Add("tick", "@every 200ms", job); err != nil {
				t.Fatal(err)
			}
			if err := scheduler.Start(); err != nil {
				t.Fatal(err)
			}
			return scheduler
		}

		begin := time.Now()
		first, second := start(), start()
		time.Sleep(900 * time.Millisecond)
		first.Stop()
		second.Stop()

		ticks := int32(time.Since(begin)/(200*time.Millisecond)) + 1
		if got := runs.Load(); got < 2 || got > ticks {
			t.Errorf("runs = %d, want 2 to %d: one run per tick", got, ticks)
		}
	})
}
