package cartridge

import (
	"testing"
)

func TestNewFullStackInertia(t *testing.T) {
	// Test that NewFullStackInertia creates an app with the correct type
	app := NewFullStackInertia()
	
	if app == nil {
		t.Fatal("Expected NewFullStackInertia to return an app, got nil")
	}
	
	// The app should have the right configuration for Inertia.js
	// This includes CSRF enabled, CORS disabled, and specific path exclusions
	if !app.config.EnableCSRF {
		t.Error("Expected FullStackInertia app to have CSRF enabled")
	}
	
	if app.config.EnableCORS {
		t.Error("Expected FullStackInertia app to have CORS disabled")
	}
	
	if app.config.EnableRateLimit {
		t.Error("Expected FullStackInertia app to have rate limiting disabled")
	}
	
	// Check for Inertia-specific CSRF exclusions
	found := false
	for _, path := range app.config.CSRFExcludedPaths {
		if path == "/inertia/" || path == "/_inertia/" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected FullStackInertia app to have Inertia-specific CSRF exclusions")
	}
	
	// Test Inertia-specific features
	if !app.IsInertiaApp() {
		t.Error("Expected IsInertiaApp() to return true for FullStackInertia app")
	}
	
	if app.Inertia() == nil {
		t.Error("Expected Inertia manager to be initialized")
	}
}

func TestAppTypeConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		factory        func(...AppOption) *App
		expectedCSRF   bool
		expectedCORS   bool
		expectedLimit  bool
	}{
		{
			name:           "FullStack",
			factory:        NewFullStack,
			expectedCSRF:   true,
			expectedCORS:   false,
			expectedLimit:  false,
		},
		{
			name:           "APIOnly",
			factory:        NewAPIOnly,
			expectedCSRF:   false,
			expectedCORS:   true,
			expectedLimit:  true,
		},
		{
			name:           "FullStackInertia",
			factory:        NewFullStackInertia,
			expectedCSRF:   true,
			expectedCORS:   false,
			expectedLimit:  false,
		},
		{
			name:           "Generic",
			factory:        New,
			expectedCSRF:   true,
			expectedCORS:   false,
			expectedLimit:  false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := tt.factory()
			
			if app.config.EnableCSRF != tt.expectedCSRF {
				t.Errorf("Expected CSRF to be %v, got %v", tt.expectedCSRF, app.config.EnableCSRF)
			}
			
			if app.config.EnableCORS != tt.expectedCORS {
				t.Errorf("Expected CORS to be %v, got %v", tt.expectedCORS, app.config.EnableCORS)
			}
			
			if app.config.EnableRateLimit != tt.expectedLimit {
				t.Errorf("Expected RateLimit to be %v, got %v", tt.expectedLimit, app.config.EnableRateLimit)
			}
		})
	}
}