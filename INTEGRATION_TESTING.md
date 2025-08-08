# 🧪 Cartridge Integration Testing Framework

The Cartridge framework includes a powerful integration testing system that allows you to test your complete web applications with real HTTP requests, database interactions, and session management.

## 🚀 Quick Start

```go
func TestMyApplication(t *testing.T) {
    // Create your application with the same setup as production
    app := cartridge.NewFullStack(
        cartridge.WithEnvironment(cartridge.EnvTest),
        cartridge.WithCSRF(true),
    )
    
    // Add your actual routes
    app.Post("/api/users", func(ctx *cartridge.Context) error {
        var user map[string]interface{}
        ctx.ParseJSON(&user)
        ctx.Require(user, "name", "email")
        
        // Your business logic here...
        ctx.DBExec("INSERT INTO users (name, email) VALUES (?, ?)", 
            user["name"], user["email"])
        
        return ctx.Status(201).JSON(cartridge.Map{
            "id": 123,
            "name": user["name"], 
            "email": user["email"],
        })
    })
    
    // Create test client
    client := cartridge.NewIntegrationTestClient(t, app)
    
    // Test your endpoints
    client.WithCleanDatabase(func() {
        userData := map[string]interface{}{
            "name":  "John Doe",
            "email": "john@example.com",
        }
        
        var createdUser map[string]interface{}
        client.POST("/api/users").
            WithJSON(userData).
            Expect().
            ExpectCreated().
            ExpectJSON(&createdUser).
            ExpectJSONPath("name", "John Doe").
            ExpectJSONPath("email", "john@example.com")
    })
}
```

## 🎯 Key Features

### ✅ **Real Application Testing**
- Tests your actual application setup (same as production)
- Real HTTP requests through Fiber
- Real database transactions
- Real middleware stack

### ✅ **Fluent API**
- Chainable methods for readable tests
- Intuitive request building
- Comprehensive assertions

### ✅ **State Management**
- Automatic cookie handling
- CSRF token extraction and injection
- Session persistence across requests

### ✅ **Database Integration**
- Clean database state for each test
- Transaction rollback support
- Seed data utilities

## 📚 API Reference

### Creating Test Clients

```go
// Create test client for your app
client := cartridge.NewIntegrationTestClient(t, app)
```

### Making Requests

```go
// HTTP Methods
client.GET("/api/users")
client.POST("/api/users")
client.PUT("/api/users/123")
client.DELETE("/api/users/123")
client.PATCH("/api/users/123")

// Request customization
client.POST("/api/users").
    WithJSON(userData).                    // JSON body
    WithForm(formData).                    // Form data
    WithMultipartForm(fields, files).      // File uploads
    WithHeader("Accept", "application/json"). // Custom headers
    WithQuery("page", "1").                // Query parameters
    WithAuth("bearer-token").              // Authorization
    WithCSRF().                           // CSRF token
    WithRetry(3, time.Second).            // Retry logic
    Expect()                              // Execute request
```

### Response Assertions

```go
response := client.GET("/api/users").Expect()

// Status codes
response.ExpectOK()                    // 200
response.ExpectCreated()               // 201
response.ExpectBadRequest()            // 400
response.ExpectUnauthorized()          // 401
response.ExpectForbidden()             // 403
response.ExpectNotFound()              // 404
response.ExpectStatus(422)             // Custom status

// Content assertions
response.ExpectJSON(&target)           // Parse JSON
response.ExpectJSONPath("user.name", "John") // JSON path
response.ExpectHTML()                  // HTML content
response.ExpectBodyContains("success") // Body substring
response.ExpectHeader("Content-Type", "application/json")
response.ExpectCookie("session", "abc123")
response.ExpectRedirect("/dashboard")

// Utility methods
bodyString := response.GetBodyString()
response.GetJSON(&target)
response.Debug()                       // Log response details
```

### Database Testing

```go
// Clean database for each test
client.WithCleanDatabase(func() {
    // Your test code here
    // Database is automatically cleaned up after
})

// Seed test data
client.SeedDatabase(func(db interface{}) {
    if gormDB, ok := db.(*gorm.DB); ok {
        gormDB.Exec("INSERT INTO users (name, email) VALUES (?, ?)", 
            "Test User", "test@example.com")
    }
})

// Test with transactions (auto-rollback)
client.WithTransaction(func(txClient *cartridge.IntegrationTestClient) {
    // All database operations are rolled back
})
```

### Authentication Testing

```go
// Login and maintain session
client.LoginWithCredentials("/login", "admin@example.com", "password")

// Or login with JSON
client.LoginWithJSON("/api/login", map[string]interface{}{
    "email":    "admin@example.com",
    "password": "password",
})

// Access protected routes (cookies/session maintained automatically)
client.GET("/dashboard").Expect().ExpectOK()
```

### Authentication Test Helpers

Cartridge provides specialized helpers for testing authentication and authorization:

```go
// Test that routes require authentication (redirects to login)
client.AssertRequiresAuth("GET", "/admin/dashboard")
client.AssertRequiresAuth("POST", "/admin/users")
client.AssertRequiresAuth("PUT", "/admin/users/1")
client.AssertRequiresAuth("DELETE", "/admin/users/1")

// Test with custom login redirect path
authConfig := &cartridge.AuthTestConfig{
    LoginRedirectPath: "/custom-login",
}
client.AssertRequiresAuth("GET", "/admin", authConfig)

// Test form validation errors
client.AssertValidationError("/admin/login", map[string]string{
    "username": "",
    "password": "wrongpass",
}, "Invalid username or password")

// Test successful form submissions
client.AssertSuccessfulRedirect("/admin/login", "/admin/dashboard", map[string]string{
    "username": "admin",
    "password": "admin123",
})

// Test multiple routes require authentication at once
protectedRoutes := []struct{ Method, Path string }{
    {"GET", "/admin/dashboard"},
    {"GET", "/admin/users"},
    {"POST", "/admin/users"},
    {"PUT", "/admin/users/1"},
    {"DELETE", "/admin/users/1"},
}
client.AssertMultipleRoutesRequireAuth(protectedRoutes)
```

### Advanced Authentication Testing

For more complex scenarios, use the flexible configuration helpers:

```go
// Test validation with custom HTTP method and response expectations
validationConfig := &cartridge.ValidationTestConfig{
    Method:       "PUT",
    ExpectStatus: 422,
    ContentType:  "application/json",
}
client.AssertValidationErrorWithConfig("/api/users/1", invalidData, "validation failed", validationConfig)

// Test form submissions with custom configuration
formConfig := &cartridge.FormSubmissionConfig{
    Method:            "PATCH",
    WithCSRF:          true,
    ExpectStatus:      200,
    ExpectContentType: "application/json",
    Headers: map[string]string{
        "Accept": "application/json",
    },
}
client.AssertFormSubmissionWithConfig("/api/users/1", "", userData, formConfig)

// Test JSON API validation errors
client.AssertJSONValidationError("/api/users", map[string]string{
    "name":  "",
    "email": "invalid-email",
}, map[string]string{
    "name":  "required",
    "email": "invalid format",
})
```

### Advanced Features

```go
// Parallel testing
client.ParallelTest("User Operations", map[string]func(*cartridge.IntegrationTestClient){
    "Create User": func(c *cartridge.IntegrationTestClient) {
        // Test user creation
    },
    "Update User": func(c *cartridge.IntegrationTestClient) {
        // Test user updates
    },
})

// Performance benchmarking
client.Benchmark("API Performance", 100, func() *cartridge.TestResponse {
    return client.GET("/api/users").Expect()
})

// Rate limit testing
client.TestRateLimit("/api/users", 10, time.Second*5) // 10 requests/sec for 5 seconds

// Test fixtures
userFixture := &cartridge.TestFixture{
    Name: "Standard Users",
    Setup: func(c *cartridge.IntegrationTestClient) error {
        // Setup test data
        return nil
    },
    Cleanup: func(c *cartridge.IntegrationTestClient) error {
        // Cleanup test data
        return nil
    },
}
client.LoadFixture(userFixture)

// File system testing
client.WithTempFiles(map[string][]byte{
    "config.json": []byte(`{"key": "value"}`),
    "data.csv":    []byte("name,email\nJohn,john@example.com"),
}, func(tempDir string) {
    // Test with temporary files
})
```

### Test Data Factories

```go
// Create test users with defaults
user := client.CreateUser() // Default test user

// Override specific fields
user := client.CreateUser(map[string]interface{}{
    "email": "custom@example.com",
    "name":  "Custom Name",
})

// Create multiple users
users := client.SeedUsers(5) // Creates 5 test users

// Create test posts
post := client.CreatePost(userID, map[string]interface{}{
    "title": "Custom Post Title",
})
```

### Client State Management

```go
// Set persistent headers
client.SetHeader("Accept", "application/json").
       SetUserAgent("Test Agent")

// Clear state
client.ClearCookies().ClearHeaders()
```

## 🔧 Best Practices

### 1. **Use Real Application Setup**
```go
// ✅ Good: Use the same configuration as production
app := cartridge.NewFullStack(
    cartridge.WithEnvironment(cartridge.EnvTest),
    cartridge.WithCSRF(true),
    cartridge.WithDatabase("sqlite://test.db"),
)

// ❌ Avoid: Mocking the entire application
```

### 2. **Test Complete User Flows**
```go
// ✅ Good: Test complete workflows
func TestUserRegistrationFlow(t *testing.T) {
    client := cartridge.NewIntegrationTestClient(t, app)
    
    // 1. Register user
    client.POST("/register").WithForm(userData).WithCSRF().Expect().ExpectOK()
    
    // 2. Login
    client.POST("/login").WithForm(loginData).Expect().ExpectOK()
    
    // 3. Access protected resource
    client.GET("/dashboard").Expect().ExpectOK()
    
    // 4. Update profile
    client.PUT("/profile").WithJSON(updateData).WithCSRF().Expect().ExpectOK()
}
```

### 3. **Use Database Transactions for Isolation**
```go
// ✅ Good: Each test gets clean database state
client.WithCleanDatabase(func() {
    // Test code here
})

// Or for more control:
client.WithTransaction(func(txClient *cartridge.IntegrationTestClient) {
    // All DB operations are rolled back
})
```

### 4. **Test Error Cases**
```go
// ✅ Good: Test both success and failure scenarios
func TestUserValidation(t *testing.T) {
    client := cartridge.NewIntegrationTestClient(t, app)
    
    // Test validation errors
    client.POST("/api/users").
        WithJSON(map[string]interface{}{"name": ""}). // Missing required field
        Expect().
        ExpectBadRequest().
        ExpectJSONPath("error", "validation failed")
    
    // Test success case
    client.POST("/api/users").
        WithJSON(validUserData).
        Expect().
        ExpectCreated()
}
```

### 5. **Use Descriptive Test Names**
```go
// ✅ Good: Clear test intentions
func TestUserCannotAccessOtherUsersData(t *testing.T) { }
func TestCSRFProtectionBlocksUnauthorizedRequests(t *testing.T) { }
func TestPasswordResetEmailContainsValidToken(t *testing.T) { }

// ❌ Avoid: Generic test names
func TestUser(t *testing.T) { }
func TestAPI(t *testing.T) { }
```

## 🏃‍♂️ Running Tests

### Standard Go Testing
```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test
go test -run TestUserRegistrationFlow

# Run with coverage
go test -cover ./...
```

### Integration Test Tags
Use build tags to separate integration tests:

```go
//go:build integration
// +build integration

package main

func TestCompleteWorkflow(t *testing.T) {
    // Integration test code
}
```

Run integration tests:
```bash
go test -tags=integration ./...
```

## 🎯 Example Test Suites

### API Testing
```go
func TestAPIEndpoints(t *testing.T) {
    app := cartridge.NewAPIOnly(cartridge.WithEnvironment(cartridge.EnvTest))
    // Add your API routes...
    
    client := cartridge.NewIntegrationTestClient(t, app)
    
    client.WithCleanDatabase(func() {
        // Test health check
        client.GET("/api/health").Expect().ExpectOK()
        
        // Test CRUD operations
        var user map[string]interface{}
        client.POST("/api/users").
            WithJSON(userData).
            Expect().
            ExpectCreated().
            ExpectJSON(&user)
        
        userId := user["id"]
        
        client.GET(fmt.Sprintf("/api/users/%v", userId)).
            Expect().
            ExpectOK().
            ExpectJSONPath("name", userData["name"])
        
        client.PUT(fmt.Sprintf("/api/users/%v", userId)).
            WithJSON(updateData).
            Expect().
            ExpectOK()
        
        client.DELETE(fmt.Sprintf("/api/users/%v", userId)).
            Expect().
            ExpectOK()
    })
}
```

### Web Application Testing
```go
func TestWebApplication(t *testing.T) {
    app := cartridge.NewFullStack(
        cartridge.WithEnvironment(cartridge.EnvTest),
        cartridge.WithCSRF(true),
    )
    // Add your web routes...
    
    client := cartridge.NewIntegrationTestClient(t, app)
    
    client.WithCleanDatabase(func() {
        // Test home page
        client.GET("/").
            Expect().
            ExpectOK().
            ExpectHTML().
            ExpectBodyContains("Welcome")
        
        // Test form submission with CSRF
        client.POST("/contact").
            WithForm(contactData).
            WithCSRF().
            Expect().
            ExpectRedirect("/contact/success")
        
        // Test file upload
        client.POST("/upload").
            WithMultipartForm(
                map[string]string{"description": "Test file"},
                map[string][]byte{"file": fileContent},
            ).
            WithCSRF().
            Expect().
            ExpectOK()
    })
}
```

### Complete Authentication Test Example

Here's a real-world example using the authentication helpers:

```go
func TestAuthenticationWorkflow(t *testing.T) {
    app := setupTestApp() // Your app setup
    client := cartridge.NewIntegrationTestClient(t, app)
    
    client.WithCleanDatabase(func() {
        // Test that protected routes require authentication
        protectedRoutes := []struct{ Method, Path string }{
            {"GET", "/admin/dashboard"},
            {"GET", "/admin/users"},
            {"POST", "/admin/users"},
            {"PUT", "/admin/users/1"},
            {"DELETE", "/admin/users/1"},
            {"GET", "/admin/settings"},
            {"POST", "/admin/settings"},
        }
        client.AssertMultipleRoutesRequireAuth(protectedRoutes)
        
        // Test login validation errors
        client.AssertValidationError("/admin/login", map[string]string{
            "username": "",
            "password": "",
        }, "Username and password are required")
        
        client.AssertValidationError("/admin/login", map[string]string{
            "username": "admin",
            "password": "wrongpassword",
        }, "Invalid username or password")
        
        // Test successful login
        client.AssertSuccessfulRedirect("/admin/login", "/admin/dashboard", map[string]string{
            "username": "admin",
            "password": "admin123",
        })
        
        // After login, verify access to protected routes works
        client.GET("/admin/dashboard").Expect().ExpectOK().ExpectHTML()
        client.GET("/admin/users").Expect().ExpectOK().ExpectHTML()
        
        // Test logout
        client.POST("/admin/logout").WithCSRF().Expect().ExpectRedirect("/admin/login")
        
        // Verify logout worked - should require auth again
        client.AssertRequiresAuth("GET", "/admin/dashboard")
    })
}

func TestUserManagement(t *testing.T) {
    app := setupTestApp()
    client := cartridge.NewIntegrationTestClient(t, app)
    
    client.WithCleanDatabase(func() {
        // Login as admin first
        client.LoginWithCredentials("/admin/login", "admin", "admin123")
        
        // Test user creation validation
        client.AssertValidationError("/admin/users", map[string]string{
            "name":  "",
            "email": "invalid-email",
        }, "Name is required")
        
        client.AssertValidationError("/admin/users", map[string]string{
            "name":  "John Doe",
            "email": "invalid-email",
        }, "Please enter a valid email address")
        
        // Test successful user creation
        client.AssertSuccessfulRedirect("/admin/users", "/admin/users/1", map[string]string{
            "name":  "John Doe",
            "email": "john@example.com",
            "role":  "user",
        })
        
        // Test user update validation
        client.AssertValidationError("/admin/users/1", map[string]string{
            "name":  "",
            "email": "john@example.com",
        }, "Name is required")
        
        // Test successful user update
        updateConfig := &cartridge.FormSubmissionConfig{
            Method: "PUT",
        }
        client.AssertFormSubmissionWithConfig("/admin/users/1", "/admin/users/1", map[string]string{
            "name":  "John Updated",
            "email": "john.updated@example.com",
        }, updateConfig)
    })
}

func TestAPIAuthentication(t *testing.T) {
    app := setupAPIApp() // Your API-only app setup
    client := cartridge.NewIntegrationTestClient(t, app)
    
    client.WithCleanDatabase(func() {
        // Test API endpoints require authentication
        client.AssertRequiresAuth("GET", "/api/v1/users")
        client.AssertRequiresAuth("POST", "/api/v1/users") 
        
        // Test JSON validation errors
        client.AssertJSONValidationError("/api/v1/users", map[string]string{
            "name":  "",
            "email": "invalid",
        }, map[string]string{
            "name":  "required",
            "email": "invalid format",
        })
        
        // Test successful API authentication
        var authResponse map[string]interface{}
        client.POST("/api/v1/auth/login").
            WithJSON(map[string]interface{}{
                "email":    "admin@example.com",
                "password": "admin123",
            }).
            Expect().
            ExpectOK().
            ExpectJSON(&authResponse)
        
        // Use the token for authenticated requests
        token := authResponse["token"].(string)
        client.GET("/api/v1/users").
            WithAuth(token).
            Expect().
            ExpectOK()
    })
}
```

The Cartridge integration testing framework gives you confidence that your entire application works correctly by testing the actual HTTP endpoints, database interactions, and user workflows that your production application will handle. 🚀
