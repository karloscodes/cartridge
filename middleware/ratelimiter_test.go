package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/testsupport"
)

func TestRateLimiter(t *testing.T) {
	newApp := func(options ...RateLimiterOption) *cartridge.Server {
		app := testsupport.NewTestServer(t).Server
		app.Use(RateLimiter(append([]RateLimiterOption{WithMax(1), WithDuration(time.Minute)}, options...)...))
		app.Get("/", func(c *cartridge.Context) error { return c.SendStatus(http.StatusOK) })
		return app
	}
	get := func(app *cartridge.Server, client string) *httptest.ResponseRecorder {
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
		app := newApp(WithKeyGenerator(func(c *cartridge.Context) string { return c.Get("X-Client") }))

		assert.Equal(t, http.StatusOK, get(app, "a").Code)
		assert.Equal(t, http.StatusTooManyRequests, get(app, "a").Code)
		assert.Equal(t, http.StatusOK, get(app, "b").Code, "another key has its own budget")
	})

	t.Run("answers with the WithLimitReached response", func(t *testing.T) {
		app := testsupport.NewTestServer(t).Server
		app.Use(RateLimiter(WithMax(1), WithDuration(time.Minute), WithLimitReached(func(c *cartridge.Context) error {
			c.Set("Content-Type", "text/html; charset=utf-8")
			return c.SendString("<p>Too many tries. Wait a minute.</p>")
		})))
		app.Get("/", func(c *cartridge.Context) error { return c.SendStatus(http.StatusOK) })
		first, _ := app.Test(httptest.NewRequest("GET", "/", nil))
		assert.Equal(t, http.StatusOK, first.StatusCode)

		resp, _ := app.Test(httptest.NewRequest("GET", "/", nil))

		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		assert.Equal(t, "<p>Too many tries. Wait a minute.</p>", string(body))
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.NotEmpty(t, resp.Header.Get("Retry-After"))
	})

	t.Run("reports the limit as a number", func(t *testing.T) {
		app := newApp()
		get(app, "a")

		res := get(app, "a")

		assert.Equal(t, http.StatusTooManyRequests, res.Code)
		assert.Equal(t, "1", res.Header().Get("X-RateLimit-Limit"))
	})

	t.Run("sets Retry-After to the time left in the window", func(t *testing.T) {
		app := testsupport.NewTestServer(t).Server
		app.Use(RateLimiter(WithMax(1), WithDuration(5*time.Second)))
		app.Get("/", func(c *cartridge.Context) error { return c.SendStatus(http.StatusOK) })
		get(app, "a")

		res := get(app, "a")

		assert.Equal(t, http.StatusTooManyRequests, res.Code)
		assert.Equal(t, "5", res.Header().Get("Retry-After"))
	})
}
