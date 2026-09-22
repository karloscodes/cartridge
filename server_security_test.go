package cartridge

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func newTestServer(t *testing.T, configure func(*ServerConfig)) *Server {
	t.Helper()

	cfg := DefaultServerConfig()
	cfg.EnableStaticAssets = false
	cfg.EnableRequestLogger = false
	cfg.Config = &testConfig{}
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.DBManager = &testDBManager{}
	configure(cfg)

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return srv
}

func crossSitePost(t *testing.T, srv *Server, path string) int {
	t.Helper()

	req, _ := http.NewRequest("POST", path, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := srv.app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp.StatusCode
}

func TestRouteSecFetchSite(t *testing.T) {
	ok := func(c *Context) error { return c.SendString("ok") }

	t.Run("a route can opt in when the server default is off", func(t *testing.T) {
		srv := newTestServer(t, func(c *ServerConfig) { c.EnableSecFetchSite = false })
		srv.Post("/settings", ok, &RouteConfig{EnableSecFetchSite: Bool(true)})

		if status := crossSitePost(t, srv, "/settings"); status != http.StatusForbidden {
			t.Errorf("status = %d, want 403: the route opted in", status)
		}
	})

	t.Run("a route can opt out when the server default is on", func(t *testing.T) {
		srv := newTestServer(t, func(c *ServerConfig) { c.EnableSecFetchSite = true })
		srv.Post("/ingest", ok, &RouteConfig{EnableSecFetchSite: Bool(false)})

		if status := crossSitePost(t, srv, "/ingest"); status != http.StatusOK {
			t.Errorf("status = %d, want 200: the route opted out", status)
		}
	})

	t.Run("a route without a setting follows the server default", func(t *testing.T) {
		srv := newTestServer(t, func(c *ServerConfig) { c.EnableSecFetchSite = true })
		srv.Post("/form", ok)

		if status := crossSitePost(t, srv, "/form"); status != http.StatusForbidden {
			t.Errorf("status = %d, want 403 by default", status)
		}
	})
}

func TestTrustedProxies(t *testing.T) {
	clientIP := func(t *testing.T, trusted []string) string {
		t.Helper()
		srv := newTestServer(t, func(c *ServerConfig) {
			c.ProxyHeader = "X-Forwarded-For"
			c.TrustedProxies = trusted
		})
		srv.Get("/ip", func(c *Context) error { return c.SendString(c.IP()) })

		req, _ := http.NewRequest("GET", "/ip", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		resp, err := srv.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	t.Run("the proxy header is ignored from an untrusted peer", func(t *testing.T) {
		if ip := clientIP(t, []string{"10.0.0.0/8"}); ip == "203.0.113.7" {
			t.Errorf("IP = %s: a client outside TrustedProxies set its own IP", ip)
		}
	})

	t.Run("the proxy header is used from a trusted peer", func(t *testing.T) {
		// app.Test connects from 0.0.0.0.
		if ip := clientIP(t, []string{"0.0.0.0"}); ip != "203.0.113.7" {
			t.Errorf("IP = %s, want the forwarded client IP", ip)
		}
	})
}
