package testsupport

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/sqlite"
)

// TestServerOptions configures test server creation.
type TestServerOptions struct {
	// Migrations are the .sql files to run on the test database, as
	// cartridge.WithMigrations does. Nil gives an empty database.
	Migrations fs.FS

	// Route mounting function
	RouteMountFunc func(*cartridge.Server)

	// Custom server configuration (optional)
	ServerConfig *cartridge.ServerConfig

	// Disable middleware for simpler testing
	DisableMiddleware bool
}

// TestServer wraps a cartridge server for testing.
type TestServer struct {
	t         *testing.T
	Server    *cartridge.Server
	Logger    *slog.Logger
	Config    *TestConfig
	DBManager *sqlite.Manager
}

// NewTestServer creates a test server on a new SQLite file, without views.
func NewTestServer(t *testing.T, opts ...TestServerOptions) *TestServer {
	t.Helper()

	var options TestServerOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	dbManager := NewTestDB(t, options.Migrations)

	// Create test logger and config
	logger := NewTestLogger()
	config := NewTestConfig()

	// Build server config
	serverCfg := options.ServerConfig
	if serverCfg == nil {
		serverCfg = cartridge.DefaultServerConfig()
	}

	// Inject test dependencies
	serverCfg.Config = config
	serverCfg.Logger = logger
	serverCfg.DBManager = dbManager

	// Disable some middleware for testing if requested
	if options.DisableMiddleware {
		serverCfg.EnableRequestLogger = false
		serverCfg.EnableSecFetchSite = false
	}

	// Create server
	server, err := cartridge.NewServer(serverCfg)
	if err != nil {
		t.Fatalf("testsupport: failed to create test server: %v", err)
	}

	// Mount routes if provided
	if options.RouteMountFunc != nil {
		options.RouteMountFunc(server)
	}

	ts := &TestServer{
		t:         t,
		Server:    server,
		Logger:    logger,
		Config:    config,
		DBManager: dbManager,
	}

	return ts
}

// Request performs a test request with a JSON body and returns the
// response. It sends Sec-Fetch-Site: same-origin, as a browser on the app's
// own pages does, so CSRF protection lets the request through.
func (ts *TestServer) Request(method, path string, body ...string) *http.Response {
	ts.t.Helper()

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(body[0])
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	return ts.Do(req)
}

// Do performs req and returns the response. It sets Sec-Fetch-Site:
// same-origin when req has no Sec-Fetch-Site header.
func (ts *TestServer) Do(req *http.Request) *http.Response {
	ts.t.Helper()

	if req.Header.Get("Sec-Fetch-Site") == "" {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	resp, err := ts.Server.Test(req)
	if err != nil {
		ts.t.Fatalf("testsupport: request failed: %v", err)
	}
	return resp
}

// Get performs a GET request.
func (ts *TestServer) Get(path string) *http.Response {
	return ts.Request("GET", path)
}

// Post performs a POST request with JSON body.
func (ts *TestServer) Post(path, body string) *http.Response {
	return ts.Request("POST", path, body)
}

// PostForm performs a POST request with a urlencoded form body.
func (ts *TestServer) PostForm(path string, form url.Values) *http.Response {
	ts.t.Helper()

	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return ts.Do(req)
}

// Put performs a PUT request with JSON body.
func (ts *TestServer) Put(path, body string) *http.Response {
	return ts.Request("PUT", path, body)
}

// Delete performs a DELETE request.
func (ts *TestServer) Delete(path string) *http.Response {
	return ts.Request("DELETE", path)
}
