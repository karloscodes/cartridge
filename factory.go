package cartridge

import (
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/sqlite"
)

// App is a fully configured cartridge application.
type App struct {
	*Application
	Config    *config.Config
	Logger    *slog.Logger
	DBManager *sqlite.Manager
}

// AppOption configures the application.
type AppOption func(*appConfig)

type appConfig struct {
	templatesFS        fs.FS
	staticFS           fs.FS
	templatesDirectory string
	workers            []BackgroundWorker
	workerFactory      func(*App) []BackgroundWorker
	init               func(*App)
	beforeStart        func(*App) error
	routes             func(*Server, *config.Config)
}

// WithTemplates sets the embedded templates filesystem for production.
func WithTemplates(fs fs.FS) AppOption {
	return func(c *appConfig) {
		c.templatesFS = fs
	}
}

// WithStatic sets the embedded static assets filesystem for production.
func WithStatic(fs fs.FS) AppOption {
	return func(c *appConfig) {
		c.staticFS = fs
	}
}

// WithTemplatesDir sets the templates directory for development.
func WithTemplatesDir(dir string) AppOption {
	return func(c *appConfig) {
		c.templatesDirectory = dir
	}
}

// WithWorkers adds background workers.
func WithWorkers(workers ...BackgroundWorker) AppOption {
	return func(c *appConfig) {
		c.workers = append(c.workers, workers...)
	}
}

// WithWorkerFactory sets a factory function to create workers after app is initialized.
// Use this when workers need access to the app's logger or database.
func WithWorkerFactory(factory func(*App) []BackgroundWorker) AppOption {
	return func(c *appConfig) {
		c.workerFactory = factory
	}
}

// WithInit sets an initialization function called after logger and database are ready.
// Use this for auth setup or other initialization.
func WithInit(fn func(*App)) AppOption {
	return func(c *appConfig) {
		c.init = fn
	}
}

// WithBeforeStart sets a function called before the app is returned.
// Use this for migrations or validation.
func WithBeforeStart(fn func(*App) error) AppOption {
	return func(c *appConfig) {
		c.beforeStart = fn
	}
}

// WithRoutes sets the route mounting function.
func WithRoutes(fn func(*Server, *config.Config)) AppOption {
	return func(c *appConfig) {
		c.routes = fn
	}
}

// NewApp creates a complete cartridge application.
//
// Example:
//
//	app, err := cartridge.NewApp("myapp",
//	    cartridge.WithTemplates(web.Templates),
//	    cartridge.WithStatic(web.Static),
//	    cartridge.WithInit(func(a *cartridge.App) {
//	        auth.Initialize(a.Config)
//	    }),
//	    cartridge.WithWorkerFactory(func(a *cartridge.App) []cartridge.BackgroundWorker {
//	        return []cartridge.BackgroundWorker{
//	            jobs.NewDispatcher(a.Config, a.Logger, a.DBManager),
//	        }
//	    }),
//	    cartridge.WithRoutes(routes.Mount),
//	    cartridge.WithBeforeStart(func(a *cartridge.App) error {
//	        return database.Migrate(a.DBManager)
//	    }),
//	)
func NewApp(appName string, opts ...AppOption) (*App, error) {
	// Apply options
	cfg := &appConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Load configuration
	appCfg, err := config.Load(appName)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Create logger
	logger := NewLogger(appCfg, nil)
	slog.SetDefault(logger)

	// Create database manager
	dbManager := sqlite.NewManager(sqlite.Config{
		Path:         appCfg.DatabaseDSN(),
		MaxOpenConns: appCfg.GetMaxOpenConns(),
		MaxIdleConns: appCfg.GetMaxIdleConns(),
		Logger:       logger,
	})

	// Build partial app for callbacks
	app := &App{
		Config:    appCfg,
		Logger:    logger,
		DBManager: dbManager,
	}

	// Run init callback
	if cfg.init != nil {
		cfg.init(app)
	}

	// Build server config
	serverCfg := &ServerConfig{
		Config:    appCfg,
		Logger:    logger,
		DBManager: dbManager,
	}

	// Configure assets based on environment
	if !appCfg.IsDevelopment() {
		serverCfg.TemplatesFS = cfg.templatesFS
		serverCfg.StaticFS = cfg.staticFS
	} else if cfg.templatesDirectory != "" {
		serverCfg.TemplatesDirectory = cfg.templatesDirectory
	}

	// Create server
	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	// Mount routes
	if cfg.routes != nil {
		cfg.routes(server, appCfg)
	}

	// Collect workers
	workers := cfg.workers
	if cfg.workerFactory != nil {
		workers = append(workers, cfg.workerFactory(app)...)
	}

	// Create application
	application, err := NewApplication(ApplicationOptions{
		Config:            appCfg,
		Logger:            logger,
		DBManager:         dbManager,
		Server:            server,
		BackgroundWorkers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
	}

	app.Application = application

	// Run before start callback
	if cfg.beforeStart != nil {
		if err := cfg.beforeStart(app); err != nil {
			return nil, fmt.Errorf("before start: %w", err)
		}
	}

	return app, nil
}
