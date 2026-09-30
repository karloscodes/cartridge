package inertia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newApp returns a function that serves a request with RenderPage.
func newApp() func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		err := RenderPage(rec, req, "TestComponent", map[string]interface{}{"foo": "bar"})
		return rec.Result(), err
	}
}

func TestRenderSetsCacheControlInDevMode(t *testing.T) {
	SetDevMode(true)
	defer SetDevMode(false)

	app := newApp()

	req, _ := http.NewRequest("GET", "/test", nil)
	resp, err := app(req)
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

	app := newApp()

	req, _ := http.NewRequest("GET", "/test", nil)
	resp, err := app(req)
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
	app := newApp()

	t.Run("sends the asset hash as the version", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Inertia", "true")

		resp, err := app(req)

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

		resp, _ := app(req)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("with a stale version asks for a full reload", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test?page=2", nil)
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", "stale")

		resp, _ := app(req)

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

		resp, _ := app(req)

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
