package cartridge

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"time"

	"github.com/karloscodes/cartridge/cache"
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
	Cache     cache.Store                // nil without WithCache
	Databases map[string]*sqlite.Manager // from WithDatabase, by name
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
	migrations    fs.FS
	serverConfig  func(*ServerConfig)
	defaults      string
	sessionPath   string
	sessionValid  func(userID uint, issuedAt time.Time) bool
	inertia       bool
	databases     map[string]sqlite.Config
	managers      map[string]DBManager
	crons         []cronSpec
	cache         bool
	cacheOptions  []cache.Option
	cacheDatabase string
	cronDatabase  string
}

type cronSpec struct {
	name, spec string
	fn         CronFunc
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

// WithMigrations runs the .sql files of fsys on the main database when
// NewApp builds the app, before the server starts. See Migrate.
//
//	//go:embed migrations/*.sql
//	var files embed.FS
//
//	migrations, _ := fs.Sub(files, "migrations")
//	cartridge.WithMigrations(migrations)
func WithMigrations(fsys fs.FS) AppOption {
	return func(o *appOptions) {
		o.migrations = fsys
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

// WithDatabaseManager adds a database that the app opened itself under
// name, for example a PostgreSQL database on another server:
//
//	shared := database.NewManager(postgres.NewDriver(), database.DefaultConfig(dsn), logger)
//	cartridge.WithDatabaseManager("shared", shared)
//
// A handler reads it with ctx.Database(name). Run and Shutdown close it.
func WithDatabaseManager(name string, m DBManager) AppOption {
	return func(o *appOptions) {
		if o.managers == nil {
			o.managers = map[string]DBManager{}
		}
		o.managers[name] = m
	}
}

// WithCache gives the app a cache in its database, in the table
// cartridge_cache, like Solid Cache in Rails. Every process on the database
// shares it, and it survives a restart. A handler reads it through
// ctx.Cache() and cache.Fetch. The store is in App.Cache.
//
//	cartridge.WithCache(cache.WithTTL(time.Hour), cache.WithMaxEntries(10000))
func WithCache(opts ...cache.Option) AppOption {
	return func(o *appOptions) {
		o.cache = true
		o.cacheOptions = opts
	}
}

// WithCacheDatabase keeps the cache of WithCache in the named database
// (from WithDatabase or WithDatabaseManager), not in the main one. Use it
// to keep cache writes away from the main SQLite file, or to share one
// cache between servers.
func WithCacheDatabase(name string) AppOption {
	return func(o *appOptions) {
		o.cacheDatabase = name
	}
}

// WithCron runs fn on a schedule. See CronScheduler.Add for the name and
// the spec:
//
//	cartridge.WithCron("send-digest", "TZ=Europe/Madrid 0 8 * * *", sendDigest)
//	cartridge.WithCron("sweep-events", "@every 30s", sweepEvents)
//
// The next run of each job is stored in the database, so a restart loses
// no run, and several processes on one database run each tick once.
func WithCron(name, spec string, fn CronFunc) AppOption {
	return func(o *appOptions) {
		o.crons = append(o.crons, cronSpec{name: name, spec: spec, fn: fn})
	}
}

// WithCronDatabase keeps the schedule state of WithCron in the named
// database (from WithDatabase or WithDatabaseManager), not in the main one.
// Use it when several servers share one database for their schedules.
func WithCronDatabase(name string) AppOption {
	return func(o *appOptions) {
		o.cronDatabase = name
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

	if o.migrations != nil {
		writer, err := dbManager.Writer()
		if err != nil {
			return nil, fmt.Errorf("cartridge: connect database: %w", err)
		}
		if err := Migrate(writer, o.migrations); err != nil {
			return nil, err
		}
		// PASSIVE never waits. A FULL checkpoint waits for every reader, and
		// a live replica keeps a read open on purpose, so FULL ran into
		// busy_timeout and gave up.
		if err := dbManager.CheckpointWAL("PASSIVE"); err != nil {
			logger.Warn("failed to checkpoint WAL after migration", slog.Any("error", err))
		}
	}

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
		if dbCfg.MaxOpenConns == 0 {
			dbCfg.MaxOpenConns = cfg.GetMaxOpenConns()
		}
		if dbCfg.MaxIdleConns == 0 {
			dbCfg.MaxIdleConns = cfg.GetMaxIdleConns()
		}
		databases[name] = sqlite.NewManager(dbCfg)
		serverCfg.Databases[name] = databases[name]
	}
	for name, m := range o.managers {
		if name == "" || m == nil {
			return nil, fmt.Errorf("cartridge: WithDatabaseManager needs a name and a manager")
		}
		if _, taken := serverCfg.Databases[name]; taken {
			return nil, fmt.Errorf("cartridge: two databases have the name %q", name)
		}
		serverCfg.Databases[name] = m
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

	var store *cache.DatabaseStore
	if o.cache {
		var m DBManager = dbManager
		if o.cacheDatabase != "" {
			named, ok := serverCfg.Databases[o.cacheDatabase]
			if !ok {
				return nil, fmt.Errorf("cartridge: WithCacheDatabase: no database named %q", o.cacheDatabase)
			}
			m = named
		}
		reader, err := m.Reader()
		if err != nil {
			return nil, fmt.Errorf("cartridge: cache: %w", err)
		}
		writer, err := m.Writer()
		if err != nil {
			return nil, fmt.Errorf("cartridge: cache: %w", err)
		}
		if store, err = cache.NewDatabaseStore(reader, writer, o.cacheOptions...); err != nil {
			return nil, fmt.Errorf("cartridge: cache: %w", err)
		}
		serverCfg.Cache = store
	} else if o.cacheDatabase != "" {
		return nil, fmt.Errorf("cartridge: WithCacheDatabase needs WithCache")
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

	if len(o.crons) > 0 {
		scheduler := NewCronScheduler(logger, dbManager)
		scheduler.Databases = serverCfg.Databases
		if o.cronDatabase != "" {
			state, ok := serverCfg.Databases[o.cronDatabase]
			if !ok {
				return nil, fmt.Errorf("cartridge: WithCronDatabase: no database named %q", o.cronDatabase)
			}
			scheduler.StoreIn(state)
		}
		for _, c := range o.crons {
			if err := scheduler.Add(c.name, c.spec, c.fn); err != nil {
				return nil, err
			}
		}
		workers = append(workers, scheduler)
	} else if o.cronDatabase != "" {
		return nil, fmt.Errorf("cartridge: WithCronDatabase needs WithCron")
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

	app := &App{Application: application, DBManager: dbManager, Session: session, Databases: databases}
	if store != nil {
		app.Cache = store
	}
	return app, nil
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
