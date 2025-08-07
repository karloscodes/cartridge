package main

import (
	"os"
	"path/filepath"

	"github.com/karloscodes/cartridge"
)

func main() {
	// Test development logging (console with colors)
	println("=== Testing Development Logging (Console + Colors) ===")
	devLogger := cartridge.NewLogger(cartridge.LogConfig{
		Level:         cartridge.LogLevelInfo,
		EnableConsole: true,
		EnableColors:  true,
		Environment:   "development",
	})
	devLogger.Info("Development log message", "user", "developer", "action", "testing")

	// Test production logging (file only, JSON)
	println("\n=== Testing Production Logging (File Only + JSON) ===")
	tempDir := "/tmp/cartridge-test-logs"
	prodLogger := cartridge.NewLogger(cartridge.LogConfig{
		Level:         cartridge.LogLevelInfo,
		Directory:     tempDir,
		EnableConsole: false,
		UseJSON:       true,
		Environment:   "production",
	})
	prodLogger.Info("Production log message", "user", "prod-user", "action", "processing")

	// Show what was written to file
	logFile := filepath.Join(tempDir, "app.log")
	if data, err := os.ReadFile(logFile); err == nil {
		println("Production log file content:")
		println(string(data))
	} else {
		println("Failed to read log file:", err.Error())
	}

	// Cleanup
	os.RemoveAll(tempDir)
}
