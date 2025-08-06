package main

import (
	"log"

	"github.com/karloscodes/cartridge/app"
)

func main() {
	// Create a new Cartridge application with functional options
	// Defaults: port 8080, development environment, CSRF enabled, CORS disabled, rate limiting disabled
	cartridgeApp, err := app.New(
		app.WithPort("3000"),                    // Override default port (8080)
		app.WithEnvironment(app.EnvDevelopment), // Explicit environment (default)
		app.WithCORS(true),                      // Enable CORS (default is false)
		// CSRF is enabled by default
		// Rate limiting is disabled by default
	)
	if err != nil {
		log.Fatal("Failed to create Cartridge application:", err)
	}

	// Or use with all defaults:
	// cartridgeApp, err := app.New()

	// Get the underlying Fiber app if needed
	// fiberApp := cartridgeApp.GetFiberApp().(*fiber.App)

	// Add your routes here
	// fiberApp.Get("/", func(c *fiber.Ctx) error {
	//     return c.SendString("Hello from Cartridge!")
	// })

	// Start the application
	if err := cartridgeApp.Start(); err != nil {
		log.Fatal("Failed to start application:", err)
	}

	// Graceful shutdown would be handled here
	defer cartridgeApp.Stop()
}
