package main

import (
	"embed"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func main() {
	app := cartridge.NewFullStack()

	// Register a simple cron job with optional description
	app.CronJob("health-check", "0 */5 * * * *", func(ctx *cartridge.CronContext) error {
		ctx.Logger.Info("Health check executed")
		return nil
	}, "Database health check")

	// Register another cron job without description
	app.CronJob("simple-task", "0 */10 * * * *", func(ctx *cartridge.CronContext) error {
		ctx.Logger.Info("Simple task executed")
		return nil
	})

	// Simple async task endpoint
	app.Post("/async/test", func(ctx *cartridge.Context) error {
		taskID := app.Async("test-task", func(asyncCtx *cartridge.AsyncContext) (interface{}, error) {
			asyncCtx.Logger.Info("Starting async task")
			time.Sleep(3 * time.Second)
			asyncCtx.Logger.Info("Async task completed")
			return map[string]interface{}{
				"message": "Task completed successfully",
				"data":    42,
			}, nil
		}, "Test async processing")

		return ctx.JSON(fiber.Map{
			"task_id": taskID,
			"message": "Async task started",
		})
	})

	// Check async task status
	app.Get("/async/status/:id", func(ctx *cartridge.Context) error {
		taskID := ctx.Params("id")
		task, err := app.AsyncStatus(taskID)
		if err != nil {
			return ctx.NotFound("Task not found")
		}
		return ctx.JSON(task)
	})

	// List all async tasks
	app.Get("/async/list", func(ctx *cartridge.Context) error {
		return ctx.JSON(app.AsyncList())
	})

	// Root endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"message": "Cartridge with new simplified API!",
			"features": []string{
				"CronJob() with optional description",
				"Run() that panics on failure - no error checking needed",
				"Async processing with goroutines",
			},
		})
	})

	// One call handles everything - no error checking needed!
	app.Run(cartridge.WithMigrations(migrationFiles, "migrations"))
}
