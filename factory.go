package cartridge

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"maps"
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
	Session   *SessionManager            // nil without WithSession
	Databases map[string]*sqlite.Manager // from WithDatabase, by name
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

	// PASSIVE never waits. A FULL checkpoint waits for every reader, and the
	// live replica keeps a read open on purpose, so FULL always ran into
	// busy_timeout (5s) and gave up.
	if err := a.DBManager.CheckpointWAL("PASSIVE"); err != nil {
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
	serverConfig  func(*ServerConfig)
	defaults      string
	sessionPath   string
	readPool      bool
	pragmas       []string
	sessionValid  func(userID uint, issuedAt time.Time) bool
	inertia       bool
	databases     map[string]sqlite.Config
}

type jobGroup struct {
	interval   time.Duration
	processors []Processor
}

// WithAssets sets the embedded templates and static files. Either can be
// nil. Development reads web/templates and the public directory from disk
// instead, and reloads templates on every render. Without templates,
// Context.Render returns an error.
//
// Templates can call {{asset "app.js"}} for the digested URL of a static
// file and {{importmap "controllers/*.js"}} for an import map. See
// Server.Asset and Server.Importmap.
func WithAssets(templates, static fs.FS) AppOption {
	return func(o *appOptions) {
		o.templatesFS = templates
		o.staticFS = static
	}
}

// WithTemplateFuncs adds functions to the templates. A function wins over a
// default function with the same name, such as asset or timeAgo.
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

// WithServerConfig changes the server config after NewApp sets its
// defaults. Use it for AllowedHosts, TrustedProxies, and
// ContentSecurityPolicy:
//
//	cartridge.WithServerConfig(func(c *cartridge.ServerConfig) {
//	    c.AllowedHosts = []string{"example.com"}
//	})
func WithServerConfig(fn func(*ServerConfig)) AppOption {
	return func(o *appOptions) {
		o.serverConfig = fn
	}
}

// WithDefaults loads the stricter defaults that cartridge added up to the
// given version. See ServerConfig.LoadDefaults. A new app uses the newest
// version:
//
//	cartridge.WithDefaults("1.7")
//
// WithServerConfig runs after it, so it can turn one default off again.
func WithDefaults(version string) AppOption {
	return func(o *appOptions) {
		o.defaults = version
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

// WithReadPool opens each SQLite database of the app with two pools: one
// write connection, and a pool of read-only connections. See
// sqlite.Config.ReadPool. App code does not change: a SELECT runs on a read
// connection, and every write and transaction runs on the write
// connection, one at a time.
func WithReadPool() AppOption {
	return func(o *appOptions) {
		o.readPool = true
	}
}

// WithPragmas runs the pragmas on every connection of the main database.
// Cartridge sets WAL, synchronous=NORMAL, busy_timeout, and immediate
// transactions. Each app adds what it needs. See sqlite.Config.Pragmas.
//
//	cartridge.WithPragmas("PRAGMA foreign_keys = ON", "PRAGMA mmap_size = 268435456")
func WithPragmas(pragmas ...string) AppOption {
	return func(o *appOptions) {
		o.pragmas = pragmas
	}
}

// WithDatabase opens another SQLite database under name, next to the main
// one. A handler reads it with ctx.Database(name) and writes it with
// ctx.DatabaseWriteTx(name, fn). The config's Logger, MaxOpenConns, and
// MaxIdleConns default to those of the main database. Set ReadOnly to open
// an existing file read-only:
//
//	cartridge.WithDatabase("shared", sqlite.Config{Path: "data/shared.sqlite3", ReadOnly: true})
//
// The databases are in App.Databases. Run and Shutdown close them.
func WithDatabase(name string, cfg sqlite.Config) AppOption {
	return func(o *appOptions) {
		if o.databases == nil {
			o.databases = map[string]sqlite.Config{}
		}
		o.databases[name] = cfg
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
		ReadPool:     o.readPool,
		Pragmas:      o.pragmas,
		Path:         cfg.DatabaseDSN(),
		MaxOpenConns: cfg.GetMaxOpenConns(),
		MaxIdleConns: cfg.GetMaxIdleConns(),
		Logger:       logger,
	})

	// The template functions call the server, which exists before the first render.
	var server *Server
	funcs := template.FuncMap{
		"asset":     func(name string) (string, error) { return server.Asset(name) },
		"importmap": func(entries ...string) (template.HTML, error) { return server.Importmap(entries...) },
	}
	maps.Copy(funcs, o.templateFuncs)

	databases := map[string]*sqlite.Manager{}
	serverCfg := DefaultServerConfig()
	serverCfg.Config = cfg
	serverCfg.Logger = logger
	serverCfg.DBManager = dbManager
	serverCfg.Databases = map[string]DBManager{}
	for name, dbCfg := range o.databases {
		if name == "" {
			return nil, fmt.Errorf("cartridge: WithDatabase needs a name")
		}
		if dbCfg.Logger == nil {
			dbCfg.Logger = logger
		}
		if o.readPool {
			dbCfg.ReadPool = true
		}

		if dbCfg.MaxOpenConns == 0 {
			dbCfg.MaxOpenConns = cfg.GetMaxOpenConns()
		}
		if dbCfg.MaxIdleConns == 0 {
			dbCfg.MaxIdleConns = cfg.GetMaxIdleConns()
		}
		databases[name] = sqlite.NewManager(dbCfg)
		serverCfg.Databases[name] = databases[name]
	}
	serverCfg.ErrorHandler = o.errorHandler
	serverCfg.ViewsEngine = newViews(cfg, o.templatesFS, funcs)
	if !cfg.IsDevelopment() {
		serverCfg.StaticFS = o.staticFS
	}
	// Only a Vite build, which Inertia apps have, puts a hash in file names.
	serverCfg.StaticNamesHashed = o.inertia
	if o.defaults != "" {
		if err := serverCfg.LoadDefaults(o.defaults); err != nil {
			return nil, err
		}
	}
	if o.serverConfig != nil {
		o.serverConfig(serverCfg)
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
		dispatcher := NewJobDispatcher(logger, dbManager, group.interval, group.processors...)
		dispatcher.Databases = serverCfg.Databases
		workers = append(workers, dispatcher)
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

	return &App{Application: application, DBManager: dbManager, Session: session, Databases: databases}, nil
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
