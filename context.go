package cartridge

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Context provides request-scoped access to application dependencies.
// It embeds fiber.Ctx to provide all HTTP request/response methods while
// adding direct field access to logger, config, and database manager.
// This eliminates the need for context.Locals and provides type-safe access.
type Context struct {
	*fiber.Ctx              // All Fiber HTTP methods (Render, JSON, etc.)
	Logger     Logger       // Request logger (shared across app)
	Config     Config       // Runtime configuration
	DBManager  DBManager    // Database connection pool
	Session    *SessionManager // Session management (may be nil if not configured)
	db         *gorm.DB     // Cached database session (lazy-loaded)
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

// HandlerFunc is the signature for cartridge request handlers.
// Handlers receive a Context with embedded Fiber context and direct access to dependencies.
type HandlerFunc func(*Context) error
