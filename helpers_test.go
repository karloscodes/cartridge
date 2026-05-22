package cartridge

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/karloscodes/cartridge/flash"
)

// newTestContext builds a *Context wired to the given fiber.Ctx, matching how
// Server.wrapHandler constructs it for real requests.
func newTestContext(c *fiber.Ctx) *Context {
	return &Context{
		Ctx:    c,
		Config: &testConfig{},
	}
}

func TestBind(t *testing.T) {
	type input struct {
		Name string `json:"name" form:"name" query:"name"`
		Age  int    `json:"age" form:"age" query:"age"`
		ID   string `json:"-" form:"-" query:"-" params:"id"`
	}

	t.Run("binds a JSON body", func(t *testing.T) {
		app := fiber.New()
		var got input
		app.Post("/users", func(c *fiber.Ctx) error {
			return newTestContext(c).Bind(&got)
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
		app := fiber.New()
		var got input
		app.Post("/users", func(c *fiber.Ctx) error {
			return newTestContext(c).Bind(&got)
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

	t.Run("overlays route params and query values", func(t *testing.T) {
		app := fiber.New()
		var got input
		app.Post("/users/:id", func(c *fiber.Ctx) error {
			return newTestContext(c).Bind(&got)
		})

		body := strings.NewReader(`{"name":"Ada"}`)
		req, _ := http.NewRequest("POST", "/users/42?age=99", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got.Name != "Ada" {
			t.Errorf("expected body value Name=Ada, got %q", got.Name)
		}
		if got.ID != "42" {
			t.Errorf("expected route param ID=42, got %q", got.ID)
		}
		if got.Age != 99 {
			t.Errorf("expected query overlay Age=99, got %d", got.Age)
		}
	})

	t.Run("empty body is not an error", func(t *testing.T) {
		app := fiber.New()
		var got input
		var bindErr error
		app.Get("/users/:id", func(c *fiber.Ctx) error {
			bindErr = newTestContext(c).Bind(&got)
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
		// Params and query overlays should still apply.
		if got.ID != "7" {
			t.Errorf("expected route param ID=7, got %q", got.ID)
		}
		if got.Name != "Linus" {
			t.Errorf("expected query Name=Linus, got %q", got.Name)
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
		app := fiber.New()
		app.Get("/page", func(c *fiber.Ctx) error {
			return newTestContext(c).Inertia("Dashboard", nil)
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
		app := fiber.New()
		app.Get("/page", func(c *fiber.Ctx) error {
			return newTestContext(c).Inertia("Dashboard", nil)
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
		app := fiber.New()
		app.Get("/page", func(c *fiber.Ctx) error {
			return newTestContext(c).Inertia("Dashboard", map[string]interface{}{"flash": "custom"})
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
		app := fiber.New()
		app.Post("/admin/websites", func(c *fiber.Ctx) error {
			return newTestContext(c).FlashError("Invalid domain").RedirectBack("/admin/websites")
		})

		req, _ := http.NewRequest("POST", "/admin/websites", nil)
		req.Header.Set("Referer", "/admin/websites/new")
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusFound {
			t.Errorf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/admin/websites/new" {
			t.Errorf("expected redirect to Referer '/admin/websites/new', got %q", loc)
		}
		assertFlashCookie(t, resp, "error", "Invalid domain")
	})

	t.Run("RedirectBack falls back when Referer is absent", func(t *testing.T) {
		app := fiber.New()
		app.Post("/admin/websites", func(c *fiber.Ctx) error {
			return newTestContext(c).FlashSuccess("Saved").RedirectBack("/admin/websites")
		})

		req, _ := http.NewRequest("POST", "/admin/websites", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusFound {
			t.Errorf("expected 302, got %d", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/admin/websites" {
			t.Errorf("expected redirect to fallback '/admin/websites', got %q", loc)
		}
		assertFlashCookie(t, resp, "success", "Saved")
	})

	t.Run("FlashInfo sets an info flash", func(t *testing.T) {
		app := fiber.New()
		app.Post("/x", func(c *fiber.Ctx) error {
			return newTestContext(c).FlashInfo("Heads up").RedirectBack("/")
		})

		req, _ := http.NewRequest("POST", "/x", nil)
		resp, err := app.Test(req)

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		assertFlashCookie(t, resp, "info", "Heads up")
	})
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
