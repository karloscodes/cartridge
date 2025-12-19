package middleware

import (
	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
)

// Recover creates a panic recovery middleware using Fiber's built-in recover.
// It enables stack traces for debugging.
func Recover() fiber.Handler {
	return fiberrecover.New(fiberrecover.Config{
		EnableStackTrace: true,
	})
}
