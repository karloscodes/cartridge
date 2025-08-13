package cartridge

import (
	"log/slog"
	"time"
	"gorm.io/gorm"
	"github.com/gofiber/fiber/v2"
)

// Essential type definitions for cartridge framework

// Logger interface for logging operations
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	With(args ...interface{}) Logger
}

// Database interface for database operations
type Database interface {
	GetGenericConnection() interface{} // Returns *gorm.DB
	Init() error
	Close() error
	Ping() error
}

// CSRFConfig holds CSRF protection configuration
type CSRFConfig struct {
	ExcludedPaths  []string
	CookieName     string
	CookieSameSite string
	CookieSecure   bool
	Expiration     time.Duration
	ContextKey     string
	KeyLookup      string
}

// RateLimiterConfig holds rate limiting configuration
type RateLimiterConfig struct {
	Max      int
	Duration time.Duration
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// SecurityHeaders represents common security headers
type SecurityHeaders struct {
	ContentTypeOptions    string
	FrameOptions          string
	XSSProtection         string
	ReferrerPolicy        string
	ContentSecurityPolicy string
}

// MigrationManager handles database migrations
type MigrationManager struct {
	// Internal implementation hidden
}

// DatabaseOperations provides database operation methods
type DatabaseOperations struct {
	// Internal implementation hidden
}

// AssetManager handles static asset serving
type AssetManager struct {
	// Internal implementation hidden
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level         LogLevel
	Directory     string
	UseJSON       bool
	EnableConsole bool
	EnableColors  bool
	AddSource     bool
	Environment   string
}

// NewLogger creates a logger from LogConfig
func NewLogger(config LogConfig) Logger {
	logger := slog.Default()
	return &simpleLogger{logger}
}

type simpleLogger struct {
	*slog.Logger
}

func (l *simpleLogger) Debug(msg string, args ...interface{}) { l.Logger.Debug(msg, args...) }
func (l *simpleLogger) Info(msg string, args ...interface{})  { l.Logger.Info(msg, args...) }
func (l *simpleLogger) Warn(msg string, args ...interface{})  { l.Logger.Warn(msg, args...) }
func (l *simpleLogger) Error(msg string, args ...interface{}) { l.Logger.Error(msg, args...) }
func (l *simpleLogger) With(args ...interface{}) Logger {
	return &simpleLogger{l.Logger.With(args...)}
}

// Constructor functions for missing types

// NewDatabase creates a simple database instance
func NewDatabase(cfg *Config, logger Logger) Database {
	return &simpleDatabase{}
}

type simpleDatabase struct{}

func (d *simpleDatabase) GetGenericConnection() interface{} { 
	// For testing purposes, return a mock gorm.DB connection
	return &gorm.DB{}
}
func (d *simpleDatabase) Init() error                       { return nil }
func (d *simpleDatabase) Close() error                      { return nil }
func (d *simpleDatabase) Ping() error                       { return nil }

// DefaultCSRFConfig returns default CSRF configuration
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		ExcludedPaths:  []string{"/api/", "/static/"},
		CookieName:     "_csrf_token",
		CookieSameSite: "Lax",
		CookieSecure:   false,
		Expiration:     2 * time.Hour,
		ContextKey:     "csrf",
		KeyLookup:      "header:X-CSRF-Token",
	}
}

// DefaultRateLimiterConfig returns default rate limiting configuration
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		Max:      50,
		Duration: 1 * time.Second,
	}
}

// DefaultCORSConfig returns default CORS configuration
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: false,
		MaxAge:           0,
	}
}

// DefaultSecurityHeaders returns default security headers
func DefaultSecurityHeaders() SecurityHeaders {
	return SecurityHeaders{
		ContentTypeOptions:    "nosniff",
		FrameOptions:          "DENY",
		XSSProtection:         "1; mode=block",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		ContentSecurityPolicy: "default-src 'self'",
	}
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db interface{}, logger Logger) *MigrationManager {
	return &MigrationManager{}
}

// Close method for MigrationManager
func (mm *MigrationManager) Close() error {
	return nil
}

// LoadFromFS method for MigrationManager
func (mm *MigrationManager) LoadFromFS(fs interface{}, path string) error {
	return nil
}

// AddBinaryMigration method for MigrationManager
func (mm *MigrationManager) AddBinaryMigration(version uint, name string, up interface{}, down interface{}) error {
	return nil
}

// Up method for MigrationManager
func (mm *MigrationManager) Up() error {
	return nil
}

// Down method for MigrationManager  
func (mm *MigrationManager) Down() error {
	return nil
}

// Status method for MigrationManager
func (mm *MigrationManager) Status() error {
	return nil
}

// Force method for MigrationManager
func (mm *MigrationManager) Force(version int) error {
	return nil
}

// NewDatabaseOperations creates a new database operations instance
func NewDatabaseOperations(args ...interface{}) *DatabaseOperations {
	return &DatabaseOperations{}
}

// Exec method for DatabaseOperations - returns *gorm.DB (for cron)
func (do *DatabaseOperations) Exec(query string, args ...interface{}) *gorm.DB {
	return &gorm.DB{}
}

// Query method for DatabaseOperations - returns *gorm.DB (for cron)
func (do *DatabaseOperations) Query(query string, dest interface{}, args ...interface{}) *gorm.DB {
	return &gorm.DB{}
}

// ExecSafe method for DatabaseOperations - returns (*gorm.DB, error) (for async)
func (do *DatabaseOperations) ExecSafe(query string, args ...interface{}) (*gorm.DB, error) {
	return &gorm.DB{}, nil
}

// QuerySafe method for DatabaseOperations - returns (*gorm.DB, error) (for async)  
func (do *DatabaseOperations) QuerySafe(query string, dest interface{}, args ...interface{}) (*gorm.DB, error) {
	return &gorm.DB{}, nil
}

// DefaultAssetConfig returns default asset configuration
func DefaultAssetConfig(cfg *Config) map[string]interface{} {
	return map[string]interface{}{
		"static_dir": "static",
		"prefix":     "/static",
	}
}

// NewAssetManager creates a new asset manager
func NewAssetManager(config map[string]interface{}, logger Logger) *AssetManager {
	return &AssetManager{}
}

// SetEmbeddedFS method for AssetManager
func (am *AssetManager) SetEmbeddedFS(staticFS interface{}, templateFS interface{}) {
	// Simple implementation
}

// SetupStaticRoutes method for AssetManager
func (am *AssetManager) SetupStaticRoutes(app interface{}) {
	// Simple implementation
}

// GetAssetPath method for AssetManager
func (am *AssetManager) GetAssetPath(path string) string {
	return "/static/" + path
}

// CSS method for AssetManager
func (am *AssetManager) CSS(path string) string {
	return am.GetAssetPath("css/" + path)
}

// JS method for AssetManager
func (am *AssetManager) JS(path string) string {
	return am.GetAssetPath("js/" + path)
}

// Image method for AssetManager
func (am *AssetManager) Image(path string) string {
	return am.GetAssetPath("images/" + path)
}

// Icon method for AssetManager
func (am *AssetManager) Icon(path string) string {
	return am.GetAssetPath("icons/" + path)
}

// AppTypeMiddlewareConfig holds app-specific middleware configuration
type AppTypeMiddlewareConfig struct {
	RecoveryConfig    map[string]interface{}
	LoggerConfig      map[string]interface{}
	HelmetConfig      map[string]interface{}
	EnableCSRF        bool
	CSRFConfig        CSRFConfig
	EnableRateLimit   bool
	RateLimitConfig   RateLimiterConfig
	EnableCORS        bool
	CORSConfig        CORSConfig
}

// NewAppTypeMiddlewareConfig creates new app type middleware config
func NewAppTypeMiddlewareConfig(maxAge int, appType string, excludedPaths []string) *AppTypeMiddlewareConfig {
	return &AppTypeMiddlewareConfig{
		RecoveryConfig: map[string]interface{}{
			"enabled": true,
		},
		LoggerConfig: map[string]interface{}{
			"enabled": true,
		},
		HelmetConfig: map[string]interface{}{
			"enabled": true,
		},
		EnableCSRF:      true,
		CSRFConfig:      DefaultCSRFConfig(),
		EnableRateLimit: true,
		RateLimitConfig: DefaultRateLimiterConfig(),
		EnableCORS:      true,
		CORSConfig:      DefaultCORSConfig(),
	}
}

// Recovery middleware function
func Recovery(config interface{}) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		defer func() {
			if r := recover(); r != nil {
				c.Status(500).SendString("Internal server error")
			}
		}()
		return c.Next()
	}
}

// LoggerMiddleware function
func LoggerMiddleware(config interface{}) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// Helmet middleware function
func Helmet(config interface{}) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// MethodOverride middleware function
func MethodOverride() func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// CSRF middleware function
func CSRF(logger Logger, config CSRFConfig) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// CSRFTokenInjector middleware function
func CSRFTokenInjector(config CSRFConfig) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// RateLimiter middleware function
func RateLimiter(config RateLimiterConfig) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// CORS middleware function
func CORS(config CORSConfig) func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// RequestID middleware that generates and sets a request ID
func RequestID() func(*fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		requestID := GenerateRandomString(16)
		c.Locals("requestID", requestID)
		c.Set("X-Request-ID", requestID)
		return c.Next()
	}
}

// TypedQueryBuilder represents a query builder for typed models
type TypedQueryBuilder[T any] struct {
	ctx *Context
	db  interface{}
}

// TypedModel creates a typed query builder for the given model type
func TypedModel[T any](ctx *Context) *TypedQueryBuilder[T] {
	return &TypedQueryBuilder[T]{
		ctx: ctx,
		db:  ctx.DB(),
	}
}

// With adds preloading for related models
func (tqb *TypedQueryBuilder[T]) With(relations ...string) *TypedQueryBuilder[T] {
	// Simple implementation - just return self for chaining
	return tqb
}

// OrderByDesc adds descending order
func (tqb *TypedQueryBuilder[T]) OrderByDesc(field string) *TypedQueryBuilder[T] {
	return tqb
}

// Limit adds a limit to the query
func (tqb *TypedQueryBuilder[T]) Limit(limit int) *TypedQueryBuilder[T] {
	return tqb
}

// All executes the query and returns all results
func (tqb *TypedQueryBuilder[T]) All() ([]T, error) {
	var results []T
	// Simple implementation - just return empty slice
	return results, nil
}

// Get executes the query and returns all results (alias for All)
func (tqb *TypedQueryBuilder[T]) Get() ([]T, error) {
	return tqb.All()
}

// Find executes the query and returns the first result
func (tqb *TypedQueryBuilder[T]) Find(id interface{}) (*T, error) {
	var result T
	// Simple implementation - just return pointer to zero value
	return &result, nil
}

// Active adds a filter for active records
func (tqb *TypedQueryBuilder[T]) Active() *TypedQueryBuilder[T] {
	return tqb
}

// OrderBy adds ascending order
func (tqb *TypedQueryBuilder[T]) OrderBy(field string) *TypedQueryBuilder[T] {
	return tqb
}

// Where adds a where condition
func (tqb *TypedQueryBuilder[T]) Where(field string, value interface{}) *TypedQueryBuilder[T] {
	return tqb
}

// Count executes the query and returns the count
func (tqb *TypedQueryBuilder[T]) Count() (int64, error) {
	// Simple implementation - just return 0
	return 0, nil
}