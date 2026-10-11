package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/karloscodes/cartridge/query"
	"github.com/karloscodes/cartridge/sqlite"
)

// seedShared writes a database file with one user and closes it.
func seedShared(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared.db")
	m := sqlite.NewManager(sqlite.Config{Path: path, Logger: testLogger()})
	writer, err := m.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL); INSERT INTO users (name) VALUES ('Ada');"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// countSharedUsers opens the file again and counts its users.
func countSharedUsers(t *testing.T, path string) int {
	t.Helper()
	m := sqlite.NewManager(sqlite.Config{Path: path, ReadOnly: true, Logger: testLogger()})
	defer func() { _ = m.Close() }()
	reader, err := m.Reader()
	if err != nil {
		t.Fatal(err)
	}
	count, err := query.One[int](context.Background(), reader, "SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func newDatabaseApp(t *testing.T, shared sqlite.Config, routes func(s *Server)) *App {
	t.Helper()
	app, err := NewApp(newAppTestConfig(t), WithDatabase("shared", shared), WithRoutes(routes))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.Databases["shared"].Close()
		_ = app.DBManager.Close()
	})
	return app
}

func TestNamedDatabases(t *testing.T) {
	t.Run("a handler reads a named database", func(t *testing.T) {
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t)}, func(s *Server) {
			s.Get("/user", func(c *Context) error {
				name, err := query.One[string](c.Context(), c.Database("shared"), "SELECT name FROM users")
				if err != nil {
					return err
				}
				return c.SendString(name)
			})
		})

		got := get(t, app, "/user")

		if got != "Ada" {
			t.Errorf("got %q, want Ada", got)
		}
	})

	t.Run("DB stays the main database", func(t *testing.T) {
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t)}, func(s *Server) {
			s.Get("/tables", func(c *Context) error {
				if _, err := query.One[int](c.Context(), c.DB(), "SELECT COUNT(*) FROM users"); err == nil {
					return c.SendString("main has the shared table")
				}
				return c.SendString("separate")
			})
		})

		got := get(t, app, "/tables")

		if got != "separate" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("DatabaseWriteTx writes the named database", func(t *testing.T) {
		path := seedShared(t)
		app := newDatabaseApp(t, sqlite.Config{Path: path}, func(s *Server) {
			s.Post("/users", func(c *Context) error {
				return c.DatabaseWriteTx("shared", func(tx *sql.Tx) error {
					_, err := query.Exec(c.Context(), tx, "INSERT INTO users (name) VALUES (?)", "Grace")
					return err
				})
			})
		})
		req := httptest.NewRequest("POST", "/users", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		resp, _ := app.Server.Test(req)

		if count := countSharedUsers(t, path); resp.StatusCode != http.StatusOK || count != 2 {
			t.Errorf("status %d, %d users; want 200 and 2", resp.StatusCode, count)
		}
	})

	t.Run("a read-only database answers DatabaseWriteTx with sqlite.ErrReadOnly", func(t *testing.T) {
		var writeErr error
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t), ReadOnly: true}, func(s *Server) {
			s.Get("/try-write", func(c *Context) error {
				writeErr = c.DatabaseWriteTx("shared", func(tx *sql.Tx) error {
					_, err := query.Exec(c.Context(), tx, "INSERT INTO users (name) VALUES (?)", "Grace")
					return err
				})
				return c.SendString("done")
			})
		})

		get(t, app, "/try-write")

		if !errors.Is(writeErr, sqlite.ErrReadOnly) {
			t.Errorf("err = %v, want sqlite.ErrReadOnly", writeErr)
		}
	})

	t.Run("an unknown name is a 500 for Database and an error for DatabaseWriteTx", func(t *testing.T) {
		var writeErr error
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t)}, func(s *Server) {
			s.Get("/read", func(c *Context) error { return c.Database("other").Ping() })
			s.Get("/write", func(c *Context) error {
				writeErr = c.DatabaseWriteTx("other", func(tx *sql.Tx) error { return nil })
				return nil
			})
		})

		resp, _ := app.Server.Test(httptest.NewRequest("GET", "/read", nil))
		get(t, app, "/write")

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("Database: status %d, want 500", resp.StatusCode)
		}
		if writeErr == nil {
			t.Error("DatabaseWriteTx returned no error")
		}
	})

	t.Run("WithDatabase without a name is an error", func(t *testing.T) {
		_, err := NewApp(newAppTestConfig(t), WithDatabase("", sqlite.Config{Path: seedShared(t)}))

		if err == nil {
			t.Error("NewApp returned no error")
		}
	})
}

func TestShutdownClosesDatabases(t *testing.T) {
	cfg := newAppTestConfig(t)
	app, err := NewApp(cfg, WithDatabase("shared", sqlite.Config{Path: filepath.Join(t.TempDir(), "shared.db")}))
	if err != nil {
		t.Fatal(err)
	}
	open := func(m *sqlite.Manager) *sql.DB {
		db, err := m.Reader()
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	main, shared := open(app.DBManager), open(app.Databases["shared"])

	err = app.Shutdown(context.Background())

	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if main.Ping() == nil || shared.Ping() == nil {
		t.Error("a database is still open after Shutdown")
	}
}

func TestJobNamedDatabases(t *testing.T) {
	// runJob runs fn once in a job of an app with the database "shared".
	runJob := func(t *testing.T, shared sqlite.Config, fn func(ctx *JobContext) error) error {
		t.Helper()
		result := make(chan error, 1)
		job := func(ctx *JobContext) error {
			select {
			case result <- fn(ctx):
			default:
			}
			return nil
		}
		app, err := NewApp(newAppTestConfig(t), WithDatabase("shared", shared), WithCron("job", "@every 20ms", job))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
		if err := app.startWorkers(); err != nil {
			t.Fatal(err)
		}
		return <-result
	}

	t.Run("a job reads a named database", func(t *testing.T) {
		var name string

		err := runJob(t, sqlite.Config{Path: seedShared(t)}, func(ctx *JobContext) error {
			db, err := ctx.Database("shared")
			if err != nil {
				return err
			}
			name, err = query.One[string](ctx, db, "SELECT name FROM users")
			return err
		})

		if err != nil || name != "Ada" {
			t.Errorf("got %q, %v, want Ada", name, err)
		}
	})

	t.Run("a job writes a named database", func(t *testing.T) {
		path := seedShared(t)

		err := runJob(t, sqlite.Config{Path: path}, func(ctx *JobContext) error {
			return ctx.DatabaseWriteTx("shared", func(tx *sql.Tx) error {
				_, err := query.Exec(ctx, tx, "INSERT INTO users (name) VALUES (?)", "Grace")
				return err
			})
		})

		if count := countSharedUsers(t, path); err != nil || count != 2 {
			t.Errorf("write: %v, users = %d, want 2", err, count)
		}
	})

	t.Run("an unknown name is an error, not a panic", func(t *testing.T) {
		err := runJob(t, sqlite.Config{Path: seedShared(t)}, func(ctx *JobContext) error {
			_, err := ctx.Database("other")
			return err
		})

		if err == nil {
			t.Error("Database returned nil, want an error")
		}
	})
}
