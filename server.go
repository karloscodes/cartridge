package cartridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
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

	// HTTP server configuration
	ErrorHandler ErrorHandler
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	BodyLimit    int // Maximum request body in bytes. Default: 4 MB

	// ProxyHeader names the header that Context.IP reads the client address
	// from, for example "X-Forwarded-For". TrustedProxies lists the IPs or
	// CIDRs whose proxy headers count. Empty trusts every peer.
	ProxyHeader    string
	TrustedProxies []string

	// Template engine configuration
	EnableTemplates    bool
	TemplatesFS        fs.FS  // Embedded filesystem for templates (production)
	TemplatesDirectory string // Directory for templates (development)
	ViewsEngine        Views

	// Static assets configuration
	EnableStaticAssets bool
	StaticFS           fs.FS  // Embedded filesystem for static assets (production), served under StaticPrefix
	StaticDirectory    string // Directory for static assets (development)
	StaticPrefix       string
	PublicFS           fs.FS  // Root-level public files (favicon.svg, robots.txt), served at / (production)
	PublicDirectory    string // Directory for public files in development (e.g. "web/public")

	// Middleware configuration
	EnableRequestID     bool
	EnableRecover       bool
	EnableHelmet        bool // Security headers
	EnableCompress      bool
	EnableSecFetchSite  bool // CSRF protection via Sec-Fetch-Site header
	EnableRequestLogger bool

	// SecFetchSite configuration
	// Allowed values for Sec-Fetch-Site header. Default: ["same-origin", "none"]
	// For cross-origin APIs (analytics, public endpoints): ["cross-site", "same-site", "same-origin"]
	SecFetchSiteAllowedValues []string

	// Concurrency configuration (for SQLite WAL mode)
	MaxConcurrentReads  int
	MaxConcurrentWrites int
	ConcurrencyTimeout  time.Duration
}

// DefaultServerConfig returns a configuration with sensible defaults.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		// Server defaults
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		BodyLimit:    4 * 1024 * 1024,

		// Static assets
		EnableStaticAssets: true,
		StaticPrefix:       "/assets",

		// Middleware defaults (all enabled)
		EnableTemplates:     true,
		EnableRequestID:     true,
		EnableRecover:       true,
		EnableHelmet:        true,
		EnableCompress:      true,
		EnableSecFetchSite:  true,
		EnableRequestLogger: true,

		// Concurrency defaults optimized for SQLite WAL mode
		MaxConcurrentReads:  128,
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

	buildOnce  sync.Once
	mux        *http.ServeMux
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
		cfg.ErrorHandler = createDefaultErrorHandler(cfg.Logger)
	}

	trusted, err := parsePrefixes(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	server := &Server{
		cfg: cfg,
		limiter: NewConcurrencyLimiter(
			int64(cfg.MaxConcurrentReads),
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
		s.global = append(s.global, SecurityHeaders())
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
	s.mux.ServeHTTP(w, r)
}

// fallbackPattern is the pattern of the 404 or catch-all route.
const fallbackPattern = "/"

// onlyFallbackMatches reports whether no route but the fallback matches r.
func (s *Server) onlyFallbackMatches(r *http.Request) bool {
	_, pattern := s.mux.Handler(r)
	return pattern == fallbackPattern
}

// Test serves req in memory and returns the response, like Fiber's app.Test:
// the request comes from 0.0.0.0, and the timeout argument is ignored. To
// test a specific peer address, call ServeHTTP with net/http/httptest.
func (s *Server) Test(req *http.Request, timeout ...int) (*http.Response, error) {
	req.RemoteAddr = "0.0.0.0:0"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Result(), nil
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

	if s.catchAll != "" {
		mux.Handle(fallbackPattern, s.chain(nil, func(c *Context) error {
			return c.Redirect(s.catchAll, http.StatusTemporaryRedirect)
		}))
	} else {
		mux.Handle(fallbackPattern, s.chain(nil, func(c *Context) error {
			return NewError(http.StatusNotFound, "Cannot "+c.Method()+" "+c.Path())
		}))
	}

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
	if !s.cfg.EnableStaticAssets {
		return
	}

	prefix := strings.TrimSuffix(s.cfg.StaticPrefix, "/")
	if prefix == "" {
		prefix = "/assets"
	}

	fsys := s.cfg.StaticFS
	embedded := fsys != nil
	if fsys == nil {
		dir := s.cfg.StaticDirectory
		if dir == "" {
			dir = s.cfg.Config.GetPublicDirectory()
		}
		if dir == "" {
			return
		}
		fsys = os.DirFS(dir)
	}

	files := http.StripPrefix(prefix, http.FileServerFS(fsys))
	mux.Handle("GET "+prefix+"/", s.chain(nil, func(c *Context) error {
		name := strings.TrimPrefix(c.Path(), prefix+"/")
		info, err := fs.Stat(fsys, name)
		if err != nil || info.IsDir() {
			return NewError(http.StatusNotFound, "Cannot "+c.Method()+" "+c.Path())
		}
		// Vite puts a content hash in built file names, so embedded assets
		// can be cached for a year. Development serves from disk, uncached.
		if embedded {
			c.Set("Cache-Control", "public, max-age=31536000")
		}
		files.ServeHTTP(c.Response(), c.Request())
		return nil
	}))
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
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		pattern, _ := muxPattern("/" + name)
		mux.Handle("GET "+pattern, s.chain(nil, func(c *Context) error {
			http.ServeFileFS(c.Response(), c.Request(), publicFS, name)
			return nil
		}))
	}
}

// GetLimiter returns the concurrency limiter.
func (s *Server) GetLimiter() *ConcurrencyLimiter {
	return s.limiter
}

// GetLogger returns the logger.
func (s *Server) GetLogger() Logger {
	return s.cfg.Logger
}

// GetDBManager returns the database manager.
func (s *Server) GetDBManager() DBManager {
	return s.cfg.DBManager
}

// Start starts the HTTP server on the configured port. It returns nil after
// a graceful Shutdown.
func (s *Server) Start() error {
	s.buildOnce.Do(s.build)

	port := s.cfg.Config.GetPort()
	s.mu.Lock()
	s.httpServer = &http.Server{
		Addr:              ":" + port,
		Handler:           s,
		ReadTimeout:       s.cfg.ReadTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      s.cfg.WriteTimeout,
	}
	srv := s.httpServer
	s.mu.Unlock()

	s.cfg.Logger.Info("Server started and ready to accept requests", "port", port)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// StartAsync starts the server in a goroutine.
func (s *Server) StartAsync() error {
	go func() {
		if err := s.Start(); err != nil {
			s.cfg.Logger.Error("Server error", "error", err)
		}
	}()
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

// trustsProxy reports whether proxy headers from ip count.
func (s *Server) trustsProxy(ip string) bool {
	if len(s.trustedProxies) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
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

// createDefaultErrorHandler creates a default error handler.
func createDefaultErrorHandler(logger Logger) ErrorHandler {
	return func(c *Context, err error) error {
		code := errorCode(err)

		logger.Error("Request error",
			slog.Any("error", err),
			slog.Int("status", code),
			slog.String("path", c.Path()),
			slog.String("method", c.Method()),
		)

		// JSON error response for API requests
		if c.Accepts("application/json") == "application/json" {
			return c.Status(code).JSON(Map{
				"error":   "internal_server_error",
				"message": err.Error(),
			})
		}

		// Fallback text response
		return c.Status(code).SendString(fmt.Sprintf("Error: %d - %s", code, err.Error()))
	}
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
