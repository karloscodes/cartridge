package testsupport

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/flash"
	"github.com/karloscodes/cartridge/query"
)

type note struct {
	ID   int64
	Body string
}

var notesMigrations = fstest.MapFS{
	"0001_create_notes.sql": {Data: []byte("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);")},
}

// newNotesApp is an app's own build function: its views, its routes, and
// its migrations.
func newNotesApp(cfg *config.Config) (*cartridge.App, error) {
	templates := fstest.MapFS{
		"layouts/app.html":  {Data: []byte(`<main>{{embed}}</main>`)},
		"notes/index.html":  {Data: []byte(`{{range .}}<p>{{.Body}}</p>{{else}}<p>No notes</p>{{end}}`)},
		"notes/create.html": {Data: []byte(`saved`)},
	}
	return cartridge.NewApp(cfg,
		cartridge.WithMigrations(notesMigrations),
		cartridge.WithAssets(templates, nil),
		cartridge.WithRoutes(func(s *cartridge.Server) {
			s.Get("/notes", func(c *cartridge.Context) error {
				notes, err := query.All[note](c.Context(), c.DB(), "SELECT id, body FROM notes ORDER BY id")
				if err != nil {
					return err
				}
				return c.Render("notes/index", notes, "layouts/app")
			})
			s.Post("/notes", func(c *cartridge.Context) error {
				err := c.WriteTx(func(tx *sql.Tx) error {
					_, err := query.Exec(c.Context(), tx, "INSERT INTO notes (body) VALUES (?)", c.Input("body"))
					return err
				})
				if err != nil {
					return err
				}
				return c.Render("notes/create", nil)
			})
		}),
	)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewTestApp(t *testing.T) {
	t.Run("runs the app's own migrations, routes, and views", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newNotesApp)

		created := ta.PostForm("/notes", url.Values{"body": {"Hello"}})
		list := ta.Get("/notes")

		if created.StatusCode != http.StatusOK {
			t.Fatalf("POST /notes = %d, want 200", created.StatusCode)
		}
		if got := readBody(t, list); got != "<main><p>Hello</p></main>" {
			t.Errorf("GET /notes = %q", got)
		}
	})

	t.Run("keeps the database in a file under a new temporary directory", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newNotesApp)

		_, err := os.Stat(ta.Config.DatabasePath)

		if err != nil {
			t.Errorf("no database file: %v", err)
		}
		if !strings.HasPrefix(ta.Config.DatabasePath, ta.Config.DataDirectory) || !ta.Config.IsTest() {
			t.Errorf("config %+v", ta.Config)
		}
	})

	t.Run("each test starts with an empty database", func(t *testing.T) {
		first := NewTestApp(t, "notes", newNotesApp)
		first.PostForm("/notes", url.Values{"body": {"Hello"}})

		second := NewTestApp(t, "notes", newNotesApp)
		got := readBody(t, second.Get("/notes"))

		if got != "<main><p>No notes</p></main>" {
			t.Errorf("the second app sees %q", got)
		}
	})

	t.Run("DB reads the rows the requests wrote", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newNotesApp)
		ta.PostForm("/notes", url.Values{"body": {"Hello"}})

		count, err := query.One[int](context.Background(), ta.DB(), "SELECT COUNT(*) FROM notes")

		if err != nil || count != 1 {
			t.Errorf("%d notes (%v), want 1", count, err)
		}
	})
}

// newSessionApp signs in user 7 on each login, and shows the user back.
func newSessionApp(cfg *config.Config) (*cartridge.App, error) {
	return cartridge.NewApp(cfg,
		cartridge.WithSession("/login"),
		cartridge.WithRoutes(func(s *cartridge.Server) {
			auth := &cartridge.RouteConfig{CustomMiddleware: []cartridge.HandlerFunc{s.Session().Middleware()}}
			s.Post("/login", func(c *cartridge.Context) error {
				if err := c.Session.SetSession(c, 7); err != nil {
					return err
				}
				return c.FlashSuccess("Welcome").Redirect("/me")
			})
			s.Post("/logout", func(c *cartridge.Context) error {
				c.Session.ClearSession(c)
				return c.Redirect("/login")
			})
			s.Get("/me", func(c *cartridge.Context) error {
				id, _ := c.Session.GetUserID(c)
				return c.SendString(fmt.Sprintf("user %d", id))
			}, auth)
		}),
	)
}

func TestClient(t *testing.T) {
	t.Run("keeps the session cookie, so the next request is signed in", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newSessionApp)
		browser := ta.Client()

		browser.PostForm("/login", nil)
		resp := browser.Get("/me")

		if got := readBody(t, resp); got != "user 7" {
			t.Errorf("GET /me = %d %q, want user 7", resp.StatusCode, got)
		}
	})

	t.Run("sends the flash cookie to the next page", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newSessionApp)
		browser := ta.Client()
		browser.PostForm("/login", nil)

		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		browser.Do(req)

		if _, err := req.Cookie(flash.FlashCookieName); err != nil {
			t.Error("the request after the redirect has no flash cookie")
		}
	})

	t.Run("drops a cookie that the app expires", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newSessionApp)
		browser := ta.Client()
		browser.PostForm("/login", nil)

		browser.PostForm("/logout", nil)
		resp := browser.Get("/me")

		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login" {
			t.Errorf("GET /me after logout = %d to %q, want a redirect to /login", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("a new client and the TestApp send no cookies", func(t *testing.T) {
		ta := NewTestApp(t, "notes", newSessionApp)
		ta.Client().PostForm("/login", nil)

		fromNewClient := ta.Client().Get("/me")
		fromApp := ta.Get("/me")

		if fromNewClient.StatusCode != http.StatusFound || fromApp.StatusCode != http.StatusFound {
			t.Errorf("GET /me = %d and %d, want 302 for both", fromNewClient.StatusCode, fromApp.StatusCode)
		}
	})
}
