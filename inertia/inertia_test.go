package inertia

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// render serves req with the given props and returns the response and body.
func render(t *testing.T, req *http.Request, props map[string]interface{}) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := RenderPage(rec, req, "Page", props); err != nil {
		t.Fatalf("render: %v", err)
	}
	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestInitialPageMarkup(t *testing.T) {
	hostile := `</script><script>alert(1)</script>`

	t.Run("by default the page goes in the data-page attribute (Inertia v2 clients)", func(t *testing.T) {
		_, body := render(t, httptest.NewRequest("GET", "/x", nil), map[string]interface{}{"name": "ok"})

		if !strings.Contains(body, `<div id="app" data-page='`) {
			t.Errorf("expected the data-page attribute, got:\n%s", body)
		}
		if strings.Contains(body, `type="application/json"`) {
			t.Error("did not expect the v3 page script by default")
		}
	})

	t.Run("with the script element, the page goes in a JSON script before the root div (Inertia v3)", func(t *testing.T) {
		SetScriptElement(true)
		defer SetScriptElement(false)

		_, body := render(t, httptest.NewRequest("GET", "/x", nil), map[string]interface{}{"name": hostile, "path": "/a/b"})

		open := `<script data-page="app" type="application/json">`
		start := strings.Index(body, open)
		end := strings.Index(body, `</script><div id="app"></div>`)
		if start < 0 || end < start {
			t.Fatalf("expected the page script followed by the root div, got:\n%s", body)
		}
		raw := body[start+len(open) : end]
		if strings.Contains(raw, "</") {
			t.Errorf("the script body can close the tag early: %s", raw)
		}
		if strings.Contains(raw, "&#") || strings.Contains(raw, "&lt;") {
			t.Errorf("the script body must not use HTML entities: %s", raw)
		}
		if !strings.Contains(raw, `\/a\/b`) {
			t.Errorf("expected every / escaped as \\/: %s", raw)
		}
		var page struct {
			Component string
			Props     map[string]interface{}
		}
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatalf("the script body is not valid JSON: %v", err)
		}
		if page.Props["name"] != hostile || page.Props["path"] != "/a/b" {
			t.Errorf("props did not round-trip: %v", page.Props)
		}
	})
}

func TestPageProtocol(t *testing.T) {
	inertiaReq := func(method, path string, headers map[string]string) *http.Request {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", Version())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req
	}
	decode := func(t *testing.T, body string) map[string]interface{} {
		t.Helper()
		var page struct{ Props map[string]interface{} }
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		return page.Props
	}

	t.Run("props always carry an errors object", func(t *testing.T) {
		_, body := render(t, inertiaReq("GET", "/x", nil), map[string]interface{}{"a": 1})

		errs, ok := decode(t, body)["errors"].(map[string]interface{})
		if !ok || len(errs) != 0 {
			t.Errorf("errors = %#v, want {}", decode(t, body)["errors"])
		}
	})

	t.Run("a stale version gets 409 with the current version", func(t *testing.T) {
		resp, _ := render(t, inertiaReq("GET", "/x", map[string]string{"X-Inertia-Version": "stale"}), map[string]interface{}{})

		if resp.StatusCode != http.StatusConflict || resp.Header.Get("X-Inertia-Version") != Version() {
			t.Errorf("got %d, X-Inertia-Version=%q, want 409 and %q", resp.StatusCode, resp.Header.Get("X-Inertia-Version"), Version())
		}
	})

	t.Run("a partial reload can exclude props", func(t *testing.T) {
		_, body := render(t, inertiaReq("GET", "/x", map[string]string{
			"X-Inertia-Partial-Component": "Page",
			"X-Inertia-Partial-Except":    "b",
		}), map[string]interface{}{"a": 1, "b": 2})

		props := decode(t, body)
		if props["a"] == nil || props["b"] != nil {
			t.Errorf("props = %v, want a without b", props)
		}
	})

	t.Run("the HTML response varies on X-Inertia", func(t *testing.T) {
		resp, _ := render(t, httptest.NewRequest("GET", "/x", nil), map[string]interface{}{})

		if resp.Header.Get("Vary") != "X-Inertia" {
			t.Errorf("Vary = %q, want X-Inertia", resp.Header.Get("Vary"))
		}
	})
}

func TestInitialPagePreloads(t *testing.T) {
	SetDevMode(true) // re-read the manifest on every render
	defer SetDevMode(false)
	SetManifestData([]byte(`{
		"src/inertia.tsx": {"file": "assets/inertia-A.js", "isEntry": true, "css": ["assets/inertia-A.css"], "imports": ["_vendor-B.js"]},
		"_vendor-B.js": {"file": "assets/vendor-B.js"},
		"_charts-C.js": {"file": "assets/charts-C.js"},
		"src/pages/Page.tsx": {"file": "assets/Page-D.js", "isDynamicEntry": true, "imports": ["_vendor-B.js", "_charts-C.js"], "css": ["assets/Page-D.css"]},
		"src/pages/Other.tsx": {"file": "assets/Other-E.js", "isDynamicEntry": true}
	}`))
	defer SetManifestData(nil)

	_, body := render(t, httptest.NewRequest("GET", "/x", nil), map[string]interface{}{})

	for _, want := range []string{
		`<link rel="modulepreload" href="/assets/vendor-B.js">`,
		`<link rel="modulepreload" href="/assets/Page-D.js">`,
		`<link rel="modulepreload" href="/assets/charts-C.js">`,
		`<link rel="stylesheet" href="/assets/Page-D.css">`,
		`<script type="module" src="/assets/inertia-A.js">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(body, "Other-E.js") {
		t.Error("preloaded a page that is not being shown")
	}
	if n := strings.Count(body, "vendor-B.js"); n != 1 {
		t.Errorf("vendor-B.js preloaded %d times, want once", n)
	}
}

func TestRenderKeepsAStricterCacheControl(t *testing.T) {
	SetDevMode(false)
	rec := httptest.NewRecorder()
	rec.Header().Set("Cache-Control", "private, no-store")

	err := Render(rec, httptest.NewRequest("GET", "/test", nil), "Dashboard", Props{})

	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
}
