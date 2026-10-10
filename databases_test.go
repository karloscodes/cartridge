package cartridge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
