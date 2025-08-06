package cartridge

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"
)

// AssetConfig holds asset serving configuration
type AssetConfig struct {
	StaticPath        string
	CacheDuration     string
	EnableCompression bool
	EnableBrowsing    bool
	IndexFile         string
}

// DefaultAssetConfig returns default asset configuration based on environment
func DefaultAssetConfig(cfg *Config) AssetConfig {
	if cfg.IsDevelopment() {
		return AssetConfig{
			StaticPath:        "./static",
			CacheDuration:     "0",
			EnableCompression: false,
			EnableBrowsing:    true,
			IndexFile:         "index.html",
		}
	}

	return AssetConfig{
		StaticPath:        "/static",
		CacheDuration:     "24h",
		EnableCompression: true,
		EnableBrowsing:    false,
		IndexFile:         "index.html",
	}
}

// AssetManager handles static asset serving
type AssetManager struct {
	config      AssetConfig
	logger      Logger
	staticFS    embed.FS
	templateFS  embed.FS
	useEmbedded bool
}

// NewAssetManager creates a new asset manager
func NewAssetManager(config AssetConfig, logger Logger) *AssetManager {
	return &AssetManager{
		config:      config,
		logger:      logger,
		useEmbedded: config.StaticPath == "/static", // Use embedded if path starts with /
	}
}

// SetEmbeddedFS sets the embedded filesystem for static assets
func (am *AssetManager) SetEmbeddedFS(staticFS, templateFS embed.FS) {
	am.staticFS = staticFS
	am.templateFS = templateFS
	am.useEmbedded = true
	am.logger.Debug("Embedded filesystem configured for assets")
}

// GetStaticFileSystem returns the filesystem for static assets
func (am *AssetManager) GetStaticFileSystem() fs.FS {
	if am.useEmbedded && am.staticFS != (embed.FS{}) {
		// Return subdirectory of embedded FS
		subFS, err := fs.Sub(am.staticFS, "static")
		if err != nil {
			am.logger.Warn("Failed to get static subdirectory from embedded FS",
				"error", err)
			return am.staticFS
		}
		return subFS
	}

	// Return OS filesystem for development
	return nil // This would return os.DirFS(am.config.StaticPath) when available
}

// GetTemplateFileSystem returns the filesystem for templates
func (am *AssetManager) GetTemplateFileSystem() fs.FS {
	if am.useEmbedded && am.templateFS != (embed.FS{}) {
		// Return subdirectory of embedded FS
		subFS, err := fs.Sub(am.templateFS, "templates")
		if err != nil {
			am.logger.Warn("Failed to get templates subdirectory from embedded FS",
				"error", err)
			return am.templateFS
		}
		return subFS
	}

	// Return OS filesystem for development
	return nil // This would return os.DirFS("./templates") when available
}

// SetupStaticRoutes configures static asset routes
func (am *AssetManager) SetupStaticRoutes(app interface{}) {
	am.logger.Info("Setting up static asset routes",
		"path", am.config.StaticPath,
		"cache_duration", am.config.CacheDuration,
		"compression", am.config.EnableCompression)

	// TODO: This would configure Fiber static routes when available
	/*
		fiberApp := app.(*fiber.App)

		if am.useEmbedded {
			// Use embedded filesystem
			fiberApp.Static("/static", "./", fiber.Static{
				Embed:      am.staticFS,
				MaxAge:     parseDuration(am.config.CacheDuration),
				Compress:   am.config.EnableCompression,
				Browse:     am.config.EnableBrowsing,
				Index:      am.config.IndexFile,
			})
		} else {
			// Use OS filesystem
			fiberApp.Static("/static", am.config.StaticPath, fiber.Static{
				MaxAge:   parseDuration(am.config.CacheDuration),
				Compress: am.config.EnableCompression,
				Browse:   am.config.EnableBrowsing,
				Index:    am.config.IndexFile,
			})
		}
	*/
}

// GetAssetPath returns the path for a static asset
func (am *AssetManager) GetAssetPath(filename string) string {
	if am.useEmbedded {
		return "/static/" + filename
	}
	return "/static/" + filename
}

// AssetExists checks if an asset exists
func (am *AssetManager) AssetExists(filename string) bool {
	if am.useEmbedded && am.staticFS != (embed.FS{}) {
		path := filepath.Join("static", filename)
		_, err := am.staticFS.Open(path)
		return err == nil
	}

	// This would check OS filesystem when available
	return false
}

// ReadAsset reads an asset file
func (am *AssetManager) ReadAsset(filename string) ([]byte, error) {
	if am.useEmbedded && am.staticFS != (embed.FS{}) {
		path := filepath.Join("static", filename)
		return am.staticFS.ReadFile(path)
	}

	// This would read from OS filesystem when available
	return nil, fs.ErrNotExist
}

// ListAssets lists all assets in a directory
func (am *AssetManager) ListAssets(dir string) ([]string, error) {
	var assets []string

	if am.useEmbedded && am.staticFS != (embed.FS{}) {
		basePath := filepath.Join("static", dir)
		err := fs.WalkDir(am.staticFS, basePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() {
				// Remove the base path prefix
				relPath := strings.TrimPrefix(path, basePath)
				relPath = strings.TrimPrefix(relPath, "/")
				if relPath != "" {
					assets = append(assets, relPath)
				}
			}

			return nil
		})

		return assets, err
	}

	// This would list OS filesystem when available
	return assets, nil
}

// AssetInfo returns information about an asset
type AssetInfo struct {
	Name    string
	Size    int64
	ModTime string
	IsDir   bool
}

// GetAssetInfo returns information about an asset
func (am *AssetManager) GetAssetInfo(filename string) (*AssetInfo, error) {
	if am.useEmbedded && am.staticFS != (embed.FS{}) {
		path := filepath.Join("static", filename)
		info, err := fs.Stat(am.staticFS, path)
		if err != nil {
			return nil, err
		}

		return &AssetInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
			IsDir:   info.IsDir(),
		}, nil
	}

	return nil, fs.ErrNotExist
}

// CSS helper functions
func (am *AssetManager) CSS(filename string) string {
	return am.GetAssetPath("css/" + filename)
}

// JS helper functions
func (am *AssetManager) JS(filename string) string {
	return am.GetAssetPath("js/" + filename)
}

// Image helper functions
func (am *AssetManager) Image(filename string) string {
	return am.GetAssetPath("images/" + filename)
}

// Icon helper functions
func (am *AssetManager) Icon(filename string) string {
	return am.GetAssetPath("icons/" + filename)
}

// Font helper functions
func (am *AssetManager) Font(filename string) string {
	return am.GetAssetPath("fonts/" + filename)
}

// AssetVersioning provides simple asset versioning for cache busting
type AssetVersioning struct {
	version string
	enabled bool
}

// NewAssetVersioning creates a new asset versioning instance
func NewAssetVersioning(version string, enabled bool) *AssetVersioning {
	return &AssetVersioning{
		version: version,
		enabled: enabled,
	}
}

// VersionedPath returns a versioned asset path
func (av *AssetVersioning) VersionedPath(path string) string {
	if !av.enabled || av.version == "" {
		return path
	}

	// Add version as query parameter
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}

	return path + separator + "v=" + av.version
}

// ManifestAssets provides asset manifest support for more advanced versioning
type ManifestAssets struct {
	manifest map[string]string
	enabled  bool
}

// NewManifestAssets creates a new manifest assets instance
func NewManifestAssets(manifest map[string]string, enabled bool) *ManifestAssets {
	return &ManifestAssets{
		manifest: manifest,
		enabled:  enabled,
	}
}

// ManifestPath returns the manifest path for an asset
func (ma *ManifestAssets) ManifestPath(path string) string {
	if !ma.enabled || ma.manifest == nil {
		return path
	}

	if versionedPath, exists := ma.manifest[path]; exists {
		return versionedPath
	}

	return path
}

// GetContentType returns the MIME type for a file extension
func GetContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
