package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestSecFetchSiteMiddleware(t *testing.T) {
	t.Run("blocks missing header", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware())
		app.Post("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		req := httptest.NewRequest("POST", "/test", nil)
		// No Sec-Fetch-Site header

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "Should block missing header")
	})

	t.Run("allows valid browser headers", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware(SecFetchSiteConfig{
			AllowedValues: []string{"same-origin", "same-site", "cross-site", "none"},
		}))
		app.Post("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		validHeaders := []string{"same-origin", "same-site", "cross-site", "none"}
		for _, header := range validHeaders {
			req := httptest.NewRequest("POST", "/test", nil)
			req.Header.Set("Sec-Fetch-Site", header)

			resp, err := app.Test(req)
			assert.NoError(t, err)
			assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Should allow %s", header)
		}
	})

	t.Run("blocks invalid header values", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware(SecFetchSiteConfig{
			AllowedValues: []string{"same-origin"},
		}))
		app.Post("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "Should block cross-site when not in allowed list")
	})

	t.Run("only validates configured methods", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware(SecFetchSiteConfig{
			Methods: []string{"POST"},
		}))
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})
		app.Post("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		// GET should pass without header
		getReq := httptest.NewRequest("GET", "/test", nil)
		resp, err := app.Test(getReq)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "GET should not be validated")

		// POST should fail without header
		postReq := httptest.NewRequest("POST", "/test", nil)
		resp, err = app.Test(postReq)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "POST should be validated")
	})

	t.Run("Next function skips validation", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware(SecFetchSiteConfig{
			Next: func(c *fiber.Ctx) bool {
				return c.Path() == "/skip"
			},
		}))
		app.Post("/skip", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})
		app.Post("/validate", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		// /skip should pass without header
		skipReq := httptest.NewRequest("POST", "/skip", nil)
		resp, err := app.Test(skipReq)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "/skip should be skipped")

		// /validate should fail without header
		validateReq := httptest.NewRequest("POST", "/validate", nil)
		resp, err = app.Test(validateReq)
		assert.NoError(t, err)
		assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "/validate should be validated")
	})
}

func TestSecFetchSiteStrictMode(t *testing.T) {
	t.Run("blocks common server-to-server tools", func(t *testing.T) {
		app := fiber.New()
		app.Use(SecFetchSiteMiddleware(SecFetchSiteConfig{
			AllowedValues: []string{"cross-site", "same-site", "same-origin", "none"},
			Methods:       []string{"POST"},
		}))
		app.Post("/api/events", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		userAgents := []string{
			"curl/7.68.0",
			"PostmanRuntime/7.29.0",
			"python-requests/2.28.1",
			"node-fetch/1.0",
			"Wget/1.20.3",
		}

		for _, ua := range userAgents {
			req := httptest.NewRequest("POST", "/api/events", nil)
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Origin", "https://example.com") // Even with spoofed origin

			resp, err := app.Test(req)
			assert.NoError(t, err)
			assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "Should block %s", ua)
		}
	})
}
