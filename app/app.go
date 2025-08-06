package app

import (
	"embed"
	"fmt"

	"github.com/karloscodes/cartridge/auth"
	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/database"
	"github.com/karloscodes/cartridge/logging"
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
}

// AppOption is a functional option for configuring the Cartridge application
type AppOption func(*CartridgeConfig)

// Application represents the Cartridge web application
type Application struct {
	config     CartridgeConfig
	logger     logging.Logger
	database   database.Database
	authConfig auth.AuthConfig
	fiberApp   interface{} // *fiber.App instance
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

// defaultConfig returns a default CartridgeConfig
func defaultConfig() CartridgeConfig {
	cfg := config.New()

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

	return CartridgeConfig{
		Environment:     environment,
		Port:            port,
		TrustedProxies:  []string{},
		Concurrency:     DefaultConcurrency,
		ErrorHandler:    nil,
		EnableCSRF:      true,  // Default to enabled
		EnableCORS:      false, // Default to disabled
		EnableRateLimit: false, // Default to disabled
	}
}

// New creates a new Cartridge application with functional options
// This is the main factory function that returns a Cartridge Application
//
// Note: Fiber integration is currently in placeholder mode due to dependency resolution issues.
// The application structure is ready for Fiber integration once the dependency is properly available.
func New(options ...AppOption) (*Application, error) {
	// Start with default configuration
	cfg := defaultConfig()

	// Apply functional options
	for _, option := range options {
		option(&cfg)
	}

	// Create dependencies
	deps, err := createDependencies()
	if err != nil {
		return nil, fmt.Errorf("failed to create dependencies: %w", err)
	}

	// Create application
	app := &Application{
		config:     cfg,
		logger:     deps.logger,
		database:   deps.database,
		authConfig: deps.authConfig,
	}

	// Setup the application
	if err := app.setup(); err != nil {
		return nil, fmt.Errorf("failed to setup application: %w", err)
	}

	return app, nil
}

// dependencies holds all application dependencies
type dependencies struct {
	config     *config.Config
	logger     logging.Logger
	database   database.Database
	authConfig auth.AuthConfig
}

// createDependencies creates all application dependencies
func createDependencies() (*dependencies, error) {
	// Load configuration
	cfg := config.New()

	// Setup logging
	logger := logging.NewLogger(logging.LogConfig{
		Level:         logging.LogLevel(cfg.LogLevel),
		Directory:     cfg.LogsDirectory,
		MaxSize:       cfg.LogsMaxSizeInMb,
		MaxBackups:    cfg.LogsMaxBackups,
		MaxAge:        cfg.LogsMaxAgeInDays,
		UseJSON:       cfg.IsProduction(),
		UseColor:      cfg.IsDevelopment(),
		EnableConsole: true,
	})

	// Setup database
	dbInstance := database.NewDatabase(cfg, logger)
	if err := dbInstance.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Setup authentication
	authConfig := auth.DefaultAuthConfig(cfg.PrivateKey, cfg.IsProduction())

	return &dependencies{
		config:     cfg,
		logger:     logger,
		database:   dbInstance,
		authConfig: authConfig,
	}, nil
}

// setup configures the Fiber application with all middleware and settings
func (app *Application) setup() error {
	app.logger.Info("Setting up Cartridge application",
		logging.Field{Key: "environment", Value: app.config.Environment},
		logging.Field{Key: "port", Value: app.config.Port})

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

	app.logger.Info("Cartridge application setup completed")
	return nil
}

// createFiberApp creates and configures the Fiber application
func (app *Application) createFiberApp() error {
	// TODO: Implement when Fiber dependency is resolved
	// fiberApp := fiber.New(fiber.Config{
	//     AppName:      "Cartridge App",
	//     ErrorHandler: app.getFiberErrorHandler(),
	//     Prefork:      app.config.Environment == EnvProduction,
	// })
	// app.fiberApp = fiberApp

	app.logger.Info("Fiber app creation placeholder - implement when dependency is available")
	return nil
}

// setupMiddleware configures all middleware for the application
func (app *Application) setupMiddleware() {
	app.logger.Info("Setting up middleware stack")

	// Global middleware stack (in order):
	// 1. Request ID
	// 2. Recovery
	// 3. Logger
	// 4. Helmet (security headers)
	// 5. Database injection
	// 6. Method override

	// Example of what this would look like with Fiber:
	/*
		fiberApp := app.fiberApp.(*fiber.App)

		// Request ID for tracing
		fiberApp.Use(middleware.RequestID())

		// Recovery with stack traces
		fiberApp.Use(middleware.Recovery(app.logger))

		// HTTP request logging
		fiberApp.Use(middleware.Logger(app.logger))

		// Security headers
		fiberApp.Use(middleware.Helmet("strict-origin-when-cross-origin"))

		// Database injection
		fiberApp.Use(middleware.DatabaseInjection(app.database))

		// Method override for forms
		fiberApp.Use(middleware.MethodOverride())

		// Conditional middleware based on configuration
		if app.config.EnableCSRF {
			csrfConfig := middleware.DefaultCSRFConfig()
			csrfConfig.CookieSecure = app.config.Environment == "production"
			fiberApp.Use(middleware.CSRF(app.logger, csrfConfig))
		}

		if app.config.EnableRateLimit {
			rateLimitConfig := middleware.DefaultRateLimiterConfig()
			if app.config.Environment == "production" {
				rateLimitConfig.Max = 60
				rateLimitConfig.Duration = 1 * time.Minute
			}
			fiberApp.Use(middleware.RateLimiter(rateLimitConfig))
		}

		if app.config.EnableCORS {
			corsConfig := middleware.DefaultCORSConfig()
			if app.config.Environment == "production" {
				corsConfig.AllowOrigins = []string{"https://yourdomain.com"}
			}
			fiberApp.Use(middleware.CORS(corsConfig))
		}
	*/
}

// setupStaticAssets configures static asset serving
func (app *Application) setupStaticAssets() {
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
func (app *Application) setupTemplateEngine() {
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
func (app *Application) getErrorHandler() interface{} {
	if app.config.ErrorHandler != nil {
		return app.config.ErrorHandler
	}

	return func(err error) error {
		// Environment-specific error handling
		if app.config.Environment == "development" {
			// Development: detailed error messages with stack traces
			app.logger.Error("Request error (development)",
				logging.Field{Key: "error", Value: err.Error()})
		} else {
			// Production: generic error messages, detailed logging
			app.logger.Error("Request error (production)",
				logging.Field{Key: "error", Value: err.Error()})
		}

		return nil // This would be a proper fiber response
	}
}

// GetFiberApp returns the underlying Fiber application
// Returns nil until Fiber dependency is properly resolved
func (app *Application) GetFiberApp() interface{} {
	// TODO: Return actual *fiber.App when dependency is resolved
	// return app.fiberApp
	return nil
}

// GetDatabase returns the database instance for testing purposes
func (app *Application) GetDatabase() database.Database {
	return app.database
}

// Start starts the web application
func (app *Application) Start() error {
	app.logger.Info("Starting web application",
		logging.Field{Key: "environment", Value: app.config.Environment},
		logging.Field{Key: "port", Value: app.config.Port})

	// Start the Fiber server
	// TODO: Implement when Fiber dependency is resolved
	// if app.fiberApp != nil {
	//     return app.fiberApp.(*fiber.App).Listen(":" + app.config.Port)
	// }

	app.logger.Info("Application started successfully (placeholder mode)")
	return nil
}

// Stop gracefully stops the web application
func (app *Application) Stop() error {
	app.logger.Info("Stopping web application")

	// Close database connections
	if err := app.database.Close(); err != nil {
		app.logger.Error("Failed to close database",
			logging.Field{Key: "error", Value: err})
	}

	app.logger.Info("Application stopped")
	return nil
}
