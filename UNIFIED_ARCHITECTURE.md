# Cartridge Unified Architecture

This document explains the new unified, config-based architecture that powers all Cartridge application types. The system follows "convention over configuration" principles while providing maximum flexibility.

## Core Philosophy

- **Unified Core**: All app types (Generic, FullStack, APIOnly, FullStackInertia) use the same underlying infrastructure
- **Convention over Configuration**: Smart defaults based on application type, with easy override options
- **Profile-Based**: Different app types are just configuration profiles on a common core
- **User-Friendly**: Simple factory functions hide complexity while providing power users access to advanced options

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    User-Facing API                         │
├─────────────────────────────────────────────────────────────┤
│  NewGeneric()  │  NewFullStack()  │  NewAPIOnly()  │  NewFullStackInertia()  │
├─────────────────────────────────────────────────────────────┤
│                  Unified Core (Profile-Based)              │
├─────────────────────────────────────────────────────────────┤
│  AppProfile  │  CartridgeConfig  │  Feature Toggles  │  Convention Defaults  │
├─────────────────────────────────────────────────────────────┤
│              Common Infrastructure                          │
├─────────────────────────────────────────────────────────────┤
│  Fiber  │  Database  │  Auth  │  Middleware  │  Assets  │  Inertia  │  Cron  │
└─────────────────────────────────────────────────────────────┘
```

## Application Profiles

Each application type has an optimized profile with intelligent defaults:

### APIOnly Profile
**Use Case**: REST APIs, microservices, headless backends

```go
app := cartridge.NewAPIOnly()
```

**Optimized Settings**:
- ✅ CORS enabled (cross-origin requests)
- ✅ Rate limiting enabled (API protection)  
- ❌ CSRF disabled (stateless)
- ❌ Sessions disabled (stateless)
- ❌ Templates disabled (JSON responses)
- ❌ Static files disabled (no UI)
- ✅ Database, Auth, Cron, Async enabled

### FullStack Profile
**Use Case**: Traditional server-rendered web applications

```go
app := cartridge.NewFullStack()
```

**Optimized Settings**:
- ✅ CSRF enabled (form protection)
- ❌ CORS disabled (same-origin)
- ❌ Rate limiting disabled (not typically needed)
- ✅ Sessions enabled (web app state)
- ✅ Templates enabled (HTML rendering)
- ✅ Static files enabled (CSS, JS, images)
- ✅ Database, Auth, Cron, Async enabled

**Convention-based paths**:
- Templates: `./templates/`
- Static files: `./public/`
- Migrations: `./migrations/`
- Assets: `./assets/`

### FullStackInertia Profile  
**Use Case**: Modern SPAs with server-side routing (Vue.js, React)

```go
app := cartridge.NewFullStackInertia()
```

**Optimized Settings**:
- ✅ CSRF enabled (form protection)
- ❌ CORS disabled (same-origin SPA)
- ❌ Rate limiting disabled (not needed for SPAs)
- ✅ Sessions enabled (authentication)
- ❌ Templates disabled (Inertia handles this)
- ✅ Static files enabled (compiled assets)
- ✅ **Inertia.js integration enabled**
- ✅ Database, Auth, Cron, Async enabled

**Convention-based paths** (following Inertia.js/Laravel conventions):
- Templates: `./resources/views/`
- Assets: `./resources/assets/`
- Static files: `./public/`
- Migrations: `./migrations/`
- Dev server: `http://localhost:5173` (Vite default)

### Generic Profile
**Use Case**: Prototyping, maximum flexibility, custom configurations

```go
app := cartridge.NewGeneric()
// or equivalently:
app := cartridge.New()
```

**Balanced defaults** suitable for most use cases with all features available.

## Convention Over Configuration Examples

### Basic Usage (Zero Configuration)

```go
// API server with optimal defaults
api := cartridge.NewAPIOnly()
api.Get("/users", func(ctx *cartridge.Context) error {
    return ctx.JSON(map[string]string{"users": "data"})
})

// Traditional web app with optimal defaults  
web := cartridge.NewFullStack()
web.Get("/", func(ctx *cartridge.Context) error {
    return ctx.SendString("<h1>Welcome</h1>")
})

// Modern SPA with Inertia.js integration
spa := cartridge.NewFullStackInertia()
spa.Get("/dashboard", func(ctx *cartridge.Context) error {
    return spa.InertiaRender("Dashboard", map[string]interface{}{
        "user": getCurrentUser(),
    })(ctx)
})
```

### Configuration Override (When Needed)

```go
// API with custom settings
api := cartridge.NewAPIOnly(
    cartridge.WithPort("8080"),
    cartridge.WithEnvironment(cartridge.EnvProduction),
    cartridge.WithFeature("csrf", true), // Enable CSRF for this API
)

// Web app with custom template path
web := cartridge.NewFullStack(
    cartridge.WithPort("3000"),
    // Custom template path would be handled by additional options
)

// Inertia app with custom Vite dev server
spa := cartridge.NewFullStackInertia(
    cartridge.WithPort("4000"),
    cartridge.WithEnvironment(cartridge.EnvDevelopment),
    // Custom Inertia config would be handled by additional options
)
```

### Feature Introspection

```go
app := cartridge.NewFullStackInertia()

// Check what features are enabled
if app.IsFeatureEnabled("inertia") {
    log.Println("Inertia.js is available")
}

if app.IsFeatureEnabled("csrf") {
    log.Println("CSRF protection is enabled")
}

// Get profile information
profile := app.Profile()
log.Printf("Using %s profile: %s", profile.Name, profile.Description)
log.Printf("Templates path: %s", profile.TemplatePath)
log.Printf("Assets path: %s", profile.AssetsPath)
```

## Benefits of the Unified Architecture

### 1. Consistency
All app types use the same underlying infrastructure, ensuring consistent behavior and reducing maintenance overhead.

### 2. Convention Over Configuration
Smart defaults mean you can get started with zero configuration, but everything is customizable when needed.

### 3. Type Safety
All configurations are validated at compile time with proper Go types.

### 4. Extensibility
New app types can be added easily by creating new profiles without changing the core infrastructure.

### 5. Migration Path
Easy to migrate between app types by changing the factory function and adjusting settings as needed.

### 6. Feature Discovery
The `IsFeatureEnabled()` method makes it easy to check what capabilities are available.

## Inertia.js Integration

The FullStackInertia profile includes full Inertia.js support:

```go
app := cartridge.NewFullStackInertia()

// Render Inertia pages
app.Get("/dashboard", func(ctx *cartridge.Context) error {
    return app.InertiaRender("Dashboard", map[string]interface{}{
        "stats": getDashboardStats(),
        "user":  getCurrentUser(),
    })(ctx)
})

// Advanced Inertia usage
app.Get("/users", func(ctx *cartridge.Context) error {
    helper := app.InertiaHelper(ctx)
    if helper != nil {
        return helper.Render("Users/Index", map[string]interface{}{
            "users": getUsers(),
        })
    }
    return ctx.Status(500).JSON(map[string]string{"error": "Inertia not available"})
})

// External redirects (full page reload)
app.Get("/external", func(ctx *cartridge.Context) error {
    return app.InertiaHelper(ctx).Location("https://example.com")
})
```

## Advanced Usage

### Custom Profiles

```go
// Create a custom profile for specialized use cases
customProfile := cartridge.GetProfileForType(cartridge.AppTypeAPIOnly)
customProfile.Name = "CustomAPI"
customProfile.EnableSessions = true // Enable sessions for this API

// Apply custom profile
app := cartridge.NewGeneric(cartridge.WithProfile(customProfile))
```

### Profile Modification

```go
// Get and inspect default profiles
profiles := cartridge.GetDefaultProfiles()
inertiaProfile := profiles[cartridge.AppTypeFullStackInertia]

log.Printf("Inertia profile paths:")
log.Printf("  Templates: %s", inertiaProfile.TemplatePath)
log.Printf("  Assets: %s", inertiaProfile.AssetsPath)
log.Printf("  Dev Server: %s", inertiaProfile.DevServerURL)
```

## Migration Guide

### From Legacy Cartridge

```go
// Old way (still works for backward compatibility)
app := cartridge.New(cartridge.WithPort("3000"))

// New way (explicit about intent)
app := cartridge.NewFullStack(cartridge.WithPort("3000"))
```

### Between App Types

```go
// Easy to switch between app types
// From:
app := cartridge.NewFullStack()

// To:
app := cartridge.NewFullStackInertia()
// Most settings transfer automatically via profiles
```

## Testing

The unified architecture is thoroughly tested:

```bash
# Run all tests
go test -v .

# Run only unified architecture tests  
go test -v -run TestUnified

# Run profile tests
go test -v -run TestProfile
```

## Summary

The unified architecture provides:

- **Developer Experience**: Convention over configuration with smart defaults
- **Flexibility**: Full customization when needed
- **Consistency**: Common infrastructure across all app types  
- **Maintainability**: Single codebase, multiple optimized configurations
- **Extensibility**: Easy to add new app types and features

This architecture ensures that Cartridge scales from simple prototypes to complex production applications while maintaining a simple, user-friendly API.