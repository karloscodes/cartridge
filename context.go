package cartridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Map is a shortcut for map[string]any, handy for JSON responses and template data.
type Map = map[string]any

// HandlerFunc is the signature for cartridge request handlers and middleware.
// Middleware calls ctx.Next() to run the rest of the chain.
type HandlerFunc func(*Context) error

// Cookie describes a cookie to set on the response.
type Cookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	MaxAge   int
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite string // "Lax", "Strict", "None", or "" for the browser default
}

// Context carries one request through its handler chain. It wraps the
// http.ResponseWriter and *http.Request, and gives direct access to the
// logger, config, database manager, and session manager.
type Context struct {
	Logger    Logger          // Request logger (shared across app)
	Config    Config          // Runtime configuration
	DBManager DBManager       // Database connection pool
	Session   *SessionManager // Session management (may be nil if not configured)

	w        http.ResponseWriter // current writer; middleware (gzip) may wrap it
	base     *responseWriter     // the unwrapped writer, which tracks the status
	r        *http.Request
	server   *Server
	handlers []HandlerFunc
	index    int
	status   int
	locals   map[any]any
	body     []byte
	bodyRead bool
	db       *gorm.DB // Cached database session (lazy-loaded)
}

// DB provides a per-request database session with context attached.
// The connection is cached after first call within the same request.
// Panics if the database connection fails (caught by recover middleware).
func (ctx *Context) DB() *gorm.DB {
	if ctx.db != nil {
		return ctx.db
	}

	db := ctx.DBManager.GetConnection()
	if db == nil {
		if ctx.Logger != nil {
			ctx.Logger.Error("failed to get database connection")
		}
		panic("cartridge: database connection failed")
	}

	// Attach the request context for cancellation support and cache it
	ctx.db = db.WithContext(ctx.Context())
	return ctx.db
}

// Next runs the next handler in the chain.
func (ctx *Context) Next() error {
	ctx.index++
	if ctx.index < len(ctx.handlers) {
		return ctx.handlers[ctx.index](ctx)
	}
	return nil
}

// Request returns the underlying *http.Request.
func (ctx *Context) Request() *http.Request { return ctx.r }

// Response returns the http.ResponseWriter for this request.
func (ctx *Context) Response() http.ResponseWriter { return ctx.w }

// Context returns the request's context.Context.
func (ctx *Context) Context() context.Context { return ctx.r.Context() }

// Method returns the HTTP method.
func (ctx *Context) Method() string { return ctx.r.Method }

// Path returns the URL path.
func (ctx *Context) Path() string { return ctx.r.URL.Path }

// OriginalURL returns the request URI with its query string.
func (ctx *Context) OriginalURL() string { return ctx.r.URL.RequestURI() }

// Get returns a request header, or defaultValue if it is empty.
func (ctx *Context) Get(key string, defaultValue ...string) string {
	return orDefault(ctx.r.Header.Get(key), defaultValue)
}

// Set sets a response header.
func (ctx *Context) Set(key, value string) { ctx.w.Header().Set(key, value) }

// Vary adds fields to the Vary response header.
func (ctx *Context) Vary(fields ...string) {
	for _, f := range fields {
		ctx.w.Header().Add("Vary", f)
	}
}

// Params returns a route parameter, or defaultValue if it is empty.
// The route "/users/:id" has the parameter "id", and "/files/*" has "*".
func (ctx *Context) Params(key string, defaultValue ...string) string {
	if key == "*" {
		key = "wildcard"
	}
	return orDefault(ctx.r.PathValue(key), defaultValue)
}

// ParamsInt returns a route parameter as an int. When the parameter is not a
// number, it returns defaultValue if given, and an error if not.
func (ctx *Context) ParamsInt(key string, defaultValue ...int) (int, error) {
	v, err := strconv.Atoi(ctx.Params(key))
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0], nil
		}
		return 0, fmt.Errorf("failed to convert: %w", err)
	}
	return v, nil
}

// Query returns a query string value, or defaultValue if it is empty.
func (ctx *Context) Query(key string, defaultValue ...string) string {
	return orDefault(ctx.r.URL.Query().Get(key), defaultValue)
}

// Cookies returns a request cookie value, or defaultValue if it is empty.
func (ctx *Context) Cookies(key string, defaultValue ...string) string {
	value := ""
	if c, err := ctx.r.Cookie(key); err == nil {
		value = c.Value
	}
	return orDefault(value, defaultValue)
}

// Cookie sets a cookie on the response.
func (ctx *Context) Cookie(c *Cookie) {
	hc := &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		MaxAge:   c.MaxAge,
		Expires:  c.Expires,
		Secure:   c.Secure,
		HttpOnly: c.HTTPOnly,
	}
	if hc.Path == "" {
		hc.Path = "/"
	}
	switch strings.ToLower(c.SameSite) {
	case "lax":
		hc.SameSite = http.SameSiteLaxMode
	case "strict":
		hc.SameSite = http.SameSiteStrictMode
	case "none":
		hc.SameSite = http.SameSiteNoneMode
	}
	http.SetCookie(ctx.w, hc)
}

// ClearCookie expires the named cookies. With no names, it expires every
// cookie the request sent.
func (ctx *Context) ClearCookie(names ...string) {
	if len(names) == 0 {
		for _, c := range ctx.r.Cookies() {
			names = append(names, c.Name)
		}
	}
	for _, name := range names {
		http.SetCookie(ctx.w, &http.Cookie{Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(0, 0)})
	}
}

// Locals stores a value for the rest of the request when value is given,
// and returns the value stored for key.
func (ctx *Context) Locals(key any, value ...any) any {
	if len(value) > 0 {
		if ctx.locals == nil {
			ctx.locals = map[any]any{}
		}
		ctx.locals[key] = value[0]
		return value[0]
	}
	return ctx.locals[key]
}

// IP returns the client address. When ServerConfig.ProxyHeader is set and
// the peer is a trusted proxy, it returns the leftmost entry of that header.
func (ctx *Context) IP() string {
	peer := remoteIP(ctx.r)
	if ctx.server != nil && ctx.server.cfg.ProxyHeader != "" && ctx.server.trustsProxy(peer) {
		if v := ctx.r.Header.Get(ctx.server.cfg.ProxyHeader); v != "" {
			first, _, _ := strings.Cut(v, ",")
			return strings.TrimSpace(first)
		}
	}
	return peer
}

// Hostname returns the Host header.
func (ctx *Context) Hostname() string { return ctx.r.Host }

// Protocol returns "https" or "http". A trusted proxy's X-Forwarded-Proto counts.
func (ctx *Context) Protocol() string {
	if ctx.r.TLS != nil {
		return "https"
	}
	if ctx.server == nil || ctx.server.trustsProxy(remoteIP(ctx.r)) {
		if p := ctx.r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
			return p
		}
	}
	return "http"
}

// BaseURL returns the scheme and host, for example "https://example.com".
func (ctx *Context) BaseURL() string { return ctx.Protocol() + "://" + ctx.Hostname() }

// Accepts returns the first offer that the Accept header allows, or "".
// With no Accept header, it returns the first offer.
func (ctx *Context) Accepts(offers ...string) string {
	if len(offers) == 0 {
		return ""
	}
	accept := ctx.r.Header.Get("Accept")
	if accept == "" {
		return offers[0]
	}
	for _, part := range strings.Split(accept, ",") {
		spec, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		for _, offer := range offers {
			if mimeMatches(spec, offer) {
				return offer
			}
		}
	}
	return ""
}

// Body returns the raw request body. It reads the body once and caches it.
func (ctx *Context) Body() []byte {
	if !ctx.bodyRead {
		ctx.bodyRead = true
		if ctx.r.Body != nil {
			ctx.body, _ = io.ReadAll(ctx.r.Body)
			ctx.r.Body.Close()
		}
	}
	// Give later readers (form parsing) a fresh copy of the body.
	ctx.r.Body = io.NopCloser(bytes.NewReader(ctx.body))
	return ctx.body
}

// FormValue returns a value from a urlencoded or multipart form body.
func (ctx *Context) FormValue(key string) string {
	ctx.Body()
	return ctx.r.PostFormValue(key)
}

// BodyParser decodes the body into out by Content-Type: JSON, urlencoded
// form, or multipart form. Form fields map by the `form` struct tag.
func (ctx *Context) BodyParser(out any) error {
	ct, _, _ := mime.ParseMediaType(ctx.r.Header.Get("Content-Type"))
	switch {
	case strings.HasSuffix(ct, "json"):
		return json.Unmarshal(ctx.Body(), out)
	case ct == "application/x-www-form-urlencoded", ct == "multipart/form-data":
		ctx.Body()
		if err := ctx.r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
			return NewError(http.StatusBadRequest, err.Error())
		}
		return decodeValues(ctx.r.PostForm, "form", out)
	}
	return NewError(http.StatusUnprocessableEntity)
}

// ParamsParser decodes route parameters into out by the `params` struct tag.
func (ctx *Context) ParamsParser(out any) error {
	values := url.Values{}
	for _, name := range ctx.paramNames() {
		values.Set(name, ctx.r.PathValue(name))
	}
	return decodeValues(values, "params", out)
}

// QueryParser decodes the query string into out by the `query` struct tag.
func (ctx *Context) QueryParser(out any) error {
	return decodeValues(ctx.r.URL.Query(), "query", out)
}

// Status sets the response status code. Call a Send method after it.
func (ctx *Context) Status(code int) *Context {
	ctx.status = code
	return ctx
}

// SendStatus sends the status code, with its status text as the body.
func (ctx *Context) SendStatus(code int) error {
	ctx.status = code
	if code == http.StatusNoContent || code == http.StatusNotModified {
		ctx.writeHeader()
		return nil
	}
	return ctx.SendString(http.StatusText(code))
}

// SendString sends a text body. The Content-Type defaults to text/plain.
func (ctx *Context) SendString(body string) error {
	if ctx.w.Header().Get("Content-Type") == "" {
		ctx.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	return ctx.Send([]byte(body))
}

// Send sends a raw body.
func (ctx *Context) Send(body []byte) error {
	// A known length lets Compress skip bodies too small to gain from gzip.
	if ctx.w.Header().Get("Content-Length") == "" {
		ctx.w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	ctx.writeHeader()
	if len(body) == 0 {
		return nil
	}
	_, err := ctx.w.Write(body)
	return err
}

// Write writes to the response body, so Context is an io.Writer.
func (ctx *Context) Write(p []byte) (int, error) {
	ctx.writeHeader()
	return ctx.w.Write(p)
}

// JSON sends v as a JSON body.
func (ctx *Context) JSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx.w.Header().Set("Content-Type", "application/json")
	return ctx.Send(data)
}

// SendStream copies r to the body. Pass size to set Content-Length.
func (ctx *Context) SendStream(r io.Reader, size ...int) error {
	if len(size) > 0 && size[0] >= 0 {
		ctx.w.Header().Set("Content-Length", strconv.Itoa(size[0]))
	}
	ctx.writeHeader()
	_, err := io.Copy(ctx.w, r)
	return err
}

// SendFile sends a file from disk. The path is relative to the working directory.
func (ctx *Context) SendFile(path string) error {
	ctx.base.override = ctx.status
	http.ServeFile(ctx.w, ctx.r, path)
	return nil
}

// Redirect sends a redirect to location, with status 302 by default.
func (ctx *Context) Redirect(location string, status ...int) error {
	ctx.status = http.StatusFound
	if len(status) > 0 {
		ctx.status = status[0]
	}
	// Inertia needs a 303 after PUT, PATCH or DELETE: a 302 lets the browser
	// repeat the original method on the redirect target.
	if ctx.status == http.StatusFound && ctx.Get("X-Inertia") != "" {
		switch ctx.Method() {
		case http.MethodPut, http.MethodPatch, http.MethodDelete:
			ctx.status = http.StatusSeeOther
		}
	}
	ctx.w.Header().Set("Location", location)
	ctx.writeHeader()
	return nil
}

// Render renders a template with the server's Views engine.
func (ctx *Context) Render(name string, data any, layouts ...string) error {
	if ctx.server == nil || ctx.server.cfg.ViewsEngine == nil {
		return fmt.Errorf("cartridge: no views engine configured")
	}
	var buf bytes.Buffer
	if err := ctx.server.cfg.ViewsEngine.Render(&buf, name, data, layouts...); err != nil {
		return err
	}
	ctx.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return ctx.Send(buf.Bytes())
}

// writeHeader sends the status code once, 200 by default.
func (ctx *Context) writeHeader() {
	if ctx.base.wroteHeader {
		return
	}
	code := ctx.status
	if code == 0 {
		code = http.StatusOK
	}
	ctx.w.WriteHeader(code)
}

// statusCode returns the status sent, or the status set so far.
func (ctx *Context) statusCode() int {
	if ctx.base.wroteHeader {
		return ctx.base.status
	}
	if ctx.status != 0 {
		return ctx.status
	}
	return http.StatusOK
}

type paramNamesKey struct{}

func (ctx *Context) paramNames() []string {
	names, _ := ctx.Locals(paramNamesKey{}).([]string)
	return names
}

// responseWriter records the status and whether the header went out.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	override    int // replaces a 200 from a handler like http.ServeFile
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if w.override != 0 && code == http.StatusOK {
		code = w.override
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func orDefault(v string, defaultValue []string) string {
	if v == "" && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func mimeMatches(spec, offer string) bool {
	if spec == "*/*" || spec == offer {
		return true
	}
	if prefix, ok := strings.CutSuffix(spec, "/*"); ok {
		return strings.HasPrefix(offer, prefix+"/")
	}
	return false
}
