package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"
)

// Recover creates a middleware that recovers from panics and logs the error.
// It prevents the server from crashing and returns a 500 Internal Server Error.
func Recover(logger Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic recovered: %v", r)
				stack := debug.Stack()

				logger.Error("Panic recovered",
					"error", err,
					"path", c.Path(),
					"method", c.Method(),
					"stack", string(stack),
				)

				// Set the error on the context
				c.Status(fiber.StatusInternalServerError)
				_ = c.JSON(fiber.Map{
					"error":   "Internal Server Error",
					"message": "An unexpected error occurred",
				})
			}
		}()

		return c.Next()
	}
}
