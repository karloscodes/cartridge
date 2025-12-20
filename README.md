# Cartridge - Go Web Framework

An opinionated, batteries-included Go web framework built on [Fiber](https://gofiber.io) for server-side rendered applications.

> **Note**: This module is under active development and APIs may change.

## Features

- **SSR-first** - Server-side rendering with Go templates
- **Multiple databases** - SQLite (with WAL) or PostgreSQL support
- **Session management** - Secure cookie-based sessions with HMAC signing
- **Background jobs** - Simple job dispatcher for async processing
- **Structured logging** - JSON/text logging with log rotation
- **Middleware** - Rate limiting, concurrency control, security headers

## Quick Start

```bash
go get github.com/karloscodes/cartridge
```

### Using NewSSRApp (Recommended for SQLite)

`NewSSRApp` is the high-level factory for SSR applications with SQLite:

```go
package main

import (
    "time"
    "github.com/karloscodes/cartridge"
    "myapp/web"
)

func main() {
    app, err := cartridge.NewSSRApp("myapp",
        cartridge.WithAssets(web.Templates, web.Static),
        cartridge.WithSession("/login"),
        cartridge.WithRoutes(func(s *cartridge.Server) {
            s.Get("/", homeHandler)
            s.Get("/users", usersHandler)
        }),
    )
    if err != nil {
        panic(err)
    }

    if err := app.MigrateDatabase(myMigrator); err != nil {
        panic(err)
    }

    if err := app.Run(); err != nil {
        panic(err)
    }
}

func homeHandler(ctx *cartridge.Context) error {
    return ctx.Render("home", fiber.Map{"title": "Welcome"})
}
```

### Using NewApplication (For Custom Setups)

`NewApplication` is the lower-level constructor for full control over dependencies. Use this when you need PostgreSQL, a custom database manager, or non-SSR applications:

```go
package main

import (
    "log/slog"
    "github.com/karloscodes/cartridge"
    "github.com/karloscodes/cartridge/database"
    "github.com/karloscodes/cartridge/postgres"
)

func main() {
    // Create your own dependencies
    logger := slog.Default()

    // Use PostgreSQL
    dbManager := database.NewManager(
        postgres.NewDriver(),
        &database.Config{
            DSN:          "host=localhost user=app dbname=myapp",
            MaxOpenConns: 25,
            MaxIdleConns: 5,
            Postgres: database.PostgresOptions{
                SSLMode:  "disable",
                Timezone: "UTC",
            },
        },
        logger,
    )

    // Create application with custom dependencies
    app, err := cartridge.NewApplication(cartridge.ApplicationOptions{
        Config:    myConfig,    // implements cartridge.Config interface
        Logger:    logger,
        DBManager: dbManager,   // implements cartridge.DBManager interface
        RouteMountFunc: func(s *cartridge.Server) {
            s.Get("/", homeHandler)
            s.Post("/api/items", createItemHandler)
        },
    })
    if err != nil {
        panic(err)
    }

    if err := app.Run(); err != nil {
        panic(err)
    }
}
```

## Database Support

Cartridge supports multiple databases through a pluggable driver interface.

### SQLite (Default)

SQLite is the default for `NewSSRApp`. It uses WAL mode and immediate transactions for optimal concurrency:

```go
import "github.com/karloscodes/cartridge/sqlite"

dbManager := sqlite.NewManager(sqlite.Config{
    Path:         "storage/app.db",
    MaxOpenConns: 1,              // SQLite works best with 1 connection
    MaxIdleConns: 1,
    BusyTimeout:  5000,           // ms
    EnableWAL:    true,           // Write-Ahead Logging (default: true)
    TxImmediate:  true,           // Immediate transaction locks (default: true)
    Logger:       logger,
})
```

### PostgreSQL

For PostgreSQL, use the generic database manager with the PostgreSQL driver:

```go
import (
    "github.com/karloscodes/cartridge/database"
    "github.com/karloscodes/cartridge/postgres"
)

dbManager := database.NewManager(
    postgres.NewDriver(),
    &database.Config{
        DSN:          "host=localhost port=5432 user=app password=secret dbname=myapp",
        MaxOpenConns: 25,
        MaxIdleConns: 5,
        Postgres: database.PostgresOptions{
            SSLMode:    "prefer",    // disable, prefer, require
            Timezone:   "UTC",
            SearchPath: "public",    // optional schema
        },
    },
    logger,
)
```

### Custom Database Drivers

Implement the `database.Driver` interface for other databases:

```go
type Driver interface {
    Name() string
    Open(dsn string) gorm.Dialector
    ConfigureDSN(dsn string, cfg *Config) string
    AfterConnect(db *gorm.DB, cfg *Config, logger *slog.Logger) error
    Close(db *gorm.DB, logger *slog.Logger) error
    SupportsCheckpoint() bool
    Checkpoint(db *gorm.DB, mode string) error
}
```

## Configuration

Cartridge reads configuration from environment variables with the app name as prefix:

```bash
MYAPP_ENV=production          # development, production, test
MYAPP_PORT=8080
MYAPP_SESSION_SECRET=xxx      # Required in production
MYAPP_LOG_LEVEL=info
MYAPP_DATA_DIR=storage
```

## App Options (NewSSRApp)

```go
app, err := cartridge.NewSSRApp("myapp",
    // Custom configuration
    cartridge.WithConfig(cfg),

    // Embedded templates and static files
    cartridge.WithAssets(web.Templates, web.Static),

    // Custom template functions
    cartridge.WithTemplateFuncs(myFuncs),

    // Custom error handler
    cartridge.WithErrorHandler(myErrorHandler),

    // Enable session management
    cartridge.WithSession("/login"),

    // Background job processors
    cartridge.WithJobs(2*time.Minute, emailProcessor, webhookProcessor),

    // Route mounting
    cartridge.WithRoutes(mountRoutes),
)
```

## Database Migrations

```go
// Create a migrator with your models
migrator := cartridge.NewAutoMigrator(
    &User{},
    &Post{},
    &Comment{},
)

// Run migrations (connects, migrates, checkpoints WAL for SQLite)
if err := app.MigrateDatabase(migrator); err != nil {
    panic(err)
}
```

## Session Management

```go
// In your login handler
func loginHandler(ctx *cartridge.Context) error {
    // Validate credentials...

    session := ctx.Ctx.Locals("session").(*cartridge.SessionManager)
    if err := session.SetSession(ctx.Ctx, userID); err != nil {
        return err
    }
    return ctx.Redirect("/dashboard")
}

// Protected routes use session middleware
authConfig := &cartridge.RouteConfig{
    CustomMiddleware: []fiber.Handler{session.Middleware()},
}
s.Get("/dashboard", dashboardHandler, authConfig)
```

## Background Jobs

Jobs run on a fixed interval and process batches of work:

```go
// Implement the Processor interface
type EmailProcessor struct{}

func (p *EmailProcessor) ProcessBatch(ctx *cartridge.JobContext) error {
    ctx.Logger.Info("processing pending emails")

    var pending []Email
    if err := ctx.DB.Where("sent_at IS NULL").Find(&pending).Error; err != nil {
        return err
    }

    for _, email := range pending {
        // Send email...
        ctx.DB.Model(&email).Update("sent_at", time.Now())
    }
    return nil
}

// Register processors with interval
app, _ := cartridge.NewSSRApp("myapp",
    cartridge.WithJobs(2*time.Minute, &EmailProcessor{}, &WebhookProcessor{}),
)
```

## Interfaces

Cartridge uses interfaces for dependency injection, making it easy to swap implementations:

```go
// Config abstracts runtime configuration
type Config interface {
    IsDevelopment() bool
    IsProduction() bool
    IsTest() bool
    GetPort() string
    GetPublicDirectory() string
    GetAssetsPrefix() string
}

// DBManager abstracts database connection management
type DBManager interface {
    GetConnection() *gorm.DB
    Connect() (*gorm.DB, error)
}
```

## License

MIT License - see [LICENSE](LICENSE) file for details.
