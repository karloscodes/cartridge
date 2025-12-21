package config

import (
	"testing"
)

func TestConfig_EnvironmentMethods(t *testing.T) {
	t.Run("IsDevelopment", func(t *testing.T) {
		cfg := &Config{Environment: Development}
		if !cfg.IsDevelopment() {
			t.Error("expected IsDevelopment to return true")
		}
		if cfg.IsProduction() {
			t.Error("expected IsProduction to return false")
		}
		if cfg.IsTest() {
			t.Error("expected IsTest to return false")
		}
	})

	t.Run("IsProduction", func(t *testing.T) {
		cfg := &Config{Environment: Production}
		if cfg.IsDevelopment() {
			t.Error("expected IsDevelopment to return false")
		}
		if !cfg.IsProduction() {
			t.Error("expected IsProduction to return true")
		}
		if cfg.IsTest() {
			t.Error("expected IsTest to return false")
		}
	})

	t.Run("IsTest", func(t *testing.T) {
		cfg := &Config{Environment: Test}
		if cfg.IsDevelopment() {
			t.Error("expected IsDevelopment to return false")
		}
		if cfg.IsProduction() {
			t.Error("expected IsProduction to return false")
		}
		if !cfg.IsTest() {
			t.Error("expected IsTest to return true")
		}
	})
}

func TestConfig_GetMaxOpenConns(t *testing.T) {
	t.Run("returns configured value when set", func(t *testing.T) {
		cfg := &Config{MaxOpenConns: 20}
		if cfg.GetMaxOpenConns() != 20 {
			t.Errorf("expected 20, got %d", cfg.GetMaxOpenConns())
		}
	})

	t.Run("returns 10 in production when not set", func(t *testing.T) {
		cfg := &Config{Environment: Production}
		if cfg.GetMaxOpenConns() != 10 {
			t.Errorf("expected 10, got %d", cfg.GetMaxOpenConns())
		}
	})

	t.Run("returns 1 in development when not set", func(t *testing.T) {
		cfg := &Config{Environment: Development}
		if cfg.GetMaxOpenConns() != 1 {
			t.Errorf("expected 1, got %d", cfg.GetMaxOpenConns())
		}
	})
}

func TestConfig_GetMaxIdleConns(t *testing.T) {
	t.Run("returns configured value when set", func(t *testing.T) {
		cfg := &Config{MaxIdleConns: 15}
		if cfg.GetMaxIdleConns() != 15 {
			t.Errorf("expected 15, got %d", cfg.GetMaxIdleConns())
		}
	})

	t.Run("returns 5 in production when not set", func(t *testing.T) {
		cfg := &Config{Environment: Production}
		if cfg.GetMaxIdleConns() != 5 {
			t.Errorf("expected 5, got %d", cfg.GetMaxIdleConns())
		}
	})

	t.Run("returns 1 in development when not set", func(t *testing.T) {
		cfg := &Config{Environment: Development}
		if cfg.GetMaxIdleConns() != 1 {
			t.Errorf("expected 1, got %d", cfg.GetMaxIdleConns())
		}
	})
}

func TestConfig_InterfaceMethods(t *testing.T) {
	cfg := &Config{
		AppName:        "testapp",
		Port:           "9000",
		LogLevel:       "debug",
		LogsDirectory:  "logs",
		LogsMaxSizeMB:  10,
		LogsMaxBackups: 5,
		LogsMaxAgeDays: 7,
		SessionSecret:  "secret",
		SessionTimeout: 3600,
	}

	if cfg.GetPort() != "9000" {
		t.Errorf("GetPort: expected 9000, got %s", cfg.GetPort())
	}
	if cfg.GetPublicDirectory() != "web/static" {
		t.Errorf("GetPublicDirectory: expected web/static, got %s", cfg.GetPublicDirectory())
	}
	if cfg.GetAssetsPrefix() != "/assets" {
		t.Errorf("GetAssetsPrefix: expected /assets, got %s", cfg.GetAssetsPrefix())
	}
	if cfg.GetLogLevel() != "debug" {
		t.Errorf("GetLogLevel: expected debug, got %s", cfg.GetLogLevel())
	}
	if cfg.GetLogDirectory() != "logs" {
		t.Errorf("GetLogDirectory: expected logs, got %s", cfg.GetLogDirectory())
	}
	if cfg.GetLogMaxSizeMB() != 10 {
		t.Errorf("GetLogMaxSizeMB: expected 10, got %d", cfg.GetLogMaxSizeMB())
	}
	if cfg.GetLogMaxBackups() != 5 {
		t.Errorf("GetLogMaxBackups: expected 5, got %d", cfg.GetLogMaxBackups())
	}
	if cfg.GetLogMaxAgeDays() != 7 {
		t.Errorf("GetLogMaxAgeDays: expected 7, got %d", cfg.GetLogMaxAgeDays())
	}
	if cfg.GetAppName() != "testapp" {
		t.Errorf("GetAppName: expected testapp, got %s", cfg.GetAppName())
	}
	if cfg.GetSessionSecret() != "secret" {
		t.Errorf("GetSessionSecret: expected secret, got %s", cfg.GetSessionSecret())
	}
	if cfg.GetSessionTimeout() != 3600 {
		t.Errorf("GetSessionTimeout: expected 3600, got %d", cfg.GetSessionTimeout())
	}
}

func TestLoad(t *testing.T) {
	t.Run("loads with default values", func(t *testing.T) {
		t.Setenv("TESTAPP_ENV", "test")
		cfg, err := Load("testapp")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.AppName != "testapp" {
			t.Errorf("expected appname testapp, got %s", cfg.AppName)
		}
		if cfg.Port != "8080" {
			t.Errorf("expected port 8080, got %s", cfg.Port)
		}
		if !cfg.IsTest() {
			t.Error("expected environment to be test")
		}
	})

	t.Run("normalizes app name", func(t *testing.T) {
		t.Setenv("MYAPP_ENV", "test")
		cfg, err := Load("  MyApp  ")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.AppName != "myapp" {
			t.Errorf("expected normalized appname myapp, got %s", cfg.AppName)
		}
	})

	t.Run("uses default app name for empty", func(t *testing.T) {
		t.Setenv("APP_ENV", "test")
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if cfg.AppName != "app" {
			t.Errorf("expected default appname app, got %s", cfg.AppName)
		}
	})

	t.Run("returns error for invalid environment", func(t *testing.T) {
		t.Setenv("INVALID_ENV", "invalid")
		_, err := Load("invalid")
		if err == nil {
			t.Error("expected error for invalid environment")
		}
	})

	t.Run("requires session secret in production", func(t *testing.T) {
		t.Setenv("PRODAPP_ENV", "production")
		t.Setenv("PRODAPP_SESSION_SECRET", "")
		_, err := Load("prodapp")
		if err == nil {
			t.Error("expected error when session secret is missing in production")
		}
	})
}
