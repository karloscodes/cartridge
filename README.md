# Cartridge

A minimal, opinionated web framework built on [GoFiber](https://gofiber.io/) that I use as a boilerplate for my Go web projects.

## Features

- Request-scoped Context with dependency injection
- Clean route registration with per-route middleware
- Single handler signature: `func(*Context) error`
- Concurrency limiting middleware (useful for SQLite WAL mode)
- Application lifecycle with graceful shutdown
- Adapters for slog and zap logging

## Usage

```go
app, _ := cartridge.NewApplication(cartridge.ApplicationOptions{
    Config:    myConfig,    // implements cartridge.Config
    Logger:    myLogger,    // implements cartridge.Logger
    DBManager: myDBManager, // implements cartridge.DBManager
    RouteMountFunc: func(s *cartridge.Server) {
        s.Get("/", homeHandler)
        s.Post("/api/items", createHandler, &cartridge.RouteConfig{
            WriteConcurrency: true,
        })
    },
})

app.Run() // Blocks until SIGINT/SIGTERM
```

## License

MIT
