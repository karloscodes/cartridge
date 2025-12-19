package testsupport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/karloscodes/cartridge"
)

// TestServerOptions configures test server creation.
type TestServerOptions struct {
	// Models to auto-migrate in the test database
	Models []any

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
	App       *fiber.App
	DB        *TestDBManager
	Logger    cartridge.Logger
	Config    *TestConfig
	DBManager *TestDBManager
}

// NewTestServer creates a test server with in-memory database.
func NewTestServer(t *testing.T, opts ...TestServerOptions) *TestServer {
	t.Helper()

	var options TestServerOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	// Create test database
	db := SetupTestDB(t, TestDBOptions{Models: options.Models})
	dbManager := NewTestDBManager(db)

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
		App:       server.App(),
		DB:        dbManager,
		Logger:    logger,
		Config:    config,
		DBManager: dbManager,
	}

	return ts
}

// Request performs a test request and returns the response.
func (ts *TestServer) Request(method, path string, body ...string) *http.Response {
	ts.t.Helper()

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = strings.NewReader(body[0])
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.App.Test(req, -1)
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

// Put performs a PUT request with JSON body.
func (ts *TestServer) Put(path, body string) *http.Response {
	return ts.Request("PUT", path, body)
}

// Delete performs a DELETE request.
func (ts *TestServer) Delete(path string) *http.Response {
	return ts.Request("DELETE", path)
}
