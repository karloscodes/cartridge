package cartridge

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorHTML(t *testing.T) {
	t.Run("generates valid HTML without message", func(t *testing.T) {
		html := errorHTML(404, "Not Found", "")

		if !strings.Contains(html, "<!DOCTYPE html>") {
			t.Error("expected DOCTYPE declaration")
		}
		if !strings.Contains(html, "<title>404 - Not Found</title>") {
			t.Error("expected title with status code")
		}
		if !strings.Contains(html, ">404<") {
			t.Error("expected status code in body")
		}
		if !strings.Contains(html, ">Not Found<") {
			t.Error("expected error name in body")
		}
		if !strings.Contains(html, "← Go back home") {
			t.Error("expected back link")
		}
	})

	t.Run("includes error message when provided", func(t *testing.T) {
		html := errorHTML(500, "Internal Server Error", "connection refused")

		if !strings.Contains(html, "connection refused") {
			t.Error("expected error message in HTML")
		}
	})
}

func TestDefaultErrorHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	newApp := func(isDev bool, err error) *Server {
		app := newTestApp(t)
		app.cfg.ErrorHandler = DefaultErrorHandler(logger, isDev)
		app.Get("/fail", func(c *Context) error { return err })
		return app
	}

	t.Run("uses the status of a returned Error", func(t *testing.T) {
		app := newApp(false, NewError(http.StatusForbidden, "nope"))

		resp, _ := app.Test(httptest.NewRequest("GET", "/fail", nil))

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("answers API clients with JSON", func(t *testing.T) {
		app := newApp(false, errors.New("boom"))
		req := httptest.NewRequest("GET", "/fail", nil)
		req.Header.Set("Accept", "application/json")

		resp, _ := app.Test(req)

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), `"error":"Internal Server Error"`) {
			t.Errorf("expected a JSON error, got %s", body)
		}
	})

	t.Run("hides the cause of a 500 from the client", func(t *testing.T) {
		app := newApp(false, errors.New("dial tcp 10.0.0.3:5432: connection refused"))
		req := httptest.NewRequest("GET", "/fail", nil)
		req.Header.Set("Accept", "application/json")

		resp, _ := app.Test(req)

		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "10.0.0.3") {
			t.Errorf("the response leaks the error: %s", body)
		}
		if !strings.Contains(string(body), `"message":"Internal Server Error"`) {
			t.Errorf("expected the status text, got %s", body)
		}
	})

	t.Run("hides the cause of a 500 Error from the client", func(t *testing.T) {
		app := newApp(false, NewError(http.StatusBadGateway, "upstream secret.internal failed"))
		req := httptest.NewRequest("GET", "/fail", nil)
		req.Header.Set("Accept", "text/html")

		resp, _ := app.Test(req)

		if body, _ := io.ReadAll(resp.Body); strings.Contains(string(body), "secret.internal") {
			t.Errorf("the response leaks the error: %s", body)
		}
	})

	t.Run("shows the message of a client Error", func(t *testing.T) {
		app := newApp(false, NewError(http.StatusBadRequest, "email is required"))
		req := httptest.NewRequest("GET", "/fail", nil)
		req.Header.Set("Accept", "application/json")

		resp, _ := app.Test(req)

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"error":"Bad Request"`) || !strings.Contains(string(body), `"message":"email is required"`) {
			t.Errorf("expected the client message, got %s", body)
		}
	})

	t.Run("answers browsers with an escaped HTML page", func(t *testing.T) {
		app := newApp(true, errors.New("<script>x</script>"))
		req := httptest.NewRequest("GET", "/fail", nil)
		req.Header.Set("Accept", "text/html")

		resp, _ := app.Test(req)

		body, _ := io.ReadAll(resp.Body)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("expected text/html, got %q", ct)
		}
		if strings.Contains(string(body), "<script>x</script>") {
			t.Error("expected the error message to be escaped")
		}
	})
}
