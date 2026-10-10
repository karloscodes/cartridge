package cartridge

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// appTestConfig is an AppConfig for the test environment.
type appTestConfig struct {
	testConfig
	dbPath string
}

func (c *appTestConfig) GetAppName() string       { return "testapp" }
func (c *appTestConfig) DatabaseDSN() string      { return c.dbPath }
func (c *appTestConfig) GetSessionSecret() string { return "a-test-secret-that-is-at-least-32-bytes" }
func (c *appTestConfig) GetSessionTimeout() int   { return 3600 }
func (c *appTestConfig) GetMaxOpenConns() int     { return 1 }
func (c *appTestConfig) GetMaxIdleConns() int     { return 1 }

func newAppTestConfig(t *testing.T) *appTestConfig {
	t.Helper()
	return &appTestConfig{dbPath: filepath.Join(t.TempDir(), "test.db")}
}

func get(t *testing.T, app *App, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	app.Server.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

func TestNewApp(t *testing.T) {
	t.Run("without config, it returns an error", func(t *testing.T) {
		_, err := NewApp(nil)

		if err == nil {
			t.Error("NewApp returned nil, want an error")
		}
	})

	t.Run("WithServerConfig changes the server config", func(t *testing.T) {
		app, err := NewApp(newAppTestConfig(t),
			WithServerConfig(func(c *ServerConfig) { c.AllowedHosts = []string{"myapp.test"} }),
			WithRoutes(func(s *Server) {
				s.Get("/ping", func(c *Context) error { return c.SendString("pong") })
			}))
		if err != nil {
			t.Fatal(err)
		}

		got := get(t, app, "/ping")

		if got == "pong" {
			t.Error("a request for example.com got the page, want it rejected")
		}
	})

	t.Run("mounts the routes and connects the database", func(t *testing.T) {
		app, err := NewApp(newAppTestConfig(t), WithRoutes(func(s *Server) {
			s.Get("/ping", func(c *Context) error {
				if c.DB() == nil {
					return c.SendString("no db")
				}
				return c.SendString("pong")
			})
		}))
		if err != nil {
			t.Fatal(err)
		}

		got := get(t, app, "/ping")

		if got != "pong" {
			t.Errorf("got %q, want pong", got)
		}
	})

	t.Run("renders the embedded templates", func(t *testing.T) {
		templates := fstest.MapFS{"home.html": {Data: []byte("Hello {{.Name}}")}}
		app, err := NewApp(newAppTestConfig(t),
			WithAssets(templates, nil),
			WithRoutes(func(s *Server) {
				s.Get("/", func(c *Context) error { return c.Render("home", Map{"Name": "Ada"}) })
			}),
		)
		if err != nil {
			t.Fatal(err)
		}

		got := get(t, app, "/")

		if got != "Hello Ada" {
			t.Errorf("got %q, want Hello Ada", got)
		}
	})

	t.Run("templates link the static files by digested URL", func(t *testing.T) {
		templates := fstest.MapFS{"home.html": {Data: []byte(`<script src="{{asset "app.js"}}"></script>{{importmap "app.js"}}`)}}
		static := fstest.MapFS{"app.js": {Data: []byte("console.log(1)")}}
		app, err := NewApp(newAppTestConfig(t),
			WithAssets(templates, static),
			WithRoutes(func(s *Server) {
				s.Get("/", func(c *Context) error { return c.Render("home", nil) })
			}),
		)
		if err != nil {
			t.Fatal(err)
		}

		got := get(t, app, "/")

		url, _ := app.Server.Asset("app.js")
		if !strings.Contains(got, `<script src="`+url+`"></script>`) || !strings.Contains(got, `"app": "`+url+`"`) {
			t.Errorf("page = %s", got)
		}
	})

	t.Run("with WithSession, the routes see the session manager", func(t *testing.T) {
		var seen *SessionManager
		app, err := NewApp(newAppTestConfig(t),
			WithSession("/login"),
			WithRoutes(func(s *Server) { seen = s.Session() }),
		)
		if err != nil {
			t.Fatal(err)
		}

		if app.Session == nil || seen != app.Session {
			t.Error("the routes did not get the app session manager")
		}
	})

	t.Run("starts and stops the workers", func(t *testing.T) {
		worker := newRecordingWorker()
		cfg := &portAppConfig{appTestConfig: newAppTestConfig(t), port: freePort(t)}
		app, err := NewApp(cfg,
			WithWorker(worker),
			WithJobs(time.Hour),
		)
		if err != nil {
			t.Fatal(err)
		}

		if err := app.StartAsync(); err != nil {
			t.Fatal(err)
		}
		<-worker.started
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}

		if !worker.stopped.Load() {
			t.Error("the worker did not stop")
		}
	})
}

type portAppConfig struct {
	*appTestConfig
	port string
}

func (c *portAppConfig) GetPort() string { return c.port }
