package cartridge

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	resp, err := srv.Test(req)
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
	// serve sends a request from peer with the given headers and returns the body.
	serve := func(t *testing.T, srv *Server, peer string, headers map[string]string) string {
		t.Helper()
		req := httptest.NewRequest("GET", "/whoami", nil)
		req.RemoteAddr = peer + ":1234"
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	newServer := func(t *testing.T, trusted ...string) *Server {
		t.Helper()
		srv := newTestServer(t, func(c *ServerConfig) {
			c.ProxyHeader = "X-Forwarded-For"
			c.TrustedProxies = trusted
		})
		srv.Get("/whoami", func(c *Context) error { return c.SendString(c.IP() + " " + c.Protocol()) })
		return srv
	}

	t.Run("with no trusted proxies, the headers are ignored", func(t *testing.T) {
		srv := newServer(t)

		got := serve(t, srv, "198.51.100.9", map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Forwarded-Proto": "https"})

		if got != "198.51.100.9 http" {
			t.Errorf("got %q, want the peer address over http", got)
		}
	})

	t.Run("from an untrusted peer, the headers are ignored", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		got := serve(t, srv, "198.51.100.9", map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Forwarded-Proto": "https"})

		if got != "198.51.100.9 http" {
			t.Errorf("got %q, want the peer address over http", got)
		}
	})

	t.Run("from a trusted peer, the headers count", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		got := serve(t, srv, "10.0.0.2", map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Forwarded-Proto": "https"})

		if got != "203.0.113.7 https" {
			t.Errorf("got %q, want the forwarded address over https", got)
		}
	})

	t.Run("a client cannot prepend a fake address", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		got := serve(t, srv, "10.0.0.2", map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.7, 10.0.0.5"})

		if got != "203.0.113.7 http" {
			t.Errorf("got %q, want the rightmost untrusted address", got)
		}
	})

	t.Run("with malformed entries", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		cases := map[string]string{
			"skips garbage":                    "203.0.113.7, garbage",
			"skips empty entries":              "203.0.113.7, , ",
			"skips words like unknown":         "not-an-ip, 203.0.113.7, unknown",
			"reads an IPv4 entry with a port":  "1.2.3.4, 203.0.113.7:5678",
			"reads a quoted entry":             `1.2.3.4, "203.0.113.7"`,
			"reads a quoted entry with a port": `"203.0.113.7:443", 10.0.0.5`,
			"skips garbage behind a proxy":     "203.0.113.7, garbage, 10.0.0.5",
		}
		for name, header := range cases {
			t.Run(name, func(t *testing.T) {
				got := serve(t, srv, "10.0.0.2", map[string]string{"X-Forwarded-For": header})

				if got != "203.0.113.7 http" {
					t.Errorf("X-Forwarded-For %q: got %q, want 203.0.113.7", header, got)
				}
			})
		}
	})

	t.Run("reads a bracketed IPv6 entry with a port", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		got := serve(t, srv, "10.0.0.2", map[string]string{"X-Forwarded-For": "[2001:db8::1]:443"})

		if got != "2001:db8::1 http" {
			t.Errorf("got %q, want the IPv6 address without the port", got)
		}
	})

	t.Run("with only garbage or proxies in the header, returns the peer", func(t *testing.T) {
		srv := newServer(t, "10.0.0.0/8")

		got := serve(t, srv, "10.0.0.2", map[string]string{"X-Forwarded-For": "garbage, 10.0.0.5"})

		if got != "10.0.0.2 http" {
			t.Errorf("got %q, want the peer address", got)
		}
	})
}
