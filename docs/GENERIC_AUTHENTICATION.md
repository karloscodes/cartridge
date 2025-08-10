# Generic Authentication Middleware for Cartridge

This document describes the generic authentication middleware pattern that has been upstreamed to Cartridge from the license key manager project. This pattern eliminates the boilerplate needed for authentication in every Cartridge application.

## Overview

The generic authentication middleware provides:
- **Type-safe user retrieval** with Go generics
- **Configurable login redirect paths**
- **Flexible user model support** through interfaces
- **Robust error handling** with custom error handlers
- **Zero boilerplate** authentication for new applications

## Core Components

### 1. User Interface

Any model can implement authentication by satisfying the `User` interface:

```go
type User interface {
    GetID() string
}
```

### 2. AuthMiddlewareConfig

Configure the authentication middleware behavior:

```go
type AuthMiddlewareConfig struct {
    LoginRedirectPath string                                    // Where to redirect unauthenticated users
    UserContextKey    string                                    // Key used to store user in context
    UserFinder        func(ctx *Context, userID string) (User, error) // Function to find user by ID
    OnAuthError       func(ctx *Context, err error) error       // Custom error handler (optional)
}
```

### 3. Generic Functions

- `RequireAuth(config AuthMiddlewareConfig) func(*Context) error` - Creates authentication middleware
- `GetCurrentUser[T User](ctx *Context, userContextKey string) T` - Type-safe user retrieval
- `GetCurrentUserByKey(ctx *Context, key string) User` - Interface-based user retrieval

## Usage Examples

### Example 1: Basic Admin Authentication

```go
// models/admin.go
type AdminUser struct {
    ID       uint   `gorm:"primaryKey"`
    Username string `gorm:"uniqueIndex"`
    Email    string
}

// Implement the User interface
func (au *AdminUser) GetID() string {
    return fmt.Sprintf("%d", au.ID)
}

// handlers/auth.go
func AdminUserFinder(ctx *cartridge.Context, userID string) (cartridge.User, error) {
    id, err := strconv.ParseUint(userID, 10, 32)
    if err != nil {
        return nil, fmt.Errorf("invalid user ID: %w", err)
    }
    
    var admin AdminUser
    if err := ctx.DB().First(&admin, uint(id)).Error; err != nil {
        return nil, fmt.Errorf("admin not found: %w", err)
    }
    
    return &admin, nil
}

func CreateAdminAuth() func(*cartridge.Context) error {
    config := cartridge.AuthMiddlewareConfig{
        LoginRedirectPath: "/admin/login",
        UserContextKey:    "current_admin", 
        UserFinder:        AdminUserFinder,
    }
    return cartridge.RequireAuth(config)
}

// Use the middleware
var RequireAdminAuth = CreateAdminAuth()

func GetCurrentAdmin(ctx *cartridge.Context) *AdminUser {
    return cartridge.GetCurrentUser[*AdminUser](ctx, "current_admin")
}
```

### Example 2: Multi-User Type Application

```go
// Support both admin and regular users
type Customer struct {
    ID    uint   `gorm:"primaryKey"`
    Email string `gorm:"uniqueIndex"`
    Name  string
}

func (c *Customer) GetID() string {
    return fmt.Sprintf("customer_%d", c.ID)
}

// Customer authentication
func CustomerUserFinder(ctx *cartridge.Context, userID string) (cartridge.User, error) {
    if !strings.HasPrefix(userID, "customer_") {
        return nil, fmt.Errorf("not a customer ID")
    }
    
    idStr := strings.TrimPrefix(userID, "customer_")
    id, err := strconv.ParseUint(idStr, 10, 32)
    if err != nil {
        return nil, fmt.Errorf("invalid customer ID: %w", err)
    }
    
    var customer Customer
    if err := ctx.DB().First(&customer, uint(id)).Error; err != nil {
        return nil, fmt.Errorf("customer not found: %w", err)
    }
    
    return &customer, nil
}

func CreateCustomerAuth() func(*cartridge.Context) error {
    config := cartridge.AuthMiddlewareConfig{
        LoginRedirectPath: "/login",
        UserContextKey:    "current_customer",
        UserFinder:        CustomerUserFinder,
    }
    return cartridge.RequireAuth(config)
}
```

### Example 3: Custom Error Handling

```go
func CreateAuthWithCustomErrors() func(*cartridge.Context) error {
    config := cartridge.AuthMiddlewareConfig{
        LoginRedirectPath: "/admin/login",
        UserContextKey:    "current_admin",
        UserFinder:        AdminUserFinder,
        OnAuthError: func(ctx *cartridge.Context, err error) error {
            ctx.Logger.Warn("Authentication failed", "error", err, "ip", ctx.Fiber.IP())
            
            // Custom error response for API endpoints
            if strings.HasPrefix(ctx.Path(), "/api/") {
                return ctx.Status(401).JSON(map[string]string{
                    "error": "Authentication required",
                    "login_url": "/admin/login",
                })
            }
            
            // Standard redirect for web endpoints
            return ctx.Redirect("/admin/login")
        },
    }
    return cartridge.RequireAuth(config)
}
```

### Example 4: Route Protection

```go
// main.go
func main() {
    app := cartridge.New()
    
    // Public routes
    app.Get("/", homeHandler)
    app.Get("/login", loginPageHandler)
    app.Post("/login", loginHandler)
    
    // Protected admin routes
    admin := app.Group("/admin")
    admin.Use(RequireAdminAuth) // Apply authentication to all admin routes
    
    admin.Get("/", adminDashboard)
    admin.Get("/users", adminUsersHandler)
    admin.Post("/users", createUserHandler)
    
    app.Run()
}

func adminDashboard(ctx *cartridge.Context) error {
    admin := GetCurrentAdmin(ctx)
    
    return ctx.JSON(map[string]interface{}{
        "message": fmt.Sprintf("Welcome %s!", admin.Username),
        "admin_id": admin.ID,
    })
}
```

## Migration from Specific Authentication

### Before (License Key Manager Pattern)

```go
// Tightly coupled to AdminUser
func RequireAuth(ctx *cartridge.Context) error {
    sessionData, err := cartridge.GetAuthCookie(ctx.Fiber, ctx.Auth)
    if err != nil || sessionData == nil {
        return ctx.Redirect("/admin/login")
    }

    adminID, err := strconv.ParseUint(sessionData.UserID, 10, 32)
    if err != nil {
        return ctx.Redirect("/admin/login")
    }

    var admin models.AdminUser
    if err := ctx.DB().First(&admin, uint(adminID)).Error; err != nil {
        return ctx.Redirect("/admin/login")
    }

    ctx.SetLocal("current_admin", &admin)
    return nil
}

func GetCurrentAdmin(ctx *cartridge.Context) *models.AdminUser {
    if admin, ok := ctx.Get("current_admin").(*models.AdminUser); ok {
        return admin
    }
    return nil
}
```

### After (Generic Pattern)

```go
// Generic, reusable, type-safe
func AdminUserFinder(ctx *cartridge.Context, userID string) (cartridge.User, error) {
    adminID, err := strconv.ParseUint(userID, 10, 32)
    if err != nil {
        return nil, fmt.Errorf("invalid user ID: %w", err)
    }

    var admin models.AdminUser
    if err := ctx.DB().First(&admin, uint(adminID)).Error; err != nil {
        return nil, fmt.Errorf("admin not found: %w", err)
    }

    return &admin, nil
}

var RequireAuth = cartridge.RequireAuth(cartridge.AuthMiddlewareConfig{
    LoginRedirectPath: "/admin/login",
    UserContextKey:    "current_admin",
    UserFinder:        AdminUserFinder,
})

func GetCurrentAdmin(ctx *cartridge.Context) *models.AdminUser {
    return cartridge.GetCurrentUser[*models.AdminUser](ctx, "current_admin")
}
```

## Benefits

1. **Eliminates Boilerplate**: No more writing the same authentication logic in every app
2. **Type Safety**: Generic functions prevent runtime type assertion errors  
3. **Flexible**: Works with any user model that implements the `User` interface
4. **Configurable**: Easy to customize redirect paths, context keys, and error handling
5. **Consistent**: Provides a standard authentication pattern across all Cartridge apps
6. **Testable**: Clear separation of concerns makes testing authentication logic easier

## Fiber Integration

For applications that need direct Fiber middleware integration:

```go
// Use the SimpleAuthenticationMiddleware for basic Fiber integration
func setupRoutes(app *fiber.App) {
    // Public routes
    app.Get("/", publicHandler)
    
    // Protected routes with Fiber middleware
    app.Use("/admin", cartridge.SimpleAuthenticationMiddleware(
        func(userID string) (cartridge.User, error) {
            // Simple user finder without Cartridge context
            return findAdminUser(userID)
        },
        authConfig,
        "/admin/login",
    ))
    
    app.Get("/admin/*", adminHandlers)
}
```

This generic authentication middleware pattern represents a significant improvement in developer experience and code reusability across the Cartridge ecosystem.