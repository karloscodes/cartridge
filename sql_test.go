package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/karloscodes/cartridge/query"
)

type plainNote struct {
	ID   int64
	Body string
}

var plainMigrations = fstest.MapFS{
	"0001_create_notes.sql": {Data: []byte(`
		CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);
		INSERT INTO notes (body) VALUES ('seed');`)},
	"0002_add_pinned.sql": {Data: []byte("ALTER TABLE notes ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;")},
}

// newPlainApp returns an app whose schema comes from SQL files.
func newPlainApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app, err := NewApp(newAppTestConfig(t), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	if err := app.MigrateDatabase(NewSQLMigrator(plainMigrations)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return app
}

func TestPlainSQL(t *testing.T) {
	t.Run("a handler reads with SQL and writes with WriteSQL, without GORM", func(t *testing.T) {
		app := newPlainApp(t, WithRoutes(func(s *Server) {
			s.Get("/add", func(c *Context) error {
				return c.WriteSQL(func(tx *sql.Tx) error {
					_, err := tx.ExecContext(c.Context(), "INSERT INTO notes (body) VALUES (?)", c.Query("body"))
					return err
				})
			})
			s.Get("/notes", func(c *Context) error {
				notes, err := query.All[plainNote](c.Context(), c.SQL(), "SELECT id, body FROM notes ORDER BY id")
				if err != nil {
					return err
				}
				return c.JSON(notes)
			})
		}))
		get(t, app, "/add?body=hello")

		got := get(t, app, "/notes")

		if want := `[{"ID":1,"Body":"seed"},{"ID":2,"Body":"hello"}]`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("WriteSQL rolls back when fn fails", func(t *testing.T) {
		app := newPlainApp(t)
		failed := errors.New("stop")

		err := WriteSQL(context.Background(), app.DBManager, func(tx *sql.Tx) error {
			if _, err := tx.Exec("INSERT INTO notes (body) VALUES ('lost')"); err != nil {
				return err
			}
			return failed
		})

		db, _ := app.DBManager.Connect()
		sqlDB, _ := db.DB()
		count, countErr := query.One[int](context.Background(), sqlDB, "SELECT COUNT(*) FROM notes")
		if !errors.Is(err, failed) || countErr != nil || count != 1 {
			t.Errorf("count = %d (%v), err = %v, want the seed row only and the error", count, countErr, err)
		}
	})

	t.Run("a job reads and writes with SQL", func(t *testing.T) {
		result := make(chan error, 1)
		var count int
		job := processorFunc(func(ctx *JobContext) error {
			err := ctx.WriteSQL(func(tx *sql.Tx) error {
				_, err := tx.Exec("INSERT INTO notes (body) VALUES ('from a job')")
				return err
			})
			if err == nil {
				var db *sql.DB
				if db, err = ctx.SQL(); err == nil {
					count, err = query.One[int](ctx, db, "SELECT COUNT(*) FROM notes")
				}
			}
			select {
			case result <- err:
			default:
			}
			return nil
		})
		app := newPlainApp(t, WithJobs(time.Hour, job))

		if err := app.startWorkers(); err != nil {
			t.Fatal(err)
		}

		if err := <-result; err != nil || count != 2 {
			t.Errorf("count = %d, err = %v, want 2 rows", count, err)
		}
	})
}

func TestSQLMigrator(t *testing.T) {
	sqlDB := func(t *testing.T, app *App) *sql.DB {
		t.Helper()
		db, err := app.DBManager.Connect()
		if err != nil {
			t.Fatal(err)
		}
		s, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx := context.Background()

	t.Run("runs each file once, in the order of the names", func(t *testing.T) {
		app := newPlainApp(t)

		again := app.MigrateDatabase(NewSQLMigrator(plainMigrations))

		versions, err := query.All[string](ctx, sqlDB(t, app), "SELECT version FROM schema_migrations ORDER BY version")
		seeds, seedErr := query.One[int](ctx, sqlDB(t, app), "SELECT COUNT(*) FROM notes WHERE pinned = 0")
		if again != nil || err != nil || len(versions) != 2 || versions[0] != "0001_create_notes.sql" {
			t.Errorf("versions = %v, errors %v %v", versions, again, err)
		}
		if seedErr != nil || seeds != 1 {
			t.Errorf("seed rows = %d (%v), want 1: the first file ran once", seeds, seedErr)
		}
	})

	t.Run("a new file runs on the next start", func(t *testing.T) {
		app := newPlainApp(t)
		more := fstest.MapFS{"0003_add_title.sql": {Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT;")}}
		for name, file := range plainMigrations {
			more[name] = file
		}

		err := app.MigrateDatabase(NewSQLMigrator(more))

		_, queryErr := sqlDB(t, app).Exec("UPDATE notes SET title = 'x'")
		if err != nil || queryErr != nil {
			t.Errorf("migrate: %v, use the new column: %v", err, queryErr)
		}
	})

	t.Run("a file that fails leaves no part of itself, and is not recorded", func(t *testing.T) {
		app := newPlainApp(t)
		broken := fstest.MapFS{"0003_broken.sql": {Data: []byte("CREATE TABLE half (id INTEGER); THIS IS NOT SQL;")}}

		err := app.MigrateDatabase(NewSQLMigrator(broken))

		_, tableErr := sqlDB(t, app).Exec("SELECT 1 FROM half")
		recorded, _ := query.One[int](ctx, sqlDB(t, app), "SELECT COUNT(*) FROM schema_migrations WHERE version = '0003_broken.sql'")
		if err == nil || tableErr == nil || recorded != 0 {
			t.Errorf("err = %v, table exists = %v, recorded = %d", err, tableErr == nil, recorded)
		}
	})

	t.Run("a folder without .sql files is an error", func(t *testing.T) {
		app := newPlainApp(t)

		if err := app.MigrateDatabase(NewSQLMigrator(fstest.MapFS{"readme.txt": {Data: []byte("x")}})); err == nil {
			t.Error("Migrate returned nil, want an error")
		}
	})

	t.Run("a file name with a quote is an error", func(t *testing.T) {
		app := newPlainApp(t)

		if err := app.MigrateDatabase(NewSQLMigrator(fstest.MapFS{"0003_it's.sql": {Data: []byte("SELECT 1;")}})); err == nil {
			t.Error("Migrate returned nil, want an error")
		}
	})
}
