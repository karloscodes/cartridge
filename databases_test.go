package cartridge

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/sqlite"
)

type sharedUser struct {
	ID   uint
	Name string
}

// seedShared writes a database file with one user and closes it.
func seedShared(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared.db")
	m := sqlite.NewManager(sqlite.Config{Path: path})
	db, err := m.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&sharedUser{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&sharedUser{Name: "Ada"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	return path
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
				var user sharedUser
				if err := c.Database("shared").First(&user).Error; err != nil {
					return err
				}
				return c.SendString(user.Name)
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
				if c.DB().Migrator().HasTable(&sharedUser{}) {
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
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t)}, func(s *Server) {
			s.Post("/users", func(c *Context) error {
				return c.DatabaseWriteTx("shared", func(tx *gorm.DB) error {
					return tx.Create(&sharedUser{Name: "Grace"}).Error
				})
			})
		})
		req := httptest.NewRequest("POST", "/users", nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")

		resp, _ := app.Server.Test(req)

		var count int64
		app.Databases["shared"].GetConnection().Model(&sharedUser{}).Count(&count)
		if resp.StatusCode != http.StatusOK || count != 2 {
			t.Errorf("status %d, %d users; want 200 and 2", resp.StatusCode, count)
		}
	})

	t.Run("a read-only database answers DatabaseWriteTx with sqlite.ErrReadOnly", func(t *testing.T) {
		var writeErr error
		app := newDatabaseApp(t, sqlite.Config{Path: seedShared(t), ReadOnly: true}, func(s *Server) {
			s.Get("/try-write", func(c *Context) error {
				writeErr = c.DatabaseWriteTx("shared", func(tx *gorm.DB) error {
					return tx.Create(&sharedUser{Name: "Grace"}).Error
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
			s.Get("/read", func(c *Context) error { return c.Database("other").Error })
			s.Get("/write", func(c *Context) error {
				writeErr = c.DatabaseWriteTx("other", func(tx *gorm.DB) error { return nil })
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
		db, err := m.Connect()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		return sqlDB
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

type processorFunc func(ctx *JobContext) error

func (f processorFunc) ProcessBatch(ctx *JobContext) error { return f(ctx) }

func TestJobNamedDatabases(t *testing.T) {
	// runJob runs fn once in a job of an app with the database "shared".
	runJob := func(t *testing.T, shared sqlite.Config, fn func(ctx *JobContext) error) error {
		t.Helper()
		result := make(chan error, 1)
		job := processorFunc(func(ctx *JobContext) error {
			select {
			case result <- fn(ctx):
			default:
			}
			return nil
		})
		app, err := NewApp(newAppTestConfig(t), WithDatabase("shared", shared), WithJobs(time.Hour, job))
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
		var user sharedUser

		err := runJob(t, sqlite.Config{Path: seedShared(t)}, func(ctx *JobContext) error {
			db, err := ctx.Database("shared")
			if err != nil {
				return err
			}
			return db.First(&user).Error
		})

		if err != nil || user.Name != "Ada" {
			t.Errorf("got %q, %v, want Ada", user.Name, err)
		}
	})

	t.Run("a job writes a named database", func(t *testing.T) {
		path := seedShared(t)

		err := runJob(t, sqlite.Config{Path: path}, func(ctx *JobContext) error {
			return ctx.DatabaseWriteTx("shared", func(tx *gorm.DB) error {
				return tx.Create(&sharedUser{Name: "Grace"}).Error
			})
		})

		if err != nil {
			t.Fatalf("write: %v", err)
		}
		m := sqlite.NewManager(sqlite.Config{Path: path})
		t.Cleanup(func() { _ = m.Close() })
		db, _ := m.Connect()
		var count int64
		db.Model(&sharedUser{}).Count(&count)
		if count != 2 {
			t.Errorf("users = %d, want 2", count)
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
