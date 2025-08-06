package config

import (
	"os"
	"testing"
)

func TestConfig_New(t *testing.T) {
	// Save original environment
	originalEnv := os.Getenv("APP_ENV")
	originalPort := os.Getenv("APP_PORT")
	
	// Clean up after test
	defer func() {
		os.Setenv("APP_ENV", originalEnv)
		os.Setenv("APP_PORT", originalPort)
	}()
	
	// Test with environment variables
	os.Setenv("APP_ENV", "test")
	os.Setenv("APP_PORT", "8080")
	
	cfg := New()
	
	if cfg.Environment != "test" {
		t.Errorf("Expected environment 'test', got '%s'", cfg.Environment)
	}
	
	if cfg.Port != "8080" {
		t.Errorf("Expected port '8080', got '%s'", cfg.Port)
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	cfg := &Config{Environment: "development"}
	if !cfg.IsDevelopment() {
		t.Error("Expected IsDevelopment() to return true for development environment")
	}
	
	cfg.Environment = "production"
	if cfg.IsDevelopment() {
		t.Error("Expected IsDevelopment() to return false for production environment")
	}
}

func TestConfig_IsProduction(t *testing.T) {
	cfg := &Config{Environment: "production"}
	if !cfg.IsProduction() {
		t.Error("Expected IsProduction() to return true for production environment")
	}
	
	cfg.Environment = "development"
	if cfg.IsProduction() {
		t.Error("Expected IsProduction() to return false for development environment")
	}
}

func TestConfig_IsTest(t *testing.T) {
	cfg := &Config{Environment: "test"}
	if !cfg.IsTest() {
		t.Error("Expected IsTest() to return true for test environment")
	}
	
	cfg.Environment = "development"
	if cfg.IsTest() {
		t.Error("Expected IsTest() to return false for development environment")
	}
}

func TestGetDefaultDatabaseURL(t *testing.T) {
	tests := []struct {
		env      string
		expected string
	}{
		{"test", ":memory:"},
		{"production", "data/production.db"},
		{"development", "data/development.db"},
		{"unknown", "data/development.db"},
	}
	
	for _, test := range tests {
		result := getDefaultDatabaseURL(test.env)
		if result != test.expected {
			t.Errorf("Expected %s for environment %s, got %s", test.expected, test.env, result)
		}
	}
}
