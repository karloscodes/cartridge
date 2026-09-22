package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiter(t *testing.T) {
	newApp := func(options ...RateLimiterOption) *fiber.App {
		app := fiber.New()
		app.Use(RateLimiter(append([]RateLimiterOption{WithMax(1), WithDuration(time.Minute)}, options...)...))
		app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
		return app
	}
	get := func(app *fiber.App, client string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Client", client)
		resp, _ := app.Test(req)
		rec := httptest.NewRecorder()
		rec.Code = resp.StatusCode
		for k, v := range resp.Header {
			rec.Header()[k] = v
		}
		return rec
	}

	t.Run("keys requests with WithKeyGenerator", func(t *testing.T) {
		app := newApp(WithKeyGenerator(func(c *fiber.Ctx) string { return c.Get("X-Client") }))

		assert.Equal(t, fiber.StatusOK, get(app, "a").Code)
		assert.Equal(t, fiber.StatusTooManyRequests, get(app, "a").Code)
		assert.Equal(t, fiber.StatusOK, get(app, "b").Code, "another key has its own budget")
	})

	t.Run("reports the limit as a number", func(t *testing.T) {
		app := newApp()
		get(app, "a")

		res := get(app, "a")

		assert.Equal(t, fiber.StatusTooManyRequests, res.Code)
		assert.Equal(t, "1", res.Header().Get("X-RateLimit-Limit"))
	})
}
