package cartridge

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/karloscodes/cartridge/inertia"
	"github.com/karloscodes/cartridge/sqlite"
)

// AppConfig is the configuration NewApp needs. config.Load returns a
// *config.Config that implements it.
type AppConfig interface {
	Config

	// GetAppName returns the application name. The session cookie is
	// "{name}_session".
	GetAppName() string

	// DatabaseDSN returns the path of the SQLite file.
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

// App is an application with a logger, a SQLite database, an HTTP server
// and, with WithSession, sessions. Run starts it.
type App struct {
	*Application
	DBManager *sqlite.Manager
	Session   *SessionManager // nil without WithSession
}

// MigrateDatabase runs the migrator, then checkpoints the WAL.
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

// AppOption configures NewApp.
type AppOption func(*appOptions)

type appOptions struct {
	templatesFS   fs.FS
	staticFS      fs.FS
	templateFuncs template.FuncMap
	errorHandler  ErrorHandler
	routes        func(*Server)
	workers       []BackgroundWorker
	jobGroups     []jobGroup
	sessionPath   string
	sessionValid  func(userID uint, issuedAt time.Time) bool
	inertia       bool
}

type jobGroup struct {
	interval   time.Duration
	processors []Processor
}

// WithAssets sets the embedded templates and static files. Either can be
// nil. Development reads web/templates and the public directory from disk
// instead, and reloads templates on every render. Without templates,
// Context.Render returns an error.
func WithAssets(templates, static fs.FS) AppOption {
	return func(o *appOptions) {
		o.templatesFS = templates
		o.staticFS = static
	}
}

// WithTemplateFuncs adds functions to the templates.
func WithTemplateFuncs(funcs template.FuncMap) AppOption {
	return func(o *appOptions) {
		o.templateFuncs = funcs
	}
}

// WithErrorHandler replaces DefaultErrorHandler.
func WithErrorHandler(handler ErrorHandler) AppOption {
	return func(o *appOptions) {
		o.errorHandler = handler
	}
}

// WithRoutes mounts the routes. Server.Session is set when it runs.
func WithRoutes(fn func(*Server)) AppOption {
	return func(o *appOptions) {
		o.routes = fn
	}
}

// WithJobs runs the processors in one dispatcher at the interval. Call it
// again for another interval.
func WithJobs(interval time.Duration, processors ...Processor) AppOption {
	return func(o *appOptions) {
		o.jobGroups = append(o.jobGroups, jobGroup{interval: interval, processors: processors})
	}
}

// WithWorker runs a BackgroundWorker alongside the server.
func WithWorker(worker BackgroundWorker) AppOption {
	return func(o *appOptions) {
		o.workers = append(o.workers, worker)
	}
}

// WithSession enables sessions. RequireAuth redirects to loginPath. The
// cookie name is "{appname}_session".
func WithSession(loginPath string) AppOption {
	return func(o *appOptions) {
		o.sessionPath = loginPath
	}
}

// WithSessionCheck sets SessionConfig.Valid: a session counts only while
// valid returns true. Use it to end sessions after a logout or a password
// change. It needs WithSession.
func WithSessionCheck(valid func(userID uint, issuedAt time.Time) bool) AppOption {
	return func(o *appOptions) {
		o.sessionValid = valid
	}
}

// WithInertia prepares the inertia package: in development it re-reads the
// Vite manifest on every request. Set the page title and other settings
// with the inertia package functions.
func WithInertia() AppOption {
	return func(o *appOptions) {
		o.inertia = true
	}
}

// NewApp creates an application with a logger, a SQLite database, and an
// HTTP server. For another database or a custom server, use NewApplication.
//
//	cfg, err := config.Load("myapp")
//	app, err := cartridge.NewApp(cfg,
//	    cartridge.WithAssets(web.Templates(), web.Static()),
//	    cartridge.WithRoutes(mountRoutes),
//	    cartridge.WithSession("/login"),
//	)
func NewApp(cfg AppConfig, opts ...AppOption) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cartridge: config is required")
	}
	o := &appOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.inertia {
		inertia.SetDevMode(cfg.IsDevelopment())
	}

	logger := NewLogger(cfg, nil)
	slog.SetDefault(logger)

	dbManager := sqlite.NewManager(sqlite.Config{
		Path:         cfg.DatabaseDSN(),
		MaxOpenConns: cfg.GetMaxOpenConns(),
		MaxIdleConns: cfg.GetMaxIdleConns(),
		Logger:       logger,
	})

	serverCfg := DefaultServerConfig()
	serverCfg.Config = cfg
	serverCfg.Logger = logger
	serverCfg.DBManager = dbManager
	serverCfg.ErrorHandler = o.errorHandler
	serverCfg.ViewsEngine = newViews(cfg, o.templatesFS, o.templateFuncs)
	if !cfg.IsDevelopment() {
		serverCfg.StaticFS = o.staticFS
	}

	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, fmt.Errorf("cartridge: create server: %w", err)
	}

	var session *SessionManager
	if o.sessionPath != "" {
		session, err = NewSessionManager(SessionConfig{
			CookieName: cfg.GetAppName() + "_session",
			Secret:     cfg.GetSessionSecret(),
			TTL:        time.Duration(cfg.GetSessionTimeout()) * time.Second,
			Insecure:   !cfg.IsProduction(),
			LoginPath:  o.sessionPath,
			Valid:      o.sessionValid,
		})
		if err != nil {
			return nil, err
		}
		server.SetSession(session)
	}

	if o.routes != nil {
		o.routes(server)
	}

	workers := o.workers
	for _, group := range o.jobGroups {
		workers = append(workers, NewJobDispatcher(logger, dbManager, group.interval, group.processors...))
	}

	application, err := NewApplication(ApplicationOptions{
		Config:            cfg,
		Logger:            logger,
		DBManager:         dbManager,
		Server:            server,
		BackgroundWorkers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("cartridge: create application: %w", err)
	}

	return &App{Application: application, DBManager: dbManager, Session: session}, nil
}

// newViews returns nil without templates. Development reads web/templates
// from disk and reloads them on every render.
func newViews(cfg Config, templates fs.FS, funcs template.FuncMap) Views {
	if templates == nil {
		return nil
	}
	if cfg.IsDevelopment() {
		return NewHTMLViews(os.DirFS("web/templates"), funcs, true)
	}
	return NewHTMLViews(templates, funcs, false)
}
