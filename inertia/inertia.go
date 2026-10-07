package inertia

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/karloscodes/cartridge/flash"
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
	assetVersion string                   // Hash of the built asset paths, sent as the Inertia version
	devMode      bool                     // When true, re-read manifest on every request
	pageTitle    string                   // HTML page title; empty by default, set with SetTitle
	manifestData []byte                   // Embedded manifest data (used when filesystem not available)
	scriptPage   bool                     // Send the first page in a JSON script element (Inertia v3)
	manifest     map[string]ManifestEntry // Parsed Vite manifest, for preload links
)

// entryKey is the Vite entry; pagesDir holds one module per Inertia page,
// named like the component ("Dashboard" -> src/pages/Dashboard.tsx).
const (
	entryKey = "src/inertia.tsx"
	pagesDir = "src/pages/"
)

// SetScriptElement chooses how the first page reaches the client. Inertia v3
// reads it from <script data-page="app" type="application/json">; v2 reads
// the data-page attribute on the root div, which stays the default so apps on
// the v2 client keep working.
func SetScriptElement(enabled bool) {
	scriptPage = enabled
}

// SetDevMode enables or disables development mode.
// In dev mode, the manifest is re-read on every request to pick up
// changes from vite rebuilds without restarting the server.
func SetDevMode(enabled bool) {
	devMode = enabled
}

// SetTitle sets the HTML page title for server-rendered pages.
func SetTitle(title string) {
	pageTitle = title
}

// SetManifestData sets the embedded manifest JSON data.
// Use this when the manifest is embedded in the binary and not available on filesystem.
func SetManifestData(data []byte) {
	manifestData = data
}

// readManifest reads the Vite manifest and returns JS and CSS paths
func readManifest() (js, css string, entries map[string]ManifestEntry) {
	// Default fallback paths (without hashes)
	js = "/assets/inertia.js"
	css = "/assets/inertia.css"

	// Try to read the manifest file from multiple locations
	var data []byte
	var err error

	// Try dist/.vite/manifest.json first (production build location)
	data, err = os.ReadFile("dist/.vite/manifest.json")
	if err != nil {
		// Fallback to web/dist/.vite/manifest.json
		data, err = os.ReadFile("web/dist/.vite/manifest.json")
		if err != nil {
			// Use embedded manifest data if available
			if len(manifestData) > 0 {
				data = manifestData
			} else {
				return // Use fallback paths
			}
		}
	}

	if err := json.Unmarshal(data, &entries); err != nil {
		return // Use fallback paths
	}

	// Find the entry point (src/inertia.tsx)
	if entry, ok := entries[entryKey]; ok {
		js = "/" + entry.File
		if len(entry.CSS) > 0 {
			css = "/" + entry.CSS[0]
		}
	}
	return
}

// loadManifest reads the Vite manifest and extracts asset paths.
// In production, uses sync.Once to cache for performance.
// In dev mode, re-reads on every call to pick up vite rebuilds.
func loadManifest() {
	if devMode {
		// In dev mode, always re-read the manifest
		jsFile, cssFile, manifest = readManifest()
		assetVersion = versionFor(jsFile, cssFile)
		return
	}

	// In production, cache the manifest
	manifestOnce.Do(func() {
		jsFile, cssFile, manifest = readManifest()
		assetVersion = versionFor(jsFile, cssFile)
	})
}

// preloadTags lists the chunks the first page needs: the entry's shared
// imports and the page's own module with its imports and CSS. The browser then
// fetches them in parallel instead of finding each one after the last loads.
func preloadTags(component string) string {
	if manifest == nil {
		return ""
	}
	seen := map[string]bool{}
	var tags strings.Builder
	var walk func(key string)
	walk = func(key string) {
		entry, ok := manifest[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		if key != entryKey {
			tags.WriteString(`<link rel="modulepreload" href="/` + html.EscapeString(entry.File) + `">` + "\n    ")
			for _, css := range entry.CSS {
				if !seen[css] {
					seen[css] = true
					tags.WriteString(`<link rel="stylesheet" href="/` + html.EscapeString(css) + `">` + "\n    ")
				}
			}
		}
		for _, imp := range entry.Imports {
			walk(imp)
		}
	}
	walk(entryKey)
	walk(pagesDir + component + ".tsx")
	return tags.String()
}

// versionFor derives the Inertia asset version from the built asset paths.
// Vite puts a content hash in each file name, so a new build gives a new version.
func versionFor(js, css string) string {
	sum := sha256.Sum256([]byte(js + "|" + css))
	return hex.EncodeToString(sum[:8])
}

// Version returns the current Inertia asset version.
func Version() string {
	loadManifest()
	return assetVersion
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
// Usage: return inertia.RenderPage(w, r, "Dashboard", props)
func RenderPage(w http.ResponseWriter, r *http.Request, component string, props map[string]interface{}) error {
	return Render(w, r, component, props)
}

// Render sends an Inertia response
// Automatically detects if request is Inertia (AJAX) or initial page load
// Automatically injects flash messages from context if available
// Supports deferred props via X-Inertia-Partial-Data header
func Render(w http.ResponseWriter, r *http.Request, component string, props map[string]interface{}) error {
	// Load asset paths from manifest (cached in production, fresh in dev)
	loadManifest()

	// Build full URL with query string for proper Inertia navigation
	fullURL := r.URL.RequestURI()

	// Stale client after a deploy: ask it to do a full page load of the new assets.
	// Check before reading the flash, so the flash survives for the reloaded page.
	if r.Header.Get("X-Inertia") != "" && r.Method == http.MethodGet {
		if v := r.Header.Get("X-Inertia-Version"); v != "" && v != assetVersion {
			w.Header().Set("X-Inertia-Location", fullURL)
			w.Header().Set("X-Inertia-Version", assetVersion)
			w.WriteHeader(http.StatusConflict)
			return nil
		}
	}

	// Auto-inject flash message if not already set
	if _, exists := props["flash"]; !exists {
		props["flash"] = flash.GetFlash(w, r)
	}

	// The protocol expects an errors object on every page.
	if _, exists := props["errors"]; !exists {
		props["errors"] = map[string]interface{}{}
	}

	// HTML and JSON answers share a URL; caches must keep them apart.
	w.Header().Set("Vary", "X-Inertia")

	// Check if this is an Inertia request (subsequent navigation)
	if r.Header.Get("X-Inertia") != "" {
		// Set required Inertia response headers
		w.Header().Set("X-Inertia", "true")

		// Check for partial reload (deferred props request)
		partialData := r.Header.Get("X-Inertia-Partial-Data")
		partialExcept := r.Header.Get("X-Inertia-Partial-Except")
		partialComponent := r.Header.Get("X-Inertia-Partial-Component")

		// A partial reload returns only the requested props, minus the excluded ones.
		if (partialData != "" || partialExcept != "") && (partialComponent == "" || partialComponent == component) {
			resolvedProps := resolveProps(props, partialData, partialComponent, component)
			for _, key := range strings.Split(partialExcept, ",") {
				delete(resolvedProps, strings.TrimSpace(key))
			}
			resolvedProps["errors"] = props["errors"]
			return writeJSON(w, map[string]interface{}{
				"component": component,
				"props":     resolvedProps,
				"url":       fullURL,
				"version":   assetVersion,
			})
		}

		// For full Inertia navigation, exclude deferred props and include deferredProps metadata
		resolvedProps, deferredKeys := resolvePropsForInitialLoad(props)

		response := map[string]interface{}{
			"component": component,
			"props":     resolvedProps,
			"url":       fullURL,
			"version":   assetVersion,
		}

		// Add deferred props metadata if any exist
		if len(deferredKeys) > 0 {
			response["deferredProps"] = deferredKeys
		}

		return writeJSON(w, response)
	}

	// Initial page load - exclude deferred props and collect their names
	resolvedProps, deferredKeys := resolvePropsForInitialLoad(props)

	page := map[string]interface{}{
		"component": component,
		"props":     resolvedProps,
		"url":       fullURL,
		"version":   assetVersion,
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Cache control: dev mode never caches (prevents stale hashed asset references),
	// production allows caching but requires revalidation to pick up new deploys.
	// A stricter header set earlier, such as the session middleware's, stays.
	if devMode {
		w.Header().Set("Cache-Control", "no-store")
	} else if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	// Build CSS link tag only if we have a CSS file
	cssLink := ""
	if cssFile != "" {
		cssLink = `<link rel="stylesheet" href="` + cssFile + `">`
	}

	// v3 reads the page from a JSON script element. json.Marshal already
	// escapes < > &, and escaping / as well keeps "</script>" out of the body.
	// v2 reads the HTML-escaped data-page attribute on the root div.
	root := `<div id="app" data-page='` + html.EscapeString(string(pageJSON)) + `'></div>`
	if scriptPage {
		root = `<script data-page="app" type="application/json">` +
			strings.ReplaceAll(string(pageJSON), "/", `\/`) + `</script><div id="app"></div>`
	}

	htmlContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <link rel="icon" type="image/svg+xml" href="/favicon.svg">
    <title>` + html.EscapeString(pageTitle) + `</title>
    ` + cssLink + `
    ` + preloadTags(component) + `
</head>
<body>
    ` + root + `
    <script type="module" src="` + jsFile + `"></script>
</body>
</html>`

	_, err = w.Write([]byte(htmlContent))
	return err
}

// writeJSON sends v as a JSON body.
func writeJSON(w http.ResponseWriter, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	return err
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
