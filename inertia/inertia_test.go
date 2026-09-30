package inertia

import (
	"encoding/json"
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

func TestRenderAssetVersion(t *testing.T) {
	SetDevMode(false)
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		return RenderPage(c, "TestComponent", map[string]interface{}{"foo": "bar"})
	})

	t.Run("sends the asset hash as the version", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Inertia", "true")

		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		var page struct{ Version string }
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if page.Version == "" || page.Version == "v1" || page.Version != Version() {
			t.Errorf("expected version %q, got %q", Version(), page.Version)
		}
	})

	t.Run("with a matching version renders the page", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", Version())

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("with a stale version asks for a full reload", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test?page=2", nil)
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", "stale")

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("X-Inertia-Location"); loc != "/test?page=2" {
			t.Errorf("expected X-Inertia-Location /test?page=2, got %q", loc)
		}
	})

	t.Run("with a stale version on a non-Inertia request renders HTML", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Inertia-Version", "stale")

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestVersionFor(t *testing.T) {
	a := versionFor("/assets/inertia-abc.js", "/assets/inertia-abc.css")
	b := versionFor("/assets/inertia-def.js", "/assets/inertia-abc.css")

	if a == b {
		t.Error("expected a new JS build to change the version")
	}
	if a != versionFor("/assets/inertia-abc.js", "/assets/inertia-abc.css") {
		t.Error("expected the same build to give the same version")
	}
}
