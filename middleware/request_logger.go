package middleware

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RequestLogger emits structured request logs using the provided logger.
// Health check endpoints (/_health) are not logged to reduce noise.
func RequestLogger(logger Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		stop := time.Since(start)

		// Skip logging health check endpoints
		path := c.Path()
		if strings.HasPrefix(path, "/_health") {
			return err
		}

		logger.Info("http request",
			"method", c.Method(),
			"path", path,
			"status", c.Response().StatusCode(),
			"duration", stop,
			"ip", c.IP(),
		)

		return err
	}
}
