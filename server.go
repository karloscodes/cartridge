package cartridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Views renders named templates. HTMLViews implements it.
type Views interface {
	Render(w io.Writer, name string, data any, layouts ...string) error
}

// ServerConfig provides comprehensive server configuration with sensible defaults.
type ServerConfig struct {
	// Core dependencies (required)
	Config    Config
	Logger    Logger
	DBManager DBManager

	// Databases holds more databases by name, for Context.Database and
	// Context.DatabaseWriteTx. NewApp fills it from WithDatabase.
	Databases map[string]DBManager

	// HTTP server configuration
	ErrorHandler ErrorHandler
	// ReadTimeout limits the time to read a request. Default: 30s.
	ReadTimeout time.Duration
	// WriteTimeout limits the time to write a response. Default: 30s. A
	// stream (server-sent events, a large download) lifts it per request:
	//
	//	http.NewResponseController(ctx.Response()).SetWriteDeadline(time.Time{})
	WriteTimeout time.Duration
	// BodyLimit is the maximum request body in bytes. Default: 4 MB.
	//
	// NewServer sets the default for each of these three when it is 0. A
	// negative value turns the limit off.
	BodyLimit int

	// ProxyHeader names the header that Context.IP reads the client address
	// from, for example "X-Forwarded-For". TrustedProxies lists the IPs or
	// CIDRs of your proxies. Context.IP and Context.Protocol read proxy
	// headers only from these peers. Empty trusts no peer.
	//
	// Context.IP walks the header from right to left past every trusted
	// address. So list only your own proxies. If you list whole private
	// ranges and clients can also come from a private network, such a client
	// can add a fake private address and choose its own IP. Protocol is not
	// affected.
	ProxyHeader    string
	TrustedProxies []string

	// AllowedHosts lists the Host header values this server answers, for
	// example "example.com". An entry matches with or without the port. An
	// entry that starts with a dot, ".example.com", also allows every
	// subdomain. A
	// request for another host gets a 400, so a forged Host header cannot
	// reach Context.Hostname or Context.BaseURL and the links built from
	// them. Loopback hosts (localhost, 127.0.0.1, ::1) always pass, so
	// health checks work. Empty allows every host.
	AllowedHosts []string

	// BlockExternalRedirects makes Context.Redirect refuse a location on
	// another host: it returns an error, and the client gets a 500. A
	// redirect that must leave the site uses Context.RedirectExternal. So a
	// request value that reaches Redirect cannot send the user to another
	// site. LoadDefaults("1.7") turns it on.
	BlockExternalRedirects bool

	// ViewsEngine renders templates for Context.Render.
	ViewsEngine Views

	// Static assets configuration
	EnableStaticAssets bool
	StaticFS           fs.FS  // Embedded filesystem for static assets (production), served under StaticPrefix
	StaticDirectory    string // Directory for static assets (development)
	StaticPrefix       string
	// StaticNamesHashed says that the names of the embedded static files
	// hold a hash of their content, as in a Vite build. The server then
	// sends a file at its plain URL with a one-year cache. When it is false,
	// a plain URL is sent with "Cache-Control: no-cache" and an ETag, so a
	// deploy with a changed file reaches the browser; link such files with
	// Server.Asset for the one-year cache. DefaultServerConfig sets it to
	// true. NewApp sets it to true only with WithInertia.
	StaticNamesHashed bool
	PublicFS          fs.FS  // Root-level public files (favicon.svg, robots.txt), served at / (production)
	PublicDirectory   string // Directory for public files in development (e.g. "web/public")

	// Middleware configuration
	EnableRequestID bool
	EnableRecover   bool
	EnableHelmet    bool // Security headers, see SecurityHeaders

	// ContentSecurityPolicy is the Content-Security-Policy header value.
	// EnableHelmet sends it on every response when it is not empty.
	ContentSecurityPolicy string
	EnableCompress        bool
	EnableSecFetchSite    bool // CSRF protection via Sec-Fetch-Site header
	EnableRequestLogger   bool

	// SecFetchSite configuration
	// Allowed values for Sec-Fetch-Site header. Default: ["same-origin", "none"]
	// For cross-origin APIs (analytics, public endpoints): ["cross-site", "same-site", "same-origin"]
	SecFetchSiteAllowedValues []string

	// Concurrency configuration (for SQLite WAL mode)
	MaxConcurrentWrites int
	ConcurrencyTimeout  time.Duration
}

// Limits that NewServer sets when the config leaves them at 0.
const (
	defaultTimeout   = 30 * time.Second
	defaultBodyLimit = 4 * 1024 * 1024
)

// versionedDefaults lists, in order, the stricter defaults that each
// cartridge version added. A new default that can break an app goes here,
// not into DefaultServerConfig.
var versionedDefaults = []struct {
	version string
	apply   func(*ServerConfig)
}{
	{"1.6", func(*ServerConfig) {}},
	{"1.7", func(c *ServerConfig) { c.BlockExternalRedirects = true }},
}

// LoadDefaults turns on the stricter defaults that cartridge added up to
// and including the given version, such as "1.7". It works like
// load_defaults in Rails: a release can add a safer default without a
// change for apps that did not ask for it. A new app loads the newest
// version. An older app raises the version when it is ready, and can turn
// one default off again after the call:
//
//	cfg := cartridge.DefaultServerConfig()
//	if err := cfg.LoadDefaults("1.7"); err != nil { ... }
//	cfg.BlockExternalRedirects = false
//
// Without LoadDefaults, the server behaves as in 1.6. An unknown version is
// an error.
func (c *ServerConfig) LoadDefaults(version string) error {
	last := -1
	for i, d := range versionedDefaults {
		if d.version == version {
			last = i
		}
	}
	if last < 0 {
		known := make([]string, len(versionedDefaults))
		for i, d := range versionedDefaults {
			known[i] = d.version
		}
		return fmt.Errorf("cartridge: no defaults for version %q, want one of %s", version, strings.Join(known, ", "))
	}
	for _, d := range versionedDefaults[:last+1] {
		d.apply(c)
	}
	return nil
}

// DefaultServerConfig returns a configuration with sensible defaults.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		// Server defaults
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
		BodyLimit:    defaultBodyLimit,

		// Static assets
		EnableStaticAssets: true,
		StaticPrefix:       "/assets",
		StaticNamesHashed:  true,

		// Middleware defaults (all enabled)
		EnableRequestID:     true,
		EnableRecover:       true,
		EnableHelmet:        true,
		EnableCompress:      true,
		EnableSecFetchSite:  true,
		EnableRequestLogger: true,

		// Concurrency defaults optimized for SQLite WAL mode
		MaxConcurrentWrites: 8,
		ConcurrencyTimeout:  5 * time.Second,
	}
}

// RouteConfig allows per-route middleware customization.
type RouteConfig struct {
	// EnableCORS enables CORS for this route. A CORS route without its own
	// OPTIONS route gets one that answers preflight requests.
	EnableCORS bool
	CORSConfig *CORSConfig

	// WriteConcurrency enables write concurrency limiting for this route.
	WriteConcurrency bool

	// EnableSecFetchSite controls CSRF protection for this route. nil follows
	// ServerConfig.EnableSecFetchSite. Bool(false) opts out (public or
	// cross-origin routes); Bool(true) opts in even when the server default is off.
	EnableSecFetchSite *bool

	// CustomMiddleware are additional middleware to run before the handler.
	CustomMiddleware []HandlerFunc
}

// Bool returns a pointer to a bool value. Useful for optional config fields.
func Bool(v bool) *bool { return &v }

// defaultCORSConfig is the CORS policy for a route that enables CORS without its own config.
var defaultCORSConfig = CORSConfig{
	AllowOrigins: "*",
	AllowMethods: "GET,POST,PUT,DELETE,PATCH,OPTIONS",
	AllowHeaders: "Origin, Content-Type, Accept, Authorization",
}

// route is a registered route, kept until the server builds its mux.
type route struct {
	method   string
	path     string
	handlers []HandlerFunc
	cors     *CORSConfig
}

// Server is the cartridge HTTP server with a clean route registration API.
type Server struct {
	cfg            *ServerConfig
	limiter        *ConcurrencyLimiter
	catchAll       string
	session        *SessionManager
	global         []HandlerFunc
	routes         []route
	trustedProxies []netip.Prefix

	digestsOnce sync.Once
	digests     *assetDigests
	digestsErr  error

	buildOnce  sync.Once
	mux        *http.ServeMux
	notFound   http.Handler
	badHost    http.Handler
	mu         sync.Mutex
	httpServer *http.Server
}

// Session returns the session manager. Returns nil if sessions are not enabled.
func (s *Server) Session() *SessionManager {
	return s.session
}

// SetSession sets the session manager. Called by the factory after creation.
func (s *Server) SetSession(sm *SessionManager) {
	s.session = sm
}

// NewServer creates a new cartridge server with the provided configuration.
func NewServer(cfg *ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cartridge: config is required")
	}
	if cfg.Config == nil {
		return nil, fmt.Errorf("cartridge: runtime config is required")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("cartridge: logger is required")
	}
	if cfg.DBManager == nil {
		return nil, fmt.Errorf("cartridge: database manager is required")
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = DefaultErrorHandler(cfg.Logger, cfg.Config.IsDevelopment())
	}
	// A ServerConfig built without DefaultServerConfig still gets the limits.
	// Without them, any client can send an endless body or hold a connection open.
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = defaultTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultTimeout
	}
	if cfg.BodyLimit == 0 {
		cfg.BodyLimit = defaultBodyLimit
	}

	trusted, err := parsePrefixes(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	server := &Server{
		cfg: cfg,
		limiter: NewConcurrencyLimiter(
			int64(cfg.MaxConcurrentWrites),
			cfg.ConcurrencyTimeout,
			cfg.Logger,
		),
		trustedProxies: trusted,
	}
	server.setupGlobalMiddleware()
	return server, nil
}

// setupGlobalMiddleware lists the middleware that runs on every request.
func (s *Server) setupGlobalMiddleware() {
	if s.cfg.EnableRequestID {
		s.global = append(s.global, RequestID())
	}
	if s.cfg.EnableRequestLogger {
		s.global = append(s.global, RequestLogger(s.cfg.Logger))
	}
	if s.cfg.EnableRecover {
		s.global = append(s.global, Recover())
	}
	if s.cfg.EnableHelmet {
		s.global = append(s.global, serverSecurityHeaders(s.cfg))
	}
	if s.cfg.EnableCompress {
		s.global = append(s.global, Compress())
	}
	// SecFetchSite CSRF protection is applied per route in registerRoute,
	// so routes can opt out with EnableSecFetchSite: Bool(false).
}

// Use adds middleware that runs on every request, after the built-in middleware.
func (s *Server) Use(middleware ...HandlerFunc) {
	if s.mux != nil {
		panic("cartridge: middleware added after the server started")
	}
	s.global = append(s.global, middleware...)
}

// SetCatchAllRedirect configures a fallback redirect for unmatched routes.
func (s *Server) SetCatchAllRedirect(path string) {
	s.catchAll = path
}

// Get registers a GET route. It also answers HEAD requests.
func (s *Server) Get(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodGet, path, handler, cfg...)
}

// Post registers a POST route.
func (s *Server) Post(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodPost, path, handler, cfg...)
}

// Put registers a PUT route.
func (s *Server) Put(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodPut, path, handler, cfg...)
}

// Delete registers a DELETE route.
func (s *Server) Delete(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodDelete, path, handler, cfg...)
}

// Patch registers a PATCH route.
func (s *Server) Patch(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodPatch, path, handler, cfg...)
}

// Options registers an OPTIONS route.
func (s *Server) Options(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodOptions, path, handler, cfg...)
}

// Head registers a HEAD route.
func (s *Server) Head(path string, handler HandlerFunc, cfg ...*RouteConfig) {
	s.registerRoute(http.MethodHead, path, handler, cfg...)
}

// registerRoute records a route with its middleware chain.
// Chain order: SecFetchSite, CORS, write concurrency, custom middleware, handler.
func (s *Server) registerRoute(method, path string, handler HandlerFunc, cfgs ...*RouteConfig) {
	if s.mux != nil {
		panic("cartridge: route " + method + " " + path + " registered after the server started")
	}

	var routeCfg *RouteConfig
	if len(cfgs) > 0 {
		routeCfg = cfgs[0]
	}

	var handlers []HandlerFunc

	// The server default decides, and a route's EnableSecFetchSite overrides
	// it either way: Bool(false) opts a route out, Bool(true) opts it in.
	secFetch := s.cfg.EnableSecFetchSite
	if routeCfg != nil && routeCfg.EnableSecFetchSite != nil {
		secFetch = *routeCfg.EnableSecFetchSite
	}
	if secFetch {
		secFetchCfg := SecFetchSiteConfig{}
		if len(s.cfg.SecFetchSiteAllowedValues) > 0 {
			secFetchCfg.AllowedValues = s.cfg.SecFetchSiteAllowedValues
		}
		handlers = append(handlers, SecFetchSiteMiddleware(secFetchCfg))
	}

	var corsCfg *CORSConfig
	if routeCfg != nil {
		if routeCfg.EnableCORS {
			corsCfg = routeCfg.CORSConfig
			if corsCfg == nil {
				corsCfg = &defaultCORSConfig
			}
			handlers = append(handlers, CORS(*corsCfg))
		}
		if routeCfg.WriteConcurrency {
			handlers = append(handlers, WriteConcurrencyLimitMiddleware(s.limiter))
		}
		handlers = append(handlers, routeCfg.CustomMiddleware...)
	}
	handlers = append(handlers, handler)

	s.routes = append(s.routes, route{method: method, path: path, handlers: handlers, cors: corsCfg})
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.buildOnce.Do(s.build)

	if !hostAllowed(r.Host, s.cfg.AllowedHosts) {
		s.badHost.ServeHTTP(w, r)
		return
	}

	// Like Fiber, "/admin/" matches the route "/admin". Trim the slash only
	// when no route but the fallback matches, so "/assets/" still reaches the
	// "/assets/" subtree instead of redirecting to itself.
	if p := r.URL.Path; len(p) > 1 && strings.HasSuffix(p, "/") && s.onlyFallbackMatches(r) {
		r.URL.Path = strings.TrimRight(p, "/")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		r.URL.RawPath = ""
	}
	// ServeMux auto-redirects an unclean path ("//x", "/a/../b", "/a//b") to
	// its cleaned form with a 307. Fiber 404'd these, so match that: serve the
	// not-found handler instead of leaking a redirect before the middleware runs.
	// Compare against the cleaned path, but allow one trailing slash: that is
	// matched separately (Fiber let "/admin/" hit "/admin", "/files/" hit the
	// "/files/*" subtree). "//x", "/a/../b", "/a//b" still 404.
	if p := strings.TrimSuffix(r.URL.Path, "/"); p != "" && p != path.Clean(p) {
		s.notFound.ServeHTTP(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// fallbackPattern is the pattern of the 404 or catch-all route.
const fallbackPattern = "/"

// onlyFallbackMatches reports whether no route but the fallback matches r.
func (s *Server) onlyFallbackMatches(r *http.Request) bool {
	_, pattern := s.mux.Handler(r)
	return pattern == fallbackPattern
}

// Test serves req in memory and returns the response, like Fiber's app.Test.
// The request comes from 0.0.0.0; to test another peer address, call
// ServeHTTP with net/http/httptest. The optional timeout is in milliseconds:
// when the handler runs longer, Test returns an error. Without a timeout,
// or with 0 or less, Test waits for the handler.
func (s *Server) Test(req *http.Request, timeout ...int) (*http.Response, error) {
	req.RemoteAddr = "0.0.0.0:0"
	rec := httptest.NewRecorder()
	if len(timeout) == 0 || timeout[0] <= 0 {
		s.ServeHTTP(rec, req)
		return rec.Result(), nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(rec, req)
	}()
	select {
	case <-done:
		return rec.Result(), nil
	case <-time.After(time.Duration(timeout[0]) * time.Millisecond):
		return nil, fmt.Errorf("cartridge: test request took longer than %dms", timeout[0])
	}
}

// build turns the registered routes into a ServeMux.
func (s *Server) build() {
	mux := http.NewServeMux()

	s.mountStaticAssets(mux)
	s.mountPublicFiles(mux)

	explicitOptions := map[string]bool{}
	for _, rt := range s.routes {
		if rt.method == http.MethodOptions {
			explicitOptions[normalizePath(rt.path)] = true
		}
	}

	for _, rt := range s.routes {
		pattern, params := muxPattern(rt.path)
		mux.Handle(rt.method+" "+pattern, s.chain(params, rt.handlers...))

		// A CORS route needs an OPTIONS route for browser preflight requests.
		key := normalizePath(rt.path)
		if rt.cors != nil && !explicitOptions[key] {
			explicitOptions[key] = true
			mux.Handle(http.MethodOptions+" "+pattern, s.chain(params, CORS(*rt.cors), func(c *Context) error {
				return c.SendStatus(http.StatusNoContent)
			}))
		}
	}

	notFound := s.chain(nil, func(c *Context) error {
		return NewError(http.StatusNotFound)
	})
	if s.catchAll != "" {
		mux.Handle(fallbackPattern, s.chain(nil, func(c *Context) error {
			return c.Redirect(s.catchAll, http.StatusTemporaryRedirect)
		}))
	} else {
		mux.Handle(fallbackPattern, notFound)
	}

	s.notFound = notFound
	s.badHost = s.chain(nil, func(c *Context) error {
		return NewError(http.StatusBadRequest, "unknown host")
	})
	s.mux = mux
}

// chain builds an http.Handler that runs the global middleware, then handlers.
func (s *Server) chain(params []string, handlers ...HandlerFunc) http.Handler {
	all := make([]HandlerFunc, 0, len(s.global)+len(handlers))
	all = append(all, s.global...)
	all = append(all, handlers...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := &responseWriter{ResponseWriter: w}
		ctx := &Context{
			Logger:    s.cfg.Logger,
			Config:    s.cfg.Config,
			DBManager: s.cfg.DBManager,
			Session:   s.session,
			w:         base,
			base:      base,
			r:         r,
			server:    s,
			handlers:  all,
			index:     -1,
		}
		if len(params) > 0 {
			ctx.Locals(paramNamesKey{}, params)
		}

		if limit := int64(s.cfg.BodyLimit); limit > 0 && r.Body != nil {
			if r.ContentLength > limit {
				s.handleError(ctx, NewError(http.StatusRequestEntityTooLarge))
				return
			}
			r.Body = http.MaxBytesReader(base, r.Body, limit)
		}

		if err := ctx.Next(); err != nil {
			s.handleError(ctx, err)
		}
	})
}

// handleError runs the error handler, unless the response already started.
func (s *Server) handleError(ctx *Context, err error) {
	if ctx.base.wroteHeader {
		s.cfg.Logger.Error("error after response started", slog.Any("error", err), slog.String("path", ctx.Path()))
		return
	}
	if herr := s.cfg.ErrorHandler(ctx, err); herr != nil && !ctx.base.wroteHeader {
		http.Error(ctx.base, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// mountStaticAssets serves static files under StaticPrefix.
func (s *Server) mountStaticAssets(mux *http.ServeMux) {
	fsys, prefix, embedded := s.staticFiles()
	if fsys == nil {
		return
	}
	files := http.StripPrefix(prefix, http.FileServerFS(fsys))
	mux.Handle("GET "+prefix+"/", s.chain(nil, func(c *Context) error {
		name := strings.TrimPrefix(c.Path(), prefix+"/")
		if hasDotSegment(name) {
			return NewError(http.StatusNotFound)
		}
		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
			// A name with a content hash (Vite) can be cached for a year.
			// A plain name can get new content at any deploy, and a file on
			// disk at any time, so the browser must check them on each use.
			// The ETag makes that check a 304 for an embedded file, which
			// has no modification time.
			switch {
			case embedded && s.cfg.StaticNamesHashed:
				c.Set("Cache-Control", "public, max-age=31536000")
			case embedded:
				c.Set("Cache-Control", "no-cache")
				if digests, err := s.assetDigests(); err == nil {
					c.Set("ETag", digests.etag(name))
				}
			default:
				c.Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(c.Response(), c.Request())
			return nil
		}

		// A digested name from Server.Asset.
		digests, err := s.assetDigests()
		if err != nil {
			return NewError(http.StatusNotFound)
		}
		original, ok := digests.original(name)
		if !ok {
			return NewError(http.StatusNotFound)
		}
		if embedded {
			c.Set("Cache-Control", immutableCacheControl)
		} else {
			c.Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(c.Response(), c.Request(), fsys, original)
		return nil
	}))
}

// hasDotSegment reports whether a segment of the slash-separated name starts
// with a dot, like ".env", ".git/config", or ".DS_Store". These files are
// never public.
func hasDotSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

// mountPublicFiles serves root-level public files (favicon.svg, robots.txt, etc.).
// In production, serves from PublicFS (embedded). In development, serves from PublicDirectory (disk).
func (s *Server) mountPublicFiles(mux *http.ServeMux) {
	var publicFS fs.FS
	if s.cfg.PublicFS != nil {
		publicFS = s.cfg.PublicFS
	} else if s.cfg.PublicDirectory != "" {
		publicFS = os.DirFS(s.cfg.PublicDirectory)
	} else {
		return
	}

	entries, err := fs.ReadDir(publicFS, ".")
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		pattern, _ := muxPattern("/" + name)
		mux.Handle("GET "+pattern, s.chain(nil, func(c *Context) error {
			http.ServeFileFS(c.Response(), c.Request(), publicFS, name)
			return nil
		}))
	}
}

// GetLogger returns the logger.
func (s *Server) GetLogger() Logger {
	return s.cfg.Logger
}

// GetDBManager returns the database manager.
func (s *Server) GetDBManager() DBManager {
	return s.cfg.DBManager
}

// Start starts the HTTP server on the configured port. It blocks, and
// returns nil after a graceful Shutdown.
func (s *Server) Start() error {
	ln, err := s.listen()
	if err != nil {
		return err
	}
	return s.serve(ln)
}

// StartAsync binds the port, then serves in a goroutine. It returns the
// bind error, for example when the port is in use.
func (s *Server) StartAsync() error {
	ln, err := s.listen()
	if err != nil {
		return err
	}
	go func() {
		if err := s.serve(ln); err != nil {
			s.cfg.Logger.Error("Server error", "error", err)
		}
	}()
	return nil
}

// listen builds the http.Server and binds the configured port.
func (s *Server) listen() (net.Listener, error) {
	s.buildOnce.Do(s.build)

	port := s.cfg.Config.GetPort()
	ln, err := net.Listen("tcp", net.JoinHostPort(listenHost(s.cfg.Config), port))
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.httpServer = &http.Server{
		Handler:           s,
		ReadTimeout:       s.cfg.ReadTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      s.cfg.WriteTimeout,
	}
	s.mu.Unlock()

	s.cfg.Logger.Info("Server started and ready to accept requests", "port", port)
	return ln, nil
}

// listenHost returns the address to bind. A Config with a GetHost method
// chooses it. Otherwise production binds every interface, and development
// and test bind loopback only: they run with a public session secret and
// detailed error pages, so the network must not reach them.
func listenHost(cfg Config) string {
	if h, ok := cfg.(interface{ GetHost() string }); ok && h.GetHost() != "" {
		return h.GetHost()
	}
	if cfg.IsProduction() {
		return ""
	}
	return "127.0.0.1"
}

// serve accepts connections on ln until Shutdown.
func (s *Server) serve(ln net.Listener) error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the server. It waits for open requests
// until ctx is done.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// hostAllowed reports whether the Host header value is in allowed, with or
// without its port. An empty list allows every host. Loopback hosts always pass.
func hostAllowed(host string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	if strings.EqualFold(name, "localhost") || name == "127.0.0.1" || name == "::1" {
		return true
	}
	lower := strings.ToLower(name)
	for _, a := range allowed {
		// ".example.com" allows example.com and its subdomains. Only a plain
		// host name can match by its end: text such as
		// "evil.com#x.example.com" also ends in ".example.com".
		if domain, ok := strings.CutPrefix(strings.ToLower(a), "."); ok {
			if plainHostName(lower) && (lower == domain || strings.HasSuffix(lower, "."+domain)) {
				return true
			}
			continue
		}
		if strings.EqualFold(a, host) || strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

// plainHostName reports whether name holds only letters, digits, dashes,
// and dots, and starts with a letter or a digit.
func plainHostName(name string) bool {
	if name == "" || name[0] == '.' || name[0] == '-' {
		return false
	}
	for _, r := range name {
		letter := 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
		digit := '0' <= r && r <= '9'
		if !letter && !digit && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// trustsProxy reports whether ip is in TrustedProxies.
func (s *Server) trustsProxy(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return s.trustsAddr(addr)
}

// trustsAddr reports whether addr is in TrustedProxies.
func (s *Server) trustsAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range s.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func parsePrefixes(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("cartridge: invalid trusted proxy %q", s)
		}
		out = append(out, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
	}
	return out, nil
}

var fiberParam = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// muxPattern converts a Fiber-style path ("/users/:id", "/files/*") to a
// ServeMux pattern ("/users/{id}", "/files/{wildcard...}") and lists its
// parameter names.
func muxPattern(path string) (string, []string) {
	path = normalizePath(path)
	if path == "/" {
		return "/{$}", nil
	}

	var params []string
	for _, m := range fiberParam.FindAllStringSubmatch(path, -1) {
		params = append(params, m[1])
	}
	pattern := fiberParam.ReplaceAllString(path, "{$1}")
	if strings.HasSuffix(pattern, "/*") {
		pattern = strings.TrimSuffix(pattern, "*") + "{wildcard...}"
		params = append(params, "wildcard")
	} else if strings.ContainsAny(pattern, "*?+") {
		panic("cartridge: unsupported route pattern " + path)
	}
	return pattern, params
}

// normalizePath drops a trailing slash, like the request paths in ServeHTTP.
func normalizePath(path string) string {
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if path == "" {
		return "/"
	}
	return path
}
