package cartridge

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/romsar/gonertia"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// InertiaConfig holds configuration for Inertia.js integration
type InertiaConfig struct {
	// Version is the asset version for cache invalidation
	Version string
	// RootTemplate is the path to the root HTML template
	RootTemplate string
	// AssetsPath is the path to the assets directory
	AssetsPath string
	// SSR enables server-side rendering
	SSR bool
	// DevServer is the Vite dev server URL (development only)
	DevServer string
}

// DefaultInertiaConfig returns default Inertia.js configuration
func DefaultInertiaConfig() InertiaConfig {
	return InertiaConfig{
		Version:      "1.0.0",
		RootTemplate: "./resources/views/app.html",
		AssetsPath:   "./public",
		SSR:          false,
		DevServer:    "http://localhost:5173",
	}
}

// InertiaManager handles Inertia.js integration
type InertiaManager struct {
	config    InertiaConfig
	inertia   *gonertia.Inertia
	isDevMode bool
}

// NewInertiaManager creates a new Inertia.js manager
func NewInertiaManager(config InertiaConfig, isDevMode bool) *InertiaManager {
	return &InertiaManager{
		config:    config,
		isDevMode: isDevMode,
	}
}

// Setup initializes the Inertia.js integration
func (im *InertiaManager) Setup(app *fiber.App) error {
	// Create gonertia instance with proper API
	inertiaInstance, err := gonertia.New(
		im.getRootTemplate(),
		gonertia.WithVersion(im.config.Version),
	)
	if err != nil {
		return err
	}

	im.inertia = inertiaInstance

	// Add Inertia middleware
	app.Use(im.InertiaMiddleware())

	return nil
}

// InertiaMiddleware returns the Inertia.js middleware
func (im *InertiaManager) InertiaMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Set Inertia instance in context for handlers to use
		c.Locals("inertia", im.inertia)
		return c.Next()
	}
}

// Render renders an Inertia.js page
func (im *InertiaManager) Render(c *fiber.Ctx, component string, props map[string]interface{}) error {
	// Convert Fiber context to HTTP request/response for gonertia
	var req http.Request
	var w http.ResponseWriter

	// Use fasthttpadaptor to convert between fasthttp and net/http
	fasthttpadaptor.ConvertRequest(c.Context(), &req, true)
	
	// Create a custom response writer that writes to Fiber context
	w = &fiberResponseWriter{ctx: c}

	// Set props in context
	ctx := context.Background()
	for key, value := range props {
		ctx = gonertia.SetProp(ctx, key, value)
	}

	// Render with gonertia
	return im.inertia.Render(w, req.WithContext(ctx), component)
}

// Location performs an external redirect (full page reload)
func (im *InertiaManager) Location(c *fiber.Ctx, url string) error {
	// Convert Fiber context to HTTP for gonertia
	var req http.Request
	fasthttpadaptor.ConvertRequest(c.Context(), &req, true)
	w := &fiberResponseWriter{ctx: c}
	
	im.inertia.Location(w, req.WithContext(context.Background()), url)
	return nil
}

// Back redirects back to the previous page
func (im *InertiaManager) Back(c *fiber.Ctx) error {
	referer := c.Get("Referer")
	if referer == "" {
		referer = "/"
	}
	return c.Redirect(referer)
}

// fiberResponseWriter implements http.ResponseWriter for Fiber contexts
type fiberResponseWriter struct {
	ctx *fiber.Ctx
}

func (w *fiberResponseWriter) Header() http.Header {
	header := make(http.Header)
	w.ctx.Response().Header.VisitAll(func(key, value []byte) {
		header.Set(string(key), string(value))
	})
	return header
}

func (w *fiberResponseWriter) Write(data []byte) (int, error) {
	return w.ctx.Write(data)
}

func (w *fiberResponseWriter) WriteHeader(statusCode int) {
	w.ctx.Status(statusCode)
}

// getRootTemplate returns the root HTML template content
func (im *InertiaManager) getRootTemplate() string {
	// Default root template for Inertia.js
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0" />
    <title>{{ .Title }}</title>
    {{ if .IsDevMode }}
    <!-- Vite Dev Server -->
    <script type="module" src="http://localhost:5173/@vite/client"></script>
    <script type="module" src="http://localhost:5173/src/main.js"></script>
    {{ else }}
    <!-- Production Assets -->
    <link rel="stylesheet" href="/build/assets/app.css" />
    <script type="module" src="/build/assets/app.js"></script>
    {{ end }}
</head>
<body>
    <div id="app" data-page="{{ .Page }}"></div>
</body>
</html>`
}

// Share sets shared props in the Inertia context
func (im *InertiaManager) Share(ctx context.Context, key string, value interface{}) context.Context {
	return gonertia.SetProp(ctx, key, value)
}

// InertiaContext provides helper methods for Inertia.js in handlers
type InertiaContext struct {
	*Context
	manager *InertiaManager
}

// NewInertiaContext creates a new Inertia context wrapper
func NewInertiaContext(ctx *Context, manager *InertiaManager) *InertiaContext {
	return &InertiaContext{
		Context: ctx,
		manager: manager,
	}
}

// Render renders an Inertia.js component with props
func (ic *InertiaContext) Render(component string, props map[string]interface{}) error {
	return ic.manager.Render(ic.Fiber, component, props)
}

// Location performs an external redirect
func (ic *InertiaContext) Location(url string) error {
	return ic.manager.Location(ic.Fiber, url)
}

// Back redirects to the previous page
func (ic *InertiaContext) Back() error {
	return ic.manager.Back(ic.Fiber)
}