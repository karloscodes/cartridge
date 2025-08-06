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
- `GET /products` - List all products (CRUD)
- `POST /products` - Create product (CRUD)  
- `GET /products/:id` - Get product (CRUD)
- `PUT /products/:id` - Update product (CRUD)
- `DELETE /products/:id` - Delete product (CRUD)
- `GET /health` - Custom health check with middleware config
- `GET /config` - App and middleware configuration

### Default Health Endpoints (Automatic)
- `GET /_health` - Comprehensive health check (database, config, status)
- `GET /_ready` - Readiness probe (for Kubernetes)
- `GET /_live` - Liveness probe (basic ping)

## Controller Pattern

```go
type ProductController struct {
    *cartridge.Context  // Gets DB, Logger, Config, Auth, Middleware
}

func NewProductController(ctx *cartridge.Context) cartridge.CrudController {
    return &ProductController{Context: ctx}
}

func (pc *ProductController) Index(c *fiber.Ctx) error {
    // Direct slog usage - simple and clean
    pc.Logger.Info("Fetching products", "endpoint", "index")
    
    // Database access
    products := pc.DB.Query("SELECT * FROM products")
    
    // Config access
    env := pc.Context.Config.Environment
    
    // Middleware config access
    corsOrigins := pc.Middleware.CORS.AllowOrigins
    
    return c.JSON(products)
}
```

## Key Benefits

- **Minimal Boilerplate**: Just embed `*cartridge.Context`
- **Full Access**: DB, Logger, Config, Auth, Middleware all available
- **Structured Logging**: Modern slog with key-value pairs
- **Graceful Operations**: Proper shutdown and cleanup
- **Type Safety**: All configs strongly typed and accessible