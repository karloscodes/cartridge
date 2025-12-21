package cartridge

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/sqlite"
)

// App is a fully configured cartridge application.
type App struct {
	*Application
	Config    *config.Config
	Logger    *slog.Logger
	DBManager *sqlite.Manager
	Server    *Server
	Session   *SessionManager
}

// MigrateDatabase runs database migrations using the provided migrator.
// It connects to the database, runs migrations, and checkpoints WAL.
func (a *App) MigrateDatabase(migrator Migrator) error {
	db, err := a.DBManager.Connect()
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	if err := migrator.Migrate(db); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	if err := a.DBManager.CheckpointWAL("FULL"); err != nil {
		a.Logger.Warn("failed to checkpoint WAL after migration", slog.Any("error", err))
	}

	return nil
}

// GetDB returns the database connection.
func (a *App) GetDB() (*gorm.DB, error) {
	return a.DBManager.Connect()
}

// AppOption configures the application.
type AppOption func(*appConfig)

// jobGroup represents a set of processors with their interval.
type jobGroup struct {
	interval   time.Duration
	processors []Processor
}

type appConfig struct {
	cfg           *config.Config
	templatesFS   fs.FS
	staticFS      fs.FS
	templateFuncs template.FuncMap
	errorHandler  fiber.ErrorHandler
	init          func(*App)
	routes        func(*Server)
	jobGroups     []jobGroup
	sessionPath   string // login path for session middleware
}

// WithConfig provides a pre-loaded config instead of loading one.
func WithConfig(cfg *config.Config) AppOption {
	return func(c *appConfig) {
		c.cfg = cfg
	}
}

// WithAssets sets embedded templates and static files for production.
func WithAssets(templates, static fs.FS) AppOption {
	return func(c *appConfig) {
		c.templatesFS = templates
		c.staticFS = static
	}
}

// WithTemplateFuncs adds custom template functions.
func WithTemplateFuncs(funcs template.FuncMap) AppOption {
	return func(c *appConfig) {
		c.templateFuncs = funcs
	}
}

// WithErrorHandler sets a custom error handler.
func WithErrorHandler(handler fiber.ErrorHandler) AppOption {
	return func(c *appConfig) {
		c.errorHandler = handler
	}
}

// WithInit sets initialization callback (e.g., auth setup).
func WithInit(fn func(*App)) AppOption {
	return func(c *appConfig) {
		c.init = fn
	}
}

// WithRoutes sets the route mounting function.
func WithRoutes(fn func(*Server)) AppOption {
	return func(c *appConfig) {
		c.routes = fn
	}
}

// WithJobs registers background job processors with a shared interval.
// Call multiple times to create separate dispatchers with different schedules.
func WithJobs(interval time.Duration, processors ...Processor) AppOption {
	return func(c *appConfig) {
		c.jobGroups = append(c.jobGroups, jobGroup{
			interval:   interval,
			processors: processors,
		})
	}
}

// WithSession enables session management with auto-derived cookie name.
// The cookie name is "{appname}_session" (e.g., "formlander_session").
func WithSession(loginPath string) AppOption {
	return func(c *appConfig) {
		c.sessionPath = loginPath
	}
}

// NewSSRApp creates a server-side rendered application with sensible defaults.
//
// Example:
//
//	app, err := cartridge.NewSSRApp("myapp",
//	    cartridge.WithAssets(web.Templates, web.Static),
//	    cartridge.WithTemplateFuncs(templateFuncs()),
//	    cartridge.WithJobs(2*time.Minute, webhookJob, emailJob),
//	    cartridge.WithRoutes(mountRoutes),
//	)
func NewSSRApp(appName string, opts ...AppOption) (*App, error) {
	// Apply options
	cfg := &appConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Load config
	var appCfg *config.Config
	var err error
	if cfg.cfg != nil {
		appCfg = cfg.cfg
	} else {
		appCfg, err = config.Load(appName)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
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

	// Create views engine
	viewsEngine := createViewsEngine(appCfg, cfg.templatesFS, cfg.templateFuncs)

	// Build server config
	serverCfg := DefaultServerConfig()
	serverCfg.Config = appCfg
	serverCfg.Logger = logger
	serverCfg.DBManager = dbManager
	serverCfg.ViewsEngine = viewsEngine
	// In development mode, serve static from disk for hot-reload
	// In production, use embedded filesystem
	if !appCfg.IsDevelopment() && cfg.staticFS != nil {
		serverCfg.StaticFS = cfg.staticFS
	}
	if cfg.errorHandler != nil {
		serverCfg.ErrorHandler = cfg.errorHandler
	} else {
		serverCfg.ErrorHandler = DefaultErrorHandler(logger, appCfg.IsDevelopment())
	}

	// Create server
	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	// Create session manager if enabled and attach to server
	var sessionMgr *SessionManager
	if cfg.sessionPath != "" {
		sessionMgr = NewSessionManager(SessionConfig{
			CookieName: appCfg.AppName + "_session",
			Secret:     appCfg.GetSessionSecret(),
			TTL:        time.Duration(appCfg.GetSessionTimeout()) * time.Second,
			Secure:     appCfg.IsProduction(),
			LoginPath:  cfg.sessionPath,
		})
		server.SetSession(sessionMgr)
	}

	// Mount routes (session is available via server.Session())
	if cfg.routes != nil {
		cfg.routes(server)
	}

	// Build app
	app := &App{
		Config:    appCfg,
		Logger:    logger,
		DBManager: dbManager,
		Server:    server,
		Session:   sessionMgr,
	}

	// Run init callback
	if cfg.init != nil {
		cfg.init(app)
	}

	// Create job dispatchers for each job group
	var workers []BackgroundWorker
	for _, group := range cfg.jobGroups {
		dispatcher := NewJobDispatcher(logger, dbManager, group.interval, group.processors...)
		workers = append(workers, dispatcher)
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
	return app, nil
}

// createViewsEngine creates the template engine with provided functions.
func createViewsEngine(cfg *config.Config, templatesFS fs.FS, funcs template.FuncMap) *html.Engine {
	var engine *html.Engine

	if !cfg.IsDevelopment() && templatesFS != nil {
		engine = html.NewFileSystem(http.FS(templatesFS), ".html")
	} else {
		engine = html.New("web/templates", ".html")
	}

	// Add render function (needs engine access)
	engine.AddFunc("render", func(name string, data any) (template.HTML, error) {
		if !engine.Loaded {
			if err := engine.Load(); err != nil {
				return "", err
			}
		}
		tpl := engine.Templates.Lookup(name)
		if tpl == nil {
			return "", fmt.Errorf("template %q not found", name)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	})

	// Add provided template functions
	for name, fn := range funcs {
		engine.AddFunc(name, fn)
	}

	// Development mode settings
	engine.Debug(cfg.IsDevelopment())
	if cfg.IsDevelopment() {
		engine.Reload(true)
	}

	return engine
}
