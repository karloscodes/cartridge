package cartridge

import (
	"net/http"
	"net/url"
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
	Next func(c *Context) bool
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
func SecFetchSiteMiddleware(config ...SecFetchSiteConfig) HandlerFunc {
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

	return func(c *Context) error {
		if cfg.Next != nil && cfg.Next(c) {
			return c.Next()
		}

		// Only validate configured methods
		if !methodSet[c.Method()] {
			return c.Next()
		}

		secFetchSite := c.Get("Sec-Fetch-Site")

		// Browsers send Sec-Fetch-Site only to HTTPS and localhost. Over plain
		// HTTP they still send Origin on every POST, so an Origin that matches
		// the host counts as same-origin. A cross-site Origin without the
		// header stays blocked: that is what a spoofing tool sends.
		if secFetchSite == "" && sameOrigin(c.Get("Origin"), c.Hostname()) {
			secFetchSite = "same-origin"
		}

		// Reject when both headers are missing: curl, Postman, Python requests,
		// Node.js fetch, and other server-to-server tools.
		if secFetchSite == "" {
			return c.Status(http.StatusForbidden).JSON(Map{
				"error":   "forbidden",
				"message": "browser requests only",
			})
		}

		if !allowedSet[secFetchSite] {
			return c.Status(http.StatusForbidden).JSON(Map{
				"error":   "forbidden",
				"message": "cross-site request blocked",
			})
		}

		return c.Next()
	}
}

// sameOrigin reports whether the Origin header names host.
func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	return origin != "" && err == nil && u.Host != "" && u.Host == host
}
