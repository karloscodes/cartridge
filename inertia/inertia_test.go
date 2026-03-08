package inertia

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRenderSetsCacheControlInDevMode(t *testing.T) {
	SetDevMode(true)
	defer SetDevMode(false)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return RenderPage(c, "TestComponent", map[string]interface{}{"foo": "bar"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("dev mode: expected Cache-Control 'no-store', got %q", cc)
	}
}

func TestRenderSetsCacheControlInProductionMode(t *testing.T) {
	SetDevMode(false)

	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return RenderPage(c, "TestComponent", map[string]interface{}{"foo": "bar"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("production mode: expected Cache-Control 'no-cache', got %q", cc)
	}
}
