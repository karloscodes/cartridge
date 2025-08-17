package cartridge

import (
	"testing"
)

// TestUnifiedArchitecture validates that the new unified, config-based architecture works correctly
func TestUnifiedArchitecture(t *testing.T) {
	t.Run("ProfileBasedConfiguration", func(t *testing.T) {
		// Test that each app type gets the correct profile
		testCases := []struct {
			name     string
			factory  func(...AppOption) *App
			appType  AppType
			expected map[string]bool
		}{
			{
				name:    "APIOnly",
				factory: NewAPIOnly,
				appType: AppTypeAPIOnly,
				expected: map[string]bool{
					"csrf":      false, // APIs don't need CSRF
					"cors":      true,  // APIs need CORS
					"ratelimit": true,  // APIs benefit from rate limiting
					"sessions":  false, // APIs are stateless
					"templates": false, // APIs don't render HTML
					"static":    false, // APIs don't serve static files
					"inertia":   false, // APIs don't use Inertia
					"database":  true,  // APIs usually need data
					"auth":      true,  // APIs need auth
					"cron":      true,  // Background jobs common
					"async":     true,  // Async processing common
				},
			},
			{
				name:    "FullStack",
				factory: NewFullStack,
				appType: AppTypeFullStack,
				expected: map[string]bool{
					"csrf":      true,  // Web apps need CSRF
					"cors":      false, // Web apps typically same-origin
					"ratelimit": false, // Not typically needed
					"sessions":  true,  // Web apps use sessions
					"templates": true,  // Web apps render HTML
					"static":    true,  // Web apps serve static files
					"inertia":   false, // Traditional web apps don't use Inertia
					"database":  true,  // Web apps need data
					"auth":      true,  // Web apps need auth
					"cron":      true,  // Background jobs may be needed
					"async":     true,  // Async processing may be needed
				},
			},
			{
				name:    "FullStackInertia",
				factory: NewFullStackInertia,
				appType: AppTypeFullStackInertia,
				expected: map[string]bool{
					"csrf":      true,  // Inertia needs CSRF for forms
					"cors":      false, // Inertia is same-origin
					"ratelimit": false, // Not typically needed for SPAs
					"sessions":  true,  // Inertia benefits from sessions
					"templates": false, // Inertia uses its own templates
					"static":    true,  // Need to serve compiled assets
					"inertia":   true,  // Obviously needs Inertia
					"database":  true,  // SPAs usually need data
					"auth":      true,  // Auth is common
					"cron":      true,  // Background jobs may be needed
					"async":     true,  // Async processing may be needed
				},
			},
			{
				name:    "Generic",
				factory: NewGeneric,
				appType: AppTypeGeneric,
				expected: map[string]bool{
					"csrf":      true,  // Balanced default
					"cors":      false, // Balanced default
					"ratelimit": false, // Balanced default
					"sessions":  true,  // Most apps use sessions
					"templates": true,  // Most apps render HTML
					"static":    true,  // Most apps serve static files
					"inertia":   false, // Not enabled by default
					"database":  true,  // Most apps need data
					"auth":      true,  // Most apps need auth
					"cron":      true,  // Background jobs common
					"async":     true,  // Async processing common
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				app := tc.factory()

				// Test app type
				if app.appType != tc.appType {
					t.Errorf("Expected app type %v, got %v", tc.appType, app.appType)
				}

				// Test profile name
				profile := app.Profile()
				if profile.Name != tc.name {
					t.Errorf("Expected profile name %s, got %s", tc.name, profile.Name)
				}

				// Test feature configuration
				for feature, expected := range tc.expected {
					actual := app.IsFeatureEnabled(feature)
					if actual != expected {
						t.Errorf("Feature %s: expected %v, got %v", feature, expected, actual)
					}
				}
			})
		}
	})

	t.Run("ConventionOverConfiguration", func(t *testing.T) {
		// Test that convention-based paths are set correctly
		inertiaApp := NewFullStackInertia()
		profile := inertiaApp.Profile()

		expectedPaths := map[string]string{
			"templates":  "./resources/views",
			"assets":     "./resources/assets",
			"static":     "./public",
			"migrations": "./migrations",
		}

		actualPaths := map[string]string{
			"templates":  profile.TemplatePath,
			"assets":     profile.AssetsPath,
			"static":     profile.StaticPath,
			"migrations": profile.MigrationPath,
		}

		for pathType, expected := range expectedPaths {
			if actual := actualPaths[pathType]; actual != expected {
				t.Errorf("Inertia app %s path: expected %s, got %s", pathType, expected, actual)
			}
		}

		// Test Vite dev server URL for Inertia
		if profile.DevServerURL != "http://localhost:5173" {
			t.Errorf("Expected Vite dev server URL http://localhost:5173, got %s", profile.DevServerURL)
		}
	})

	t.Run("UserOptionsOverrideDefaults", func(t *testing.T) {
		// Test that user options can override profile defaults
		app := NewAPIOnly(
			WithFeature("csrf", true), // Override: enable CSRF for this API
		)

		// CSRF should now be enabled, overriding the APIOnly default
		if !app.IsFeatureEnabled("csrf") {
			t.Error("Expected CSRF to be enabled after override")
		}

		// Other features should remain as per APIOnly profile
		if !app.IsFeatureEnabled("cors") {
			t.Error("Expected CORS to remain enabled for APIOnly")
		}
	})

	t.Run("InertiaIntegration", func(t *testing.T) {
		// Test that Inertia integration is properly set up for FullStackInertia apps
		inertiaApp := NewFullStackInertia()

		if !inertiaApp.IsInertiaApp() {
			t.Error("Expected IsInertiaApp() to return true for FullStackInertia")
		}

		if inertiaApp.Inertia() == nil {
			t.Error("Expected Inertia manager to be initialized")
		}

		// Test that non-Inertia apps don't have Inertia
		apiApp := NewAPIOnly()
		if apiApp.IsInertiaApp() {
			t.Error("Expected IsInertiaApp() to return false for APIOnly")
		}

		if apiApp.Inertia() != nil {
			t.Error("Expected Inertia manager to be nil for APIOnly")
		}
	})

	t.Run("BackwardCompatibility", func(t *testing.T) {
		// Test that the old New() function still works
		app := New()
		if app == nil {
			t.Error("Expected New() to create an app")
		}

		// Should create a Generic app
		if app.appType != AppTypeGeneric {
			t.Errorf("Expected New() to create Generic app, got %v", app.appType)
		}
	})
}

// TestProfileCustomization tests custom profile creation and modification
func TestProfileCustomization(t *testing.T) {
	t.Run("CustomProfile", func(t *testing.T) {
		// Create a custom profile for a specialized use case
		customProfile := GetProfileForType(AppTypeAPIOnly)
		customProfile.Name = "CustomAPI"
		customProfile.EnableSessions = true // Enable sessions for this API

		// This would be used like: app := newAppWithProfile(AppTypeGeneric, WithProfile(customProfile))
		// For now, just test that the profile has the expected values
		if !customProfile.EnableSessions {
			t.Error("Expected custom profile to have sessions enabled")
		}

		if customProfile.EnableCSRF {
			t.Error("Expected custom profile to inherit CSRF disabled from APIOnly")
		}
	})

	t.Run("ProfileDefaults", func(t *testing.T) {
		profiles := GetDefaultProfiles()

		// Test that all required app types have profiles
		requiredTypes := []AppType{
			AppTypeGeneric,
			AppTypeFullStack,
			AppTypeAPIOnly,
			AppTypeFullStackInertia,
		}

		for _, appType := range requiredTypes {
			if _, exists := profiles[appType]; !exists {
				t.Errorf("Missing profile for app type %v", appType)
			}
		}

		// Test that each profile has a name and description
		for appType, profile := range profiles {
			if profile.Name == "" {
				t.Errorf("Profile for app type %v has empty name", appType)
			}
			if profile.Description == "" {
				t.Errorf("Profile for app type %v has empty description", appType)
			}
		}
	})
}