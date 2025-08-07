# Cartridge Examples

Clean, focused example demonstrating the improved Cartridge framework.

## Features Demonstrated

✅ **Fiber Integration** - Full Fiber v2.52.4 web framework integration  
✅ **Direct slog Usage** - Simplified logging using Go's slog directly  
✅ **GORM with slog** - Database ORM with structured logging integration  
✅ **Configurable CORS** - Set allowed origins via `WithCORSOrigins([]string{...})`  
✅ **Graceful Shutdown** - Proper signal handling and cleanup  
✅ **Database Management** - Connection pooling and graceful disconnect  
✅ **Middleware Stack** - Complete middleware setup with CORS, CSRF, Rate Limiting  
✅ **Clean Controller Pattern** - Minimal boilerplate with `*cartridge.Context`  
✅ **Default Health Endpoints** - Automatic health, readiness, and liveness checks  
✅ **Auto Port Configuration** - Uses configured port via `app.Start()`  

## Running the Example

```bash
cd examples/
go run main.go
```

## Usage Examples

### Ultimate Functional Pattern - One Parameter Only!

```go
// Just write functions - ultimate simplicity!
func ListProducts(ctx *cartridge.Context) error {
    // Easy database access
    db := ctx.DB()
    
    // Easy parameter access with defaults
    limit := ctx.QueryInt("limit", 10)
    search := ctx.Query("search", "")
    featured := ctx.QueryBool("featured", false)
    id := ctx.Params("id")
    
    // Clear parsing methods
    err := ctx.ParseJSON(&jsonData)  // For JSON APIs
    err := ctx.ParseForm(&formData)  // For HTML forms
    category := ctx.FormValue("category", "general")
    
    return ctx.JSON(data)         // Clean API responses
    // OR
    return ctx.Render(data)       // Forms/HTML with CSRF + metadata
}

// Register routes - super clean!
app.Get("/products", ListProducts)
app.Post("/products", CreateProduct)          // JSON API
app.Post("/products/form", CreateProductForm) // Form handling
```

### Smart CSRF Token Handling

```go
// For APIs - clean JSON, no CSRF tokens
return ctx.JSON(data)

// For forms/HTML - includes CSRF tokens + metadata  
return ctx.Render(data)
// Response: { "data": {...}, "csrf_token": "abc123", "meta": {...} }

// Framework automatically handles CSRF validation via middleware
```

### Configurable CORS Origins

```go
// Development with specific frontend origins
app := cartridge.NewAPIOnly(
    cartridge.WithCORS(true),
    cartridge.WithCORSOrigins([]string{
        "http://localhost:3000",  // React dev server
        "http://localhost:5173",  // Vite dev server
    }),
)

// Production with your domain
app := cartridge.NewAPIOnly(
    cartridge.WithEnvironment("production"),
    cartridge.WithCORS(true),
    cartridge.WithCORSOrigins([]string{
        "https://yourapp.com",
        "https://www.yourapp.com",
    }),
)
```

### Simplified slog Usage

```go
// Direct slog usage - no wrapper needed
logger := app.Logger()
logger.Info("Starting server", "port", "8080")
logger.Error("Database error", "error", err, "table", "users")

// With structured context
contextLogger := logger.With("service", "auth", "version", "1.0")
contextLogger.Info("User login", "user_id", 123)
```

### GORM Integration with slog

```go
// GORM automatically logs through slog
db := app.GetDatabase().GetGenericConnection().(*gorm.DB)

// All GORM operations are logged with structured data:
// - SQL queries with execution time
// - Slow query warnings (>200ms)
// - Error logging with full context
// - Debug info in development mode

// Example logged output:
// DEBUG Database query duration=1.2ms sql="SELECT * FROM users WHERE id = ?" rows=1
// WARN Slow database query duration=250ms sql="SELECT * FROM posts INNER JOIN users..." rows=100
// ERROR Database query failed error="UNIQUE constraint failed" sql="INSERT INTO users..." rows=0
```

## Testing Graceful Shutdown

1. Start the server: `go run main.go`
2. Press `Ctrl+C` to send SIGINT
3. Watch the graceful shutdown process in logs

## API Endpoints

### Custom Endpoints
- `GET /` - Welcome message with feature list
- `GET /products?search=...&limit=...` - List products with search and pagination
- `GET /products/:id` - Get product by ID
- `POST /products` - Create product (JSON API)
- `POST /products/form` - Create product (HTML form)
- `PUT /products/:id` - Update product by ID
- `DELETE /products/:id?confirm=true` - Delete product with confirmation

### Default Health Endpoints (Automatic)
- `GET /_health` - Comprehensive health check (database, config, status)
- `GET /_ready` - Readiness probe (for Kubernetes)
- `GET /_live` - Liveness probe (basic ping)

## Ultimate Functional Handler Pattern

```go
// Just write pure functions - ultimate simplicity!
func ListProducts(ctx *cartridge.Context) error {
    // Direct slog usage - simple and clean
    ctx.Logger.Info("Fetching products", "endpoint", "list")
    
    // Easy query parameters with defaults
    limit := ctx.QueryInt("limit", 10)
    search := ctx.Query("search", "")
    
    // Clean database access
    db := ctx.DB()
    var products []map[string]interface{}
    
    // Build dynamic query
    query := "SELECT * FROM products"
    if search != "" {
        query += " WHERE name LIKE ?"
        db.Raw(query+" LIMIT ?", "%"+search+"%", limit).Scan(&products)
    } else {
        db.Raw(query+" LIMIT ?", limit).Scan(&products)
    }
    
    // Smart response - CSRF only when needed
    return ctx.JSON(products)      // Clean API
    // OR ctx.Render(products)     // HTML with CSRF + metadata
}

// Register the function - one parameter only!
app.Get("/products", ListProducts)
```

## Key Benefits

- **Zero Boilerplate**: No controllers, no factories, just pure functions
- **Automatic Context Injection**: Framework handles dependency injection
- **Full Access**: DB, Logger, Config, Auth, Middleware all available via context
- **Structured Logging**: Modern slog with key-value pairs
- **Graceful Operations**: Proper shutdown and cleanup
- **Type Safety**: All configs strongly typed and accessible
- **Clean Syntax**: Separate lines, easy to read and maintain