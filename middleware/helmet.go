package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/helmet"
)

// Helmet creates a security headers middleware using Fiber's built-in helmet.
// It sets various HTTP headers to help protect against common web vulnerabilities.
func Helmet() fiber.Handler {
	return helmet.New(helmet.Config{
		ReferrerPolicy: "same-origin",
	})
}

// HelmetWithConfig creates a Helmet middleware with custom configuration.
func HelmetWithConfig(config helmet.Config) fiber.Handler {
	return helmet.New(config)
}
