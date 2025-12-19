package testsupport

import (
	"io"
	"log/slog"
)

// TestConfig implements cartridge.Config for testing.
type TestConfig struct {
	port            string
	environment     string
	publicDirectory string
	assetsPrefix    string
}

// NewTestConfig creates a test configuration with sensible defaults.
func NewTestConfig() *TestConfig {
	return &TestConfig{
		port:            "0", // Random port
		environment:     "test",
		publicDirectory: "",
		assetsPrefix:    "/assets",
	}
}

// IsDevelopment returns false for test config.
func (c *TestConfig) IsDevelopment() bool { return false }

// IsProduction returns false for test config.
func (c *TestConfig) IsProduction() bool { return false }

// IsTest returns true for test config.
func (c *TestConfig) IsTest() bool { return true }

// GetPort returns the configured port.
func (c *TestConfig) GetPort() string { return c.port }

// GetPublicDirectory returns the public assets directory.
func (c *TestConfig) GetPublicDirectory() string { return c.publicDirectory }

// GetAssetsPrefix returns the assets URL prefix.
func (c *TestConfig) GetAssetsPrefix() string { return c.assetsPrefix }

// NewTestLogger creates a slog.Logger that discards all output.
// Use this for tests where you don't need to verify log messages.
func NewTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
