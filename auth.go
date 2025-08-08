package cartridge

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"gorm.io/gorm"
	// TODO: Re-enable when dependency is resolved
	// "golang.org/x/crypto/bcrypt"
)

// SessionData holds encrypted session information
type SessionData struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CookieAuthConfig holds authentication configuration for cookie-based auth
type CookieAuthConfig struct {
	SecretKey      string
	CookieName     string
	CookieMaxAge   int
	CookieSecure   bool
	CookieHTTPOnly bool
	CookieSameSite string
	CookieDomain   string
	CookiePath     string
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig(secretKey string, isProduction bool) CookieAuthConfig {
	return CookieAuthConfig{
		SecretKey:      secretKey,
		CookieName:     "auth_session",
		CookieMaxAge:   7 * 24 * 60 * 60, // 7 days in seconds
		CookieSecure:   isProduction,
		CookieHTTPOnly: true,
		CookieSameSite: "Lax",
		CookieDomain:   "",
		CookiePath:     "/",
	}
}

// SetAuthCookie creates and sets an encrypted authentication cookie
func SetAuthCookie(c interface{}, config CookieAuthConfig, userID string) error {
	// TODO: Implement when Fiber dependency is resolved
	// sessionData := SessionData{
	//     UserID:    userID,
	//     ExpiresAt: time.Now().Add(time.Duration(config.CookieMaxAge) * time.Second),
	// }

	// encryptedSession, err := encryptSessionData(sessionData, config.SecretKey)
	// if err != nil {
	//     return fmt.Errorf("failed to encrypt session data: %w", err)
	// }

	// cookie := &fiber.Cookie{
	//     Name:     config.CookieName,
	//     Value:    encryptedSession,
	//     Path:     config.CookiePath,
	//     Domain:   config.CookieDomain,
	//     MaxAge:   config.CookieMaxAge,
	//     Secure:   config.CookieSecure,
	//     HTTPOnly: config.CookieHTTPOnly,
	//     SameSite: config.CookieSameSite,
	// }

	// c.(*fiber.Ctx).Cookie(cookie)
	return nil
}

// GetAuthCookie retrieves and decrypts an authentication cookie
func GetAuthCookie(c interface{}, config CookieAuthConfig) (*SessionData, error) {
	// TODO: Implement when Fiber dependency is resolved
	// cookieValue := c.(*fiber.Ctx).Cookies(config.CookieName)
	// if cookieValue == "" {
	//     return nil, errors.New("no authentication cookie found")
	// }

	// sessionData, err := decryptSessionData(cookieValue, config.SecretKey)
	// if err != nil {
	//     return nil, fmt.Errorf("failed to decrypt session data: %w", err)
	// }

	// if time.Now().After(sessionData.ExpiresAt) {
	//     return nil, errors.New("session expired")
	// }

	// return sessionData, nil
	return nil, errors.New("authentication not available in placeholder mode")
}

// ClearAuthCookie removes the authentication cookie
func ClearAuthCookie(c interface{}, config CookieAuthConfig) {
	// TODO: Implement when Fiber dependency is resolved
	// cookie := &fiber.Cookie{
	//     Name:     config.CookieName,
	//     Value:    "",
	//     Path:     config.CookiePath,
	//     Domain:   config.CookieDomain,
	//     MaxAge:   -1,
	//     Secure:   config.CookieSecure,
	//     HTTPOnly: config.CookieHTTPOnly,
	//     SameSite: config.CookieSameSite,
	// }
	// c.(*fiber.Ctx).Cookie(cookie)
}

// encryptSessionData encrypts session data using AES-GCM
func encryptSessionData(data SessionData, secretKey string) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(secretKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, jsonData, nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// decryptSessionData decrypts session data using AES-GCM
func decryptSessionData(encryptedData, secretKey string) (*SessionData, error) {
	data, err := base64.URLEncoding.DecodeString(encryptedData)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher([]byte(secretKey))
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	var sessionData SessionData
	if err := json.Unmarshal(plaintext, &sessionData); err != nil {
		return nil, err
	}

	return &sessionData, nil
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	// TODO: Implement when bcrypt dependency is resolved
	// hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	// if err != nil {
	//     return "", fmt.Errorf("failed to hash password: %w", err)
	// }
	// return string(hash), nil
	return "", fmt.Errorf("password hashing not available in placeholder mode")
}

// VerifyPassword verifies a password against its hash
func VerifyPassword(password, hash string) bool {
	// TODO: Implement when bcrypt dependency is resolved
	// err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	// return err == nil
	return false
}

// User interface that models can implement for generic authentication
type User interface {
	GetID() string
}

// AuthMiddlewareConfig holds configuration for generic authentication middleware
type AuthMiddlewareConfig struct {
	LoginRedirectPath string                                                    // Where to redirect unauthenticated users
	UserContextKey    string                                                    // Key used to store user in context
	UserFinder        func(ctx *Context, userID string) (User, error)          // Function to find user by ID from database
	OnAuthError       func(ctx *Context, err error) error                      // Custom error handler (optional)
}

// DefaultAuthMiddlewareConfig returns sensible defaults for authentication middleware
func DefaultAuthMiddlewareConfig() AuthMiddlewareConfig {
	return AuthMiddlewareConfig{
		LoginRedirectPath: "/admin/login",
		UserContextKey:    "current_user",
		UserFinder:        nil, // Must be provided by the application
		OnAuthError:       nil, // Will use default redirect behavior
	}
}

// RequireAuthInterface creates a generic authentication middleware that works with any user model
// This middleware:
// 1. Checks for a valid auth cookie using Cartridge's auth system
// 2. Verifies the user still exists in the database using the provided UserFinder
// 3. Stores the user in context for handlers to access
// 4. Redirects to login on authentication failure
func RequireAuthInterface(config AuthMiddlewareConfig) func(*Context) error {
	if config.UserFinder == nil {
		panic("AuthMiddlewareConfig.UserFinder is required")
	}
	
	// Apply defaults for missing config
	if config.LoginRedirectPath == "" {
		config.LoginRedirectPath = "/admin/login"
	}
	if config.UserContextKey == "" {
		config.UserContextKey = "current_user"
	}

	return func(ctx *Context) error {
		// Get session data using cartridge's auth system
		sessionData, err := GetAuthCookie(ctx.Fiber, ctx.Auth)
		if err != nil || sessionData == nil {
			if config.OnAuthError != nil {
				return config.OnAuthError(ctx, err)
			}
			return ctx.Redirect(config.LoginRedirectPath)
		}

		// Find user in database using provided finder function
		user, err := config.UserFinder(ctx, sessionData.UserID)
		if err != nil {
			if config.OnAuthError != nil {
				return config.OnAuthError(ctx, err)
			}
			return ctx.Redirect(config.LoginRedirectPath)
		}

		// Store current user in context for handlers to use
		ctx.SetLocal(config.UserContextKey, user)
		return nil
	}
}

// GetCurrentUserInterface retrieves the current authenticated user from context with type safety
// T must implement the User interface
func GetCurrentUserInterface[T User](ctx *Context, userContextKey string) T {
	var zero T
	if user, ok := ctx.Get(userContextKey).(T); ok {
		return user
	}
	return zero
}

// GetCurrentUserByKey retrieves the current user from context using a specific key
// This is useful when you have multiple user types or custom context keys
func GetCurrentUserByKey(ctx *Context, key string) User {
	if user, ok := ctx.Get(key).(User); ok {
		return user
	}
	return nil
}

// === OPTION 3: Generic Authentication Middleware with Configuration Struct ===

// AuthConfig holds configuration for generic authentication middleware using Go generics
// This approach provides type safety without requiring interface implementation
type AuthConfig[T any] struct {
	LoginPath   string                                    // Where to redirect when auth fails
	UserLookup  func(*gorm.DB, string) (*T, error)      // How to look up user by ID
	ContextKey  string                                   // Where to store user in context
}

// RequireAuth creates a generic authentication middleware that works with any user type
// This middleware provides complete type safety using Go generics
func RequireAuth[T any](config AuthConfig[T]) func(*Context) error {
	return func(ctx *Context) error {
		sessionData, err := GetAuthCookie(ctx.Fiber, ctx.Auth)
		if err != nil || sessionData == nil {
			return ctx.Redirect(config.LoginPath)
		}

		user, err := config.UserLookup(ctx.DB(), sessionData.UserID)
		if err != nil {
			return ctx.Redirect(config.LoginPath)
		}

		ctx.SetLocal(config.ContextKey, user)
		return nil
	}
}

// GetCurrentUser retrieves the current authenticated user from context with complete type safety
// This function uses Go generics to ensure the returned type matches exactly what was stored
func GetCurrentUser[T any](ctx *Context, contextKey string) *T {
	if user, ok := ctx.Get(contextKey).(*T); ok {
		return user
	}
	return nil
}

// NewAdminAuthConfig creates a pre-configured AuthConfig for common admin authentication scenarios
// This convenience helper reduces boilerplate for the most common use case
func NewAdminAuthConfig[T any](userLookup func(*gorm.DB, string) (*T, error)) AuthConfig[T] {
	return AuthConfig[T]{
		LoginPath:  "/admin/login",
		UserLookup: userLookup,
		ContextKey: "current_admin",
	}
}

// === USAGE EXAMPLES ===

// Example usage showing how to implement the generic authentication pattern:
//
//	// 1. Define your user model (no interface implementation required)
//	type AdminUser struct {
//		ID       uint   `gorm:"primaryKey"`
//		Username string `gorm:"uniqueIndex"`
//		Email    string `json:"email"`
//		Active   bool   `json:"active"`
//	}
//
//	// 2. Create a user lookup function
//	func FindAdminUser(db *gorm.DB, userID string) (*AdminUser, error) {
//		id, err := strconv.ParseUint(userID, 10, 32)
//		if err != nil {
//			return nil, fmt.Errorf("invalid user ID: %w", err)
//		}
//
//		var admin AdminUser
//		if err := db.Where("active = ?", true).First(&admin, uint(id)).Error; err != nil {
//			return nil, fmt.Errorf("admin not found: %w", err)
//		}
//
//		return &admin, nil
//	}
//
//	// 3. Create middleware using the generic pattern
//	var RequireAdminAuth = cartridge.RequireAuthGeneric(cartridge.GenericAuthConfig[AdminUser]{
//		LoginPath:  "/admin/login",
//		UserLookup: FindAdminUser,
//		ContextKey: "current_admin",
//	})
//
//	// Or use the convenience helper:
//	var RequireAdminAuth = cartridge.RequireAuthGeneric(cartridge.NewAdminAuthConfigGeneric(FindAdminUser))
//
//	// 4. Use in your handlers with complete type safety
//	func AdminDashboard(ctx *cartridge.Context) error {
//		// Type-safe retrieval - no casting needed!
//		admin := cartridge.GetCurrentUserGeneric[AdminUser](ctx, "current_admin")
//		if admin == nil {
//			return ctx.Unauthorized("Not authenticated")
//		}
//
//		return ctx.JSON(map[string]interface{}{
//			"message":  fmt.Sprintf("Welcome %s!", admin.Username),
//			"admin_id": admin.ID,
//			"email":    admin.Email,
//		})
//	}
//
//	// 5. Apply middleware to routes
//	func SetupRoutes(app *cartridge.App) {
//		// Public routes
//		app.Get("/", HomeHandler)
//		app.Get("/admin/login", LoginPageHandler)
//		app.Post("/admin/login", LoginHandler)
//
//		// Protected admin routes - middleware applied to all
//		admin := app.Group("/admin")
//		admin.Use(RequireAdminAuth)
//		admin.Get("/", AdminDashboard)
//		admin.Get("/users", ListUsers)
//		admin.Post("/users", CreateUser)
//	}
//
// The generic approach provides several benefits over the interface-based approach:
// - No need to implement interfaces on your models
// - Complete type safety - no runtime type assertions
// - Cleaner, more readable code
// - Better IDE support with auto-completion
// - Compile-time error checking
