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

	// BackgroundWorkers to run alongside the server.
	BackgroundWorkers []BackgroundWorker

	// BeforeStart is called after setup but before returning.
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
//	}, func(s *cartridge.Server, cfg *config.Config) {
//	    s.Get("/", homeHandler)
//	    s.Post("/submit", submitHandler)
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

	// Create application
	application, err := NewApplication(ApplicationOptions{
		Config:            cfg,
		Logger:            logger,
		DBManager:         dbManager,
		Server:            server,
		BackgroundWorkers: opts.BackgroundWorkers,
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
