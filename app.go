package cartridge

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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

// ContextHandler represents a handler function that receives the app context
type ContextHandler func(*Context) error

// HandlerFunc wraps a ContextHandler to automatically inject the app context with Fiber embedded
func (app *App) HandlerFunc(handler ContextHandler) Handler {
	return func(c *fiber.Ctx) error {
		// Create a copy of the context with Fiber embedded
		ctx := app.createRequestContext(c)

		// Error boundary for HTTP handlers
		defer func() {
			if r := recover(); r != nil {
				ctx.Logger.Error("HTTP handler panicked",
					"method", c.Method(),
					"path", c.Path(),
					"panic", r)

				// Send 500 Internal Server Error
				c.Status(500).JSON(fiber.Map{
					"error":   "Internal server error",
					"message": "An unexpected error occurred",
				})
			}
		}()

		return handler(ctx)
	}
}

// createRequestContext creates a new Context instance with Fiber embedded for the request
func (app *App) createRequestContext(c *fiber.Ctx) *Context {
	return &Context{
		Database:   app.ctx.Database,
		Logger:     app.ctx.Logger,
		Config:     app.ctx.Config,
		Auth:       app.ctx.Auth,
		Middleware: app.ctx.Middleware,
		Fiber:      c,
	}
}

// Route represents a registered route
type Route struct {
	Method  string
	Path    string
	Handler Handler
}

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

// Handler creates individual handlers with app access
func (cf *ControllerFactory) Handler(handlerFunc func(*App) Handler) Handler {
	return handlerFunc(cf.app)
}

// Context provides common app services to reduce controller boilerplate
// This is different from Go's context.Context - this holds app-level services
// while Go context.Context is for request-scoped data and cancellation
type Context struct {
	Database   Database
	Logger     Logger
	Config     CartridgeConfig
	Auth       CookieAuthConfig
	Middleware MiddlewareConfig
	Fiber      RequestContext // Embedded Fiber context for request access
}

// DB returns the GORM database instance, abstracting the type assertion
func (ctx *Context) DB() *gorm.DB {
	if ctx.Database == nil {
		return nil
	}
	return ctx.Database.GetGenericConnection().(*gorm.DB)
}

// RenderData provides common data for template rendering including CSRF tokens
type RenderData struct {
	Data      interface{} `json:"data"`
	CSRFToken string      `json:"csrf_token,omitempty"`
	Meta      Meta        `json:"meta,omitempty"`
}

// Meta provides metadata for API responses
type Meta struct {
	Environment string `json:"environment,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

// Render creates a response with CSRF token and metadata automatically included (for HTML/forms)
func (ctx *Context) Render(data interface{}) error {
	renderData := RenderData{
		Data:      data,
		CSRFToken: ctx.getCSRFToken(),
		Meta: Meta{
			Environment: ctx.Config.Environment,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			RequestID:   ctx.Fiber.Get("X-Request-ID"),
		},
	}
	return ctx.Fiber.JSON(renderData)
}

// JSON creates a clean JSON response without CSRF tokens (for APIs)
func (ctx *Context) JSON(data interface{}) error {
	return ctx.Fiber.JSON(data)
}

// RenderJSON creates a JSON response with CSRF token for API endpoints (deprecated - use JSON for clean APIs)
func (ctx *Context) RenderJSON(data interface{}) error {
	renderData := RenderData{
		Data:      data,
		CSRFToken: ctx.getCSRFToken(),
	}
	return ctx.Fiber.JSON(renderData)
}

// Convenient methods for accessing Fiber functionality

// Params returns the route parameter
func (ctx *Context) Params(key string) string {
	if ctx.Fiber != nil {
		return ctx.Fiber.Params(key)
	}
	return ""
}

// Query returns the query string parameter
func (ctx *Context) Query(key string, defaultValue ...string) string {
	if ctx.Fiber != nil {
		return ctx.Fiber.Query(key, defaultValue...)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// QueryInt returns the query string parameter as int
func (ctx *Context) QueryInt(key string, defaultValue ...int) int {
	if ctx.Fiber != nil {
		return ctx.Fiber.QueryInt(key, defaultValue...)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

// QueryBool returns the query string parameter as bool
func (ctx *Context) QueryBool(key string, defaultValue ...bool) bool {
	if ctx.Fiber != nil {
		return ctx.Fiber.QueryBool(key, defaultValue...)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return false
}

// ParseJSON parses JSON request body into the provided struct
func (ctx *Context) ParseJSON(out interface{}) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.BodyParser(out)
	}
	return fmt.Errorf("fiber context not available")
}

// ParseForm parses form data into the provided struct
func (ctx *Context) ParseForm(out interface{}) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.BodyParser(out)
	}
	return fmt.Errorf("fiber context not available")
}

// FormValue returns form field value
func (ctx *Context) FormValue(key string, defaultValue ...string) string {
	if ctx.Fiber != nil {
		return ctx.Fiber.FormValue(key, defaultValue...)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// FormFile returns uploaded file from form
func (ctx *Context) FormFile(key string) (*multipart.FileHeader, error) {
	if ctx.Fiber != nil {
		return ctx.Fiber.FormFile(key)
	}
	return nil, fmt.Errorf("fiber context not available")
}

// BodyParser parses the request body (generic - deprecated, use ParseJSON/ParseForm)
func (ctx *Context) BodyParser(out interface{}) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.BodyParser(out)
	}
	return fmt.Errorf("fiber context not available")
}

// Status sets the response status code
func (ctx *Context) Status(status int) *Context {
	if ctx.Fiber != nil {
		ctx.Fiber.Status(status)
	}
	return ctx
}

// Error handling helpers - better than Must()!

// Fail returns a 500 Internal Server Error with the given message
func (ctx *Context) Fail(err error, message ...string) error {
	msg := "Internal server error"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Error("Handler failed", "error", err, "message", msg)
	return ctx.Status(500).JSON(fiber.Map{"error": msg})
}

// BadRequest returns a 400 Bad Request with the given message
func (ctx *Context) BadRequest(err error, message ...string) error {
	msg := "Bad request"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Bad request", "error", err, "message", msg)
	return ctx.Status(400).JSON(fiber.Map{"error": msg})
}

// NotFound returns a 404 Not Found with the given message
func (ctx *Context) NotFound(message ...string) error {
	msg := "Not found"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Info("Resource not found", "message", msg)
	return ctx.Status(404).JSON(fiber.Map{"error": msg})
}

// Unauthorized returns a 401 Unauthorized with the given message
func (ctx *Context) Unauthorized(message ...string) error {
	msg := "Unauthorized"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Unauthorized access", "message", msg)
	return ctx.Status(401).JSON(fiber.Map{"error": msg})
}

// Must handles regular errors - panics with Fail() if error occurs
// Usage: ctx.Must(someFunction()) - no error checking needed
func (ctx *Context) Must(err error) {
	if err != nil {
		panic(ctx.Fail(err))
	}
}

// Require validates that required fields exist in data - panics with BadRequest if missing
// Usage: ctx.Require(data, "name", "email", "price")
func (ctx *Context) Require(data map[string]interface{}, fields ...string) {
	missing := []string{}
	for _, field := range fields {
		if val, exists := data[field]; !exists || val == nil || val == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		panic(ctx.BadRequest(nil, fmt.Sprintf("Required fields missing: %s", strings.Join(missing, ", "))))
	}
}

// RequireFile gets an uploaded file or panics with BadRequest if missing
// Usage: file := ctx.RequireFile("avatar")
func (ctx *Context) RequireFile(key string) *multipart.FileHeader {
	file, err := ctx.FormFile(key)
	if err != nil {
		panic(ctx.BadRequest(err, fmt.Sprintf("Required file '%s' is missing", key)))
	}
	return file
}

// Validate runs a validator and panics with BadRequest if validation fails
// Usage: ctx.Validate(validator) - clean way to handle complex validation
func (ctx *Context) Validate(validator *Validator) {
	if !validator.IsValid() {
		panic(ctx.BadRequest(validator.Error(), validator.ErrorMessage()))
	}
}

// ValidateStruct validates a struct with validator tags and panics with BadRequest if invalid
// Usage: ctx.ValidateStruct(userStruct) - simple struct validation
func (ctx *Context) ValidateStruct(s interface{}) {
	if err := ValidateStruct(s); err != nil {
		panic(ctx.BadRequest(err, FormatValidationError(err)))
	}
}

// DBExec executes database commands (INSERT, UPDATE, DELETE) directly
// Usage: ctx.DBExec("INSERT INTO products (name) VALUES (?)", name)
func (ctx *Context) DBExec(query string, args ...interface{}) *gorm.DB {
	result := ctx.DB().Exec(query, args...)
	if result.Error != nil {
		panic(ctx.Fail(result.Error))
	}
	return result
}

// DBQuery executes database SELECT queries and scans into destination
// Usage: ctx.DBQuery("SELECT * FROM products WHERE id = ?", &product, id)
func (ctx *Context) DBQuery(query string, dest interface{}, args ...interface{}) *gorm.DB {
	result := ctx.DB().Raw(query, args...).Scan(dest)
	if result.Error != nil {
		panic(ctx.Fail(result.Error))
	}
	return result
}

// Exec handles database operations and returns the *gorm.DB result (legacy)
// Usage: result := ctx.Exec(db.Exec("query")) - use DBExec instead
func (ctx *Context) Exec(result *gorm.DB) *gorm.DB {
	if result.Error != nil {
		panic(ctx.Fail(result.Error))
	}
	return result
}

// Forbidden returns a 403 Forbidden with the given message
func (ctx *Context) Forbidden(message ...string) error {
	msg := "Forbidden"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Forbidden access", "message", msg)
	return ctx.Status(403).JSON(fiber.Map{"error": msg})
}

// Conflict returns a 409 Conflict with the given message
func (ctx *Context) Conflict(message ...string) error {
	msg := "Conflict"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Resource conflict", "message", msg)
	return ctx.Status(409).JSON(fiber.Map{"error": msg})
}

// UnprocessableEntity returns a 422 Unprocessable Entity with the given message
func (ctx *Context) UnprocessableEntity(message ...string) error {
	msg := "Unprocessable entity"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Unprocessable entity", "message", msg)
	return ctx.Status(422).JSON(fiber.Map{"error": msg})
}

// TooManyRequests returns a 429 Too Many Requests with the given message
func (ctx *Context) TooManyRequests(message ...string) error {
	msg := "Too many requests"
	if len(message) > 0 {
		msg = message[0]
	}

	ctx.Logger.Warn("Rate limit exceeded", "message", msg)
	return ctx.Status(429).JSON(fiber.Map{"error": msg})
}

// getCSRFToken extracts CSRF token from fiber context
func (ctx *Context) getCSRFToken() string {
	// Use the CSRF context key from middleware config
	contextKey := ctx.Middleware.CSRF.ContextKey
	if contextKey == "" {
		contextKey = "csrf" // fallback to default
	}

	if ctx.Fiber != nil {
		if token := ctx.Fiber.Locals(contextKey); token != nil {
			if tokenStr, ok := token.(string); ok {
				return tokenStr
			}
		}
	}
	return ""
}

// WithGoContext creates a new request context with Go context support
// This bridges cartridge.Context (app services) with context.Context (request scope)
func (ctx *Context) WithGoContext(goCtx context.Context) *RequestData {
	return &RequestData{
		Context: ctx,   // App services
		ctx:     goCtx, // Go context for request scope
	}
}

// RequestData combines app services with Go context for request handling
type RequestData struct {
	*Context                 // App services (DB, Logger, Config, Auth)
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

// Context creates a handler with pre-injected app context
func (cf *ControllerFactory) Context(handlerFunc func(*Context) Handler) Handler {
	return handlerFunc(cf.app.ctx)
}

// App creates a handler with direct app access
func (cf *ControllerFactory) App(handlerFunc func(*App) Handler) Handler {
	return handlerFunc(cf.app)
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

// GORM returns the GORM database instance, abstracting the boilerplate
func (bc *BaseController) GORM() *gorm.DB {
	if bc.App != nil && bc.App.database != nil {
		return bc.App.database.GetGenericConnection().(*gorm.DB)
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
func (bc *BaseController) Auth() CookieAuthConfig {
	if bc.App != nil {
		return bc.App.authConfig
	}
	return CookieAuthConfig{}
}

// Note: BaseController render methods are deprecated - use functional handlers with Context instead

// App represents the Cartridge web application with a clean API
type App struct {
	config     CartridgeConfig
	logger     Logger
	database   Database
	authConfig CookieAuthConfig
	fiberApp   *fiber.App
	appType    AppType
	routes     map[string]Route
	ctx        *Context // Shared app context singleton
	migrations *MigrationManager
	cron       *CronManager
	async      *AsyncManager
	assets     *AssetManager      // Asset manager for static files and templates
	templates  *template.Template // Template engine for HTML rendering
}

// CartridgeConfig holds the internal configuration for Cartridge
type CartridgeConfig struct {
	Environment       string
	Port              string
	TrustedProxies    []string
	Concurrency       int
	ErrorHandler      interface{} // fiber.ErrorHandler
	StaticFS          embed.FS
	TemplateFS        embed.FS
	MigrationFS       embed.FS // Embedded filesystem for migrations
	EnableCSRF        bool
	EnableCORS        bool
	EnableRateLimit   bool
	CORSOrigins       []string
	CSRFExcludedPaths []string // Global CSRF excluded paths
}

// RunConfig holds configuration for the Run method
type RunConfig struct {
	MigrationFS  *embed.FS
	MigrationDir string
}

// RunOption is a functional option for configuring the Run method
type RunOption func(*RunConfig)

// WithMigrations configures embedded migrations to be loaded and run
func WithMigrations(fs embed.FS, dir string) RunOption {
	return func(cfg *RunConfig) {
		cfg.MigrationFS = &fs
		cfg.MigrationDir = dir
	}
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
			logger:     NewLogger(LogConfig{Environment: "development", EnableConsole: true, EnableColors: true}),
			database:   NewDatabase(&Config{}, NewLogger(LogConfig{Environment: "development", EnableConsole: true, EnableColors: true})),
			authConfig: CookieAuthConfig{},
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
		Database: app.database,
		Logger:   app.logger,
		Config:   app.config,
		Auth:     app.authConfig,
		Middleware: MiddlewareConfig{
			CSRF:      DefaultCSRFConfig(),
			RateLimit: DefaultRateLimiterConfig(),
			CORS:      DefaultCORSConfig(),
			Security:  DefaultSecurityHeaders(),
		},
	}

	// Initialize migration manager
	if db := app.database.GetGenericConnection(); db != nil {
		if gormDB, ok := db.(*gorm.DB); ok {
			app.migrations = NewMigrationManager(gormDB, app.logger)
			app.cron = NewCronManager(app.database, app.logger)
			app.async = NewAsyncManager(app.database, app.logger)
		}
	}

	// Initialize asset manager
	config := &Config{Environment: app.config.Environment}
	assetConfig := DefaultAssetConfig(config)
	app.assets = NewAssetManager(assetConfig, app.logger)

	// Set embedded filesystems if provided
	if app.config.StaticFS != (embed.FS{}) || app.config.TemplateFS != (embed.FS{}) {
		app.assets.SetEmbeddedFS(app.config.StaticFS, app.config.TemplateFS)
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

// WithCSRFExcludedPaths sets paths to exclude from CSRF protection
// Example: WithCSRFExcludedPaths([]string{"/api/", "/webhooks/", "/health/"})
// These paths will bypass CSRF token validation
func WithCSRFExcludedPaths(paths []string) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.CSRFExcludedPaths = paths
	}
}

// WithMigrationFS sets the embedded filesystem for migrations
func WithMigrationFS(fs embed.FS) AppOption {
	return func(cfg *CartridgeConfig) {
		cfg.MigrationFS = fs
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
		Environment:       environment,
		Port:              port,
		TrustedProxies:    []string{},
		Concurrency:       DefaultConcurrency,
		ErrorHandler:      nil,
		CSRFExcludedPaths: []string{"/api/", "/static/", "/_health", "/_ready", "/_live"}, // Default excluded paths
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
	authConfig CookieAuthConfig
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
		EnableConsole: cfg.IsDevelopment(), // Console only in development
		EnableColors:  cfg.IsDevelopment(), // Enable colors in development
		AddSource:     cfg.IsDevelopment(),
		Environment:   cfg.Environment, // Pass environment for smart routing
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

	// Setup template engine first (before creating Fiber app)
	if app.config.TemplateFS != (embed.FS{}) || app.config.Environment == EnvDevelopment {
		app.setupTemplateEngine()
	}

	// Setup migrations (before creating Fiber app)
	if app.config.MigrationFS != (embed.FS{}) || app.config.Environment == EnvDevelopment {
		app.setupMigrations()
	}

	// Create and configure the Fiber app (with templates if available)
	if err := app.createFiberApp(); err != nil {
		return fmt.Errorf("failed to create Fiber app: %w", err)
	}

	// Setup middleware
	app.setupMiddleware()

	// Setup static assets if provided
	if app.config.StaticFS != (embed.FS{}) {
		app.setupStaticAssets()
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

// Get registers a GET route - supports both Handler and ContextHandler
func (app *App) Get(path string, handler interface{}) *App {
	switch h := handler.(type) {
	case ContextHandler:
		app.fiberApp.Get(path, app.HandlerFunc(h))
	case func(*Context) error: // Support direct function syntax
		app.fiberApp.Get(path, app.HandlerFunc(h))
	case Handler:
		app.fiberApp.Get(path, h)
	default:
		panic("handler must be either Handler or ContextHandler")
	}
	app.logger.Debug("Registered GET route", "path", path)
	return app
}

// Post registers a POST route - supports both Handler and ContextHandler
func (app *App) Post(path string, handler interface{}) *App {
	switch h := handler.(type) {
	case ContextHandler:
		app.fiberApp.Post(path, app.HandlerFunc(h))
	case func(*Context) error: // Support direct function syntax
		app.fiberApp.Post(path, app.HandlerFunc(h))
	case Handler:
		app.fiberApp.Post(path, h)
	default:
		panic("handler must be either Handler or ContextHandler")
	}
	app.logger.Debug("Registered POST route", "path", path)
	return app
}

// Put registers a PUT route - supports both Handler and ContextHandler
func (app *App) Put(path string, handler interface{}) *App {
	switch h := handler.(type) {
	case ContextHandler:
		app.fiberApp.Put(path, app.HandlerFunc(h))
	case func(*Context) error: // Support direct function syntax
		app.fiberApp.Put(path, app.HandlerFunc(h))
	case Handler:
		app.fiberApp.Put(path, h)
	default:
		panic("handler must be either Handler or ContextHandler")
	}
	app.logger.Debug("Registered PUT route", "path", path)
	return app
}

// Delete registers a DELETE route - supports both Handler and ContextHandler
func (app *App) Delete(path string, handler interface{}) *App {
	switch h := handler.(type) {
	case ContextHandler:
		app.fiberApp.Delete(path, app.HandlerFunc(h))
	case func(*Context) error: // Support direct function syntax
		app.fiberApp.Delete(path, app.HandlerFunc(h))
	case Handler:
		app.fiberApp.Delete(path, h)
	default:
		panic("handler must be either Handler or ContextHandler")
	}
	app.logger.Debug("Registered DELETE route", "path", path)
	return app
}

// RouteGroup provides a fluent way to register multiple routes with a common base path
type RouteGroup struct {
	app      *App
	basePath string
}

// Group creates a route group with a base path
func (app *App) Group(basePath string) *RouteGroup {
	return &RouteGroup{
		app:      app,
		basePath: basePath,
	}
}

// Routes registers multiple routes for a controller in one call
func (rg *RouteGroup) Routes(controller interface{}) *RouteGroup {
	// Use reflection to find methods and register them automatically
	// This would need to be implemented based on naming conventions
	// For now, provide a manual method
	return rg
}

// GET registers a GET route within the group - supports both Handler and ContextHandler
func (rg *RouteGroup) GET(path string, handler interface{}) *RouteGroup {
	fullPath := rg.basePath + path
	rg.app.Get(fullPath, handler)
	return rg
}

// POST registers a POST route within the group - supports both Handler and ContextHandler
func (rg *RouteGroup) POST(path string, handler interface{}) *RouteGroup {
	fullPath := rg.basePath + path
	rg.app.Post(fullPath, handler)
	return rg
}

// PUT registers a PUT route within the group - supports both Handler and ContextHandler
func (rg *RouteGroup) PUT(path string, handler interface{}) *RouteGroup {
	fullPath := rg.basePath + path
	rg.app.Put(fullPath, handler)
	return rg
}

// DELETE registers a DELETE route within the group - supports both Handler and ContextHandler
func (rg *RouteGroup) DELETE(path string, handler interface{}) *RouteGroup {
	fullPath := rg.basePath + path
	rg.app.Delete(fullPath, handler)
	return rg
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
	// Create Fiber config
	config := fiber.Config{
		AppName:      "Cartridge App",
		ErrorHandler: app.getErrorHandler(),
		Prefork:      app.config.Environment == EnvProduction,
	}

	// Add custom template renderer if templates are available
	if app.templates != nil {
		config.Views = &TemplateRenderer{
			templates: app.templates,
			logger:    app.logger,
		}
	}

	fiberApp := fiber.New(config)
	app.fiberApp = fiberApp

	app.logger.Info("Fiber application created successfully")
	return nil
}

// TemplateRenderer implements Fiber's Views interface for custom template rendering
type TemplateRenderer struct {
	templates *template.Template
	logger    Logger
}

// Load is required by Fiber's Views interface (no-op for our implementation)
func (tr *TemplateRenderer) Load() error {
	return nil
}

// Render renders a template with the given data
func (tr *TemplateRenderer) Render(out io.Writer, name string, binding interface{}, layout ...string) error {
	// Use the layout if provided (Fiber convention)
	templateName := name
	if len(layout) > 0 && layout[0] != "" {
		templateName = layout[0]
	}

	// Execute the template
	tmpl := tr.templates.Lookup(templateName)
	if tmpl == nil {
		tr.logger.Error("Template not found", "name", templateName)
		return fmt.Errorf("template not found: %s", templateName)
	}

	if err := tmpl.Execute(out, binding); err != nil {
		tr.logger.Error("Failed to execute template", "name", templateName, "error", err)
		return fmt.Errorf("failed to execute template %s: %w", templateName, err)
	}

	tr.logger.Debug("Template rendered successfully", "name", templateName)
	return nil
}

// setupMiddleware configures all middleware for the application
func (app *App) setupMiddleware() {
	app.logger.Info("Setting up middleware stack")

	fiberApp := app.fiberApp

	// Get middleware configuration for this app type and environment
	middlewareConfig := NewAppTypeMiddlewareConfig(int(app.appType), app.config.Environment, app.config.CSRFExcludedPaths)

	// Global middleware stack (in order):
	// 1. Request ID for tracing
	fiberApp.Use(RequestID())

	// 2. Recovery with stack traces
	fiberApp.Use(Recovery(middlewareConfig.RecoveryConfig))

	// 3. HTTP request logging
	fiberApp.Use(LoggerMiddleware(middlewareConfig.LoggerConfig))

	// 4. Security headers
	fiberApp.Use(Helmet(middlewareConfig.HelmetConfig))

	// 5. Method override for forms
	fiberApp.Use(MethodOverride())

	// Conditional middleware based on app type configuration
	if middlewareConfig.EnableCSRF {
		fiberApp.Use(CSRF(app.logger, middlewareConfig.CSRFConfig))
		fiberApp.Use(CSRFTokenInjector(middlewareConfig.CSRFConfig))
		app.logger.Debug("CSRF protection enabled with token injection")
	}

	if middlewareConfig.EnableRateLimit {
		fiberApp.Use(RateLimiter(middlewareConfig.RateLimitConfig))
		app.logger.Debug("Rate limiting enabled", "max", middlewareConfig.RateLimitConfig.Max)
	}

	if middlewareConfig.EnableCORS {
		// Allow custom origins to override defaults
		corsConfig := middlewareConfig.CORSConfig
		if len(app.config.CORSOrigins) > 0 {
			corsConfig.AllowOrigins = app.config.CORSOrigins
		}
		fiberApp.Use(CORS(corsConfig))
		app.logger.Debug("CORS enabled", "origins", corsConfig.AllowOrigins, "credentials", corsConfig.AllowCredentials)
	}

	app.logger.Info("Middleware stack configured successfully")
}

// setupStaticAssets configures static asset serving
func (app *App) setupStaticAssets() {
	app.logger.Info("Setting up static assets")

	if app.assets == nil {
		app.logger.Warn("Asset manager not initialized")
		return
	}

	// Configure asset serving based on environment
	if app.config.Environment == EnvDevelopment {
		// Development: filesystem-based serving with no caching
		app.logger.Debug("Using filesystem-based static assets")
		app.assets.SetupStaticRoutes(app.fiberApp)
	} else {
		// Production/Test: embedded filesystem with caching
		app.logger.Debug("Using embedded static assets")
		app.assets.SetupStaticRoutes(app.fiberApp)
	}
}

// setupTemplateEngine configures the template engine
func (app *App) setupTemplateEngine() {
	app.logger.Info("Setting up template engine")

	// Initialize the template engine with custom functions
	funcMap := app.getTemplateFunctions()
	tmpl := template.New("").Funcs(funcMap)

	if app.config.Environment == EnvDevelopment {
		// Development: filesystem templates with reloading
		app.logger.Debug("Using filesystem templates with reloading")

		// Load templates from filesystem
		if err := app.loadTemplatesFromFilesystem(tmpl); err != nil {
			app.logger.Error("Failed to load templates from filesystem", "error", err)
			return
		}
	} else {
		// Production/Test: embedded templates
		app.logger.Debug("Using embedded templates")

		// Load templates from embedded filesystem
		if err := app.loadTemplatesFromEmbedded(tmpl); err != nil {
			app.logger.Error("Failed to load embedded templates", "error", err)
			return
		}
	}

	app.templates = tmpl
	app.logger.Info("Template engine configured successfully")
}

// loadTemplatesFromFilesystem loads templates from the ./templates directory
func (app *App) loadTemplatesFromFilesystem(tmpl *template.Template) error {
	templatesDir := "./templates"

	// Check if templates directory exists
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		app.logger.Warn("Templates directory does not exist", "path", templatesDir)
		return nil // Not an error, just no templates to load
	}

	// Walk through templates directory and load all .html files
	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-HTML files
		if info.IsDir() || (!strings.HasSuffix(strings.ToLower(path), ".html") && !strings.HasSuffix(strings.ToLower(path), ".gohtml")) {
			return nil
		}

		// Get relative path for template name
		relPath, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}

		// Normalize path separators for cross-platform compatibility
		templateName := filepath.ToSlash(relPath)
		
		// Strip file extension for template name
		if strings.HasSuffix(strings.ToLower(templateName), ".html") {
			templateName = templateName[:len(templateName)-5]
		} else if strings.HasSuffix(strings.ToLower(templateName), ".gohtml") {
			templateName = templateName[:len(templateName)-7]
		}

		app.logger.Debug("Loading template", "name", templateName, "path", path)

		// Read template content
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Parse template
		_, err = tmpl.New(templateName).Parse(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templateName, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to load templates from filesystem: %w", err)
	}

	app.logger.Info("Templates loaded from filesystem", "directory", templatesDir)
	return nil
}

// loadTemplatesFromEmbedded loads templates from embedded filesystem
func (app *App) loadTemplatesFromEmbedded(tmpl *template.Template) error {
	if app.config.TemplateFS == (embed.FS{}) {
		app.logger.Debug("No embedded template filesystem provided")
		return nil
	}

	templateFS := app.config.TemplateFS
	templatesDir := "templates"

	// Walk through embedded templates directory
	err := fs.WalkDir(templateFS, templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-HTML files
		if d.IsDir() || (!strings.HasSuffix(strings.ToLower(path), ".html") && !strings.HasSuffix(strings.ToLower(path), ".gohtml")) {
			return nil
		}

		// Get relative path for template name
		relPath, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}

		// Normalize path separators for cross-platform compatibility
		templateName := filepath.ToSlash(relPath)
		
		// Strip file extension for template name
		if strings.HasSuffix(strings.ToLower(templateName), ".html") {
			templateName = templateName[:len(templateName)-5]
		} else if strings.HasSuffix(strings.ToLower(templateName), ".gohtml") {
			templateName = templateName[:len(templateName)-7]
		}

		app.logger.Debug("Loading embedded template", "name", templateName, "path", path)

		// Read template content from embedded filesystem
		content, err := templateFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded template %s: %w", path, err)
		}

		// Parse template
		_, err = tmpl.New(templateName).Parse(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse embedded template %s: %w", templateName, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to load embedded templates: %w", err)
	}

	app.logger.Info("Templates loaded from embedded filesystem")
	return nil
}

// getTemplateFunctions returns custom template functions
func (app *App) getTemplateFunctions() template.FuncMap {
	return template.FuncMap{
		// Asset helpers
		"asset": func(path string) string {
			if app.assets != nil {
				return app.assets.GetAssetPath(path)
			}
			return "/static/" + path
		},
		"css": func(filename string) string {
			if app.assets != nil {
				return app.assets.CSS(filename)
			}
			return "/static/css/" + filename
		},
		"js": func(filename string) string {
			if app.assets != nil {
				return app.assets.JS(filename)
			}
			return "/static/js/" + filename
		},
		"image": func(filename string) string {
			if app.assets != nil {
				return app.assets.Image(filename)
			}
			return "/static/images/" + filename
		},
		"icon": func(filename string) string {
			if app.assets != nil {
				return app.assets.Icon(filename)
			}
			return "/static/icons/" + filename
		},

		// String utilities
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": strings.Title,
		"trim":  strings.TrimSpace,

		// Environment checks
		"isDev": func() bool {
			return app.config.Environment == EnvDevelopment
		},
		"isProd": func() bool {
			return app.config.Environment == EnvProduction
		},
		"isTest": func() bool {
			return app.config.Environment == EnvTest
		},

		// CSRF token
		"csrfToken": func() string {
			// This would typically be set per-request, but we provide a fallback
			return ""
		},

		// Configuration access
		"config": func(key string) interface{} {
			switch key {
			case "environment":
				return app.config.Environment
			case "port":
				return app.config.Port
			default:
				return nil
			}
		},
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
		if app.config.Environment == EnvDevelopment {
			// Development: detailed error messages with stack traces
			app.logger.Error("Request error (development)", "error", err.Error(), "path", c.Path())
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		} else {
			// Production/Test: generic error messages, detailed logging
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
func (app *App) Auth() CookieAuthConfig {
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

// Run starts the application with optional configurations
// This is the new unified method that handles migrations, cron jobs, and startup
// Panics on any failure for a sublime developer experience
func (app *App) Run(options ...RunOption) {
	// Apply run-time options
	runConfig := &RunConfig{}
	for _, option := range options {
		option(runConfig)
	}

	// Handle migrations if provided
	if runConfig.MigrationFS != nil {
		app.logger.Info("Loading database migrations from embedded files")
		if err := app.LoadMigrationsFromFS(*runConfig.MigrationFS, runConfig.MigrationDir); err != nil {
			app.logger.Error("Failed to load migrations", "error", err)
			panic(fmt.Errorf("failed to load migrations: %w", err))
		}

		app.logger.Info("Running database migrations")
		if err := app.Migrate(); err != nil {
			app.logger.Error("Failed to run migrations", "error", err)
			panic(fmt.Errorf("failed to run migrations: %w", err))
		}
		app.logger.Info("Database migrations completed")
	}

	// Start cron jobs if any are registered
	if app.cron != nil && app.cron.HasJobs() {
		app.logger.Info("Starting cron jobs")
		app.StartCronJobs()
		app.logger.Info("Cron jobs started")
	}

	// Start the server - panic on failure for clean developer experience
	if err := app.Start(); err != nil {
		app.logger.Error("Failed to start server", "error", err)
		panic(fmt.Errorf("failed to start server: %w", err))
	}
}

// Stop gracefully stops the web application
func (cartridge *App) Stop() error {
	cartridge.logger.Info("Stopping web application")

	// Stop cron jobs
	if cartridge.cron != nil {
		cartridge.logger.Info("Stopping cron scheduler")
		cartridge.cron.Stop()
	}

	// Close migration manager
	if cartridge.migrations != nil {
		if err := cartridge.migrations.Close(); err != nil {
			cartridge.logger.Error("Failed to close migration manager", "error", err)
		}
	}

	// Close database connections
	if err := cartridge.database.Close(); err != nil {
		cartridge.logger.Error("Failed to close database", "error", err)
	}

	cartridge.logger.Info("Application stopped")
	return nil
}

// Migration Management

// LoadMigrationsFromFS loads migrations from embedded filesystem
func (cartridge *App) LoadMigrationsFromFS(fsys embed.FS, dir string) error {
	if cartridge.migrations == nil {
		return fmt.Errorf("migration manager not initialized")
	}
	return cartridge.migrations.LoadFromFS(fsys, dir)
}

// AddBinaryMigration adds a programmatic migration to be executed in-memory
func (cartridge *App) AddBinaryMigration(version uint, description string, up, down func(*gorm.DB) error) {
	if cartridge.migrations == nil {
		cartridge.logger.Warn("Migration manager not initialized")
		return
	}
	cartridge.migrations.AddBinaryMigration(version, description, up, down)
}

// CronJob registers a new cron job with the scheduler (description is optional)
// Schedule format supports seconds: "0 30 * * * *" (every 30 seconds), "0 0 12 * * MON-FRI" (weekdays at noon)
// Panics on failure for sublime developer experience
func (cartridge *App) CronJob(id, schedule string, handler CronHandler, description ...string) {
	if cartridge.cron == nil {
		panic(fmt.Errorf("cron manager not initialized"))
	}

	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}

	if err := cartridge.cron.AddJob(id, schedule, desc, handler); err != nil {
		panic(fmt.Errorf("failed to register cron job '%s': %w", id, err))
	}
}

// AddCronJob registers a new cron job with the scheduler (legacy method)
// Schedule format supports seconds: "0 30 * * * *" (every 30 seconds), "0 0 12 * * MON-FRI" (weekdays at noon)
func (cartridge *App) AddCronJob(id, schedule, description string, handler CronHandler) error {
	if cartridge.cron == nil {
		return fmt.Errorf("cron manager not initialized")
	}
	return cartridge.cron.AddJob(id, schedule, description, handler)
}

// RemoveCronJob removes a cron job from the scheduler
func (cartridge *App) RemoveCronJob(id string) error {
	if cartridge.cron == nil {
		return fmt.Errorf("cron manager not initialized")
	}
	return cartridge.cron.RemoveJob(id)
}

// StartCronJobs starts the cron scheduler
func (cartridge *App) StartCronJobs() {
	if cartridge.cron == nil {
		cartridge.logger.Warn("Cron manager not initialized")
		return
	}
	cartridge.cron.Start()
}

// StopCronJobs gracefully stops the cron scheduler
func (cartridge *App) StopCronJobs() {
	if cartridge.cron == nil {
		return
	}
	cartridge.cron.Stop()
}

// CronStatus returns the status of all cron jobs
func (cartridge *App) CronStatus() map[string]interface{} {
	if cartridge.cron == nil {
		return map[string]interface{}{
			"error": "cron manager not initialized",
		}
	}
	return cartridge.cron.Status()
}

// Async Processing Methods

// AsyncJob runs a background task and returns the task ID and error for error checking
func (cartridge *App) AsyncJob(id string, handler AsyncHandler, args map[string]interface{}) (string, error) {
	if cartridge.async == nil {
		return "", fmt.Errorf("async manager not initialized")
	}
	taskID := cartridge.async.Run(id, handler, args)
	return taskID, nil
}

// AsyncStatus returns the status of a specific async task
func (cartridge *App) AsyncStatus(id string) (*TaskInfo, error) {
	if cartridge.async == nil {
		return nil, fmt.Errorf("async manager not initialized")
	}
	return cartridge.async.Status(id)
}

// AsyncCancel cancels a running async task
func (cartridge *App) AsyncCancel(id string) error {
	if cartridge.async == nil {
		return fmt.Errorf("async manager not initialized")
	}
	return cartridge.async.Cancel(id)
}

// AsyncList returns all async tasks with their current status
func (cartridge *App) AsyncList() map[string]interface{} {
	if cartridge.async == nil {
		return map[string]interface{}{
			"error": "async manager not initialized",
		}
	}
	return cartridge.async.List()
}

// AsyncCleanup removes completed or failed tasks older than the specified duration
func (cartridge *App) AsyncCleanup(olderThan time.Duration) int {
	if cartridge.async == nil {
		return 0
	}
	return cartridge.async.Cleanup(olderThan)
}

// AddMigration manually adds a migration (deprecated - use AddBinaryMigration or SQL files)
func (cartridge *App) AddMigration(version int, name, upSQL, downSQL string) {
	cartridge.logger.Warn("AddMigration is deprecated - use AddBinaryMigration or SQL files instead")
}

// Migrate runs all pending database migrations
func (cartridge *App) Migrate() error {
	if cartridge.migrations == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	// Initialize database first
	if err := cartridge.database.Init(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	return cartridge.migrations.Up()
}

// RollbackMigration rolls back the latest migration
func (cartridge *App) RollbackMigration() error {
	if cartridge.migrations == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	return cartridge.migrations.Down()
}

// MigrationStatus shows the status of all migrations
func (cartridge *App) MigrationStatus() error {
	if cartridge.migrations == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	return cartridge.migrations.Status()
}

// ForceMigrationVersion forces migration version (for debugging dirty states)
func (cartridge *App) ForceMigrationVersion(version int) error {
	if cartridge.migrations == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	return cartridge.migrations.Force(version)
}

// Template Management Methods

// GetTemplate returns a specific template by name
func (app *App) GetTemplate(name string) *template.Template {
	if app.templates == nil {
		return nil
	}
	return app.templates.Lookup(name)
}

// HasTemplate checks if a template exists
func (app *App) HasTemplate(name string) bool {
	return app.GetTemplate(name) != nil
}

// ListTemplates returns a list of all loaded template names
func (app *App) ListTemplates() []string {
	if app.templates == nil {
		return []string{}
	}

	var names []string
	for _, tmpl := range app.templates.Templates() {
		if tmpl.Name() != "" {
			names = append(names, tmpl.Name())
		}
	}
	return names
}

// ReloadTemplates reloads all templates (useful in development)
func (app *App) ReloadTemplates() error {
	if app.config.Environment != EnvDevelopment {
		app.logger.Warn("Template reloading is only available in development mode")
		return fmt.Errorf("template reloading only available in development mode")
	}

	app.logger.Info("Reloading templates")
	app.setupTemplateEngine()

	// Recreate Fiber app with new template renderer
	if err := app.createFiberApp(); err != nil {
		return fmt.Errorf("failed to recreate Fiber app with new templates: %w", err)
	}

	return nil
}

// Additional convenience methods for Context

// RenderTemplate renders a template with the given data
func (ctx *Context) RenderTemplate(template string, data interface{}) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.Render(template, data)
	}
	return fmt.Errorf("fiber context not available")
}


// RenderHTML renders a template using the app's template engine
func (ctx *Context) RenderHTML(templateName string, data interface{}) error {
	if ctx.Fiber == nil {
		return fmt.Errorf("fiber context not available")
	}

	// Prepare template data with defaults
	templateData := map[string]interface{}{
		"CSRFToken":     ctx.getCSRFToken(),
		"Environment":   ctx.Config.Environment,
		"IsDevelopment": ctx.Config.Environment == EnvDevelopment,
		"IsProduction":  ctx.Config.Environment == EnvProduction,
		"IsTest":        ctx.Config.Environment == EnvTest,
		"RequestPath":   ctx.Path(),
		"RequestMethod": ctx.Method(),
	}

	// Merge with provided data
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			templateData[k] = val
		}
	case nil:
		// Use only default data
	default:
		// Wrap non-map data in a "Data" field
		templateData["Data"] = data
	}

	// Use Fiber's render method which will use our custom template renderer
	return ctx.Fiber.Render(templateName, templateData)
}

// RenderHTMLTemplate renders a template using the app's template engine with enhanced error handling
func (ctx *Context) RenderHTMLTemplate(templateName string, data interface{}) error {
	if ctx.Fiber == nil {
		return fmt.Errorf("fiber context not available")
	}

	// Prepare template data with defaults
	templateData := map[string]interface{}{
		"CSRFToken":     ctx.getCSRFToken(),
		"Environment":   ctx.Config.Environment,
		"IsDevelopment": ctx.Config.Environment == EnvDevelopment,
		"IsProduction":  ctx.Config.Environment == EnvProduction,
		"IsTest":        ctx.Config.Environment == EnvTest,
		"RequestPath":   ctx.Path(),
		"RequestMethod": ctx.Method(),
	}

	// Merge with provided data
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			templateData[k] = val
		}
	case nil:
		// Use only default data
	default:
		// Wrap non-map data in a "Data" field
		templateData["Data"] = data
	}

	// Use Fiber's render method which will use our custom template renderer
	return ctx.Fiber.Render(templateName, templateData)
}

// Page renders a template with page-specific data structure
func (ctx *Context) Page(templateName string, pageData interface{}) error {
	data := map[string]interface{}{
		"Page": pageData,
		"Meta": map[string]interface{}{
			"Title":       "",
			"Description": "",
			"Keywords":    "",
		},
	}

	return ctx.RenderHTMLTemplate(templateName, data)
}

// PartialHTML renders a partial template (useful for HTMX/AJAX responses)
func (ctx *Context) PartialHTML(templateName string, data interface{}) error {
	return ctx.RenderHTMLTemplate(templateName, data)
}

// Redirect redirects the request to the specified URL
func (ctx *Context) Redirect(url string) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.Redirect(url)
	}
	return fmt.Errorf("fiber context not available")
}

// Method returns the HTTP method
func (ctx *Context) Method() string {
	if ctx.Fiber != nil {
		return ctx.Fiber.Method()
	}
	return ""
}

// Path returns the request path
func (ctx *Context) Path() string {
	if ctx.Fiber != nil {
		return ctx.Fiber.Path()
	}
	return ""
}

// Set adds a response header
func (ctx *Context) Set(key, value string) {
	if ctx.Fiber != nil {
		ctx.Fiber.Set(key, value)
	}
}

// Cookie sets a cookie
func (ctx *Context) Cookie(cookie *fiber.Cookie) {
	if ctx.Fiber != nil {
		ctx.Fiber.Cookie(cookie)
	}
}

// ClearCookie clears a cookie
func (ctx *Context) ClearCookie(name string) {
	if ctx.Fiber != nil {
		ctx.Fiber.ClearCookie(name)
	}
}

// Cookies returns the cookie value for the given key
func (ctx *Context) Cookies(key string) string {
	if ctx.Fiber != nil {
		return ctx.Fiber.Cookies(key)
	}
	return ""
}

// Next calls the next middleware in the stack
func (ctx *Context) Next() error {
	if ctx.Fiber != nil {
		return ctx.Fiber.Next()
	}
	return fmt.Errorf("fiber context not available")
}

// Get retrieves a local variable
func (ctx *Context) Get(key string) interface{} {
	if ctx.Fiber != nil {
		return ctx.Fiber.Locals(key)
	}
	return nil
}

// SetLocal sets a local variable
func (ctx *Context) SetLocal(key string, value interface{}) {
	if ctx.Fiber != nil {
		ctx.Fiber.Locals(key, value)
	}
}

// SendString sends a plain text response
func (ctx *Context) SendString(s string) error {
	if ctx.Fiber != nil {
		return ctx.Fiber.SendString(s)
	}
	return fmt.Errorf("fiber context not available")
}

// setupMigrations configures the migration system with convention-based loading
func (app *App) setupMigrations() {
	app.logger.Info("Setting up migrations")

	if app.migrations == nil {
		app.logger.Warn("Migration manager not initialized")
		return
	}

	if app.config.Environment == EnvDevelopment {
		// Development: filesystem migrations with reloading
		app.logger.Debug("Using filesystem migrations")

		// Load migrations from filesystem
		if err := app.loadMigrationsFromFilesystem(); err != nil {
			app.logger.Error("Failed to load migrations from filesystem", "error", err)
			return
		}
	} else {
		// Production/Test: embedded migrations
		app.logger.Debug("Using embedded migrations")

		// Load migrations from embedded filesystem
		if err := app.loadMigrationsFromEmbedded(); err != nil {
			app.logger.Error("Failed to load embedded migrations", "error", err)
			return
		}
	}

	app.logger.Info("Migration system configured successfully")
}

// loadMigrationsFromFilesystem loads migrations from the ./migrations directory
func (app *App) loadMigrationsFromFilesystem() error {
	migrationsDir := "./migrations"

	// Check if migrations directory exists
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		app.logger.Info("Migrations directory does not exist, skipping migration setup", "path", migrationsDir)
		return nil // Not an error, just no migrations to load
	}

	app.logger.Debug("Loading migrations from filesystem", "directory", migrationsDir)

	// Use the existing LoadFromFS method but we need to adapt it for OS filesystem
	// For now, we'll create a placeholder that logs the intention
	// In a real implementation, we'd need to adapt the migration manager for OS filesystem

	app.logger.Info("Filesystem-based migration loading detected", "directory", migrationsDir)
	app.logger.Warn("Filesystem migration loading not yet implemented - use embedded migrations for now")

	return nil
}

// loadMigrationsFromEmbedded loads migrations from embedded filesystem
func (app *App) loadMigrationsFromEmbedded() error {
	if app.config.MigrationFS == (embed.FS{}) {
		app.logger.Debug("No embedded migration filesystem provided")
		return nil
	}

	migrationFS := app.config.MigrationFS
	migrationsDir := "migrations"

	app.logger.Debug("Loading migrations from embedded filesystem", "directory", migrationsDir)

	// Use the existing migration manager to load from embedded FS
	if err := app.migrations.LoadFromFS(migrationFS, migrationsDir); err != nil {
		return fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	app.logger.Info("Migrations loaded from embedded filesystem")
	return nil
}
