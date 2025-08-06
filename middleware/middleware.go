package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/karloscodes/cartridge/logging"
)

// RequestID generates a unique request ID for tracing
func RequestID() interface{} {
	// This will be implemented to return fiber.Handler when fiber is available
	return func() {
		// Placeholder implementation
	}
}

// Recovery provides panic recovery with stack traces
func Recovery(logger logging.Logger) interface{} {
	return func() {
		// Placeholder implementation that will wrap requests in recovery
	}
}

// Logger provides HTTP request/response logging
func Logger(logger logging.Logger) interface{} {
	return func() {
		// Placeholder implementation for request logging
	}
}

// Helmet provides security headers
func Helmet(referrerPolicy string) interface{} {
	return func() {
		// Placeholder implementation for security headers
	}
}

// DatabaseInjection adds database connections to context
func DatabaseInjection(dbManager interface{}) interface{} {
	return func() {
		// Placeholder implementation for database injection
	}
}

// MethodOverride supports _method form field for PUT/DELETE via POST
func MethodOverride() interface{} {
	return func() {
		// Placeholder implementation for method override
	}
}

// CSRFConfig holds CSRF protection configuration
type CSRFConfig struct {
	ExcludedPaths  []string
	CookieName     string
	CookieSameSite string
	CookieSecure   bool
	Expiration     time.Duration
	ContextKey     string
}

// DefaultCSRFConfig returns default CSRF configuration
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		ExcludedPaths:  []string{"/api/", "/static/"},
		CookieName:     "_csrf_token",
		CookieSameSite: "Lax",
		CookieSecure:   false, // Will be set based on environment
		Expiration:     2 * time.Hour,
		ContextKey:     "csrf",
	}
}

// CSRF provides CSRF protection
func CSRF(logger logging.Logger, config CSRFConfig) interface{} {
	return func() {
		// Placeholder implementation for CSRF protection
	}
}

// RateLimiterConfig holds rate limiting configuration
type RateLimiterConfig struct {
	Max      int
	Duration time.Duration
}

// DefaultRateLimiterConfig returns default rate limiting configuration
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		Max:      50,
		Duration: 1 * time.Second,
	}
}

// RateLimiter provides IP-based rate limiting
func RateLimiter(config RateLimiterConfig) interface{} {
	return func() {
		// Placeholder implementation for rate limiting
	}
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowOrigins []string
	AllowMethods []string
	AllowHeaders []string
}

// DefaultCORSConfig returns default CORS configuration
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
	}
}

// CORS provides Cross-Origin Resource Sharing support
func CORS(config CORSConfig) interface{} {
	return func() {
		// Placeholder implementation for CORS
	}
}

// ConcurrencyLimiter manages read/write concurrency
type ConcurrencyLimiter struct {
	readSemaphore  chan struct{}
	writeSemaphore chan struct{}
	timeout        time.Duration
}

// NewConcurrencyLimiter creates a new concurrency limiter
func NewConcurrencyLimiter(readLimit, writeLimit int, timeout time.Duration) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		readSemaphore:  make(chan struct{}, readLimit),
		writeSemaphore: make(chan struct{}, writeLimit),
		timeout:        timeout,
	}
}

// WriteConcurrencyLimitMiddleware limits concurrent write operations
func WriteConcurrencyLimitMiddleware(limiter *ConcurrencyLimiter) interface{} {
	return func() {
		// Placeholder implementation for write concurrency limiting
	}
}

// RequestLogger provides structured HTTP request logging
type RequestLogger struct {
	logger logging.Logger
}

// NewRequestLogger creates a new request logger
func NewRequestLogger(logger logging.Logger) *RequestLogger {
	return &RequestLogger{
		logger: logger,
	}
}

// LogRequest logs HTTP request details
func (rl *RequestLogger) LogRequest(method, path, ip, userAgent string, status int, duration time.Duration, size int64) {
	fields := []logging.Field{
		{Key: "method", Value: method},
		{Key: "path", Value: path},
		{Key: "status", Value: status},
		{Key: "duration_ms", Value: float64(duration.Nanoseconds()) / 1e6},
		{Key: "ip", Value: ip},
		{Key: "user_agent", Value: userAgent},
		{Key: "response_size", Value: size},
	}

	message := fmt.Sprintf("%s %s", method, path)
	
	if status >= 500 {
		rl.logger.Error(message, fields...)
	} else if status >= 400 {
		rl.logger.Warn(message, fields...)
	} else {
		rl.logger.Info(message, fields...)
	}
}

// IsExcludedPath checks if a path should be excluded from middleware
func IsExcludedPath(path string, excludedPaths []string) bool {
	for _, excluded := range excludedPaths {
		if strings.HasPrefix(path, excluded) {
			return true
		}
	}
	return false
}

// SecurityHeaders represents common security headers
type SecurityHeaders struct {
	ContentTypeOptions    string
	FrameOptions         string
	XSSProtection        string
	ReferrerPolicy       string
	ContentSecurityPolicy string
}

// DefaultSecurityHeaders returns default security headers
func DefaultSecurityHeaders() SecurityHeaders {
	return SecurityHeaders{
		ContentTypeOptions:    "nosniff",
		FrameOptions:         "DENY",
		XSSProtection:        "1; mode=block",
		ReferrerPolicy:       "strict-origin-when-cross-origin",
		ContentSecurityPolicy: "default-src 'self'",
	}
}
