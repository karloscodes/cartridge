package main

import (
	"os"
	"time"

	"github.com/karloscodes/cartridge"
)

func main() {
	// Get configuration from environment
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "3001"
	}

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	// Create a simple Cartridge app
	app := cartridge.NewFullStack(
		cartridge.WithPort(port),
		cartridge.WithEnvironment(env),
		cartridge.WithCSRF(false), // Disable CSRF for demo
	)

	// Enhanced structured logging with emojis
	logger := app.Logger().With("demo", "simple", "version", "1.0")
	logger.Info("🚀 Welcome to Cartridge demo!",
		"framework", "cartridge",
		"features", []string{"colors", "structured-logs", "emojis"})

	// Simple API endpoint with enhanced logging
	app.Get("/hello", func(ctx *cartridge.Context) error {
		name := ctx.Query("name", "World")

		ctx.Logger.Info("👋 Greeting request received",
			"name", name,
			"endpoint", "/hello",
			"method", "GET")

		return ctx.JSON(map[string]interface{}{
			"message": "Hello, " + name + "! 🎉",
			"time":    time.Now(),
			"emoji":   "🚀",
		})
	})

	// Async processing demo
	app.Post("/demo/async", func(ctx *cartridge.Context) error {
		taskID, err := app.AsyncJob("demo-task", func(asyncCtx *cartridge.AsyncContext, args map[string]interface{}) (interface{}, error) {
			asyncCtx.Logger.Info("⚡ Async task started",
				"task_id", asyncCtx.TaskID,
				"args", args)

			// Simulate work
			for i := 1; i <= 3; i++ {
				time.Sleep(1 * time.Second)
				asyncCtx.Logger.Info("⚙️ Processing step",
					"step", i,
					"total", 3,
					"progress", i*33)
			}

			asyncCtx.Logger.Info("✅ Task completed successfully!",
				"task_id", asyncCtx.TaskID,
				"result", "demo-complete")

			return map[string]interface{}{
				"status": "completed",
				"emoji":  "🎯",
			}, nil
		}, map[string]interface{}{
			"demo": true,
			"type": "showcase",
		})
		if err != nil {
			ctx.Logger.Error("❌ Failed to start async task", "error", err)
			return ctx.Status(500).JSON(map[string]string{"error": err.Error()})
		}

		ctx.Logger.Info("🚀 Async task queued", "task_id", taskID)
		return ctx.JSON(map[string]string{
			"task_id": taskID,
			"message": "Task started! Check /status/" + taskID,
		})
	})

	// Task status endpoint
	app.Get("/status/:id", func(ctx *cartridge.Context) error {
		taskID := ctx.Params("id")
		task, err := app.AsyncStatus(taskID)
		if err != nil {
			return ctx.NotFound("Task not found")
		}
		return ctx.JSON(task)
	})

	logger.Info("🌟 Demo server ready!",
		"port", "3001",
		"endpoints", []string{"/hello", "/demo/async", "/status/:id"})

	logger.Info("📝 Try these commands:")
	logger.Info("   curl http://localhost:3001/hello?name=Developer")
	logger.Info("   curl -X POST http://localhost:3001/demo/async") // Start the server
	app.Run()
}
