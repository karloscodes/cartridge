// Package cartridge provides a minimal, opinionated web framework built on net/http.
//
// Cartridge is designed for building web applications with a clean, type-safe API
// and sensible defaults. It provides:
//
//   - Request-scoped Context with dependency injection (Logger, Config, DBManager)
//   - Clean route registration with per-route middleware configuration
//   - Single handler signature: func(*Context) error
//   - Built-in middleware for concurrency limiting, recovery, compression, etc.
//   - Application lifecycle management with graceful shutdown
//   - Logging with log/slog: Logger is an alias for *slog.Logger
//
// # Constructors
//
// Cartridge has two constructors.
//
// ## NewApp
//
// NewApp wires a logger, a SQLite database, sessions, embedded assets, and
// background jobs. Use it for Go HTML templates and, with WithInertia, for
// Inertia.js apps.
//
//	cfg, err := config.Load("myapp")
//	app, err := cartridge.NewApp(cfg,
//	    cartridge.WithAssets(templates, static),
//	    cartridge.WithRoutes(mountRoutes),
//	    cartridge.WithSession("/login"),
//	    cartridge.WithJobs(5*time.Minute, cleanupJob),
//	)
//
// ## NewApplication
//
// NewApplication gives full control. Use it for PostgreSQL or a custom
// database manager.
//
//	app, err := cartridge.NewApplication(cartridge.ApplicationOptions{
//	    Config:         myConfig,
//	    Logger:         myLogger,
//	    DBManager:      myDBManager,
//	    RouteMountFunc: mountRoutes,
//	})
//
// # Embedded Assets
//
// Production serves templates and static files from the embedded fs.FS.
// Development reads them from disk for hot-reload.
//
// Create an embed.go in your web package:
//
//	//go:embed dist/assets
//	var assetsFS embed.FS
//
//	func Assets() fs.FS {
//	    sub, _ := fs.Sub(assetsFS, "dist/assets")
//	    return sub
//	}
//
// # Quick Start
//
// Create a new application:
//
//	app, err := cartridge.NewApplication(cartridge.ApplicationOptions{
//		Config:    myConfig,           // implements cartridge.Config
//		Logger:    myLogger,           // implements cartridge.Logger
//		DBManager: myDBManager,        // implements cartridge.DBManager
//		RouteMountFunc: func(s *cartridge.Server) {
//			s.Get("/", homeHandler)
//			s.Post("/api/items", createItemHandler, &cartridge.RouteConfig{
//				WriteConcurrency: true,
//			})
//		},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Run handles SIGINT and SIGTERM, and returns nil after a graceful shutdown.
//	if err := app.Run(); err != nil {
//		log.Fatal(err)
//	}
//
// # Handler Signature
//
// Cartridge uses a single handler signature for all routes:
//
//	func(ctx *cartridge.Context) error
//
// The Context provides access to:
//
//   - The request and response (Request, Response) and Fiber-style helpers (Params, Query, JSON, ...)
//   - Logger for request logging
//   - Config for runtime configuration
//   - DB() for database access with request context
//
// # Logging
//
// cartridge.Logger is an alias for *slog.Logger. Pass any *slog.Logger,
// or build one from config with NewLogger.
//
// # Concurrency Limiting
//
// For SQLite with WAL mode, use WriteConcurrency to limit concurrent writes:
//
//	s.Post("/api/items", handler, &cartridge.RouteConfig{
//		WriteConcurrency: true,
//	})
//
// # CORS Configuration
//
// Enable CORS for public API routes:
//
//	s.Get("/api/public", handler, &cartridge.RouteConfig{
//		EnableCORS: true,
//	})
package cartridge
