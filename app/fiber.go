package app

import (
	"github.com/gofiber/fiber/v2"
)

// createFiberApp creates and configures a new Fiber application
func (app *Application) createFiberApp() *fiber.App {
	fiberApp := fiber.New(fiber.Config{
		AppName:      "Cartridge App",
		ErrorHandler: app.getFiberErrorHandler(),
		Prefork:      app.config.Environment == EnvProduction,
	})

	return fiberApp
}

// getFiberErrorHandler returns a Fiber-specific error handler
func (app *Application) getFiberErrorHandler() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		// Get status code from error
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		// Environment-specific error handling
		if app.config.Environment == EnvDevelopment {
			// Development: detailed error messages with stack traces
			app.logger.Error("Request error (development)",
				app.logger.Field("error", err.Error()),
				app.logger.Field("path", c.Path()),
				app.logger.Field("method", c.Method()))

			// Return detailed error in development
			return c.Status(code).JSON(fiber.Map{
				"error":  err.Error(),
				"code":   code,
				"path":   c.Path(),
				"method": c.Method(),
			})
		} else {
			// Production: generic error messages, detailed logging
			app.logger.Error("Request error (production)",
				app.logger.Field("error", err.Error()),
				app.logger.Field("path", c.Path()),
				app.logger.Field("method", c.Method()))

			// Return generic error in production
			return c.Status(code).JSON(fiber.Map{
				"error": "Internal Server Error",
				"code":  code,
			})
		}
	}
}
