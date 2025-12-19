package cartridge

import (
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/sqlite"
)

// SSRAppOptions configures a server-side rendered application.
type SSRAppOptions struct {
	// TemplatesFS is the embedded filesystem for templates (production).
	TemplatesFS fs.FS

	// StaticFS is the embedded filesystem for static assets (production).
	StaticFS fs.FS

	// TemplatesDirectory overrides template location (development).
	TemplatesDirectory string

	// BackgroundWorkerFactory creates background workers with access to app dependencies.
	// Called after database and logger are ready.
	BackgroundWorkerFactory func(cfg *config.Config, logger *slog.Logger, db *sqlite.Manager) []BackgroundWorker

	// Init is called before routes are mounted.
	// Use this for auth initialization or other setup.
	Init func(cfg *config.Config, logger *slog.Logger)

	// BeforeStart is called after everything is ready but before returning.
	// Use this to run migrations or other initialization.
	BeforeStart func(app *SSRApp) error
}

// SSRApp is a server-side rendered application with all dependencies wired.
type SSRApp struct {
	*Application
	Config    *config.Config
	Logger    *slog.Logger
	DBManager *sqlite.Manager
}

// NewSSRApp creates a complete SSR application.
//
// Example:
//
//	app, err := cartridge.NewSSRApp("myapp", cartridge.SSRAppOptions{
//	    TemplatesFS: web.Templates,
//	    StaticFS:    web.Static,
//	    Init: func(cfg *config.Config, logger *slog.Logger) {
//	        auth.Initialize(cfg)
//	    },
//	    BackgroundWorkerFactory: func(cfg *config.Config, logger *slog.Logger, db *sqlite.Manager) []cartridge.BackgroundWorker {
//	        return []cartridge.BackgroundWorker{jobs.NewDispatcher(cfg, logger, db)}
//	    },
//	}, func(s *cartridge.Server, cfg *config.Config) {
//	    s.Get("/", homeHandler)
//	})
func NewSSRApp(appName string, opts SSRAppOptions, mountRoutes func(*Server, *config.Config)) (*SSRApp, error) {
	// Load configuration
	cfg, err := config.Load(appName)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Create logger (auto-detects LogConfigProvider)
	logger := NewLogger(cfg, nil)
	slog.SetDefault(logger)

	// Run init callback if provided
	if opts.Init != nil {
		opts.Init(cfg, logger)
	}

	// Create database manager
	dbManager := sqlite.NewManager(sqlite.Config{
		Path:         cfg.DatabaseDSN(),
		MaxOpenConns: cfg.GetMaxOpenConns(),
		MaxIdleConns: cfg.GetMaxIdleConns(),
		Logger:       logger,
	})

	// Build server config
	serverCfg := &ServerConfig{
		Config:    cfg,
		Logger:    logger,
		DBManager: dbManager,
	}

	// Configure assets based on environment
	if !cfg.IsDevelopment() {
		serverCfg.TemplatesFS = opts.TemplatesFS
		serverCfg.StaticFS = opts.StaticFS
	} else if opts.TemplatesDirectory != "" {
		serverCfg.TemplatesDirectory = opts.TemplatesDirectory
	}

	// Create server
	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	// Mount routes
	if mountRoutes != nil {
		mountRoutes(server, cfg)
	}

	// Create background workers if factory provided
	var workers []BackgroundWorker
	if opts.BackgroundWorkerFactory != nil {
		workers = opts.BackgroundWorkerFactory(cfg, logger, dbManager)
	}

	// Create application
	application, err := NewApplication(ApplicationOptions{
		Config:            cfg,
		Logger:            logger,
		DBManager:         dbManager,
		Server:            server,
		BackgroundWorkers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
	}

	app := &SSRApp{
		Application: application,
		Config:      cfg,
		Logger:      logger,
		DBManager:   dbManager,
	}

	// Run BeforeStart hook if provided
	if opts.BeforeStart != nil {
		if err := opts.BeforeStart(app); err != nil {
			return nil, fmt.Errorf("before start: %w", err)
		}
	}

	return app, nil
}

// DB returns a database connection.
func (a *SSRApp) DB() *sqlite.Manager {
	return a.DBManager
}
