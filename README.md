# Cartridge - Go Web Framework

An opinionated, batteries-included Go web framework built on [Fiber](https://gofiber.io) for server-side rendered applications with SQLite.

> **Note**: This module is under active development and APIs may change.

## Features

- **SSR-first** - Server-side rendering with Go templates
- **SQLite with WAL** - Optimized database management with connection pooling
- **Session management** - Secure cookie-based sessions with HMAC signing
- **Background jobs** - Simple job dispatcher for async processing
- **Structured logging** - JSON/text logging with log rotation
- **Middleware** - Rate limiting, concurrency control, security headers

## Quick Start

```bash
go get github.com/karloscodes/cartridge
```

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

    // Run migrations
    if err := app.MigrateDatabase(myMigrator); err != nil {
        panic(err)
    }

    // Start server with graceful shutdown
    if err := app.Run(); err != nil {
        panic(err)
    }
}

func homeHandler(ctx *cartridge.Context) error {
    return ctx.Render("home", fiber.Map{"title": "Welcome"})
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

## App Options

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

// Run migrations (connects, migrates, checkpoints WAL)
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

## License

MIT License - see [LICENSE](LICENSE) file for details.
