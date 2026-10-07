package cartridge

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestRouting(t *testing.T) {
	t.Run("reads a route param", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/sites/:id/events", func(c *Context) error { return c.SendString("site " + c.Params("id")) })

		resp, _ := app.Test(httptest.NewRequest("GET", "/sites/42/events", nil))

		if got := body(t, resp); got != "site 42" {
			t.Errorf("body = %q, want %q", got, "site 42")
		}
	})

	t.Run("reads a wildcard param", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/files/*", func(c *Context) error { return c.SendString(c.Params("*")) })

		resp, _ := app.Test(httptest.NewRequest("GET", "/files/a/b.txt", nil))

		if got := body(t, resp); got != "a/b.txt" {
			t.Errorf("body = %q, want %q", got, "a/b.txt")
		}
	})

	t.Run("returns 404 for an unclean path instead of redirecting", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/login", func(c *Context) error { return c.SendString("login") })
		app.Get("/admin", func(c *Context) error { return c.SendString("admin") })

		for _, path := range []string{"//evil.example/", "//login", "/admin/../login", "/./admin", "/a//b"} {
			req := httptest.NewRequest("GET", "http://x"+path, nil)
			req.URL.Path = path // keep the unclean path; httptest may clean the URL
			resp, _ := app.Test(req)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s = %d, want 404 (no auto-redirect, like Fiber)", path, resp.StatusCode)
			}
		}
	})

	t.Run("matches a path with a trailing slash", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/admin", func(c *Context) error { return c.SendString("admin") })

		resp, _ := app.Test(httptest.NewRequest("GET", "/admin/", nil))

		if got := body(t, resp); got != "admin" {
			t.Errorf("body = %q, want %q", got, "admin")
		}
	})

	t.Run("a wildcard route matches its bare prefix without a redirect", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/files/*", func(c *Context) error { return c.SendString("files:" + c.Params("*")) })

		resp, _ := app.Test(httptest.NewRequest("GET", "/files/", nil))

		if got := body(t, resp); resp.StatusCode != http.StatusOK || got != "files:" {
			t.Errorf("got %d %q, want 200 %q", resp.StatusCode, got, "files:")
		}
	})

	t.Run("the root route matches only the root", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/", func(c *Context) error { return c.SendString("home") })

		resp, _ := app.Test(httptest.NewRequest("GET", "/missing", nil))

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("an unknown route answers Not Found without echoing the path", func(t *testing.T) {
		app := newTestApp(t)
		req := httptest.NewRequest("GET", "/missing", nil)
		req.Header.Set("Accept", "application/json")

		resp, _ := app.Test(req)

		if got := body(t, resp); got != `{"error":"Not Found","message":"Not Found"}` {
			t.Errorf("body = %s", got)
		}
	})

	t.Run("a GET route answers HEAD", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/_health", func(c *Context) error { return c.SendString("ok") })

		resp, _ := app.Test(httptest.NewRequest("HEAD", "/_health", nil))

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("GET and HEAD routes on one path can both exist", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/_health", func(c *Context) error { return c.SendString("get") })
		app.Head("/_health", func(c *Context) error { return c.SendStatus(http.StatusNoContent) })

		get, _ := app.Test(httptest.NewRequest("GET", "/_health", nil))
		head, _ := app.Test(httptest.NewRequest("HEAD", "/_health", nil))

		if get.StatusCode != http.StatusOK || head.StatusCode != http.StatusNoContent {
			t.Errorf("GET = %d, HEAD = %d, want 200 and 204", get.StatusCode, head.StatusCode)
		}
	})

	t.Run("redirects unmatched paths when a catch-all is set", func(t *testing.T) {
		app := newTestApp(t)
		app.SetCatchAllRedirect("/login")

		resp, _ := app.Test(httptest.NewRequest("GET", "/nowhere", nil))

		if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "/login" {
			t.Errorf("got %d to %q, want 307 to /login", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("runs middleware in order, before the handler", func(t *testing.T) {
		app := newTestApp(t)
		var order []string
		mw := func(name string) HandlerFunc {
			return func(c *Context) error { order = append(order, name); return c.Next() }
		}
		app.Use(mw("global"))
		app.Get("/x", func(c *Context) error { order = append(order, "handler"); return nil },
			&RouteConfig{CustomMiddleware: []HandlerFunc{mw("route")}})

		_, _ = app.Test(httptest.NewRequest("GET", "/x", nil))

		if got := strings.Join(order, ","); got != "global,route,handler" {
			t.Errorf("order = %s, want global,route,handler", got)
		}
	})
}

func TestContextResponses(t *testing.T) {
	t.Run("JSON sets the status, type, and body", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/x", func(c *Context) error { return c.Status(http.StatusAccepted).JSON(Map{"ok": true}) })

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if resp.StatusCode != http.StatusAccepted || resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("got %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if got := body(t, resp); got != `{"ok":true}` {
			t.Errorf("body = %s", got)
		}
	})

	t.Run("Redirect defaults to 302", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/x", func(c *Context) error { return c.Redirect("/done") })

		resp, _ := app.Test(httptest.NewRequest("POST", "/x", nil))

		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/done" {
			t.Errorf("got %d to %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("Inertia redirect after PUT, PATCH or DELETE is a 303", func(t *testing.T) {
		app := newTestApp(t)
		for _, m := range []string{"PUT", "PATCH", "DELETE"} {
			app.registerRoute(m, "/items/:id", func(c *Context) error { return c.Redirect("/items") })
		}
		app.Post("/items", func(c *Context) error { return c.Redirect("/items") })
		send := func(method string, inertia bool) int {
			path := "/items/1"
			if method == "POST" {
				path = "/items"
			}
			req := httptest.NewRequest(method, path, nil)
			if inertia {
				req.Header.Set("X-Inertia", "true")
			}
			resp, _ := app.Test(req)
			return resp.StatusCode
		}

		for _, m := range []string{"PUT", "PATCH", "DELETE"} {
			if got := send(m, true); got != http.StatusSeeOther {
				t.Errorf("Inertia %s redirect = %d, want 303", m, got)
			}
		}
		if got := send("DELETE", false); got != http.StatusFound {
			t.Errorf("plain DELETE redirect = %d, want 302", got)
		}
		if got := send("POST", true); got != http.StatusFound {
			t.Errorf("Inertia POST redirect = %d, want 302", got)
		}
	})

	t.Run("Query and Cookies fall back to a default", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/x", func(c *Context) error {
			return c.SendString(c.Query("range", "last_7_days") + " " + c.Cookies("_tz", "UTC"))
		})

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if got := body(t, resp); got != "last_7_days UTC" {
			t.Errorf("body = %q", got)
		}
	})

	t.Run("ParamsInt rejects a non-number", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/sites/:id", func(c *Context) error {
			if _, err := c.ParamsInt("id"); err != nil {
				return c.SendStatus(http.StatusBadRequest)
			}
			return c.SendStatus(http.StatusOK)
		})

		resp, _ := app.Test(httptest.NewRequest("GET", "/sites/abc", nil))

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("Cookie writes the attributes", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/x", func(c *Context) error {
			c.Cookie(&Cookie{Name: "sid", Value: "abc", HTTPOnly: true, Secure: true, SameSite: "Strict"})
			return nil
		})

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if got := resp.Header.Get("Set-Cookie"); got != "sid=abc; Path=/; HttpOnly; Secure; SameSite=Strict" {
			t.Errorf("Set-Cookie = %q", got)
		}
	})

	t.Run("Locals carries a value from middleware to the handler", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(func(c *Context) error { c.Locals("website_id", 7); return c.Next() })
		app.Get("/x", func(c *Context) error {
			id, _ := c.Locals("website_id").(int)
			return c.JSON(id)
		})

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if got := body(t, resp); got != "7" {
			t.Errorf("body = %q, want 7", got)
		}
	})

	t.Run("BodyParser reads form fields by tag", func(t *testing.T) {
		app := newTestApp(t)
		var got struct {
			Email    string   `form:"email"`
			Remember bool     `form:"remember"`
			Tags     []string `form:"tag"`
		}
		app.Post("/x", func(c *Context) error { return c.BodyParser(&got) })
		req := httptest.NewRequest("POST", "/x", strings.NewReader("email=a%40b.c&remember=on&tag=x&tag=y"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		_, _ = app.Test(req)

		if got.Email != "a@b.c" || !got.Remember || strings.Join(got.Tags, ",") != "x,y" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("an error after the response started keeps the response", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/x", func(c *Context) error {
			_ = c.SendString("partial")
			return NewError(http.StatusInternalServerError)
		})

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if resp.StatusCode != http.StatusOK || body(t, resp) != "partial" {
			t.Errorf("got %d", resp.StatusCode)
		}
	})
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (r *closeRecorder) Close() error {
	r.closed = true
	return nil
}

func TestSendStream(t *testing.T) {
	t.Run("closes a reader that is an io.Closer", func(t *testing.T) {
		app := newTestApp(t)
		stream := &closeRecorder{Reader: strings.NewReader("data")}
		app.Get("/x", func(c *Context) error { return c.SendStream(stream) })

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		if body(t, resp) != "data" || !stream.closed {
			t.Errorf("closed = %v", stream.closed)
		}
	})
}

func TestServerTest(t *testing.T) {
	t.Run("returns an error when the handler runs past the timeout", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/slow", func(c *Context) error {
			time.Sleep(200 * time.Millisecond)
			return nil
		})

		_, err := app.Test(httptest.NewRequest("GET", "/slow", nil), 20)

		if err == nil {
			t.Error("expected a timeout error")
		}
	})

	t.Run("returns the response within the timeout", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/fast", func(c *Context) error { return c.SendString("ok") })

		resp, err := app.Test(httptest.NewRequest("GET", "/fast", nil), 1000)

		if err != nil || body(t, resp) != "ok" {
			t.Errorf("got %v", err)
		}
	})
}

func TestBodyLimit(t *testing.T) {
	// chunked returns a POST whose length the server does not know up front.
	chunked := func(body string) *http.Request {
		req := httptest.NewRequest("POST", "/x", io.NopCloser(strings.NewReader(body)))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}

	t.Run("a config without a BodyLimit gets the 4 MB default", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/x", func(c *Context) error { return c.SendString("ok") })

		resp, _ := app.Test(httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 4*1024*1024+1))))

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", resp.StatusCode)
		}
	})

	t.Run("a negative BodyLimit turns the limit off", func(t *testing.T) {
		app := newTestApp(t)
		app.cfg.BodyLimit = -1
		app.Post("/x", func(c *Context) error { return c.SendString("ok") })

		resp, _ := app.Test(httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 4*1024*1024+1))))

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("rejects a body with a Content-Length over the limit", func(t *testing.T) {
		app := newTestApp(t)
		app.cfg.BodyLimit = 10
		app.Post("/x", func(c *Context) error { return c.SendString(string(c.Body())) })

		resp, _ := app.Test(httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 11))))

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", resp.StatusCode)
		}
	})

	t.Run("Bind rejects a chunked body over the limit", func(t *testing.T) {
		app := newTestApp(t)
		app.cfg.BodyLimit = 10
		app.Post("/x", func(c *Context) error {
			var in struct {
				Name string `form:"name"`
			}
			if err := c.Bind(&in); err != nil {
				return err
			}
			return c.SendString(in.Name)
		})

		resp, _ := app.Test(chunked("name=" + strings.Repeat("a", 20)))

		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", resp.StatusCode)
		}
	})

	t.Run("Body and FormValue give nothing for a chunked body over the limit", func(t *testing.T) {
		app := newTestApp(t)
		app.cfg.BodyLimit = 10
		app.Post("/x", func(c *Context) error {
			return c.SendString(fmt.Sprintf("%d %q", len(c.Body()), c.FormValue("name")))
		})

		resp, _ := app.Test(chunked("name=" + strings.Repeat("a", 20)))

		if got := body(t, resp); got != `0 ""` {
			t.Errorf("body = %s, want no truncated data", got)
		}
	})
}

func TestBuiltInMiddleware(t *testing.T) {
	t.Run("drops the security headers that break pages or do nothing", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(SecurityHeaders())
		app.Get("/x", func(c *Context) error { return nil })

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		for _, header := range []string{"Cross-Origin-Embedder-Policy", "X-XSS-Protection", "X-Download-Options", "X-DNS-Prefetch-Control"} {
			if got := resp.Header.Get(header); got != "" {
				t.Errorf("%s = %q, want none", header, got)
			}
		}
	})

	t.Run("sends the configured Content-Security-Policy", func(t *testing.T) {
		srv := newTestServer(t, func(c *ServerConfig) { c.ContentSecurityPolicy = "default-src 'self'" })
		srv.Get("/x", func(c *Context) error { return nil })

		resp, _ := srv.Test(httptest.NewRequest("GET", "/x", nil))

		if got := resp.Header.Get("Content-Security-Policy"); got != "default-src 'self'" {
			t.Errorf("Content-Security-Policy = %q", got)
		}
	})

	t.Run("sends HSTS only over https in production", func(t *testing.T) {
		cases := []struct {
			name   string
			config Config
			url    string
			want   string
		}{
			{"https in production", &prodConfig{}, "https://example.com/x", "max-age=31536000"},
			{"http in production", &prodConfig{}, "http://example.com/x", ""},
			{"https outside production", &testConfig{}, "https://example.com/x", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := newTestServer(t, func(c *ServerConfig) { c.Config = tc.config })
				srv.Get("/x", func(c *Context) error { return nil })

				resp, _ := srv.Test(httptest.NewRequest("GET", tc.url, nil))

				if got := resp.Header.Get("Strict-Transport-Security"); got != tc.want {
					t.Errorf("Strict-Transport-Security = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("sets the security headers", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(SecurityHeaders())
		app.Get("/x", func(c *Context) error { return nil })

		resp, _ := app.Test(httptest.NewRequest("GET", "/x", nil))

		for header, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "SAMEORIGIN",
			"Referrer-Policy":              "same-origin",
			"Cross-Origin-Opener-Policy":   "same-origin",
			"Cross-Origin-Resource-Policy": "same-origin",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
	})

	t.Run("keeps the client's request ID", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(RequestID())
		app.Get("/x", func(c *Context) error { return c.SendString(c.Locals("requestid").(string)) })
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Request-ID", "abc-123")

		resp, _ := app.Test(req)

		if resp.Header.Get("X-Request-ID") != "abc-123" || body(t, resp) != "abc-123" {
			t.Errorf("request ID = %q", resp.Header.Get("X-Request-ID"))
		}
	})

	t.Run("replaces a request ID that is too long or has other characters", func(t *testing.T) {
		for _, id := range []string{strings.Repeat("a", 65), "abc\nforged log line", "<script>", "a b"} {
			app := newTestApp(t)
			app.Use(RequestID())
			app.Get("/x", func(c *Context) error { return nil })
			req := httptest.NewRequest("GET", "/x", nil)
			req.Header.Set("X-Request-ID", id)

			resp, _ := app.Test(req)

			if got := resp.Header.Get("X-Request-ID"); got == id || len(got) != 32 {
				t.Errorf("client ID %q: got %q, want a new ID", id, got)
			}
		}
	})

	t.Run("turns a panic into a 500 and logs it", func(t *testing.T) {
		var logs strings.Builder
		app := newTestApp(t)
		app.cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
		app.Use(Recover())
		app.Get("/x", func(c *Context) error { panic("boom-secret") })
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Accept", "application/json")

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", resp.StatusCode)
		}
		if strings.Contains(body(t, resp), "boom-secret") {
			t.Error("the response leaks the panic value")
		}
		if !strings.Contains(logs.String(), "panic recovered") || !strings.Contains(logs.String(), "boom-secret") {
			t.Errorf("expected the panic in the log, got %s", logs.String())
		}
	})

	t.Run("gzips a response the client accepts", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(Compress())
		app.Get("/x", func(c *Context) error { return c.SendString(strings.Repeat("fusionaly ", 200)) })
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Accept-Encoding", "gzip, br")

		resp, _ := app.Test(req)

		if resp.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", resp.Header.Get("Content-Encoding"))
		}
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatalf("gzip: %v", err)
		}
		plain, _ := io.ReadAll(zr)
		if string(plain) != strings.Repeat("fusionaly ", 200) {
			t.Errorf("decompressed body is wrong: %q", plain[:20])
		}
	})

	t.Run("does not gzip a tiny body", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(Compress())
		app.Get("/x", func(c *Context) error { return c.JSON(Map{"ok": true}) })
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Accept-Encoding", "gzip")

		resp, _ := app.Test(req)

		if resp.Header.Get("Content-Encoding") != "" || body(t, resp) != `{"ok":true}` {
			t.Errorf("tiny body was encoded: %q", resp.Header.Get("Content-Encoding"))
		}
	})

	t.Run("does not gzip an event stream", func(t *testing.T) {
		app := newTestApp(t)
		app.Use(Compress())
		app.Get("/x", func(c *Context) error {
			c.Set("Content-Type", "text/event-stream")
			return c.SendString("data: hi\n\n")
		})
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Accept-Encoding", "gzip")

		resp, _ := app.Test(req)

		if resp.Header.Get("Content-Encoding") != "" || body(t, resp) != "data: hi\n\n" {
			t.Errorf("event stream was encoded: %q", resp.Header.Get("Content-Encoding"))
		}
	})
}

func TestCORS(t *testing.T) {
	publicCORS := &CORSConfig{
		AllowOrigins:  "*",
		AllowMethods:  "POST,GET,OPTIONS",
		AllowHeaders:  "Origin, Content-Type",
		ExposeHeaders: "Retry-After",
	}
	preflight := func(path string) *http.Request {
		req := httptest.NewRequest("OPTIONS", path, nil)
		req.Header.Set("Origin", "https://blog.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		return req
	}

	t.Run("a CORS route answers preflight without its own OPTIONS route", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/mcp", func(c *Context) error { return nil }, &RouteConfig{EnableCORS: true, CORSConfig: publicCORS})

		resp, _ := app.Test(preflight("/mcp"))

		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", resp.StatusCode)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" || resp.Header.Get("Access-Control-Allow-Methods") != "POST,GET,OPTIONS" {
			t.Errorf("CORS headers = %v", resp.Header)
		}
		if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "Origin,Content-Type" {
			t.Errorf("Allow-Headers = %q, want the list without spaces, as Fiber sent it", got)
		}
	})

	t.Run("an explicit OPTIONS route still runs", func(t *testing.T) {
		app := newTestApp(t)
		cfg := &RouteConfig{EnableCORS: true, CORSConfig: publicCORS}
		app.Post("/events", func(c *Context) error { return nil }, cfg)
		app.Options("/events", func(c *Context) error { return c.SendStatus(http.StatusNoContent) }, cfg)
		req := httptest.NewRequest("OPTIONS", "/events", nil)

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("status = %d, want 204", resp.StatusCode)
		}
	})

	t.Run("a cross-origin request gets the allow and expose headers", func(t *testing.T) {
		app := newTestApp(t)
		app.Post("/events", func(c *Context) error { return c.SendStatus(http.StatusAccepted) }, &RouteConfig{EnableCORS: true, CORSConfig: publicCORS})
		req := httptest.NewRequest("POST", "/events", nil)
		req.Header.Set("Origin", "https://blog.example.com")

		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", resp.StatusCode)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" || resp.Header.Get("Access-Control-Expose-Headers") != "Retry-After" {
			t.Errorf("CORS headers = %v", resp.Header)
		}
	})

	t.Run("a listed origin is echoed and another is not", func(t *testing.T) {
		app := newTestApp(t)
		app.Get("/x", func(c *Context) error { return nil }, &RouteConfig{EnableCORS: true, CORSConfig: &CORSConfig{AllowOrigins: "https://a.com"}})
		allowed := httptest.NewRequest("GET", "/x", nil)
		allowed.Header.Set("Origin", "https://a.com")
		other := httptest.NewRequest("GET", "/x", nil)
		other.Header.Set("Origin", "https://b.com")

		respA, _ := app.Test(allowed)
		respB, _ := app.Test(other)

		if got := respA.Header.Get("Access-Control-Allow-Origin"); got != "https://a.com" {
			t.Errorf("allowed origin got %q", got)
		}
		if got := respB.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("other origin got %q, want none", got)
		}
	})
}

func TestStaticAssets(t *testing.T) {
	assets := fstest.MapFS{
		"app-abc123.js":      &fstest.MapFile{Data: []byte("console.log(1)")},
		"chunks/lib-def.css": &fstest.MapFile{Data: []byte("body{}")},
		".env":               &fstest.MapFile{Data: []byte("SECRET=1")},
		".git/config":        &fstest.MapFile{Data: []byte("[core]")},
		"chunks/.DS_Store":   &fstest.MapFile{Data: []byte("x")},
	}
	newApp := func() *Server {
		app := newTestApp(t)
		app.cfg.EnableStaticAssets = true
		app.cfg.StaticFS = assets
		app.cfg.StaticPrefix = "/assets"
		return app
	}

	t.Run("serves an embedded file with a one-year cache", func(t *testing.T) {
		resp, _ := newApp().Test(httptest.NewRequest("GET", "/assets/app-abc123.js", nil))

		if resp.StatusCode != http.StatusOK || body(t, resp) != "console.log(1)" {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000" {
			t.Errorf("Cache-Control = %q", got)
		}
	})

	t.Run("serves a nested file", func(t *testing.T) {
		resp, _ := newApp().Test(httptest.NewRequest("GET", "/assets/chunks/lib-def.css", nil))

		if resp.StatusCode != http.StatusOK || body(t, resp) != "body{}" {
			t.Errorf("status = %d", resp.StatusCode)
		}
	})

	t.Run("the prefix with a slash is a 404, not a redirect", func(t *testing.T) {
		resp, _ := newApp().Test(httptest.NewRequest("GET", "/assets/", nil))

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d to %q, want 404", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("does not list a directory", func(t *testing.T) {
		resp, _ := newApp().Test(httptest.NewRequest("GET", "/assets/chunks/", nil))

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("returns 404 for a missing file", func(t *testing.T) {
		resp, _ := newApp().Test(httptest.NewRequest("GET", "/assets/missing.js", nil))

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("hides dotfiles", func(t *testing.T) {
		app := newApp()

		for _, path := range []string{"/assets/.env", "/assets/.git/config", "/assets/chunks/.DS_Store"} {
			resp, _ := app.Test(httptest.NewRequest("GET", path, nil))

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s = %d, want 404", path, resp.StatusCode)
			}
		}
	})
}

func TestHTMLViews(t *testing.T) {
	views := NewHTMLViews(fstest.MapFS{
		"layouts/main.html":  &fstest.MapFile{Data: []byte(`<main>{{embed}}</main>`)},
		"notes/index.html":   &fstest.MapFile{Data: []byte(`{{range .}}{{render "partials/note" .}}{{end}}`)},
		"partials/note.html": &fstest.MapFile{Data: []byte(`<p>{{.}}</p>`)},
	}, nil, false)
	app := newTestApp(t)
	app.cfg.ViewsEngine = views
	app.Get("/notes", func(c *Context) error {
		return c.Render("notes/index", []string{"a", "<b>"}, "layouts/main")
	})

	resp, _ := app.Test(httptest.NewRequest("GET", "/notes", nil))

	if got := body(t, resp); got != "<main><p>a</p><p>&lt;b&gt;</p></main>" {
		t.Errorf("body = %q", got)
	}
}

func TestShutdownWaitsForOpenRequests(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	app := newTestApp(t)
	app.cfg.Config = &portConfig{port: port}
	started := make(chan struct{})
	app.Get("/slow", func(c *Context) error {
		close(started)
		time.Sleep(200 * time.Millisecond)
		return c.SendString("done")
	})
	go func() { _ = app.Start() }()
	waitForPort(t, port)

	result := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://127.0.0.1:" + port + "/slow")
		if err != nil {
			result <- err.Error()
			return
		}
		result <- body(t, resp)
	}()
	<-started
	err = app.Shutdown(context.Background())

	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := <-result; got != "done" {
		t.Errorf("open request got %q, want done", got)
	}
}

type portConfig struct {
	testConfig
	port string
}

func (c *portConfig) GetPort() string { return c.port }

func waitForPort(t *testing.T, port string) {
	t.Helper()
	for range 100 {
		if conn, err := net.Dial("tcp", "127.0.0.1:"+port); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start")
}

func TestWriteTimeout(t *testing.T) {
	t.Run("a handler can lift the write deadline for a stream", func(t *testing.T) {
		port := freePort(t)
		app := newTestApp(t)
		app.cfg.Config = &portConfig{port: port}
		app.cfg.WriteTimeout = 50 * time.Millisecond
		app.Get("/stream", func(c *Context) error {
			if err := http.NewResponseController(c.Response()).SetWriteDeadline(time.Time{}); err != nil {
				return err
			}
			time.Sleep(150 * time.Millisecond)
			return c.SendString("done")
		})
		if err := app.StartAsync(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Shutdown(context.Background()) })

		resp, err := http.Get("http://127.0.0.1:" + port + "/stream")

		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if got := body(t, resp); got != "done" {
			t.Errorf("body = %q, want done", got)
		}
	})
}
