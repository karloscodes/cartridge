package testsupport

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/config"
)

// TestApp is the app's own cartridge.App, built for one test. Its requests
// run in memory, through every middleware and the real views.
type TestApp struct {
	*cartridge.App
	Config *config.Config
	t      *testing.T
}

// NewTestApp builds the app for a test with build, the same function that
// main uses, so the test runs the real migrations and renders the real
// views. build gets a config for the test environment whose data directory
// is a new t.TempDir(), so the SQLite file is new for each test. Put files
// in cfg.DataDirectory before NewApp to start from a copy of a database.
// NewTestApp reads no env vars and no .env file. At cleanup it closes the
// databases.
//
//	func newApp(cfg *config.Config) (*cartridge.App, error) {
//	    app, err := cartridge.NewApp(cfg, cartridge.WithAssets(web.Templates(), web.Static()), ...)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return app, app.MigrateDatabase(cartridge.NewAutoMigrator(&Note{}))
//	}
//
//	ta := testsupport.NewTestApp(t, "myapp", newApp)
//	resp := ta.PostForm("/notes", url.Values{"body": {"Hello"}})
func NewTestApp(t *testing.T, appName string, build func(cfg *config.Config) (*cartridge.App, error)) *TestApp {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		AppName:          appName,
		Environment:      config.Test,
		Port:             "0",
		LogLevel:         "error",
		LogsDirectory:    filepath.Join(dir, "logs"),
		SessionSecret:    "testsupport-session-secret-of-at-least-32-bytes",
		SessionTimeout:   3600,
		DataDirectory:    dir,
		DatabaseFilename: appName + ".db",
		DatabasePath:     filepath.Join(dir, appName+".test.db"),
	}

	app, err := build(cfg)
	if app != nil {
		t.Cleanup(func() {
			for _, db := range app.Databases {
				_ = db.Close()
			}
			_ = app.DBManager.Close()
		})
	}
	if err != nil {
		t.Fatalf("testsupport: build the app: %v", err)
	}
	if app == nil {
		t.Fatal("testsupport: build returned no app")
	}
	return &TestApp{App: app, Config: cfg, t: t}
}

// DB returns the main database, to set up or check rows.
func (ta *TestApp) DB() *gorm.DB {
	ta.t.Helper()
	db, err := ta.DBManager.Connect()
	if err != nil {
		ta.t.Fatalf("testsupport: connect the database: %v", err)
	}
	return db
}

// SQL returns the *sql.DB of the main database, to set up or check rows
// without GORM, for example with the app's sqlc queries.
func (ta *TestApp) SQL() *sql.DB {
	ta.t.Helper()
	db, err := ta.DB().DB()
	if err != nil {
		ta.t.Fatalf("testsupport: connect the database: %v", err)
	}
	return db
}

// Do performs req and returns the response. It sets Sec-Fetch-Site:
// same-origin when req has no Sec-Fetch-Site header, as a browser on the
// app's own pages does, so CSRF protection lets the request through.
func (ta *TestApp) Do(req *http.Request) *http.Response {
	ta.t.Helper()
	if req.Header.Get("Sec-Fetch-Site") == "" {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	resp, err := ta.Server.Test(req)
	if err != nil {
		ta.t.Fatalf("testsupport: request failed: %v", err)
	}
	return resp
}

// Request performs a request with a JSON body and returns the response.
func (ta *TestApp) Request(method, path string, body ...string) *http.Response {
	ta.t.Helper()
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(body[0])
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	return ta.Do(req)
}

// Get performs a GET request.
func (ta *TestApp) Get(path string) *http.Response {
	ta.t.Helper()
	return ta.Do(httptest.NewRequest(http.MethodGet, path, nil))
}

// Post performs a POST request with a JSON body.
func (ta *TestApp) Post(path, body string) *http.Response {
	ta.t.Helper()
	return ta.Request(http.MethodPost, path, body)
}

// PostForm performs a POST request with a urlencoded form body.
func (ta *TestApp) PostForm(path string, form url.Values) *http.Response {
	ta.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ta.Do(req)
}

// Client sends requests to the app like one browser: it keeps the cookies
// the app sets and sends them back. A test signs in once, and the next
// requests have the session and the flash message. A Client does not follow
// redirects.
type Client struct {
	app *TestApp
	jar *cookiejar.Jar
}

// Client returns a new client with no cookies. TestApp's own request
// methods send no cookies.
func (ta *TestApp) Client() *Client {
	jar, _ := cookiejar.New(nil) // New never fails without options
	return &Client{app: ta, jar: jar}
}

// Do performs req with the client's cookies, and keeps the cookies of the
// response.
func (c *Client) Do(req *http.Request) *http.Response {
	c.app.t.Helper()
	// The jar matches cookies by an absolute URL; a test request has only a path.
	u := &url.URL{Scheme: "http", Host: req.Host, Path: req.URL.Path}
	for _, cookie := range c.jar.Cookies(u) {
		req.AddCookie(cookie)
	}
	resp := c.app.Do(req)
	c.jar.SetCookies(u, resp.Cookies())
	return resp
}

// Get performs a GET request.
func (c *Client) Get(path string) *http.Response {
	c.app.t.Helper()
	return c.Do(httptest.NewRequest(http.MethodGet, path, nil))
}

// Post performs a POST request with a JSON body.
func (c *Client) Post(path, body string) *http.Response {
	c.app.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.Do(req)
}

// PostForm performs a POST request with a urlencoded form body.
func (c *Client) PostForm(path string, form url.Values) *http.Response {
	c.app.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req)
}
