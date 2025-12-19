package cartridge

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	cartridgemiddleware "github.com/karloscodes/cartridge/middleware"
)

// ServerConfig provides comprehensive server configuration with sensible defaults.
type ServerConfig struct {
	// Core dependencies (required)
	Config    Config
	Logger    Logger
	DBManager DBManager

	// Fiber configuration
	ErrorHandler   fiber.ErrorHandler
	Concurrency    int
	ProxyHeader    string
	TrustedProxies []string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration

	// Template engine configuration
	EnableTemplates    bool
	TemplatesFS        fs.FS  // Embedded filesystem for templates (production)
	TemplatesDirectory string // Directory for templates (development)
	ViewsEngine        fiber.Views

	// Static assets configuration
	EnableStaticAssets bool
	StaticFS           fs.FS  // Embedded filesystem for static assets (production)
	StaticDirectory    string // Directory for static assets (development)
	StaticPrefix       string

	// Middleware configuration
	EnableRequestID     bool
	EnableRecover       bool
	EnableHelmet        bool
	EnableCompress      bool
	EnableSecFetchSite  bool // CSRF protection via Sec-Fetch-Site header
	EnableRequestLogger bool

	// Concurrency configuration (for SQLite WAL mode)
	MaxConcurrentReads  int
	MaxConcurrentWrites int
	ConcurrencyTimeout  time.Duration
}

// DefaultServerConfig returns a configuration with sensible defaults.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		// Server defaults
		Concurrency:  256 * 1024,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,

		// Static assets
		EnableStaticAssets: true,
		StaticPrefix:       "/assets",

		// Middleware defaults (all enabled)
		EnableTemplates:     true,
		EnableRequestID:     true,
		EnableRecover:       true,
		EnableHelmet:        true,
		EnableCompress:      true,
		EnableSecFetchSite:  true,
		EnableRequestLogger: true,

		// Concurrency defaults optimized for SQLite WAL mode
		MaxConcurrentReads:  128,
		MaxConcurrentWrites: 8,
		ConcurrencyTimeout:  5 * time.Second,
	}
}

// RouteConfig allows per-route middleware customization.
type RouteConfig struct {
	// EnableCORS enables CORS for this route.
	EnableCORS bool
	CORSConfig *cors.Config

	// WriteConcurrency enables write concurrency limiting for this route.
	WriteConcurrency bool

	// EnableSecFetchSite controls CSRF protection. Default true (nil = enabled).
	// Set to Bool(false) for public/cross-origin routes.
	EnableSecFetchSite *bool

	// CustomMiddleware are additional middleware to run before the handler.
	CustomMiddleware []fiber.Handler
}

// Bool returns a pointer to a bool value. Useful for optional config fields.
func Bool(v bool) *bool { return &v }

// Server is the cartridge framework server with clean route registration API.
type Server struct {
	app      *fiber.App
	cfg      *ServerConfig
	limiter  *cartridgemiddleware.ConcurrencyLimiter
	catchAll string
}

// NewServer creates a new cartridge server with the provided configuration.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cartridge: config is required")
	}
	if cfg.Config == nil {
		return nil, fmt.Errorf("cartridge: runtime config is required")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("cartridge: logger is required")
	}
	if cfg.DBManager == nil {
		return nil, fmt.Errorf("cartridge: database manager is required")
	}

	// Build Fiber configuration
	fiberCfg := fiber.Config{
		DisableDefaultDate:    true,
		DisableStartupMessage: true,
		Concurrency:           cfg.Concurrency,
		ReadTimeout:           cfg.ReadTimeout,
		WriteTimeout:          cfg.WriteTimeout,
	}

	if cfg.ProxyHeader != "" {
		fiberCfg.ProxyHeader = cfg.ProxyHeader
	}
	if len(cfg.TrustedProxies) > 0 {
		fiberCfg.TrustedProxies = cfg.TrustedProxies
	}

	// Add custom views engine if provided
	if cfg.ViewsEngine != nil {
		fiberCfg.Views = cfg.ViewsEngine
	}

	// Add error handler
	if cfg.ErrorHandler != nil {
		fiberCfg.ErrorHandler = cfg.ErrorHandler
	} else {
		fiberCfg.ErrorHandler = createDefaultErrorHandler(cfg.Logger, cfg.Config)
	}

	app := fiber.New(fiberCfg)

	// Create concurrency limiter
	limiter := cartridgemiddleware.NewConcurrencyLimiter(
		int64(cfg.MaxConcurrentReads),
		int64(cfg.MaxConcurrentWrites),
		cfg.ConcurrencyTimeout,
		cfg.Logger,
	)

	server := &Server{
		app:     app,
		cfg:     cfg,
		limiter: limiter,
	}

	// Setup global middleware
	server.setupGlobalMiddleware()

	// Setup static assets
	server.setupStaticAssets()

	return server, nil
}

// setupGlobalMiddleware applies standard middleware to all routes.
func (s *Server) setupGlobalMiddleware() {
	if s.cfg.EnableRequestID {
		s.app.Use(requestid.New())
	}

	if s.cfg.EnableRecover {
		s.app.Use(cartridgemiddleware.Recover())
	}

	if s.cfg.EnableHelmet {
		s.app.Use(cartridgemiddleware.Helmet())
	}

	if s.cfg.EnableCompress {
		s.app.Use(compress.New(compress.Config{
			Level: compress.LevelDefault,
		}))
	}

	// SecFetchSite CSRF protection (can be disabled per-route)
	if s.cfg.EnableSecFetchSite {
		s.app.Use(cartridgemiddleware.SecFetchSiteMiddleware(cartridgemiddleware.SecFetchSiteConfig{
			Next: func(c *fiber.Ctx) bool {
				if skip, ok := c.Locals("skip_sec_fetch_site").(bool); ok && skip {
					return true
				}
				return false
			},
		}))
	}

	if s.cfg.EnableRequestLogger {
		s.app.Use(cartridgemiddleware.RequestLogger(s.cfg.Logger))
	}
}

// setupStaticAssets configures static file serving.
func (s *Server) setupStaticAssets() {
	if !s.cfg.EnableStaticAssets {
		return
	}

	prefix := s.cfg.StaticPrefix
	if prefix == "" {
		prefix = "/assets"
	}

	if s.cfg.StaticFS != nil {
		// Use embedded filesystem (production)
		s.app.Use(prefix, filesystem.New(filesystem.Config{
			Root:       http.FS(s.cfg.StaticFS),
			Browse:     false,
			MaxAge:     int((24 * time.Hour).Seconds()),
			PathPrefix: "",
		}))
	} else {
		// Use directory (development)
		dir := s.cfg.StaticDirectory
		if dir == "" {
			dir = s.cfg.Config.GetPublicDirectory()
		}
		if dir != "" {
			s.app.Static(prefix, dir, fiber.Static{
				Compress:      true,
				ByteRange:     true,
				Browse:        false,
				CacheDuration: 24 * time.Hour,
			})
		}
	}
}

// SetCatchAllRedirect configures a fallback redirect for unmatched routes.
func (s *Server) SetCatchAllRedirect(path string) {
	s.catchAll = path
}

// Get registers a GET route.
func (s *Server) Get(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodGet, path, handler, cfg...)
}

// Post registers a POST route.
func (s *Server) Post(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodPost, path, handler, cfg...)
}

// Put registers a PUT route.
func (s *Server) Put(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodPut, path, handler, cfg...)
}

// Delete registers a DELETE route.
func (s *Server) Delete(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodDelete, path, handler, cfg...)
}

// Patch registers a PATCH route.
func (s *Server) Patch(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodPatch, path, handler, cfg...)
}

// Options registers an OPTIONS route.
func (s *Server) Options(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodOptions, path, handler, cfg...)
}

// Head registers a HEAD route.
func (s *Server) Head(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(fiber.MethodHead, path, handler, cfg...)
}

// registerRoute is the core route registration method.
func (s *Server) registerRoute(method, path string, handler HandlerFunc, cfgs ...*RouteConfig) {
	var routeCfg *RouteConfig
	if len(cfgs) > 0 {
		routeCfg = cfgs[0]
	}

	// Calculate capacity for handlers slice
	capacity := 1 // At least the handler itself
	if routeCfg != nil {
		capacity += len(routeCfg.CustomMiddleware)
		if routeCfg.EnableCORS {
			capacity++
		}
		if routeCfg.WriteConcurrency {
			capacity++
		}
	}

	handlers := make([]fiber.Handler, 0, capacity)

	if routeCfg != nil {
		// Skip SecFetchSite if explicitly disabled
		if routeCfg.EnableSecFetchSite != nil && !*routeCfg.EnableSecFetchSite {
			handlers = append(handlers, func(c *fiber.Ctx) error {
				c.Locals("skip_sec_fetch_site", true)
				return c.Next()
			})
		}

		// Add CORS if enabled (must come first for preflight handling)
		if routeCfg.EnableCORS {
			corsCfg := routeCfg.CORSConfig
			if corsCfg == nil {
				corsCfg = &cors.Config{
					AllowOrigins: "*",
					AllowMethods: "GET,POST,PUT,DELETE,PATCH,OPTIONS",
					AllowHeaders: "Origin, Content-Type, Accept, Authorization",
				}
			}
			handlers = append(handlers, cors.New(*corsCfg))
		}

		// Add write concurrency limiting if enabled
		if routeCfg.WriteConcurrency {
			handlers = append(handlers, cartridgemiddleware.WriteConcurrencyLimitMiddleware(s.limiter))
		}

		// Add custom middleware
		if len(routeCfg.CustomMiddleware) > 0 {
			handlers = append(handlers, routeCfg.CustomMiddleware...)
		}
	}

	// Add the wrapped handler
	handlers = append(handlers, s.wrapHandler(handler))

	s.app.Add(method, path, handlers...)
}

// wrapHandler converts a cartridge HandlerFunc to a Fiber handler.
func (s *Server) wrapHandler(handler HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := &Context{
			Ctx:       c,
			Logger:    s.cfg.Logger,
			Config:    s.cfg.Config,
			DBManager: s.cfg.DBManager,
		}
		// Store context in locals for middleware access
		c.Locals("cartridge_ctx", ctx)
		return handler(ctx)
	}
}

// App returns the underlying Fiber application for advanced usage.
func (s *Server) App() *fiber.App {
	return s.app
}

// GetLimiter returns the concurrency limiter.
func (s *Server) GetLimiter() *cartridgemiddleware.ConcurrencyLimiter {
	return s.limiter
}

// GetLogger returns the logger.
func (s *Server) GetLogger() Logger {
	return s.cfg.Logger
}

// Start starts the HTTP server on the configured port.
func (s *Server) Start() error {
	// Add catch-all redirect if configured
	if s.catchAll != "" {
		s.app.All("*", func(c *fiber.Ctx) error {
			return c.Redirect(s.catchAll, fiber.StatusTemporaryRedirect)
		})
	}

	port := s.cfg.Config.GetPort()
	s.cfg.Logger.Info("Server started and ready to accept requests", "port", port)
	return s.app.Listen(":" + port)
}

// StartAsync starts the server in a goroutine.
func (s *Server) StartAsync() error {
	go func() {
		if err := s.Start(); err != nil {
			s.cfg.Logger.Error("Server error", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- s.app.Shutdown()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// createDefaultErrorHandler creates a default error handler.
func createDefaultErrorHandler(logger Logger, cfg Config) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		logger.Error("Request error",
			slog.Any("error", err),
			slog.Int("status", code),
			slog.String("path", c.Path()),
			slog.String("method", c.Method()),
		)

		// JSON error response for API requests
		if c.Accepts(fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
			return c.Status(code).JSON(fiber.Map{
				"error":   "internal_server_error",
				"message": err.Error(),
			})
		}

		// Fallback text response
		return c.Status(code).SendString(fmt.Sprintf("Error: %d - %s", code, err.Error()))
	}
}
