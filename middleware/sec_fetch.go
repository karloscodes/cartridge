package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// SecFetchSiteConfig configures the Sec-Fetch-Site middleware.
type SecFetchSiteConfig struct {
	// AllowedValues specifies which Sec-Fetch-Site values are permitted.
	// Default: ["same-origin", "none"] (same-origin requests and direct navigation)
	AllowedValues []string

	// Methods specifies which HTTP methods require validation.
	// Default: ["POST", "PUT", "DELETE", "PATCH"]
	Methods []string

	// Next defines a function to skip this middleware when returning true.
	Next func(c *fiber.Ctx) bool
}

// DefaultSecFetchSiteConfig returns the default configuration.
func DefaultSecFetchSiteConfig() SecFetchSiteConfig {
	return SecFetchSiteConfig{
		AllowedValues: []string{"same-origin", "none"},
		Methods:       []string{"POST", "PUT", "DELETE", "PATCH"},
	}
}

// SecFetchSiteMiddleware validates the Sec-Fetch-Site header to prevent CSRF and spoofing attacks.
// Modern browsers automatically set this header, and it cannot be spoofed by JavaScript or server-to-server tools.
//
// STRICT MODE: Requests without Sec-Fetch-Site header are REJECTED.
// This blocks: curl, Postman, Python requests, Node.js fetch, older browsers (pre-2020).
//
// Sec-Fetch-Site values:
//   - "same-origin": Request from the same origin (scheme + host + port)
//   - "same-site": Request from the same site (different subdomain allowed)
//   - "cross-site": Request from a different site
//   - "none": Direct navigation (user typed URL, bookmark, etc.)
//
// By default, this middleware allows "same-origin" and "none" for state-changing methods.
// For analytics endpoints, configure AllowedValues to include "cross-site".
func SecFetchSiteMiddleware(config ...SecFetchSiteConfig) fiber.Handler {
	cfg := DefaultSecFetchSiteConfig()
	if len(config) > 0 {
		cfg = config[0]
		if cfg.AllowedValues == nil {
			cfg.AllowedValues = DefaultSecFetchSiteConfig().AllowedValues
		}
		if cfg.Methods == nil {
			cfg.Methods = DefaultSecFetchSiteConfig().Methods
		}
	}

	methodSet := make(map[string]bool, len(cfg.Methods))
	for _, m := range cfg.Methods {
		methodSet[m] = true
	}

	allowedSet := make(map[string]bool, len(cfg.AllowedValues))
	for _, v := range cfg.AllowedValues {
		allowedSet[v] = true
	}

	return func(c *fiber.Ctx) error {
		if cfg.Next != nil && cfg.Next(c) {
			return c.Next()
		}

		// Only validate configured methods
		if !methodSet[c.Method()] {
			return c.Next()
		}

		secFetchSite := c.Get("Sec-Fetch-Site")

		// Reject if header is missing - this prevents server-to-server spoofing
		// Blocks: curl, Postman, Python requests, Node.js fetch, etc.
		// Also blocks older browsers (pre-2020) that don't support this header.
		if secFetchSite == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":   "forbidden",
				"message": "browser requests only",
			})
		}

		if !allowedSet[secFetchSite] {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":   "forbidden",
				"message": "cross-site request blocked",
			})
		}

		return c.Next()
	}
}
