package inertia

import (
	"encoding/json"
	"html"
	"os"
	"strings"
	"sync"

	"fusionaly/internal/config"
	"github.com/karloscodes/cartridge/pkg/flash"

	"github.com/gofiber/fiber/v2"
	inertiapkg "github.com/petaki/inertia-go"
)

// ManifestEntry represents an entry in the Vite manifest
type ManifestEntry struct {
	File    string   `json:"file"`
	Name    string   `json:"name"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	Imports []string `json:"imports"`
	CSS     []string `json:"css"`
}

var (
	manifestOnce sync.Once
	jsFile       string
	cssFile      string
)

// readManifest reads the Vite manifest and returns JS and CSS paths
func readManifest() (js, css string) {
	// Default fallback paths (without hashes)
	js = "/assets/inertia.js"
	css = "/assets/inertia.css"

	// Try to read the manifest file
	data, err := os.ReadFile("web/dist/.vite/manifest.json")
	if err != nil {
		return // Use fallback paths
	}

	var manifest map[string]ManifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		return // Use fallback paths
	}

	// Find the entry point (src/inertia.tsx)
	if entry, ok := manifest["src/inertia.tsx"]; ok {
		js = "/" + entry.File
		if len(entry.CSS) > 0 {
			css = "/" + entry.CSS[0]
		}
	}
	return
}

// loadManifest reads the Vite manifest and extracts asset paths
// In development mode, it reloads the manifest on every request to support hot rebuilds
// In production, it caches the manifest for performance
func loadManifest() {
	cfg := config.GetConfig()
	if cfg.IsDevelopment() {
		// Always reload in development to pick up new builds
		jsFile, cssFile = readManifest()
		return
	}

	// Cache in production
	manifestOnce.Do(func() {
		jsFile, cssFile = readManifest()
	})
}

// Props is a type alias for map[string]interface{} to make handler code cleaner
// Usage: props := inertia.Props{"title": "Dashboard", "data": myData}
type Props = map[string]interface{}

// DeferredProp wraps a function that will be called only when the prop is requested
// via partial reload. On initial page load, deferred props are excluded.
type DeferredProp struct {
	Callback func() interface{}
	Group    string // Optional group name for batching requests
}

// Defer creates a deferred prop that loads lazily after initial page render
func Defer(callback func() interface{}) DeferredProp {
	return DeferredProp{Callback: callback}
}

// DeferGroup creates a deferred prop with a group name for batching
func DeferGroup(callback func() interface{}, group string) DeferredProp {
	return DeferredProp{Callback: callback, Group: group}
}

// RenderPage is a convenience wrapper that creates an Inertia instance and renders a component
// Usage: return inertia.RenderPage(c, "Dashboard", props)
func RenderPage(c *fiber.Ctx, component string, props map[string]interface{}) error {
	i := inertiapkg.New("web/dist/inertia.html", "app", "v1")
	return Render(c, i, component, props)
}

// Render sends an Inertia response
// Automatically detects if request is Inertia (AJAX) or initial page load
// Automatically injects flash messages from context if available
// Supports deferred props via X-Inertia-Partial-Data header
func Render(c *fiber.Ctx, i *inertiapkg.Inertia, component string, props map[string]interface{}) error {
	// Load asset paths from manifest (only done once)
	loadManifest()

	// Build full URL with query string for proper Inertia navigation
	fullURL := c.Path()
	if queryString := string(c.Request().URI().QueryString()); queryString != "" {
		fullURL = fullURL + "?" + queryString
	}

	// Auto-inject flash message if not already set
	if _, exists := props["flash"]; !exists {
		props["flash"] = flash.GetFlash(c)
	}

	// Check if this is an Inertia request (subsequent navigation)
	if c.Get("X-Inertia") != "" {
		// Set required Inertia response headers
		c.Set("X-Inertia", "true")
		c.Set("Vary", "X-Inertia")

		// Check for partial reload (deferred props request)
		partialData := c.Get("X-Inertia-Partial-Data")
		partialComponent := c.Get("X-Inertia-Partial-Component")

		// If this is a partial reload request, only return requested props
		if partialData != "" && (partialComponent == "" || partialComponent == component) {
			resolvedProps := resolveProps(props, partialData, partialComponent, component)
			return c.JSON(fiber.Map{
				"component": component,
				"props":     resolvedProps,
				"url":       fullURL,
				"version":   "v1",
			})
		}

		// For full Inertia navigation, exclude deferred props and include deferredProps metadata
		resolvedProps, deferredKeys := resolvePropsForInitialLoad(props)

		response := fiber.Map{
			"component": component,
			"props":     resolvedProps,
			"url":       fullURL,
			"version":   "v1",
		}

		// Add deferred props metadata if any exist
		if len(deferredKeys) > 0 {
			response["deferredProps"] = deferredKeys
		}

		return c.JSON(response)
	}

	// Initial page load - exclude deferred props and collect their names
	resolvedProps, deferredKeys := resolvePropsForInitialLoad(props)

	page := map[string]interface{}{
		"component": component,
		"props":     resolvedProps,
		"url":       fullURL,
		"version":   "v1", // Static version for now
	}

	// Add deferred props metadata if any exist
	if len(deferredKeys) > 0 {
		page["deferredProps"] = deferredKeys
	}

	pageJSON, err := json.Marshal(page)
	if err != nil {
		return err
	}

	// Send file directly
	c.Set("Content-Type", "text/html")

	// Build CSS link tag only if we have a CSS file
	cssLink := ""
	if cssFile != "" {
		cssLink = `<link rel="stylesheet" href="` + cssFile + `">`
	}

	// Use manifest-resolved asset paths and HTML-escape the JSON to prevent attribute injection
	htmlContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Fusionaly</title>
    ` + cssLink + `
</head>
<body>
    <div id="app" data-page='` + html.EscapeString(string(pageJSON)) + `'></div>
    <script type="module" src="` + jsFile + `"></script>
</body>
</html>`

	return c.SendString(htmlContent)
}

// resolveProps handles partial reload requests for deferred props
func resolveProps(props map[string]interface{}, partialData, partialComponent, component string) map[string]interface{} {
	// If not a partial reload or wrong component, return all non-deferred props
	if partialData == "" || (partialComponent != "" && partialComponent != component) {
		resolved := make(map[string]interface{})
		for key, value := range props {
			if deferred, ok := value.(DeferredProp); ok {
				// Execute deferred prop callback
				resolved[key] = deferred.Callback()
			} else {
				resolved[key] = value
			}
		}
		return resolved
	}

	// Parse requested prop names from comma-separated list
	requestedProps := make(map[string]bool)
	for _, prop := range strings.Split(partialData, ",") {
		requestedProps[strings.TrimSpace(prop)] = true
	}

	// Only return requested props, executing deferred callbacks as needed
	resolved := make(map[string]interface{})
	for key, value := range props {
		if requestedProps[key] {
			if deferred, ok := value.(DeferredProp); ok {
				resolved[key] = deferred.Callback()
			} else {
				resolved[key] = value
			}
		}
	}

	return resolved
}

// resolvePropsForInitialLoad excludes deferred props and returns their keys
func resolvePropsForInitialLoad(props map[string]interface{}) (map[string]interface{}, map[string][]string) {
	resolved := make(map[string]interface{})
	deferredKeys := make(map[string][]string) // group name -> prop keys

	for key, value := range props {
		if deferred, ok := value.(DeferredProp); ok {
			// Collect deferred prop keys by group
			group := deferred.Group
			if group == "" {
				group = "default"
			}
			deferredKeys[group] = append(deferredKeys[group], key)
		} else {
			resolved[key] = value
		}
	}

	return resolved, deferredKeys
}
