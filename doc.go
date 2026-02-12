// Package cartridge provides a minimal, opinionated web framework built on GoFiber.
//
// Cartridge is designed for building web applications with a clean, type-safe API
// and sensible defaults. It provides:
//
//   - Request-scoped Context with dependency injection (Logger, Config, DBManager)
//   - Clean route registration with per-route middleware configuration
//   - Single handler signature: func(*Context) error
//   - Built-in middleware for concurrency limiting, recovery, compression, etc.
//   - Application lifecycle management with graceful shutdown
//   - Support for both slog and zap logging via adapters
//
// # Application Factories
//
// Cartridge provides three levels of application creation:
//
// ## NewApplication (Low-Level)
//
// Full control over all components. Use when you have custom requirements.
//
//	app, err := cartridge.NewApplication(cartridge.ApplicationOptions{
//	    Config:         myConfig,
//	    Logger:         myLogger,
//	    DBManager:      myDBManager,
//	    RouteMountFunc: mountRoutes,
//	})
//
// ## NewSSRApp (Server-Side Rendered HTML Templates)
//
// For traditional SSR apps with Go HTML templates. Handles logger, DB, sessions,
// embedded assets, and background jobs automatically.
//
//	app, err := cartridge.NewSSRApp("myapp",
//	    cartridge.WithAssets(templates, static),
//	    cartridge.WithRoutes(mountRoutes),
//	    cartridge.WithSession("/login"),
//	    cartridge.WithJobs(5*time.Minute, cleanupJob),
//	)
//
// ## NewInertiaApp (Inertia.js SPA)
//
// For Inertia.js apps (React/Vue SPA with server-side routing). Handles Inertia
// dev mode, embedded assets, cross-origin APIs, and background workers.
//
//	app, err := cartridge.NewInertiaApp(
//	    cartridge.InertiaWithConfig(cfg),
//	    cartridge.InertiaWithStaticAssets(web.Assets()),
//	    cartridge.InertiaWithRoutes(mountRoutes),
//	    cartridge.InertiaWithWorker(jobsManager),
//	    cartridge.InertiaWithSession("/login"),
//	    cartridge.InertiaWithCrossOriginAPI(),
//	)
//
// # Embedded Assets
//
// Both NewSSRApp and NewInertiaApp support embedded assets for single-binary deployment:
//
//   - Production: Assets served from embedded fs.FS (no external files needed)
//   - Development: Assets served from disk for hot-reload
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
//	// Start with signal handling and graceful shutdown
//	app.Run()
//
// # Handler Signature
//
// Cartridge uses a single handler signature for all routes:
//
//	func(ctx *cartridge.Context) error
//
// The Context provides access to:
//
//   - All Fiber HTTP methods via embedded *fiber.Ctx
//   - Logger for request logging
//   - Config for runtime configuration
//   - DB() for database access with request context
//
// # Logger Adapters
//
// Cartridge provides adapters for common logging libraries:
//
//	// For slog (Go 1.21+ stdlib)
//	logger := cartridge.NewSlogAdapter(slog.Default())
//
//	// For zap
//	zapLogger, _ := zap.NewProduction()
//	logger := cartridge.NewZapAdapter(zapLogger)
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
