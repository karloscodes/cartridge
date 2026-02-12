package cartridge

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/karloscodes/cartridge/inertia"
	"github.com/karloscodes/cartridge/sqlite"
)

// InertiaApp is a fully configured Inertia.js application.
type InertiaApp struct {
	*Application
	DBManager *sqlite.Manager
	Session   *SessionManager
}

// InertiaOption configures the Inertia application.
type InertiaOption func(*inertiaConfig)

// inertiaJobGroup represents a set of processors with their interval.
type inertiaJobGroup struct {
	interval   time.Duration
	processors []Processor
}

type inertiaConfig struct {
	cfg              Config
	staticFS         fs.FS
	customDBManager  DBManager
	routes           func(*Server)
	jobGroups        []inertiaJobGroup
	workers          []BackgroundWorker
	sessionPath      string
	crossOriginAPI   bool
	pageTitle        string
	catchAllRedirect string
}

// InertiaWithConfig provides a pre-loaded config instead of using default.
func InertiaWithConfig(cfg Config) InertiaOption {
	return func(c *inertiaConfig) {
		c.cfg = cfg
	}
}

// InertiaWithStaticAssets sets embedded static files for production.
// In development mode, assets are served from disk for hot-reload.
func InertiaWithStaticAssets(static fs.FS) InertiaOption {
	return func(c *inertiaConfig) {
		c.staticFS = static
	}
}

// InertiaWithDBManager provides a custom database manager.
// Use this when you need a custom DB manager with additional methods (e.g., migrations).
func InertiaWithDBManager(dbManager DBManager) InertiaOption {
	return func(c *inertiaConfig) {
		c.customDBManager = dbManager
	}
}

// InertiaWithRoutes sets the route mounting function.
func InertiaWithRoutes(fn func(*Server)) InertiaOption {
	return func(c *inertiaConfig) {
		c.routes = fn
	}
}

// InertiaWithJobs registers background job processors with a shared interval.
// Call multiple times to create separate dispatchers with different schedules.
// Each call creates ONE dispatcher that runs all given processors at the interval.
func InertiaWithJobs(interval time.Duration, processors ...Processor) InertiaOption {
	return func(c *inertiaConfig) {
		c.jobGroups = append(c.jobGroups, inertiaJobGroup{
			interval:   interval,
			processors: processors,
		})
	}
}

// InertiaWithWorker adds a custom background worker to the application.
// Use this for workers that implement BackgroundWorker directly (Start/Stop).
func InertiaWithWorker(worker BackgroundWorker) InertiaOption {
	return func(c *inertiaConfig) {
		c.workers = append(c.workers, worker)
	}
}

// InertiaWithSession enables session management.
// The cookie name is "{appname}_session".
func InertiaWithSession(loginPath string) InertiaOption {
	return func(c *inertiaConfig) {
		c.sessionPath = loginPath
	}
}

// InertiaWithCrossOriginAPI configures SecFetchSite to allow cross-origin requests.
// Use this for analytics APIs or public endpoints that receive cross-site requests.
func InertiaWithCrossOriginAPI() InertiaOption {
	return func(c *inertiaConfig) {
		c.crossOriginAPI = true
	}
}

// InertiaWithPageTitle sets the HTML page title for Inertia pages.
func InertiaWithPageTitle(title string) InertiaOption {
	return func(c *inertiaConfig) {
		c.pageTitle = title
	}
}

// InertiaWithCatchAllRedirect sets a fallback redirect for unmatched routes.
func InertiaWithCatchAllRedirect(path string) InertiaOption {
	return func(c *inertiaConfig) {
		c.catchAllRedirect = path
	}
}

// FactoryConfig extends Config with factory-specific methods.
// Config loaders should implement this interface to work with NewInertiaApp.
type FactoryConfig interface {
	Config

	// GetAppName returns the application name.
	GetAppName() string

	// DatabaseDSN returns the database connection string.
	DatabaseDSN() string

	// GetSessionSecret returns the session encryption key.
	GetSessionSecret() string

	// GetSessionTimeout returns the session timeout in seconds.
	GetSessionTimeout() int

	// GetMaxOpenConns returns the max open database connections.
	GetMaxOpenConns() int

	// GetMaxIdleConns returns the max idle database connections.
	GetMaxIdleConns() int
}

// NewInertiaApp creates an Inertia.js application with sensible defaults.
//
// Example:
//
//	app, err := cartridge.NewInertiaApp(
//	    cartridge.WithConfig(cfg),
//	    cartridge.WithStaticAssets(web.Assets()),
//	    cartridge.WithRoutes(mountRoutes),
//	    cartridge.WithJobs(60*time.Second, eventProcessor),
//	    cartridge.WithSession("/login"),
//	    cartridge.WithCrossOriginAPI(),
//	)
func NewInertiaApp(opts ...InertiaOption) (*InertiaApp, error) {
	// Apply options
	cfg := &inertiaConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Require config
	if cfg.cfg == nil {
		return nil, fmt.Errorf("cartridge: config is required (use WithConfig)")
	}

	// Cast to FactoryConfig for extended methods
	factoryCfg, ok := cfg.cfg.(FactoryConfig)
	if !ok {
		return nil, fmt.Errorf("cartridge: config must implement FactoryConfig interface")
	}

	// Enable Inertia dev mode in development (re-reads manifest on every request)
	if cfg.cfg.IsDevelopment() {
		inertia.SetDevMode(true)
	}

	// Set page title if provided
	if cfg.pageTitle != "" {
		inertia.SetTitle(cfg.pageTitle)
	}

	// Create logger
	logger := NewLogger(cfg.cfg, nil)

	// Create or use provided database manager
	var dbManager DBManager
	var sqliteManager *sqlite.Manager

	if cfg.customDBManager != nil {
		dbManager = cfg.customDBManager
	} else {
		sqliteManager = sqlite.NewManager(sqlite.Config{
			Path:         factoryCfg.DatabaseDSN(),
			MaxOpenConns: factoryCfg.GetMaxOpenConns(),
			MaxIdleConns: factoryCfg.GetMaxIdleConns(),
			Logger:       logger,
		})
		dbManager = sqliteManager
	}

	// Build server config
	serverCfg := DefaultServerConfig()
	serverCfg.Config = cfg.cfg
	serverCfg.Logger = logger
	serverCfg.DBManager = dbManager

	// Use embedded static assets in production, disk in development for hot-reload
	if !cfg.cfg.IsDevelopment() && cfg.staticFS != nil {
		serverCfg.StaticFS = cfg.staticFS
	}

	// Configure SecFetchSite for cross-origin APIs (analytics, public endpoints)
	if cfg.crossOriginAPI {
		serverCfg.SecFetchSiteAllowedValues = []string{"cross-site", "same-site", "same-origin"}
	}

	// Create server
	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, fmt.Errorf("cartridge: create server: %w", err)
	}

	// Create session manager if enabled and attach to server
	var sessionMgr *SessionManager
	if cfg.sessionPath != "" {
		sessionMgr = NewSessionManager(SessionConfig{
			CookieName: factoryCfg.GetAppName() + "_session",
			Secret:     factoryCfg.GetSessionSecret(),
			TTL:        time.Duration(factoryCfg.GetSessionTimeout()) * time.Second,
			Secure:     cfg.cfg.IsProduction(),
			LoginPath:  cfg.sessionPath,
		})
		server.SetSession(sessionMgr)
	}

	// Mount routes (session is available via server.Session())
	if cfg.routes != nil {
		cfg.routes(server)
	}

	// Set catch-all redirect if configured
	if cfg.catchAllRedirect != "" {
		server.SetCatchAllRedirect(cfg.catchAllRedirect)
	}

	// Collect background workers
	var workers []BackgroundWorker

	// Add custom workers
	workers = append(workers, cfg.workers...)

	// Create job dispatchers for each job group
	for _, group := range cfg.jobGroups {
		dispatcher := NewJobDispatcher(logger, dbManager, group.interval, group.processors...)
		workers = append(workers, dispatcher)
	}

	// Create application
	application, err := NewApplication(ApplicationOptions{
		Config:            cfg.cfg,
		Logger:            logger,
		DBManager:         dbManager,
		Server:            server,
		BackgroundWorkers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("cartridge: create application: %w", err)
	}

	return &InertiaApp{
		Application: application,
		DBManager:   sqliteManager,
		Session:     sessionMgr,
	}, nil
}
