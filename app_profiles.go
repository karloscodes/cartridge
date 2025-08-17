package cartridge

// AppProfile defines the configuration profile for different application types
// This creates a unified, config-based core that all app types can build upon
type AppProfile struct {
	Name        string
	Description string
	
	// Core middleware settings
	EnableCSRF      bool
	EnableCORS      bool
	EnableRateLimit bool
	EnableSessions  bool
	
	// Security and routing
	CSRFExcludedPaths []string
	TrustedProxies    []string
	
	// Features and integrations
	EnableInertia    bool
	EnableTemplates  bool
	EnableStatic     bool
	EnableDatabase   bool
	EnableAuth       bool
	EnableCron       bool
	EnableAsync      bool
	
	// Convention-based defaults
	StaticPath       string
	TemplatePath     string
	MigrationPath    string
	AssetsPath       string
	
	// Environment-specific overrides
	DevServerURL     string
	
	// Custom initializers
	InertiaConfig    *InertiaConfig
	MiddlewareStack  []MiddlewareConfig
}

// GetDefaultProfiles returns the predefined application profiles
func GetDefaultProfiles() map[AppType]AppProfile {
	return map[AppType]AppProfile{
		AppTypeGeneric: {
			Name:        "Generic",
			Description: "Flexible application with balanced defaults",
			
			EnableCSRF:      true,
			EnableCORS:      false,
			EnableRateLimit: false,
			EnableSessions:  true,
			EnableTemplates: true,
			EnableStatic:    true,
			EnableDatabase:  true,
			EnableAuth:      true,
			EnableCron:      true,
			EnableAsync:     true,
			
			CSRFExcludedPaths: []string{"/api/", "/static/", "/_health", "/_ready", "/_live"},
			TrustedProxies:    []string{},
			
			StaticPath:    "./public",
			TemplatePath:  "./templates",
			MigrationPath: "./migrations",
			AssetsPath:    "./assets",
		},
		
		AppTypeFullStack: {
			Name:        "FullStack",
			Description: "Traditional server-rendered web application",
			
			EnableCSRF:      true,
			EnableCORS:      false,
			EnableRateLimit: false,
			EnableSessions:  true,
			EnableTemplates: true,
			EnableStatic:    true,
			EnableDatabase:  true,
			EnableAuth:      true,
			EnableCron:      true,
			EnableAsync:     true,
			
			CSRFExcludedPaths: []string{"/api/", "/static/", "/_health", "/_ready", "/_live"},
			TrustedProxies:    []string{},
			
			StaticPath:    "./public",
			TemplatePath:  "./templates",
			MigrationPath: "./migrations",
			AssetsPath:    "./assets",
		},
		
		AppTypeAPIOnly: {
			Name:        "APIOnly",
			Description: "Lightweight API-focused application",
			
			EnableCSRF:      false,  // APIs typically don't need CSRF
			EnableCORS:      true,   // APIs need CORS for cross-origin requests
			EnableRateLimit: true,   // APIs benefit from rate limiting
			EnableSessions:  false,  // APIs are typically stateless
			EnableTemplates: false,  // APIs don't render HTML
			EnableStatic:    false,  // APIs don't serve static files
			EnableDatabase:  true,   // APIs usually need data persistence
			EnableAuth:      true,   // APIs need authentication/authorization
			EnableCron:      true,   // Background jobs are common in APIs
			EnableAsync:     true,   // Async processing is common in APIs
			
			CSRFExcludedPaths: []string{"/api/", "/_health", "/_ready", "/_live"}, // More permissive for APIs
			TrustedProxies:    []string{},
			
			StaticPath:    "./public",     // Keep defaults but won't be used
			TemplatePath:  "./templates",  // Keep defaults but won't be used
			MigrationPath: "./migrations",
			AssetsPath:    "./assets",
		},
		
		AppTypeFullStackInertia: {
			Name:        "FullStackInertia",
			Description: "Modern SPA with server-side routing using Inertia.js",
			
			EnableCSRF:      true,   // Inertia needs CSRF for forms
			EnableCORS:      false,  // Inertia doesn't need CORS (same-origin)
			EnableRateLimit: false,  // Usually not needed for SPAs
			EnableSessions:  true,   // Inertia benefits from sessions
			EnableInertia:   true,   // Enable Inertia.js integration
			EnableTemplates: false,  // Inertia uses its own template system
			EnableStatic:    true,   // Need to serve compiled assets
			EnableDatabase:  true,   // SPAs usually need data
			EnableAuth:      true,   // Authentication is common
			EnableCron:      true,   // Background jobs may be needed
			EnableAsync:     true,   // Async processing may be needed
			
			// Inertia-specific CSRF exclusions
			CSRFExcludedPaths: []string{"/api/", "/static/", "/_health", "/_ready", "/_live", "/inertia/", "/_inertia/"},
			TrustedProxies:    []string{},
			
			StaticPath:    "./public",
			TemplatePath:  "./resources/views", // Inertia convention
			MigrationPath: "./migrations",
			AssetsPath:    "./resources/assets", // Inertia/Vite convention
			DevServerURL:  "http://localhost:5173", // Vite default
			
			InertiaConfig: &InertiaConfig{
				Version:      "1.0.0",
				RootTemplate: "./resources/views/app.html",
				AssetsPath:   "./public",
				SSR:          false,
				DevServer:    "http://localhost:5173",
			},
		},
	}
}

// ApplyProfile applies an application profile to the CartridgeConfig
func ApplyProfile(config *CartridgeConfig, profile AppProfile) {
	// Apply middleware settings
	config.EnableCSRF = profile.EnableCSRF
	config.EnableCORS = profile.EnableCORS
	config.EnableRateLimit = profile.EnableRateLimit
	
	// Apply security settings
	config.CSRFExcludedPaths = make([]string, len(profile.CSRFExcludedPaths))
	copy(config.CSRFExcludedPaths, profile.CSRFExcludedPaths)
	
	config.TrustedProxies = make([]string, len(profile.TrustedProxies))
	copy(config.TrustedProxies, profile.TrustedProxies)
}

// GetProfileForType returns the default profile for an app type
func GetProfileForType(appType AppType) AppProfile {
	profiles := GetDefaultProfiles()
	if profile, exists := profiles[appType]; exists {
		return profile
	}
	// Fallback to Generic if type not found
	return profiles[AppTypeGeneric]
}

// WithProfile creates an AppOption that applies a custom profile
func WithProfile(profile AppProfile) AppOption {
	return func(config *CartridgeConfig) {
		ApplyProfile(config, profile)
	}
}

// WithFeature enables or disables a specific feature
func WithFeature(feature string, enabled bool) AppOption {
	return func(config *CartridgeConfig) {
		// This can be extended to support dynamic feature toggling
		switch feature {
		case "csrf":
			config.EnableCSRF = enabled
		case "cors":
			config.EnableCORS = enabled
		case "ratelimit":
			config.EnableRateLimit = enabled
		}
	}
}