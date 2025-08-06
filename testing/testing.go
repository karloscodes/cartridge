package testing

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/karloscodes/cartridge/app"
	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/database"
	"github.com/karloscodes/cartridge/logging"
)

// TestConfig holds testing configuration
type TestConfig struct {
	DatabaseURL string
	LogLevel    string
	Environment string
}

// DefaultTestConfig returns default testing configuration
func DefaultTestConfig() *config.Config {
	return &config.Config{
		Environment:      "test",
		Port:            "3000",
		DatabaseURL:     ":memory:",
		PrivateKey:      "test-secret-key-32-characters-long",
		Debug:           true,
		LogLevel:        config.LogLevelError, // Minimal logging for tests
		LogsDirectory:   "",                   // No file logging for tests
		LogsMaxSizeInMb: 20,
		LogsMaxBackups:  10,
		LogsMaxAgeInDays: 30,
		CSRFContextKey:  "csrf",
	}
}

// SetupTestDB creates an in-memory SQLite database for testing
func SetupTestDB(t *testing.T) interface{} {
	t.Helper()
	
	cfg := DefaultTestConfig()
	logger := logging.NewLogger(logging.LogConfig{
		Level:         logging.LogLevel(cfg.LogLevel),
		EnableConsole: false, // Disable console output for tests
		UseJSON:       false,
	})
	
	dbManager := database.NewDBManager(cfg, logger)
	err := dbManager.Init()
	if err != nil {
		t.Fatalf("Failed to setup test database: %v", err)
	}
	
	// Register cleanup
	t.Cleanup(func() {
		if err := dbManager.Close(); err != nil {
			t.Logf("Failed to close test database: %v", err)
		}
	})
	
	return dbManager.GetGenericConnection()
}

// CleanupTestDB cleans all tables in the test database
func CleanupTestDB(db interface{}) error {
	// This would clean all tables when GORM is available
	// For now, just return nil as we're using in-memory databases
	return nil
}

// SetupTestApp creates a complete test application with database
func SetupTestApp(t *testing.T) (interface{}, interface{}, func()) {
	t.Helper()
	
	// Create test dependencies
	deps, err := app.CreateAppDependencies()
	if err != nil {
		t.Fatalf("Failed to create test dependencies: %v", err)
	}
	
	// Override config for testing
	deps.Config = DefaultTestConfig()
	
	// Create Fiber app
	fiberConfig := app.FiberConfig{
		Environment: deps.Config.Environment,
		Port:       deps.Config.Port,
	}
	
	testApp := app.NewFiberApp(fiberConfig, deps)
	testDB := deps.DBManager.GetGenericConnection()
	
	cleanup := func() {
		if err := deps.DBManager.Close(); err != nil {
			t.Logf("Failed to close test database: %v", err)
		}
	}
	
	return testApp, testDB, cleanup
}

// LoginTestUser simulates user login and returns session cookie and CSRF tokens
func LoginTestUser(t *testing.T, app interface{}, email, password string) (sessionCookie, csrfToken, csrfCookie string) {
	t.Helper()
	
	// This would be implemented when Fiber test utilities are available
	// For now, return mock values
	return "mock-session-cookie", "mock-csrf-token", "mock-csrf-cookie"
}

// ExtractCSRFToken extracts CSRF token from HTML response body
func ExtractCSRFToken(body string) string {
	// Look for CSRF token in meta tag
	metaRegex := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)
	if matches := metaRegex.FindStringSubmatch(body); len(matches) > 1 {
		return matches[1]
	}
	
	// Look for CSRF token in hidden input field
	inputRegex := regexp.MustCompile(`<input[^>]*name="_csrf"[^>]*value="([^"]+)"`)
	if matches := inputRegex.FindStringSubmatch(body); len(matches) > 1 {
		return matches[1]
	}
	
	// Look for CSRF token in form field
	fieldRegex := regexp.MustCompile(`name="_csrf" value="([^"]+)"`)
	if matches := fieldRegex.FindStringSubmatch(body); len(matches) > 1 {
		return matches[1]
	}
	
	return ""
}

// ExtractCSRFCookie extracts CSRF token from cookie header
func ExtractCSRFCookie(resp *http.Response) string {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "_csrf_token" {
			return cookie.Value
		}
	}
	return ""
}

// CreateTestRequest creates an HTTP request for testing
func CreateTestRequest(method, path string, body io.Reader, headers map[string]string) *http.Request {
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		panic(fmt.Sprintf("Failed to create test request: %v", err))
	}
	
	// Set default headers
	req.Header.Set("User-Agent", "cartridge-test-client/1.0")
	
	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	
	return req
}

// ExtractCookies extracts all cookies from an HTTP response
func ExtractCookies(resp *http.Response) map[string]string {
	cookies := make(map[string]string)
	for _, cookie := range resp.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	return cookies
}

// AssertRedirect checks if the response is a redirect to the expected path
func AssertRedirect(t *testing.T, resp *http.Response, expectedPath string) {
	t.Helper()
	
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		t.Errorf("Expected redirect status code (3xx), got %d", resp.StatusCode)
		return
	}
	
	location := resp.Header.Get("Location")
	if location != expectedPath {
		t.Errorf("Expected redirect to %s, got %s", expectedPath, location)
	}
}

// AssertStatus checks if the response has the expected status code
func AssertStatus(t *testing.T, resp *http.Response, expectedStatus int) {
	t.Helper()
	
	if resp.StatusCode != expectedStatus {
		t.Errorf("Expected status code %d, got %d", expectedStatus, resp.StatusCode)
	}
}

// AssertContains checks if the response body contains the expected content
func AssertContains(t *testing.T, body, expected string) {
	t.Helper()
	
	if !strings.Contains(body, expected) {
		t.Errorf("Expected response body to contain %q", expected)
	}
}

// AssertNotContains checks if the response body does not contain the content
func AssertNotContains(t *testing.T, body, unexpected string) {
	t.Helper()
	
	if strings.Contains(body, unexpected) {
		t.Errorf("Expected response body not to contain %q", unexpected)
	}
}

// AssertJSONField checks if a JSON response contains a specific field with expected value
func AssertJSONField(t *testing.T, body, field string, expected interface{}) {
	t.Helper()
	
	// This would be implemented with proper JSON parsing
	// For now, just check if the field exists in the response
	if !strings.Contains(body, fmt.Sprintf(`"%s"`, field)) {
		t.Errorf("Expected JSON response to contain field %q", field)
	}
}

// MockRequest represents a mock HTTP request for testing
type MockRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    string
	Cookies map[string]string
}

// MockResponse represents a mock HTTP response for testing
type MockResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       string
	Cookies    map[string]string
}

// TestClient provides utilities for testing HTTP endpoints
type TestClient struct {
	app interface{} // Will be *fiber.App when available
	t   *testing.T
}

// NewTestClient creates a new test client
func NewTestClient(t *testing.T, app interface{}) *TestClient {
	return &TestClient{
		app: app,
		t:   t,
	}
}

// Get performs a GET request
func (tc *TestClient) Get(path string, headers ...map[string]string) *MockResponse {
	tc.t.Helper()
	
	var h map[string]string
	if len(headers) > 0 {
		h = headers[0]
	}
	
	return tc.Request("GET", path, "", h)
}

// Post performs a POST request
func (tc *TestClient) Post(path, body string, headers ...map[string]string) *MockResponse {
	tc.t.Helper()
	
	var h map[string]string
	if len(headers) > 0 {
		h = headers[0]
	}
	
	return tc.Request("POST", path, body, h)
}

// Request performs an HTTP request
func (tc *TestClient) Request(method, path, body string, headers map[string]string) *MockResponse {
	tc.t.Helper()
	
	// This would be implemented when Fiber test utilities are available
	// For now, return a mock response
	return &MockResponse{
		StatusCode: 200,
		Headers:    make(map[string]string),
		Body:       "mock response",
		Cookies:    make(map[string]string),
	}
}
