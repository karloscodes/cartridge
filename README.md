# Cartridge

An opinionated web framework boilerplate built on [GoFiber](https://gofiber.io/) that I use for my Go projects.

## What it does

- Request-scoped context with dependency injection (logger, config, db)
- Single handler signature: `func(*Context) error`
- Per-route middleware configuration
- Graceful shutdown with signal handling
- Concurrency limiting for SQLite applications (WAL mode)
- Logger adapters for slog and zap

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
