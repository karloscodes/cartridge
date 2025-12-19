package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// RequestLogger emits structured request logs using the provided logger.
func RequestLogger(logger Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		stop := time.Since(start)

		logger.Info("http request",
			"method", c.Method(),
			"path", c.Path(),
			"status", c.Response().StatusCode(),
			"duration", stop,
			"ip", c.IP(),
		)

		return err
	}
}
