package cartridge

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Recover turns a panic in the chain into a 500 error. It logs the panic
// and the stack with the server's logger. The client sees only the status.
func Recover() HandlerFunc {
	return func(c *Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				if r == http.ErrAbortHandler {
					panic(r)
				}
				if c.Logger != nil {
					c.Logger.Error("panic recovered",
						"panic", fmt.Sprint(r),
						"stack", string(debug.Stack()),
						"method", c.Method(),
						"path", c.Path(),
					)
				}
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return c.Next()
	}
}

// requestIDKey is the Locals key for the request ID.
const requestIDKey = "requestid"

// RequestID sets the X-Request-ID response header. It keeps the ID the
// client sent, or makes a new one, and stores it in Locals("requestid").
// The client's ID counts only when it is up to 64 letters, digits, and
// dashes, so it cannot inject text into logs or headers.
func RequestID() HandlerFunc {
	return func(c *Context) error {
		id := c.Get("X-Request-ID")
		if !validRequestID(id) {
			b := make([]byte, 16)
			rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Set("X-Request-ID", id)
		c.Locals(requestIDKey, id)
		return c.Next()
	}
}

// validRequestID reports whether id is 1 to 64 characters of A-Z, a-z,
// 0-9, and "-".
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// SecurityHeaders sets security response headers that suit a typical
// server-rendered app. It does not send Cross-Origin-Embedder-Policy: its
// require-corp mode blocks cross-origin images and scripts on your pages.
// Cross-Origin-Resource-Policy stays same-origin: it stops other sites
// from embedding your responses. A route that other sites must load, such
// as a public script, sets the header to "cross-origin".
func SecurityHeaders() HandlerFunc {
	headers := [][2]string{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "SAMEORIGIN"},
		{"Referrer-Policy", "same-origin"},
		{"Cross-Origin-Opener-Policy", "same-origin"},
		{"Cross-Origin-Resource-Policy", "same-origin"},
		{"Origin-Agent-Cluster", "?1"},
		{"X-Permitted-Cross-Domain-Policies", "none"},
	}
	return func(c *Context) error {
		h := c.Response().Header()
		for _, kv := range headers {
			h.Set(kv[0], kv[1])
		}
		return c.Next()
	}
}

// serverSecurityHeaders adds to SecurityHeaders the headers that depend on
// the server config: ServerConfig.ContentSecurityPolicy, and HSTS for an
// https request in production.
func serverSecurityHeaders(cfg *ServerConfig) HandlerFunc {
	static := SecurityHeaders()
	return func(c *Context) error {
		h := c.Response().Header()
		if cfg.ContentSecurityPolicy != "" {
			h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
		}
		// HSTS tells the browser to refuse plain http for a year, so send it
		// only over https.
		if cfg.Config.IsProduction() && c.Protocol() == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		return static(c)
	}
}

// RequestLogger logs each request with its status and duration.
// Health checks (/_health) are not logged.
func RequestLogger(logger Logger) HandlerFunc {
	return func(c *Context) error {
		start := time.Now()
		err := c.Next()
		if strings.HasPrefix(c.Path(), "/_health") {
			return err
		}

		status := c.statusCode()
		if err != nil {
			status = errorCode(err)
		}
		logger.Info("http request",
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration", time.Since(start),
			"ip", c.IP(),
		)
		return err
	}
}

// Compress gzips responses for clients that accept gzip. It skips event
// streams, already-encoded bodies, and bodyless responses.
func Compress() HandlerFunc {
	return func(c *Context) error {
		if !strings.Contains(c.Get("Accept-Encoding"), "gzip") || c.Method() == http.MethodHead {
			return c.Next()
		}
		gw := &gzipWriter{ResponseWriter: c.w}
		c.w = gw
		c.Response().Header().Add("Vary", "Accept-Encoding")
		err := c.Next()
		gw.close()
		c.w = gw.ResponseWriter
		return err
	}
}

// gzipWriter decides on the first write whether to compress.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (w *gzipWriter) WriteHeader(code int) {
	if !w.decided {
		w.decided = true
		h := w.Header()
		if compressible(code, h) {
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			h.Del("Accept-Ranges")
			w.gz = gzip.NewWriter(w.ResponseWriter)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipWriter) Write(p []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *gzipWriter) Flush() {
	if w.gz != nil {
		w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipWriter) close() {
	if w.gz != nil {
		w.gz.Close()
	}
}

func compressible(code int, h http.Header) bool {
	if code < 200 || code == http.StatusNoContent || code == http.StatusNotModified || code == http.StatusPartialContent {
		return false
	}
	if h.Get("Content-Encoding") != "" {
		return false
	}
	// gzip makes bodies under 1 KB larger, not smaller.
	if n, err := strconv.Atoi(h.Get("Content-Length")); err == nil && n < 1024 {
		return false
	}
	ct := h.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "text/event-stream"),
		strings.HasPrefix(ct, "image/") && !strings.HasPrefix(ct, "image/svg"),
		strings.HasPrefix(ct, "video/"), strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "font/woff"), ct == "application/zip", ct == "application/gzip":
		return false
	}
	return true
}

// CORSConfig configures CORS for a route. Lists are comma-separated.
type CORSConfig struct {
	AllowOrigins     string // "*" allows any origin
	AllowMethods     string
	AllowHeaders     string // empty echoes Access-Control-Request-Headers
	ExposeHeaders    string
	AllowCredentials bool
	MaxAge           int // seconds; 0 omits the header
}

// CORS answers preflight requests with 204 and adds CORS headers to other
// requests that carry an Origin header.
func CORS(cfg CORSConfig) HandlerFunc {
	if cfg.AllowOrigins == "" {
		cfg.AllowOrigins = "*"
	}
	if cfg.AllowMethods == "" {
		cfg.AllowMethods = "GET,POST,HEAD,PUT,DELETE,PATCH"
	}
	origins := splitList(strings.ToLower(cfg.AllowOrigins))
	allowAll := false
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
	}
	// Lists go out without spaces, as Fiber's CORS middleware sent them.
	allowMethods := strings.ReplaceAll(cfg.AllowMethods, " ", "")
	allowHeaders := strings.ReplaceAll(cfg.AllowHeaders, " ", "")
	exposeHeaders := strings.ReplaceAll(cfg.ExposeHeaders, " ", "")

	return func(c *Context) error {
		origin := strings.ToLower(c.Get("Origin"))
		if origin == "" {
			if !allowAll {
				c.Vary("Origin")
			}
			return c.Next()
		}

		preflight := c.Method() == http.MethodOptions && c.Get("Access-Control-Request-Method") != ""
		if c.Method() == http.MethodOptions && !preflight {
			c.Vary("Origin")
			return c.Next()
		}

		allowOrigin := ""
		if allowAll {
			allowOrigin = "*"
		} else {
			for _, o := range origins {
				if o == origin {
					allowOrigin = origin
				}
			}
		}

		h := c.Response().Header()
		if allowOrigin != "" {
			h.Set("Access-Control-Allow-Origin", allowOrigin)
			if cfg.AllowCredentials && allowOrigin != "*" {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if exposeHeaders != "" {
			h.Set("Access-Control-Expose-Headers", exposeHeaders)
		}

		if !preflight {
			if !allowAll {
				c.Vary("Origin")
			}
			return c.Next()
		}

		c.Vary("Access-Control-Request-Method", "Access-Control-Request-Headers", "Origin")
		h.Set("Access-Control-Allow-Methods", allowMethods)
		if allowHeaders != "" {
			h.Set("Access-Control-Allow-Headers", allowHeaders)
		} else if req := c.Get("Access-Control-Request-Headers"); req != "" {
			h.Set("Access-Control-Allow-Headers", req)
		}
		if cfg.MaxAge > 0 {
			h.Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
