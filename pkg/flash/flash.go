// Package flash provides flash message utilities for HTTP requests
package flash

import "github.com/gofiber/fiber/v2"

// GetFlash retrieves flash messages from the request context
func GetFlash(c *fiber.Ctx) interface{} {
return c.Locals("flash")
}

// SetFlash sets a flash message in the request context
func SetFlash(c *fiber.Ctx, message interface{}) {
c.Locals("flash", message)
}
