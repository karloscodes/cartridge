package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/cache"
	"github.com/karloscodes/cartridge/database"
	"github.com/karloscodes/cartridge/postgres"
	"github.com/karloscodes/cartridge/query"
)

func TestConfigureDSN(t *testing.T) {
	configure := func(dsn string, opts database.PostgresOptions) string {
		return postgres.NewDriver().ConfigureDSN(dsn, &database.Config{Postgres: opts})
	}
	defaults := database.PostgresOptions{SSLMode: "prefer", Timezone: "UTC"}

	t.Run("adds the options to a URL", func(t *testing.T) {
		got := configure("postgres://app@db.internal/shop", defaults)

		if want := "postgres://app@db.internal/shop?sslmode=prefer&TimeZone=UTC"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("adds the options to a URL that has parameters", func(t *testing.T) {
		got := configure("postgres://db.internal/shop?application_name=shop", defaults)

		if want := "postgres://db.internal/shop?application_name=shop&sslmode=prefer&TimeZone=UTC"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("adds the options to a keyword DSN", func(t *testing.T) {
		got := configure("host=db.internal user=app dbname=shop", defaults)

		if want := "host=db.internal user=app dbname=shop sslmode=prefer TimeZone=UTC"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("keeps an option that the DSN sets itself", func(t *testing.T) {
		cases := map[string]string{
			"postgres://db.internal/shop?sslmode=verify-full":  "postgres://db.internal/shop?sslmode=verify-full&TimeZone=UTC",
			"host=db.internal dbname=shop sslmode=verify-full": "host=db.internal dbname=shop sslmode=verify-full TimeZone=UTC",
		}
		for dsn, want := range cases {
			if got := configure(dsn, defaults); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}
	})

	t.Run("adds the search path, escaped for its DSN form", func(t *testing.T) {
		opts := database.PostgresOptions{SearchPath: "tenant one,public"}
		cases := map[string]string{
			"postgres://db.internal/shop":  "postgres://db.internal/shop?search_path=tenant+one%2Cpublic",
			"host=db.internal dbname=shop": `host=db.internal dbname=shop search_path='tenant one,public'`,
		}
		for dsn, want := range cases {
			if got := configure(dsn, opts); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}
	})
}

type note struct {
	ID   uint
	Body string
}

// newManager connects to the database in CARTRIDGE_POSTGRES_DSN, in a new
// schema that the test owns. Without the env var, the test is skipped.
func newManager(t *testing.T, maxOpenConns int) (*database.Manager, string) {
	t.Helper()
	dsn := os.Getenv("CARTRIDGE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CARTRIDGE_POSTGRES_DSN is not set")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	schema := fmt.Sprintf("t_%d", time.Now().UnixNano())

	admin := database.NewManager(postgres.NewDriver(), database.DefaultConfig(dsn), logger)
	adminDB, err := admin.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := adminDB.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = admin.Close()
	})

	cfg := database.DefaultConfig(dsn)
	cfg.MaxOpenConns = maxOpenConns
	cfg.MaxIdleConns = maxOpenConns
	cfg.Postgres.SearchPath = schema
	m := database.NewManager(postgres.NewDriver(), cfg, logger)
	t.Cleanup(func() { _ = m.Close() })
	return m, schema
}

func connect(t *testing.T, m *database.Manager) *gorm.DB {
	t.Helper()
	db, err := m.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db
}

func TestPostgres(t *testing.T) {
	t.Run("stores and reads a row", func(t *testing.T) {
		m, _ := newManager(t, 2)
		db := connect(t, m)
		if err := db.AutoMigrate(&note{}); err != nil {
			t.Fatal(err)
		}

		err := db.Create(&note{Body: "Hello"}).Error

		var got note
		if err != nil || db.First(&got).Error != nil || got.Body != "Hello" {
			t.Errorf("got %+v, %v", got, err)
		}
	})

	t.Run("every pooled connection uses the search path", func(t *testing.T) {
		m, schema := newManager(t, 4)
		sqlDB, err := connect(t, m).DB()
		if err != nil {
			t.Fatal(err)
		}
		// Hold four connections at once, so each one is another connection.
		var conns []*sql.Conn
		for range 4 {
			conn, err := sqlDB.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			conns = append(conns, conn)
		}

		for i, conn := range conns {
			var path string
			if err := conn.QueryRowContext(context.Background(), "SHOW search_path").Scan(&path); err != nil {
				t.Fatal(err)
			}
			if path != schema {
				t.Errorf("connection %d: search_path = %q, want %q", i, path, schema)
			}
		}
	})

	t.Run("Write commits its transaction", func(t *testing.T) {
		m, _ := newManager(t, 2)
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
		m, _ := newManager(t, 2)
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
		m, _ := newManager(t, 2)
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

	t.Run("writes run at the same time", func(t *testing.T) {
		m := newManagerFor(t)
		db := connect(t, m)
		if err := db.AutoMigrate(&note{}); err != nil {
			t.Fatal(err)
		}
		firstStarted, secondDone := make(chan struct{}), make(chan error, 1)
		go func() {
			<-firstStarted
			secondDone <- cartridge.Write(context.Background(), m, func(tx *gorm.DB) error {
				return tx.Create(&note{Body: "second"}).Error
			})
		}()

		// The first write stays open until the second one is done.
		var second error
		err := cartridge.Write(context.Background(), m, func(tx *gorm.DB) error {
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

	t.Run("the GORM cache fetches once, then reads the stored value", func(t *testing.T) {
		m := newManagerFor(t)
		db := connect(t, m)
		if err := db.AutoMigrate(&cache.CacheRecord{}); err != nil {
			t.Fatal(err)
		}
		fetches := 0
		c, err := cache.NewGormCache(db, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, func(key string) (string, error) {
			fetches++
			return "value of " + key, nil
		})
		if err != nil {
			t.Fatal(err)
		}

		first, err1 := c.Get("user:1")
		again, err2 := c.Get("user:1")
		removed := c.InvalidateByPrefix("user:")

		if err1 != nil || err2 != nil || first != "value of user:1" || again != first || fetches != 1 {
			t.Errorf("got %q then %q, %d fetches, errors %v %v", first, again, fetches, err1, err2)
		}
		if removed != 1 {
			t.Errorf("removed %d entries, want 1", removed)
		}
	})

	t.Run("SQL files migrate the schema, and WriteSQL and query use it without GORM", func(t *testing.T) {
		m := newManagerFor(t)
		db := connect(t, m)
		ctx := context.Background()
		files := fstest.MapFS{
			"0001_create_items.sql": {Data: []byte("CREATE TABLE items (id SERIAL PRIMARY KEY, name TEXT NOT NULL); INSERT INTO items (name) VALUES ('seed');")},
			"0002_add_done.sql":     {Data: []byte("ALTER TABLE items ADD COLUMN done BOOLEAN NOT NULL DEFAULT FALSE;")},
		}
		if err := cartridge.NewSQLMigrator(files).Migrate(db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		if err := cartridge.NewSQLMigrator(files).Migrate(db); err != nil {
			t.Fatalf("migrate again: %v", err)
		}

		err := cartridge.WriteSQL(ctx, m, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO items (name) VALUES ($1)", "written")
			return err
		})

		sqlDB, _ := db.DB()
		names, queryErr := query.All[string](ctx, sqlDB, "SELECT name FROM items WHERE done = FALSE ORDER BY id")
		if err != nil || queryErr != nil || len(names) != 2 || names[0] != "seed" || names[1] != "written" {
			t.Errorf("names = %v, write: %v, query: %v", names, err, queryErr)
		}
	})
}

func newManagerFor(t *testing.T) *database.Manager {
	t.Helper()
	m, _ := newManager(t, 2)
	return m
}
