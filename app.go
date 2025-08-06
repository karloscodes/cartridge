package cartridge

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Environment constants
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"
)

// Default configuration values
const (
	DefaultPort        = "8080"
	DefaultConcurrency = 256
)

// RequestContext represents the request context using Fiber
type RequestContext = *fiber.Ctx


// Handler represents a route handler function
type Handler = fiber.Handler

// Route represents a registered route
type Route struct {
	Method  string
	Path    string
	Handler Handler
}

// CrudController defines the interface for CRUD operations
// Implement this interface to use with app.CrudRoute() or app.Resource()
type CrudController interface {
	// Index displays a listing of the resource (GET /resource)
	Index(*fiber.Ctx) error

	// Show displays the specified resource (GET /resource/:id)
	Show(*fiber.Ctx) error

	// New shows the form for creating a new resource (GET /resource/new)
	// Only used in full-stack applications with forms
	New(*fiber.Ctx) error

	// Create stores a newly created resource (POST /resource)
	Create(*fiber.Ctx) error

	// Edit shows the form for editing the specified resource (GET /resource/:id/edit)
	// Only used in full-stack applications with forms
	Edit(*fiber.Ctx) error

	// Update updates the specified resource (PUT /resource/:id)
	Update(*fiber.Ctx) error

	// Delete removes the specified resource (DELETE /resource/:id)
	Delete(*fiber.Ctx) error
}

// Controller is an alias for CrudController for backward compatibility
type Controller = CrudController

// AppAware is an optional interface that controllers can implement
// to receive the app instance for dependency injection
type AppAware interface {
	SetApp(*App)
}

// ControllerFactory creates controllers with app injection and middleware access
type ControllerFactory struct {
	app *App
}

// MiddlewareConfig provides access to all middleware configurations
type MiddlewareConfig struct {
	CSRF      CSRFConfig
	RateLimit RateLimiterConfig
	CORS      CORSConfig
	Security  SecurityHeaders
}

// NewController creates a controller factory for dependency injection
func (app *App) NewController() *ControllerFactory {
	return &ControllerFactory{app: app}
}

// Create creates a new controller instance with app injection
func (cf *ControllerFactory) Create(controllerFunc func(*App) CrudController) CrudController {
	return controllerFunc(cf.app)
}

// Handler creates individual handlers with app access
func (cf *ControllerFactory) Handler(handlerFunc func(*App) Handler) Handler {
	return handlerFunc(cf.app)
}

// Context provides common app services to reduce controller boilerplate
// This is different from Go's context.Context - this holds app-level services
// while Go context.Context is for request-scoped data and cancellation
type Context struct {
	DB         Database
	Logger     Logger
	Config     CartridgeConfig
	Auth       AuthConfig
	Middleware MiddlewareConfig
}

// WithGoContext creates a new request context with Go context support
// This bridges cartridge.Context (app services) with context.Context (request scope)
func (ctx *Context) WithGoContext(goCtx context.Context) *RequestData {
	return &RequestData{
		Context: ctx,    // App services
		ctx:     goCtx,  // Go context for request scope
	}
}

// RequestData combines app services with Go context for request handling
type RequestData struct {
	*Context              // App services (DB, Logger, Config, Auth)
	ctx      context.Context // Go context for request-scoped data and cancellation
}

// GoContext returns the underlying Go context for request-scoped operations
func (rd *RequestData) GoContext() context.Context {
	return rd.ctx
}

// WithValue adds a key-value pair to the request context
func (rd *RequestData) WithValue(key, value interface{}) *RequestData {
	return &RequestData{
		Context: rd.Context,
		ctx:     context.WithValue(rd.ctx, key, value),
	}
}

// WithTimeout adds a timeout to the request context
func (rd *RequestData) WithTimeout(timeout time.Duration) (*RequestData, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(rd.ctx, timeout)
	return &RequestData{
		Context: rd.Context,
		ctx:     ctx,
	}, cancel
}

// Value retrieves a value from the request context
func (rd *RequestData) Value(key interface{}) interface{} {
	return rd.ctx.Value(key)
}

// Ctx returns the shared app context singleton
func (app *App) Ctx() *Context {
	return app.ctx
}

// NewServices creates a Ctx struct from the app (deprecated - use Ctx())
func (app *App) NewServices() *Context {
	return app.ctx
}

// Controller creates a controller with pre-injected app context
func (cf *ControllerFactory) Controller(controllerFunc func(*Context) CrudController) CrudController {
	return controllerFunc(cf.app.ctx)
}

// App creates a controller with direct app access (even cleaner)
func (cf *ControllerFactory) App(controllerFunc func(*App) CrudController) CrudController {
	return controllerFunc(cf.app)
}

// BaseController provides a convenient base for controllers that need app access
// Embed this in your controllers to get access to app services
type BaseController struct {
	App *App
}

// SetApp implements the AppAware interface
func (bc *BaseController) SetApp(app *App) {
	bc.App = app
}

// Helper methods for common operations

// DB returns the database instance
func (bc *BaseController) DB() Database {
	if bc.App != nil {
		return bc.App.database
	}
	return nil
}

// Logger returns the logger instance
func (bc *BaseController) Logger() Logger {
	if bc.App != nil {
		return bc.App.logger
	}
	return nil
}

// Config returns the app configuration
func (bc *BaseController) Config() CartridgeConfig {
	if bc.App != nil {
		return bc.App.config
	}
	return CartridgeConfig{}
}

// Auth returns the auth configuration
func (bc *BaseController) Auth() AuthConfig {
	if bc.App != nil {
		return bc.App.authConfig
	}
	return AuthConfig{}
}

// App represents the Cartridge web application with a clean API
type App struct {
	config     CartridgeConfig
	logger     Logger
	database   Database
	authConfig AuthConfig
	fiberApp   *fiber.App
	appType    AppType
	routes     map[string]Route
	ctx        *Context // Shared app context singleton
}

// CartridgeConfig holds the internal configuration for Cartridge
type CartridgeConfig struct {
	Environment     string
	Port            string
	TrustedProxies  []string
	Concurrency     int
	ErrorHandler    interface{} // fiber.ErrorHandler
	StaticFS        embed.FS
	TemplateFS      embed.FS
	EnableCSRF      bool
	EnableCORS      bool
	EnableRateLimit bool
	CORSOrigins     []string
}

// AppOption is a functional option for configuring the Cartridge application
type AppOption func(*CartridgeConfig)

// AppType represents different application types
type AppType int

const (
	AppTypeGeneric AppType = iota
	AppTypeFullStack
	AppTypeAPIOnly
)

// New creates a new generic Cartridge app with sublime developer experience
// Returns a single App instance for the streamlined API
func New(options ...AppOption) *App {
	return newApp(AppTypeGeneric, options...)
}

// NewFullStack creates a new full-stack Cartridge application with templates, sessions, etc.
func NewFullStack(options ...AppOption) *App {
	return newApp(AppTypeFullStack, options...)
}

// NewAPIOnly creates a new lightweight API-only Cartridge application
func NewAPIOnly(options ...AppOption) *App {
	return newApp(AppTypeAPIOnly, options...)
}

// newApp is the internal constructor for all app types
func newApp(appType AppType, options ...AppOption) *App {
	// Start with default configuration based on app type
	cfg := defaultConfigForType(appType)

	// Apply functional options
	for _, option := range options {
		option(&cfg)
	}

	// Create dependencies with error handling
	deps, err := createDependencies()
	if err != nil {
		// In a real implementation, we'd handle this better
		// For now, log and continue with placeholders
		fmt.Printf("Warning: Failed to create dependencies: %v\n", err)
		deps = &dependencies{
			logger:     NewLogger(LogConfig{}),
			database:   NewDatabase(&Config{}, NewLogger(LogConfig{})),
			authConfig: AuthConfig{},
		}
	}

	// Create application
	app := &App{
		config:     cfg,
		logger:     deps.logger,
		database:   deps.database,
		authConfig: deps.authConfig,
		appType:    appType,
		routes:     make(map[string]Route),
	}

	// Initialize shared app context singleton
	app.ctx = &Context{
		DB:     app.database,
		Logger: app.logger,
		Config: app.config,
		Auth:   app.authConfig,
		Middleware: MiddlewareConfig{
			CSRF:      DefaultCSRFConfig(),
			RateLimit: DefaultRateLimiterConfig(),
			CORS:      DefaultCORSConfig(),
			Security:  DefaultSecurityHeaders(),
		},
	}

	// Setup the application (placeholder mode)
	app.setup()

	return app
}

// Functional options for configuring the application

// WithPort sets the port for the application
func WithPort(port string) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.Port = port
	}
}

// WithEnvironment sets the environment for the application
func WithEnvironment(env string) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.Environment = env
	}
}

// WithTrustedProxies sets trusted proxies for the application
func WithTrustedProxies(proxies []string) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.TrustedProxies = proxies
	}
}

// WithConcurrency sets the concurrency level for the application
func WithConcurrency(concurrency int) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.Concurrency = concurrency
	}
}

// WithErrorHandler sets a custom error handler
func WithErrorHandler(handler interface{}) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.ErrorHandler = handler
	}
}

// WithStaticFS sets the embedded filesystem for static assets
func WithStaticFS(fs embed.FS) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.StaticFS = fs
	}
}

// WithTemplateFS sets the embedded filesystem for templates
func WithTemplateFS(fs embed.FS) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.TemplateFS = fs
	}
}

// WithCSRF enables or disables CSRF protection
func WithCSRF(enabled bool) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.EnableCSRF = enabled
	}
}

// WithCORS enables or disables CORS
func WithCORS(enabled bool) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.EnableCORS = enabled
	}
}

// WithRateLimit enables or disables rate limiting
func WithRateLimit(enabled bool) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.EnableRateLimit = enabled
	}
}

// WithCORSOrigins sets allowed CORS origins
func WithCORSOrigins(origins []string) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.CORSOrigins = origins
	}
}

// defaultConfigForType returns a default CartridgeConfig based on app type
func defaultConfigForType(appType AppType) CartridgeConfig {
	cfg := NewConfig()

	// Use environment from config if set, otherwise default to development
	environment := cfg.Environment
	if environment == "" {
		environment = EnvDevelopment
	}

	// Use port from config if set, otherwise default to 8080
	port := cfg.Port
	if port == "" {
		port = DefaultPort
	}

	baseConfig := CartridgeConfig{
		Environment:    environment,
		Port:           port,
		TrustedProxies: []string{},
		Concurrency:    DefaultConcurrency,
		ErrorHandler:   nil,
	}

	// Configure based on app type
	switch appType {
	case AppTypeFullStack:
		baseConfig.EnableCSRF = true
		baseConfig.EnableCORS = false
		baseConfig.EnableRateLimit = false
	case AppTypeAPIOnly:
		baseConfig.EnableCSRF = false
		baseConfig.EnableCORS = true
		baseConfig.EnableRateLimit = true
	default: // AppTypeGeneric
		baseConfig.EnableCSRF = true
		baseConfig.EnableCORS = false
		baseConfig.EnableRateLimit = false
	}

	return baseConfig
}

// defaultConfig returns a default CartridgeConfig (legacy support)
func defaultConfig() CartridgeConfig {
	return defaultConfigForType(AppTypeGeneric)
}

// New creates a new Cartridge application with functional options
// This is the main factory function that returns a Cartridge Application
//
// Note: Fiber integration is currently in placeholder mode due to dependency resolution issues.
// The application structure is ready for Fiber integration once the dependency is properly available.

// dependencies holds all application dependencies
type dependencies struct {
	config     *Config
	logger     Logger
	database   Database
	authConfig AuthConfig
}

// createDependencies creates all application dependencies
func createDependencies() (*dependencies, error) {
	// Load configuration
	cfg := NewConfig()

	// Setup logging
	logger := NewLogger(LogConfig{
		Level:         LogLevel(cfg.LogLevel),
		Directory:     cfg.LogsDirectory,
		UseJSON:       cfg.IsProduction(),
		EnableConsole: true,
		AddSource:     cfg.IsDevelopment(),
	})

	// Setup database
	dbInstance := NewDatabase(cfg, logger)
	if err := dbInstance.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Setup authentication
	authConfig := DefaultAuthConfig(cfg.PrivateKey, cfg.IsProduction())

	return &dependencies{
		config:     cfg,
		logger:     logger,
		database:   dbInstance,
		authConfig: authConfig,
	}, nil
}

// setup configures the Fiber application with all middleware and settings
func (app *App) setup() error {
	app.logger.Info("Setting up Cartridge application",
		"environment", app.config.Environment,
		"port", app.config.Port)

	// Create and configure the Fiber app
	if err := app.createFiberApp(); err != nil {
		return fmt.Errorf("failed to create Fiber app: %w", err)
	}

	// Setup middleware
	app.setupMiddleware()

	// Setup static assets if provided
	if app.config.StaticFS != (embed.FS{}) {
		app.setupStaticAssets()
	}

	// Setup template engine if provided
	if app.config.TemplateFS != (embed.FS{}) {
		app.setupTemplateEngine()
	}

	// Setup default routes
	app.setupDefaultRoutes()

	app.logger.Info("Cartridge application setup completed")
	return nil
}

// setupDefaultRoutes sets up default endpoints like health checks
func (app *App) setupDefaultRoutes() {
	// Default health check endpoint
	app.Get("/_health", app.defaultHealthCheck)
	app.Get("/_ready", app.defaultReadinessCheck)
	app.Get("/_live", app.defaultLivenessCheck)
	
	app.logger.Info("Default routes configured", 
		"health", "/_health",
		"readiness", "/_ready", 
		"liveness", "/_live")
}

// defaultHealthCheck provides comprehensive health information
func (app *App) defaultHealthCheck(c RequestContext) error {
	startTime := time.Now()
	
	// Check database connectivity
	dbHealthy := true
	var dbError string
	if app.database != nil {
		if err := app.database.Ping(); err != nil {
			dbHealthy = false
			dbError = err.Error()
		}
	}

	// Collect system information
	health := map[string]interface{}{
		"status":      "healthy",
		"timestamp":   startTime.UTC().Format(time.RFC3339),
		"environment": app.config.Environment,
		"version":     "1.0.0", // Could be injected via build flags
		"uptime":      time.Since(startTime).String(),
		"checks": map[string]interface{}{
			"database": map[string]interface{}{
				"healthy": dbHealthy,
				"error":   dbError,
			},
		},
		"config": map[string]interface{}{
			"port":        app.config.Port,
			"environment": app.config.Environment,
			"csrf":        app.config.EnableCSRF,
			"cors":        app.config.EnableCORS,
			"rate_limit":  app.config.EnableRateLimit,
		},
	}

	// Set appropriate status code
	statusCode := 200
	if !dbHealthy {
		health["status"] = "unhealthy"
		statusCode = 503
	}

	app.logger.Info("Health check requested", 
		"status", health["status"],
		"db_healthy", dbHealthy)

	return c.Status(statusCode).JSON(health)
}

// defaultReadinessCheck indicates if the app is ready to receive traffic
func (app *App) defaultReadinessCheck(c RequestContext) error {
	// Check if all critical services are ready
	ready := true
	checks := make(map[string]interface{})

	// Database readiness
	if app.database != nil {
		if err := app.database.Ping(); err != nil {
			ready = false
			checks["database"] = map[string]interface{}{
				"ready": false,
				"error": err.Error(),
			}
		} else {
			checks["database"] = map[string]interface{}{
				"ready": true,
			}
		}
	}

	response := map[string]interface{}{
		"ready":     ready,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks":    checks,
	}

	statusCode := 200
	if !ready {
		statusCode = 503
	}

	app.logger.Debug("Readiness check requested", "ready", ready)

	return c.Status(statusCode).JSON(response)
}

// defaultLivenessCheck indicates if the app is alive (basic ping)
func (app *App) defaultLivenessCheck(c RequestContext) error {
	response := map[string]interface{}{
		"alive":     true,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"pid":       os.Getpid(),
	}

	app.logger.Debug("Liveness check requested")

	return c.JSON(response)
}

// HTTP Methods for the streamlined API

// Get registers a GET route
func (app *App) Get(path string, handler Handler) *App {
	app.fiberApp.Get(path, handler)
	app.logger.Debug("Registered GET route", "path", path)
	return app
}

// Post registers a POST route
func (app *App) Post(path string, handler Handler) *App {
	app.fiberApp.Post(path, handler)
	app.logger.Debug("Registered POST route", "path", path)
	return app
}

// Put registers a PUT route
func (app *App) Put(path string, handler Handler) *App {
	app.fiberApp.Put(path, handler)
	app.logger.Debug("Registered PUT route", "path", path)
	return app
}

// Delete registers a DELETE route
func (app *App) Delete(path string, handler Handler) *App {
	app.fiberApp.Delete(path, handler)
	app.logger.Debug("Registered DELETE route", "path", path)
	return app
}

// CrudRouteWithController registers all CRUD routes for a resource with a controller
// This creates routes like: GET /products, POST /products, GET /products/:id, etc.
// The controller must implement the CrudController interface
// If the controller also implements AppAware, it will receive the app instance
func (app *App) CrudRouteWithController(resource string, controller CrudController) *App {
	// Inject app instance if controller supports it
	if appAware, ok := controller.(AppAware); ok {
		appAware.SetApp(app)
	}

	basePath := "/" + resource

	// Index: GET /products
	app.Get(basePath, controller.Index)

	// New: GET /products/new (for forms - only in full-stack apps)
	if app.appType == AppTypeFullStack {
		app.Get(basePath+"/new", controller.New)
	}

	// Create: POST /products
	app.Post(basePath, controller.Create)

	// Show: GET /products/:id
	app.Get(basePath+"/:id", controller.Show)

	// Edit: GET /products/:id/edit (for forms - only in full-stack apps)
	if app.appType == AppTypeFullStack {
		app.Get(basePath+"/:id/edit", controller.Edit)
	}

	// Update: PUT /products/:id
	app.Put(basePath+"/:id", controller.Update)

	// Delete: DELETE /products/:id
	app.Delete(basePath+"/:id", controller.Delete)

	_, hasAppAccess := controller.(AppAware)
	fmt.Printf("✓ Registered CRUD routes for %s (with app injection: %t)\n", resource, hasAppAccess)
	return app
}

// Resource is a convenience method that works like CrudRouteWithController
// Use this for more Rails-like syntax: app.Resource("products", controller)
func (app *App) Resource(name string, controller CrudController) *App {
	return app.CrudRouteWithController(name, controller)
}

// CrudRoutes represents a fluent API for building CRUD routes
// This provides app context automatically and a cleaner syntax
type CrudRoutes struct {
	app      *App
	resource string
	BaseController
}

// CrudRoute creates a fluent CRUD route builder (opinionated approach)
// Usage: products := app.CrudRoute("products")
//
//	products.Index(handler).Show(handler).Create(handler)
func (app *App) CrudRoute(resource string) *CrudRoutes {
	cr := &CrudRoutes{
		app:      app,
		resource: resource,
		BaseController: BaseController{
			App: app, // Inject app immediately
		},
	}

	fmt.Printf("✓ Created CRUD route builder for %s (with app context)\n", resource)
	return cr
}

// Fluent API methods for CrudRoutes

// Index registers GET /resource
func (cr *CrudRoutes) Index(handler Handler) *CrudRoutes {
	cr.app.Get("/"+cr.resource, handler)
	return cr
}

// Show registers GET /resource/:id
func (cr *CrudRoutes) Show(handler Handler) *CrudRoutes {
	cr.app.Get("/"+cr.resource+"/:id", handler)
	return cr
}

// Create registers POST /resource
func (cr *CrudRoutes) Create(handler Handler) *CrudRoutes {
	cr.app.Post("/"+cr.resource, handler)
	return cr
}

// Update registers PUT /resource/:id
func (cr *CrudRoutes) Update(handler Handler) *CrudRoutes {
	cr.app.Put("/"+cr.resource+"/:id", handler)
	return cr
}

// Delete registers DELETE /resource/:id
func (cr *CrudRoutes) Delete(handler Handler) *CrudRoutes {
	cr.app.Delete("/"+cr.resource+"/:id", handler)
	return cr
}

// New registers GET /resource/new (for full-stack apps)
func (cr *CrudRoutes) New(handler Handler) *CrudRoutes {
	if cr.app.appType == AppTypeFullStack {
		cr.app.Get("/"+cr.resource+"/new", handler)
	}
	return cr
}

// Edit registers GET /resource/:id/edit (for full-stack apps)
func (cr *CrudRoutes) Edit(handler Handler) *CrudRoutes {
	if cr.app.appType == AppTypeFullStack {
		cr.app.Get("/"+cr.resource+"/:id/edit", handler)
	}
	return cr
}

// All registers all CRUD routes with a single controller (convenience method)
func (cr *CrudRoutes) All(controller CrudController) *CrudRoutes {
	// Inject app if controller supports it
	if appAware, ok := controller.(AppAware); ok {
		appAware.SetApp(cr.app)
	}

	return cr.Index(controller.Index).
		Show(controller.Show).
		Create(controller.Create).
		Update(controller.Update).
		Delete(controller.Delete).
		New(controller.New).
		Edit(controller.Edit)
}

// Listen starts the server with graceful shutdown
func (app *App) Listen(addr string) error {
	app.logger.Info("Starting Cartridge server", "address", addr)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		app.logger.Info("Server ready to handle requests")
		serverErrors <- app.fiberApp.Listen(addr)
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErrors:
		if err != nil {
			app.logger.Error("Server failed", "error", err)
			return err
		}
		
	case sig := <-sigChan:
		app.logger.Info("Shutdown signal received", "signal", sig.String())
	}

	// Graceful shutdown
	return app.Shutdown()
}

// Shutdown gracefully shuts down the application
func (app *App) Shutdown() error {
	app.logger.Info("Starting graceful shutdown")

	// Create timeout context for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown steps
	shutdownComplete := make(chan error, 1)
	go func() {
		shutdownComplete <- app.performShutdown(ctx)
	}()

	// Wait for shutdown to complete or timeout
	select {
	case err := <-shutdownComplete:
		if err != nil {
			app.logger.Error("Shutdown failed", "error", err)
			return err
		}
		app.logger.Info("Graceful shutdown completed")
		return nil
		
	case <-ctx.Done():
		app.logger.Error("Shutdown timed out")
		return ctx.Err()
	}
}

// performShutdown performs the actual shutdown steps
func (app *App) performShutdown(ctx context.Context) error {
	app.logger.Info("Performing shutdown steps")

	// 1. Stop accepting new requests
	if app.fiberApp != nil {
		app.logger.Info("Shutting down HTTP server")
		if err := app.fiberApp.Shutdown(); err != nil {
			app.logger.Error("Failed to shutdown server", "error", err)
			return err
		}
	}

	// 2. Close database connections
	if app.database != nil {
		app.logger.Info("Closing database connections")
		if err := app.database.Close(); err != nil {
			app.logger.Error("Failed to close database", "error", err)
			return err
		}
	}

	// 3. Additional cleanup tasks can be added here
	app.logger.Info("Shutdown steps completed")
	return nil
}

// Internal setup and utility methods

// createFiberApp creates and configures the Fiber application
func (app *App) createFiberApp() error {
	fiberApp := fiber.New(fiber.Config{
		AppName:      "Cartridge App",
		ErrorHandler: app.getErrorHandler(),
		Prefork:      app.config.Environment == EnvProduction,
	})
	app.fiberApp = fiberApp

	app.logger.Info("Fiber application created successfully")
	return nil
}

// setupMiddleware configures all middleware for the application
func (app *App) setupMiddleware() {
	app.logger.Info("Setting up middleware stack")

	fiberApp := app.fiberApp

	// Global middleware stack (in order):
	// 1. Request ID for tracing
	fiberApp.Use(RequestID())

	// 2. Recovery with stack traces
	fiberApp.Use(Recovery(app.logger))

	// 3. HTTP request logging
	fiberApp.Use(LoggerMiddleware(app.logger))

	// 4. Security headers
	fiberApp.Use(Helmet("strict-origin-when-cross-origin"))

	// 5. Database injection
	fiberApp.Use(DatabaseInjection(app.database))

	// 6. Method override for forms
	fiberApp.Use(MethodOverride())

	// Conditional middleware based on configuration
	if app.config.EnableCSRF {
		csrfConfig := DefaultCSRFConfig()
		csrfConfig.CookieSecure = app.config.Environment == "production"
		fiberApp.Use(CSRF(app.logger, csrfConfig))
		app.logger.Debug("CSRF protection enabled")
	}

	if app.config.EnableRateLimit {
		rateLimitConfig := DefaultRateLimiterConfig()
		if app.config.Environment == "production" {
			rateLimitConfig.Max = 60
			rateLimitConfig.Duration = 1 * time.Minute
		}
		fiberApp.Use(RateLimiter(rateLimitConfig))
		app.logger.Debug("Rate limiting enabled", "max", rateLimitConfig.Max)
	}

	if app.config.EnableCORS {
		var corsConfig CORSConfig
		if app.config.Environment == "production" {
			corsConfig = ProductionCORSConfig(app.config.CORSOrigins)
		} else {
			corsConfig = DefaultCORSConfig()
			// Override with custom origins if provided
			if len(app.config.CORSOrigins) > 0 {
				corsConfig.AllowOrigins = app.config.CORSOrigins
			}
		}
		fiberApp.Use(CORS(corsConfig))
		app.logger.Debug("CORS enabled", "origins", corsConfig.AllowOrigins, "credentials", corsConfig.AllowCredentials)
	}

	app.logger.Info("Middleware stack configured successfully")
}

// setupStaticAssets configures static asset serving
func (app *App) setupStaticAssets() {
	app.logger.Info("Setting up static assets")

	if app.config.Environment == "development" {
		// Development: filesystem-based serving with no caching
		app.logger.Debug("Using filesystem-based static assets")
	} else {
		// Production: embedded filesystem with caching
		app.logger.Debug("Using embedded static assets")
	}
}

// setupTemplateEngine configures the template engine
func (app *App) setupTemplateEngine() {
	app.logger.Info("Setting up template engine")

	if app.config.Environment == "development" {
		// Development: filesystem templates with reloading
		app.logger.Debug("Using filesystem templates with reloading")
	} else {
		// Production: embedded templates
		app.logger.Debug("Using embedded templates")
	}
}

// getErrorHandler creates an environment-specific error handler
func (app *App) getErrorHandler() fiber.ErrorHandler {
	if app.config.ErrorHandler != nil {
		if handler, ok := app.config.ErrorHandler.(fiber.ErrorHandler); ok {
			return handler
		}
	}

	return func(c *fiber.Ctx, err error) error {
		// Environment-specific error handling
		if app.config.Environment == "development" {
			// Development: detailed error messages with stack traces
			app.logger.Error("Request error (development)", "error", err.Error(), "path", c.Path())
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		} else {
			// Production: generic error messages, detailed logging
			app.logger.Error("Request error (production)", "error", err.Error(), "path", c.Path())
			return c.Status(500).JSON(fiber.Map{"error": "Internal server error"})
		}
	}
}

// GetFiberApp returns the underlying Fiber application
func (app *App) GetFiberApp() *fiber.App {
	return app.fiberApp
}

// GetDatabase returns the database instance for testing purposes
func (app *App) GetDatabase() Database {
	return app.database
}

// Logger returns the logger instance (for factory pattern)
func (app *App) Logger() Logger {
	return app.logger
}

// Config returns the app configuration (for factory pattern)
func (app *App) Config() CartridgeConfig {
	return app.config
}

// Auth returns the auth configuration (for factory pattern)
func (app *App) Auth() AuthConfig {
	return app.authConfig
}


// Start starts the application with graceful shutdown using the configured port
func (app *App) Start() error {
	app.logger.Info("Starting Cartridge application")
	
	// Use port from config
	addr := ":" + app.config.Port
	
	app.logger.Info("Server will start", "address", addr)
	app.logger.Info("Press Ctrl+C to shutdown gracefully")
	
	return app.Listen(addr)
}

// Run is an alias for Start (for backward compatibility)
func (app *App) Run() error {
	return app.Start()
}

// Stop gracefully stops the web application
func (app *App) Stop() error {
	app.logger.Info("Stopping web application")

	// Close database connections
	if err := app.database.Close(); err != nil {
		app.logger.Error("Failed to close database", "error", err)
	}

	app.logger.Info("Application stopped")
	return nil
}
