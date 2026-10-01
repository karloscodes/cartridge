package cartridge

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/karloscodes/cartridge/flash"
)

func TestBind(t *testing.T) {
	type input struct {
		Name string `json:"name" form:"name" query:"name"`
		Age  int    `json:"age" form:"age" query:"age"`
		ID   string `json:"-" form:"-" query:"-" params:"id"`
	}

	t.Run("binds a JSON body", func(t *testing.T) {
		app := newTestApp(t)
		var got input
		app.Post("/users", func(c *Context) error {
			return c.Bind(&got)
		})

		body := strings.NewReader(`{"name":"Ada","age":36}`)
		req, _ := http.NewRequest("POST", "/users", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got.Name != "Ada" || got.Age != 36 {
			t.Errorf("expected {Ada 36}, got %+v", got)
		}
	})

	t.Run("binds a form-urlencoded body", func(t *testing.T) {
		app := newTestApp(t)
		var got input
		app.Post("/users", func(c *Context) error {
			return c.Bind(&got)
		})

		form := url.Values{"name": {"Grace"}, "age": {"45"}}
		req, _ := http.NewRequest("POST", "/users", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got.Name != "Grace" || got.Age != 45 {
			t.Errorf("expected {Grace 45}, got %+v", got)
		}
	})

	t.Run("ignores route params and query values", func(t *testing.T) {
		app := newTestApp(t)
		var got input
		app.Post("/users/:id", func(c *Context) error {
			return c.Bind(&got)
		})

		body := strings.NewReader(`{"name":"Ada"}`)
		req, _ := http.NewRequest("POST", "/users/42?age=99&name=Eve", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got != (input{Name: "Ada"}) {
			t.Errorf("expected only the body value, got %+v", got)
		}
	})

	t.Run("sets only form fields with an explicit form tag", func(t *testing.T) {
		app := newTestApp(t)
		var got struct {
			Email   string `form:"email"`
			IsAdmin bool
			Role    string `json:"role" form:"-"`
		}
		app.Post("/signup", func(c *Context) error {
			return c.Bind(&got)
		})

		form := url.Values{"email": {"a@b.c"}, "IsAdmin": {"true"}, "isadmin": {"true"}, "role": {"admin"}, "Role": {"admin"}}
		req, _ := http.NewRequest("POST", "/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		app.Test(req)

		if got.Email != "a@b.c" || got.IsAdmin || got.Role != "" {
			t.Errorf("expected only Email set, got %+v", got)
		}
	})

	t.Run("returns typed errors for bad input", func(t *testing.T) {
		cases := []struct {
			name, contentType, body string
			want                    int
		}{
			{"malformed JSON", "application/json", `{"name":`, http.StatusBadRequest},
			{"a value that does not fit its field", "application/x-www-form-urlencoded", "age=old", http.StatusBadRequest},
			{"an unsupported content type", "text/plain", "name=Ada", http.StatusUnsupportedMediaType},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				app := newTestApp(t)
				app.Post("/users", func(c *Context) error {
					var got input
					return c.Bind(&got)
				})
				req, _ := http.NewRequest("POST", "/users", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.contentType)

				resp, _ := app.Test(req)

				if resp.StatusCode != tc.want {
					t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
				}
			})
		}
	})

	t.Run("empty body is not an error", func(t *testing.T) {
		app := newTestApp(t)
		var got input
		var bindErr error
		app.Get("/users/:id", func(c *Context) error {
			bindErr = c.Bind(&got)
			return c.SendString("ok")
		})

		req, _ := http.NewRequest("GET", "/users/7?name=Linus", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if bindErr != nil {
			t.Errorf("expected no error for empty body, got %v", bindErr)
		}
		if got != (input{}) {
			t.Errorf("expected nothing bound, got %+v", got)
		}
	})
}

func TestQueryAndParamsParser(t *testing.T) {
	t.Run("set only fields with an explicit tag", func(t *testing.T) {
		app := newTestApp(t)
		var got struct {
			ID     string `params:"id"`
			Page   int    `query:"page"`
			Secret string `json:"-"`
			Owner  string
		}
		app.Get("/sites/:id", func(c *Context) error {
			if err := c.ParamsParser(&got); err != nil {
				return err
			}
			return c.QueryParser(&got)
		})

		app.Test(httptest.NewRequest("GET", "/sites/7?page=2&Secret=x&secret=x&Owner=eve&owner=eve&id=9", nil))

		if got.ID != "7" || got.Page != 2 || got.Secret != "" || got.Owner != "" {
			t.Errorf("expected only tagged fields set, got %+v", got)
		}
	})
}

func TestInput(t *testing.T) {
	t.Run("reads a value from a JSON body", func(t *testing.T) {
		app := newTestApp(t)
		var got string
		app.Post("/x", func(c *Context) error {
			got = c.Input("openai_api_key")
			return c.SendString("ok")
		})

		body := strings.NewReader(`{"openai_api_key":"sk-123"}`)
		req, _ := http.NewRequest("POST", "/x", body)
		req.Header.Set("Content-Type", "application/json")
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if got != "sk-123" {
			t.Errorf("expected sk-123, got %q", got)
		}
	})

	t.Run("reads a value from a form-urlencoded body", func(t *testing.T) {
		app := newTestApp(t)
		var got string
		app.Post("/x", func(c *Context) error {
			got = c.Input("domain")
			return c.SendString("ok")
		})

		form := url.Values{"domain": {"example.com"}}
		req, _ := http.NewRequest("POST", "/x", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if got != "example.com" {
			t.Errorf("expected example.com, got %q", got)
		}
	})

	t.Run("reads a route param and a query value", func(t *testing.T) {
		app := newTestApp(t)
		var gotID, gotRange string
		app.Get("/sites/:id", func(c *Context) error {
			ctx := c
			gotID = ctx.Input("id")
			gotRange = ctx.Input("range")
			return c.SendString("ok")
		})

		req, _ := http.NewRequest("GET", "/sites/42?range=7d", nil)
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if gotID != "42" {
			t.Errorf("expected route param id=42, got %q", gotID)
		}
		if gotRange != "7d" {
			t.Errorf("expected query range=7d, got %q", gotRange)
		}
	})

	t.Run("renders a non-string JSON value as its literal", func(t *testing.T) {
		app := newTestApp(t)
		var got string
		app.Post("/x", func(c *Context) error {
			got = c.Input("count")
			return c.SendString("ok")
		})

		body := strings.NewReader(`{"count":42}`)
		req, _ := http.NewRequest("POST", "/x", body)
		req.Header.Set("Content-Type", "application/json")
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if got != "42" {
			t.Errorf("expected 42, got %q", got)
		}
	})

	t.Run("returns empty string for an absent key", func(t *testing.T) {
		app := newTestApp(t)
		var got = "sentinel"
		app.Post("/x", func(c *Context) error {
			got = c.Input("missing")
			return c.SendString("ok")
		})

		body := strings.NewReader(`{"present":"yes"}`)
		req, _ := http.NewRequest("POST", "/x", body)
		req.Header.Set("Content-Type", "application/json")
		if _, err := app.Test(req); err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestInertia(t *testing.T) {
	// dataPage extracts the JSON encoded in the data-page attribute of the
	// initial (non-Inertia) HTML response, then returns its props map.
	dataPageProps := func(t *testing.T, htmlBody string) map[string]interface{} {
		t.Helper()
		const marker = `data-page='`
		start := strings.Index(htmlBody, marker)
		if start == -1 {
			t.Fatalf("data-page attribute not found in response:\n%s", htmlBody)
		}
		start += len(marker)
		end := strings.Index(htmlBody[start:], `'`)
		if end == -1 {
			t.Fatal("unterminated data-page attribute")
		}
		// The attribute value is HTML-escaped JSON.
		raw := htmlUnescape(htmlBody[start : start+end])

		var page struct {
			Props map[string]interface{} `json:"props"`
		}
		if err := json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatalf("failed to parse data-page JSON: %v\nraw: %s", err, raw)
		}
		return page.Props
	}

	t.Run("injects flash prop when a flash exists", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/page", func(c *Context) error {
			return c.Inertia("Dashboard", nil)
		})

		req, _ := http.NewRequest("GET", "/page", nil)
		// Provide a flash cookie so GetFlash returns a populated message.
		req.AddCookie(&http.Cookie{Name: flash.FlashCookieName, Value: encodeFlash(t, "success", "Saved!")})
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		props := dataPageProps(t, string(bodyBytes))

		flashProp, ok := props["flash"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected flash prop to be an object, got %T (%v)", props["flash"], props["flash"])
		}
		if flashProp["type"] != "success" || flashProp["message"] != "Saved!" {
			t.Errorf("expected flash {success Saved!}, got %v", flashProp)
		}
	})

	t.Run("flash prop is empty when no flash exists", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/page", func(c *Context) error {
			return c.Inertia("Dashboard", nil)
		})

		req, _ := http.NewRequest("GET", "/page", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		props := dataPageProps(t, string(bodyBytes))

		// GetFlash returns an empty (but non-nil) FlashMessage when no cookie
		// is present; its omitempty fields drop out, so the JSON object is empty.
		flashProp, ok := props["flash"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected flash prop to be an object, got %T (%v)", props["flash"], props["flash"])
		}
		if len(flashProp) != 0 {
			t.Errorf("expected empty flash object, got %v", flashProp)
		}
	})

	t.Run("does not overwrite a caller-supplied flash prop", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/page", func(c *Context) error {
			return c.Inertia("Dashboard", map[string]interface{}{"flash": "custom"})
		})

		req, _ := http.NewRequest("GET", "/page", nil)
		req.AddCookie(&http.Cookie{Name: flash.FlashCookieName, Value: encodeFlash(t, "error", "Boom")})
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		props := dataPageProps(t, string(bodyBytes))

		if props["flash"] != "custom" {
			t.Errorf("expected caller flash 'custom' to be preserved, got %v", props["flash"])
		}
	})
}

func TestFlashAndRedirectBack(t *testing.T) {
	t.Run("FlashError chains into RedirectBack to Referer", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/admin/websites", func(c *Context) error {
			return c.FlashError("Invalid domain").RedirectBack("/admin/websites")
		})

		req, _ := http.NewRequest("POST", "/admin/websites", nil)
		req.Header.Set("Referer", "/admin/websites/new")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusFound {
			t.Errorf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/admin/websites/new" {
			t.Errorf("expected redirect to Referer '/admin/websites/new', got %q", loc)
		}
		assertFlashCookie(t, resp, "error", "Invalid domain")
	})

	t.Run("RedirectBack follows only a Referer on this host", func(t *testing.T) {
		cases := map[string]string{
			"http://example.com/admin/sites?page=2": "/admin/sites?page=2",
			"/admin/sites":                          "/admin/sites",
			"https://evil.com/phish":                "/fallback",
			"//evil.com/phish":                      "/fallback",
			"/\\evil.com":                           "/fallback",
			"http://example.com//evil.com":          "/fallback",
			"javascript:alert(1)":                   "/fallback",
			"evil.com":                              "/fallback",
		}
		for referer, want := range cases {
			app := newTestApp(t)
			app.Post("/x", func(c *Context) error { return c.RedirectBack("/fallback") })
			req := httptest.NewRequest("POST", "http://example.com/x", nil)
			req.Header.Set("Referer", referer)

			resp, _ := app.Test(req)

			if got := resp.Header.Get("Location"); got != want {
				t.Errorf("Referer %q: Location = %q, want %q", referer, got, want)
			}
		}
	})

	t.Run("RedirectBack falls back when Referer is absent", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/admin/websites", func(c *Context) error {
			return c.FlashSuccess("Saved").RedirectBack("/admin/websites")
		})

		req, _ := http.NewRequest("POST", "/admin/websites", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusFound {
			t.Errorf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/admin/websites" {
			t.Errorf("expected redirect to fallback '/admin/websites', got %q", loc)
		}
		assertFlashCookie(t, resp, "success", "Saved")
	})

	t.Run("FlashInfo sets an info flash", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/x", func(c *Context) error {
			return c.FlashInfo("Heads up").RedirectBack("/")
		})

		req, _ := http.NewRequest("POST", "/x", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		assertFlashCookie(t, resp, "info", "Heads up")
	})

	t.Run("flash cookie is Secure only in production", func(t *testing.T) {
		for _, tc := range []struct {
			cfg        Config
			wantSecure bool
		}{
			{&testConfig{}, false},
			{&prodConfig{}, true},
		} {
			app := newTestApp(t)
			app.Post("/x", func(c *Context) error {
				c.Config = tc.cfg
				return c.FlashSuccess("Saved").RedirectBack("/")
			})
			req, _ := http.NewRequest("POST", "/x", nil)

			resp, err := app.Test(req)

			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			cookie := findCookie(resp, flash.FlashCookieName)
			if cookie == nil {
				t.Fatal("expected a flash cookie")
			}
			if cookie.Secure != tc.wantSecure {
				t.Errorf("Secure = %v, want %v", cookie.Secure, tc.wantSecure)
			}
		}
	})
}

type prodConfig struct{ testConfig }

func (c *prodConfig) IsProduction() bool { return true }
func (c *prodConfig) IsTest() bool       { return false }

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// --- test helpers ---

// htmlUnescape reverses the entity escaping applied by html.EscapeString
// to the data-page JSON. Only the entities html.EscapeString emits are handled.
func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&#39;", "'",
		"&#34;", `"`,
		"&lt;", "<",
		"&gt;", ">",
	)
	return r.Replace(s)
}

// encodeFlash builds the base64-encoded cookie value that flash.SetFlash writes,
// so a test request can carry a pre-existing flash message.
func encodeFlash(t *testing.T, msgType, message string) string {
	t.Helper()
	jsonData, err := json.Marshal(flash.FlashMessage{Type: msgType, Message: message})
	if err != nil {
		t.Fatalf("failed to marshal flash: %v", err)
	}
	return base64.StdEncoding.EncodeToString(jsonData)
}

// assertFlashCookie verifies the response set a flash cookie carrying the
// expected type and message.
func assertFlashCookie(t *testing.T, resp *http.Response, wantType, wantMessage string) {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name != flash.FlashCookieName || c.Value == "" {
			continue
		}
		jsonData, err := base64.StdEncoding.DecodeString(c.Value)
		if err != nil {
			t.Fatalf("failed to decode flash cookie: %v", err)
		}
		var fm flash.FlashMessage
		if err := json.Unmarshal(jsonData, &fm); err != nil {
			t.Fatalf("failed to unmarshal flash cookie: %v", err)
		}
		if fm.Type != wantType || fm.Message != wantMessage {
			t.Errorf("expected flash {%s %s}, got {%s %s}", wantType, wantMessage, fm.Type, fm.Message)
		}
		return
	}
	t.Fatalf("expected a %q flash cookie in response, found none", flash.FlashCookieName)
}
