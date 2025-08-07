package cartridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// IntegrationTestClient provides a fluent API for testing Cartridge applications
type IntegrationTestClient struct {
	app       *App
	fiberApp  *fiber.App
	t         *testing.T
	baseURL   string
	headers   map[string]string
	cookies   []*http.Cookie
	csrfToken string
	sessionID string
}

// NewIntegrationTestClient creates a new integration test client
func NewIntegrationTestClient(t *testing.T, app *App) *IntegrationTestClient {
	t.Helper()

	return &IntegrationTestClient{
		app:      app,
		fiberApp: app.GetFiberApp(),
		t:        t,
		headers:  make(map[string]string),
		cookies:  make([]*http.Cookie, 0),
	}
}

// TestResponse wraps an HTTP response with testing utilities
type TestResponse struct {
	*http.Response
	Body   []byte
	client *IntegrationTestClient
	t      *testing.T
}

// TestRequest represents a request being built
type TestRequest struct {
	method      string
	path        string
	headers     map[string]string
	query       url.Values
	body        io.Reader
	contentType string
	client      *IntegrationTestClient
}

// Request starts building an HTTP request
func (c *IntegrationTestClient) Request(method, path string) *TestRequest {
	return &TestRequest{
		method:  method,
		path:    path,
		headers: make(map[string]string),
		query:   make(url.Values),
		client:  c,
	}
}

// GET creates a GET request
func (c *IntegrationTestClient) GET(path string) *TestRequest {
	return c.Request("GET", path)
}

// POST creates a POST request
func (c *IntegrationTestClient) POST(path string) *TestRequest {
	return c.Request("POST", path)
}

// PUT creates a PUT request
func (c *IntegrationTestClient) PUT(path string) *TestRequest {
	return c.Request("PUT", path)
}

// DELETE creates a DELETE request
func (c *IntegrationTestClient) DELETE(path string) *TestRequest {
	return c.Request("DELETE", path)
}

// PATCH creates a PATCH request
func (c *IntegrationTestClient) PATCH(path string) *TestRequest {
	return c.Request("PATCH", path)
}

// WithHeader adds a header to the request
func (r *TestRequest) WithHeader(key, value string) *TestRequest {
	r.headers[key] = value
	return r
}

// WithQuery adds a query parameter
func (r *TestRequest) WithQuery(key, value string) *TestRequest {
	r.query.Add(key, value)
	return r
}

// WithJSON sets the request body as JSON
func (r *TestRequest) WithJSON(data interface{}) *TestRequest {
	jsonData, err := json.Marshal(data)
	if err != nil {
		r.client.t.Fatalf("Failed to marshal JSON: %v", err)
	}
	r.body = bytes.NewReader(jsonData)
	r.contentType = "application/json"
	return r
}

// WithForm sets the request body as form data
func (r *TestRequest) WithForm(data map[string]string) *TestRequest {
	formData := url.Values{}
	for key, value := range data {
		formData.Set(key, value)
	}
	r.body = strings.NewReader(formData.Encode())
	r.contentType = "application/x-www-form-urlencoded"
	return r
}

// WithMultipartForm sets the request body as multipart form data
func (r *TestRequest) WithMultipartForm(data map[string]string, files map[string][]byte) *TestRequest {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add form fields
	for key, value := range data {
		if err := writer.WriteField(key, value); err != nil {
			r.client.t.Fatalf("Failed to write form field: %v", err)
		}
	}

	// Add files
	for fieldName, fileData := range files {
		part, err := writer.CreateFormFile(fieldName, "test_file.txt")
		if err != nil {
			r.client.t.Fatalf("Failed to create form file: %v", err)
		}
		if _, err := part.Write(fileData); err != nil {
			r.client.t.Fatalf("Failed to write file data: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		r.client.t.Fatalf("Failed to close multipart writer: %v", err)
	}

	r.body = &buf
	r.contentType = writer.FormDataContentType()
	return r
}

// WithCSRF adds CSRF token to the request
func (r *TestRequest) WithCSRF() *TestRequest {
	if r.client.csrfToken != "" {
		r.headers["X-CSRF-Token"] = r.client.csrfToken
	}
	return r
}

// WithAuth adds authorization header
func (r *TestRequest) WithAuth(token string) *TestRequest {
	r.headers["Authorization"] = "Bearer " + token
	return r
}

// WithRetry executes a request with retry logic
func (r *TestRequest) WithRetry(maxAttempts int, delay time.Duration) *TestResponse {
	var lastResponse *TestResponse

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastResponse = r.Expect()

		// If successful, return immediately
		if lastResponse.StatusCode >= 200 && lastResponse.StatusCode < 300 {
			return lastResponse
		}

		// If not the last attempt, wait before retrying
		if attempt < maxAttempts {
			time.Sleep(delay)
			r.client.t.Logf("Request attempt %d failed with status %d, retrying...",
				attempt, lastResponse.StatusCode)
		}
	}

	r.client.t.Logf("Request failed after %d attempts", maxAttempts)
	return lastResponse
}

// Expect executes the request and returns a response for assertions
func (r *TestRequest) Expect() *TestResponse {
	// Build the URL
	fullPath := r.path
	if len(r.query) > 0 {
		fullPath += "?" + r.query.Encode()
	}

	// Create the HTTP request
	req := httptest.NewRequest(r.method, fullPath, r.body)

	// Set content type
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}

	// Add headers
	for key, value := range r.headers {
		req.Header.Set(key, value)
	}

	// Add client headers
	for key, value := range r.client.headers {
		if req.Header.Get(key) == "" { // Don't override request-specific headers
			req.Header.Set(key, value)
		}
	}

	// Add cookies
	for _, cookie := range r.client.cookies {
		req.AddCookie(cookie)
	}

	// Execute the request
	resp, err := r.client.fiberApp.Test(req, -1)
	if err != nil {
		r.client.t.Fatalf("Request failed: %v", err)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		r.client.t.Fatalf("Failed to read response body: %v", err)
	}
	resp.Body.Close()

	// Update client state with cookies and CSRF tokens
	r.client.updateStateFromResponse(resp, body)

	return &TestResponse{
		Response: resp,
		Body:     body,
		client:   r.client,
		t:        r.client.t,
	}
}

// updateStateFromResponse updates client state from response
func (c *IntegrationTestClient) updateStateFromResponse(resp *http.Response, body []byte) {
	// Update cookies
	if cookies := resp.Cookies(); len(cookies) > 0 {
		c.cookies = append(c.cookies, cookies...)
	}

	// Extract CSRF token from response headers or body
	if csrfToken := resp.Header.Get("X-CSRF-Token"); csrfToken != "" {
		c.csrfToken = csrfToken
	}

	// Try to extract CSRF token from JSON response
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var jsonResp map[string]interface{}
		if err := json.Unmarshal(body, &jsonResp); err == nil {
			if token, ok := jsonResp["csrf_token"].(string); ok {
				c.csrfToken = token
			}
		}
	}
}

// Response assertion methods

// ExpectStatus asserts the response status code
func (r *TestResponse) ExpectStatus(expectedStatus int) *TestResponse {
	if r.StatusCode != expectedStatus {
		r.t.Errorf("Expected status %d, got %d", expectedStatus, r.StatusCode)
	}
	return r
}

// ExpectOK asserts 200 OK status
func (r *TestResponse) ExpectOK() *TestResponse {
	return r.ExpectStatus(200)
}

// ExpectCreated asserts 201 Created status
func (r *TestResponse) ExpectCreated() *TestResponse {
	return r.ExpectStatus(201)
}

// ExpectBadRequest asserts 400 Bad Request status
func (r *TestResponse) ExpectBadRequest() *TestResponse {
	return r.ExpectStatus(400)
}

// ExpectUnauthorized asserts 401 Unauthorized status
func (r *TestResponse) ExpectUnauthorized() *TestResponse {
	return r.ExpectStatus(401)
}

// ExpectForbidden asserts 403 Forbidden status
func (r *TestResponse) ExpectForbidden() *TestResponse {
	return r.ExpectStatus(403)
}

// ExpectNotFound asserts 404 Not Found status
func (r *TestResponse) ExpectNotFound() *TestResponse {
	return r.ExpectStatus(404)
}

// ExpectJSON asserts the response contains valid JSON and unmarshals it
func (r *TestResponse) ExpectJSON(target interface{}) *TestResponse {
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		r.t.Errorf("Expected JSON content type, got %s", contentType)
		return r
	}

	if err := json.Unmarshal(r.Body, target); err != nil {
		r.t.Errorf("Failed to unmarshal JSON response: %v", err)
	}
	return r
}

// ExpectJSONPath asserts a specific JSON path has an expected value
func (r *TestResponse) ExpectJSONPath(path string, expected interface{}) *TestResponse {
	var jsonData map[string]interface{}
	if err := json.Unmarshal(r.Body, &jsonData); err != nil {
		r.t.Errorf("Failed to unmarshal JSON for path assertion: %v", err)
		return r
	}

	// Simple path resolution (e.g., "data.user.name")
	value := r.getJSONValue(jsonData, path)
	if value != expected {
		r.t.Errorf("Expected JSON path %s to be %v, got %v", path, expected, value)
	}
	return r
}

// getJSONValue gets a value from JSON using dot notation
func (r *TestResponse) getJSONValue(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		default:
			return nil
		}
	}
	return current
}

// ExpectHTML asserts the response contains HTML
func (r *TestResponse) ExpectHTML() *TestResponse {
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		r.t.Errorf("Expected HTML content type, got %s", contentType)
	}
	return r
}

// ExpectBodyContains asserts the response body contains a substring
func (r *TestResponse) ExpectBodyContains(substring string) *TestResponse {
	if !strings.Contains(string(r.Body), substring) {
		r.t.Errorf("Expected response body to contain %q", substring)
	}
	return r
}

// ExpectBodyNotContains asserts the response body does not contain a substring
func (r *TestResponse) ExpectBodyNotContains(substring string) *TestResponse {
	if strings.Contains(string(r.Body), substring) {
		r.t.Errorf("Expected response body to not contain %q", substring)
	}
	return r
}

// ExpectHeader asserts a response header has the expected value
func (r *TestResponse) ExpectHeader(header, expected string) *TestResponse {
	actual := r.Header.Get(header)
	if actual != expected {
		r.t.Errorf("Expected header %s to be %q, got %q", header, expected, actual)
	}
	return r
}

// ExpectCookie asserts a cookie was set with the expected value
func (r *TestResponse) ExpectCookie(name, expected string) *TestResponse {
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			if cookie.Value != expected {
				r.t.Errorf("Expected cookie %s to be %q, got %q", name, expected, cookie.Value)
			}
			return r
		}
	}
	r.t.Errorf("Expected cookie %s not found", name)
	return r
}

// ExpectRedirect asserts the response is a redirect to the expected location
func (r *TestResponse) ExpectRedirect(expectedLocation string) *TestResponse {
	if r.StatusCode < 300 || r.StatusCode >= 400 {
		r.t.Errorf("Expected redirect status (3xx), got %d", r.StatusCode)
		return r
	}

	location := r.Header.Get("Location")
	if location != expectedLocation {
		r.t.Errorf("Expected redirect to %q, got %q", expectedLocation, location)
	}
	return r
}

// GetBodyString returns the response body as a string
func (r *TestResponse) GetBodyString() string {
	return string(r.Body)
}

// GetJSON unmarshals the response body as JSON
func (r *TestResponse) GetJSON(target interface{}) error {
	return json.Unmarshal(r.Body, target)
}

// Debug prints the last response for debugging
func (r *TestResponse) Debug() *TestResponse {
	r.t.Logf("Response Status: %d", r.StatusCode)
	r.t.Logf("Response Headers: %v", r.Header)
	r.t.Logf("Response Body: %s", string(r.Body))
	return r
}

// Client state management

// SetHeader sets a persistent header for all requests
func (c *IntegrationTestClient) SetHeader(key, value string) *IntegrationTestClient {
	c.headers[key] = value
	return c
}

// SetUserAgent sets the User-Agent header
func (c *IntegrationTestClient) SetUserAgent(userAgent string) *IntegrationTestClient {
	return c.SetHeader("User-Agent", userAgent)
}

// ClearCookies clears all stored cookies
func (c *IntegrationTestClient) ClearCookies() *IntegrationTestClient {
	c.cookies = make([]*http.Cookie, 0)
	return c
}

// ClearHeaders clears all stored headers
func (c *IntegrationTestClient) ClearHeaders() *IntegrationTestClient {
	c.headers = make(map[string]string)
	return c
}

// Authentication helpers

// LoginWithCredentials performs a login request and stores session/CSRF tokens
func (c *IntegrationTestClient) LoginWithCredentials(loginPath, email, password string) *TestResponse {
	return c.POST(loginPath).
		WithForm(map[string]string{
			"email":    email,
			"password": password,
		}).
		WithCSRF().
		Expect()
}

// LoginWithJSON performs a JSON login request
func (c *IntegrationTestClient) LoginWithJSON(loginPath string, credentials map[string]interface{}) *TestResponse {
	return c.POST(loginPath).
		WithJSON(credentials).
		Expect()
}

// Database helpers for integration tests

// WithCleanDatabase runs a test with a clean database state
func (c *IntegrationTestClient) WithCleanDatabase(testFunc func()) {
	// Get the database connection
	db := c.app.database.GetGenericConnection()
	if db == nil {
		c.t.Fatal("Database not available for testing")
	}

	// Clean up after test
	defer func() {
		if err := CleanupTestDB(db); err != nil {
			c.t.Logf("Failed to cleanup test database: %v", err)
		}
	}()

	// Run the test
	testFunc()
}

// SeedDatabase seeds the database with test data
func (c *IntegrationTestClient) SeedDatabase(seedFunc func(db interface{})) *IntegrationTestClient {
	db := c.app.database.GetGenericConnection()
	if db != nil {
		seedFunc(db)
	}
	return c
}

// Test utilities

// Sleep adds a delay (useful for testing time-sensitive operations)
func (c *IntegrationTestClient) Sleep(duration time.Duration) *IntegrationTestClient {
	time.Sleep(duration)
	return c
}

// Advanced testing features

// TestFixture represents reusable test data and setup
type TestFixture struct {
	Name    string
	Data    map[string]interface{}
	Setup   func(*IntegrationTestClient) error
	Cleanup func(*IntegrationTestClient) error
}

// LoadFixture loads and applies a test fixture
func (c *IntegrationTestClient) LoadFixture(fixture *TestFixture) *IntegrationTestClient {
	c.t.Helper()

	if fixture.Setup != nil {
		if err := fixture.Setup(c); err != nil {
			c.t.Fatalf("Failed to setup fixture %s: %v", fixture.Name, err)
		}

		// Register cleanup
		c.t.Cleanup(func() {
			if fixture.Cleanup != nil {
				if err := fixture.Cleanup(c); err != nil {
					c.t.Logf("Failed to cleanup fixture %s: %v", fixture.Name, err)
				}
			}
		})
	}

	return c
}

// Benchmark measures request performance
func (c *IntegrationTestClient) Benchmark(name string, iterations int, requestFunc func() *TestResponse) {
	c.t.Helper()

	// Warmup
	requestFunc()

	start := time.Now()
	for i := 0; i < iterations; i++ {
		requestFunc()
	}
	duration := time.Since(start)

	avgDuration := duration / time.Duration(iterations)
	c.t.Logf("Benchmark %s: %d iterations, avg: %v, total: %v",
		name, iterations, avgDuration, duration)
}

// ParallelTest runs multiple test scenarios in parallel
func (c *IntegrationTestClient) ParallelTest(name string, scenarios map[string]func(*IntegrationTestClient)) {
	c.t.Run(name, func(t *testing.T) {
		for scenarioName, scenarioFunc := range scenarios {
			scenarioName := scenarioName // capture loop variable
			scenarioFunc := scenarioFunc

			t.Run(scenarioName, func(t *testing.T) {
				t.Parallel()

				// Create a new client for this parallel test
				parallelClient := NewIntegrationTestClient(t, c.app)
				scenarioFunc(parallelClient)
			})
		}
	})
}

// Test data factories

// CreateUser creates a test user with default or custom data
func (c *IntegrationTestClient) CreateUser(overrides ...map[string]interface{}) map[string]interface{} {
	user := map[string]interface{}{
		"name":     "Test User",
		"email":    "test@example.com",
		"password": "password123",
		"active":   true,
	}

	// Apply overrides
	for _, override := range overrides {
		for key, value := range override {
			user[key] = value
		}
	}

	return user
}

// CreatePost creates a test post with default or custom data
func (c *IntegrationTestClient) CreatePost(userID interface{}, overrides ...map[string]interface{}) map[string]interface{} {
	post := map[string]interface{}{
		"title":     "Test Post",
		"content":   "This is a test post content",
		"user_id":   userID,
		"published": true,
	}

	// Apply overrides
	for _, override := range overrides {
		for key, value := range override {
			post[key] = value
		}
	}

	return post
}

// SeedUsers creates multiple test users
func (c *IntegrationTestClient) SeedUsers(count int) []map[string]interface{} {
	c.t.Helper()

	users := make([]map[string]interface{}, count)
	for i := 0; i < count; i++ {
		users[i] = c.CreateUser(map[string]interface{}{
			"email": fmt.Sprintf("user%d@example.com", i+1),
			"name":  fmt.Sprintf("User %d", i+1),
		})
	}

	return users
}

// Database transaction testing
func (c *IntegrationTestClient) WithTransaction(testFunc func(*IntegrationTestClient)) {
	c.t.Helper()

	// Begin transaction
	db := c.app.database.GetGenericConnection()
	if gormDB, ok := db.(*gorm.DB); ok {
		tx := gormDB.Begin()

		// Create a new client with the transaction
		txClient := &IntegrationTestClient{
			app:       c.app,
			fiberApp:  c.fiberApp,
			t:         c.t,
			headers:   c.headers,
			cookies:   c.cookies,
			csrfToken: c.csrfToken,
			sessionID: c.sessionID,
		}

		defer func() {
			// Always rollback the transaction
			tx.Rollback()
		}()

		testFunc(txClient)
	} else {
		c.t.Fatal("Database transaction testing requires GORM")
	}
}

// File system testing utilities
func (c *IntegrationTestClient) WithTempFiles(files map[string][]byte, testFunc func(tempDir string)) {
	c.t.Helper()

	// Create temporary directory
	tempDir := c.t.TempDir()

	// Write files
	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		dir := filepath.Dir(filePath)

		if err := os.MkdirAll(dir, 0o755); err != nil {
			c.t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		if err := os.WriteFile(filePath, content, 0o644); err != nil {
			c.t.Fatalf("Failed to write file %s: %v", filePath, err)
		}
	}

	testFunc(tempDir)
}

// Rate limiting testing
func (c *IntegrationTestClient) TestRateLimit(endpoint string, requestsPerSecond int, duration time.Duration) {
	c.t.Helper()

	ticker := time.NewTicker(time.Second / time.Duration(requestsPerSecond))
	defer ticker.Stop()

	timeout := time.After(duration)
	var successCount, rateLimitedCount int

	for {
		select {
		case <-ticker.C:
			response := c.GET(endpoint).Expect()
			if response.StatusCode == 200 {
				successCount++
			} else if response.StatusCode == 429 {
				rateLimitedCount++
			}
		case <-timeout:
			c.t.Logf("Rate limit test completed: %d successful, %d rate limited",
				successCount, rateLimitedCount)
			return
		}
	}
}

// Example test helper functions

// ExpectHealthy checks if the application health endpoints are working
func (c *IntegrationTestClient) ExpectHealthy() {
	c.GET("/_health").Expect().ExpectOK()
	c.GET("/_ready").Expect().ExpectOK()
	c.GET("/_live").Expect().ExpectOK()
}

// ExpectCSRFProtection verifies CSRF protection is working
func (c *IntegrationTestClient) ExpectCSRFProtection(protectedPath string) {
	// Request without CSRF token should fail
	c.POST(protectedPath).
		WithForm(map[string]string{"test": "data"}).
		Expect().
		ExpectStatus(403)

	// Request with CSRF token should work (assuming proper setup)
	c.POST(protectedPath).
		WithForm(map[string]string{"test": "data"}).
		WithCSRF().
		Expect()
}
