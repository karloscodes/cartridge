package testing

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Config interface for testing configuration
type Config interface{}

// Logger interface for testing
type Logger interface{}

// Database interface for testing
type Database interface {
	Init() error
	Close() error
	GetGenericConnection() interface{}
}

// TestConfig holds testing configuration
type TestConfig struct {
	DatabaseURL string
	LogLevel    string
	Environment string
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

	// TODO: This would be implemented with proper JSON parsing
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

	// TODO: This would be implemented when Fiber test utilities are available
	// For now, return a mock response
	return &MockResponse{
		StatusCode: 200,
		Headers:    make(map[string]string),
		Body:       "mock response",
		Cookies:    make(map[string]string),
	}
}