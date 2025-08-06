package main

import (
	"log"

	"github.com/karloscodes/cartridge/app"
	"github.com/karloscodes/cartridge/logging"
)

func main() {
	// Create application dependencies
	deps, err := app.CreateAppDependencies()
	if err != nil {
		log.Fatalf("Failed to create application dependencies: %v", err)
	}

	// Create Fiber app configuration
	fiberConfig := app.FiberConfig{
		Environment: deps.Config.Environment,
		Port:       deps.Config.Port,
	}

	// Create the application
	application := app.NewFiberApp(fiberConfig, deps)

	deps.Logger.Info("Starting Cartridge application example")

	// Start the application (this would actually start a server when Fiber is available)
	if err := application.(*app.Application).Start(); err != nil {
		deps.Logger.Fatal("Failed to start application", 
			logging.Field{Key: "error", Value: err})
	}

	deps.Logger.Info("Application example completed successfully")
}
