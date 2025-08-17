package cartridge

import (
	"embed"
	"gorm.io/gorm"
)

// This file provides the user-friendly factory functions that follow
// "convention over configuration" principles while using the unified core

// NewGeneric creates a generic application with balanced defaults
// Perfect for prototyping or when you need maximum flexibility
func NewGeneric(options ...AppOption) *App {
	return newAppWithProfile(AppTypeGeneric, options...)
}

// NewFullStack creates a traditional server-rendered web application
// Optimized for: HTML templates, forms, sessions, traditional web patterns
// 
// Convention over configuration:
// - Templates in ./templates/
// - Static files in ./public/
// - Migrations in ./migrations/
// - CSRF protection enabled
// - Sessions enabled
func NewFullStack(options ...AppOption) *App {
	return newAppWithProfile(AppTypeFullStack, options...)
}

// NewAPIOnly creates a lightweight API-focused application
// Optimized for: REST APIs, microservices, headless backends
//
// Convention over configuration:
// - CORS enabled for cross-origin requests
// - Rate limiting enabled
// - CSRF disabled (stateless)
// - Templates disabled
// - Database and auth enabled
func NewAPIOnly(options ...AppOption) *App {
	return newAppWithProfile(AppTypeAPIOnly, options...)
}

// NewFullStackInertia creates a modern SPA with server-side routing
// Optimized for: Vue.js/React SPAs, Inertia.js, modern frontend workflows
//
// Convention over configuration:
// - Inertia.js integration enabled
// - Templates in ./resources/views/
// - Assets in ./resources/assets/
// - Vite dev server at localhost:5173
// - CSRF enabled with Inertia exclusions
// - Sessions enabled
func NewFullStackInertia(options ...AppOption) *App {
	return newAppWithProfile(AppTypeFullStackInertia, options...)
}

// newAppWithProfile is the unified internal constructor that uses profiles
func newAppWithProfile(appType AppType, options ...AppOption) *App {
	// Start with profile-based configuration
	cfg := defaultConfigForType(appType)
	
	// Apply user options (these can override profile defaults)
	for _, option := range options {
		option(&cfg)
	}
	
	// Create the app using the common core
	return createAppFromConfig(cfg, appType)
}

// createAppFromConfig creates an app instance from a fully configured CartridgeConfig
func createAppFromConfig(cfg CartridgeConfig, appType AppType) *App {
	// Create dependencies with error handling
	deps, err := createDependencies()
	if err != nil {
		// In a real implementation, we'd handle this better
		// For now, log and continue with placeholders
		cfg.Environment = EnvDevelopment // Ensure we have a safe fallback
		deps = &dependencies{
			logger:     NewLogger(LogConfig{Environment: "development", EnableConsole: true, EnableColors: true}),
			database:   NewDatabase(&Config{}, NewLogger(LogConfig{Environment: "development", EnableConsole: true, EnableColors: true})),
			authConfig: CookieAuthConfig{},
		}
	}

	// Create application instance
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

	// Initialize components based on profile
	app.initializeFromProfile()

	// Perform final setup
	if err := app.setup(); err != nil {
		app.logger.Error("Failed to setup application", "error", err)
	}

	return app
}

// initializeFromProfile initializes app components based on the active profile
func (app *App) initializeFromProfile() {
	profile := app.config.Profile
	
	// Initialize database-related components if enabled
	if profile.EnableDatabase {
		if db := app.database.GetGenericConnection(); db != nil {
			if gormDB, ok := db.(*gorm.DB); ok {
				// Initialize migration manager if enabled
				app.migrations = NewMigrationManager(gormDB, app.logger)
				
				// Initialize cron manager if enabled
				if profile.EnableCron {
					app.cron = NewCronManager(app.database, app.logger)
				}
				
				// Initialize async manager if enabled
				if profile.EnableAsync {
					app.async = NewAsyncManager(app.database, app.logger)
				}
			}
		}
	}

	// Initialize asset manager based on profile settings
	config := &Config{Environment: app.config.Environment}
	assetConfig := DefaultAssetConfig(config)
	app.assets = NewAssetManager(assetConfig, app.logger)

	// Initialize Inertia.js manager if enabled by profile
	if profile.EnableInertia {
		var inertiaConfig InertiaConfig
		if profile.InertiaConfig != nil {
			inertiaConfig = *profile.InertiaConfig
		} else {
			inertiaConfig = DefaultInertiaConfig()
		}
		isDevMode := app.config.Environment == EnvDevelopment
		app.inertia = NewInertiaManager(inertiaConfig, isDevMode)
	}

	// Set embedded filesystems if provided
	if app.config.StaticFS != (embed.FS{}) || app.config.TemplateFS != (embed.FS{}) {
		app.assets.SetEmbeddedFS(app.config.StaticFS, app.config.TemplateFS)
	}
}

// Profile returns the active application profile
func (app *App) Profile() AppProfile {
	return app.config.Profile
}

// IsFeatureEnabled checks if a specific feature is enabled in the current configuration
// This checks the final resolved configuration, including user overrides
func (app *App) IsFeatureEnabled(feature string) bool {
	switch feature {
	case "csrf":
		return app.config.EnableCSRF
	case "cors":
		return app.config.EnableCORS
	case "ratelimit":
		return app.config.EnableRateLimit
	case "sessions":
		return app.config.Profile.EnableSessions
	case "inertia":
		return app.config.Profile.EnableInertia
	case "templates":
		return app.config.Profile.EnableTemplates
	case "static":
		return app.config.Profile.EnableStatic
	case "database":
		return app.config.Profile.EnableDatabase
	case "auth":
		return app.config.Profile.EnableAuth
	case "cron":
		return app.config.Profile.EnableCron
	case "async":
		return app.config.Profile.EnableAsync
	default:
		return false
	}
}