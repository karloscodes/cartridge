package cartridge

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The cases in this file come from the Rails test suite and from Rails and
// Rack security advisories: host_authorization_test.rb, request_test.rb
// (remote_ip), redirect_test.rb, and request_forgery_protection_test.rb.
// They are the inputs that broke Rails or Rack before. See
// docs/rails-security-checklist.md.

func TestRailsHostCases(t *testing.T) {
	status := func(t *testing.T, allowed []string, host string) int {
		t.Helper()
		srv := newTestServer(t, func(c *ServerConfig) { c.AllowedHosts = allowed })
		srv.Get("/", func(c *Context) error { return c.SendString("ok") })
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		resp, err := srv.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	cases := []struct {
		name    string
		allowed string
		host    string
		ok      bool
	}{
		{"an allowed host", "www.example.com", "www.example.com", true},
		{"another host", "only.com", "www.example.com", false},
		{"an entry in another case", "Example.local", "example.local", true},
		{"a host in another case", "example.local", "Example.local", true},
		{"a subdomain of a dot entry", ".example.com", "www.example.com", true},
		{"the domain of a dot entry", ".example.com", "example.com", true},
		{"a deep subdomain of a dot entry", ".example.com", "a.b.example.com", true},
		{"the dot notation as the host", ".example.com", ".example.com", false},
		{"a subdomain of an exact entry", "domain.com", "secondary.sub.domain.com", false},
		{"a port on the host", "host.test", "host.test:3000", true},
		{"an entry with a port", "host.test:3000", "host.test:3000", true},
		{"two ports", "www.example.com:80", "www.example.com:80:80", false},
		{"a # before the allowed domain", ".example.com", "attacker.com#x.example.com", false},
		{"a similar host", "sub.example.com", "sub-example.com", false},
		{"a domain that only ends like the entry", ".example.com", "evilexample.com", false},
		{"an encoded dot", "example.com", "hacker%E3%80%82com", false},
		{"an encoded null byte", "example.com", "hacker%00.com", false},
		{"userinfo", "example.com", "www.theirsite.com@yoursite.com", false},
		{"userinfo before the allowed domain", ".example.com", "www.theirsite.com@x.example.com", false},
		{"a path", "example.com", "hacker.com/test/", false},
		{"a path before the allowed domain", ".example.com", "hacker.com/x.example.com", false},
		{"a leading slash", "example.com", "/hacker.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := status(t, []string{c.allowed}, c.host)

			if ok := got == http.StatusOK; ok != c.ok {
				t.Errorf("allowed %q, host %q: status = %d, want allowed = %v", c.allowed, c.host, got, c.ok)
			}
		})
	}

	t.Run("a forwarded host header does not change the host", func(t *testing.T) {
		srv := newTestServer(t, func(c *ServerConfig) { c.AllowedHosts = []string{"www.example.com"} })
		srv.Get("/", func(c *Context) error { return c.SendString(c.Hostname() + " " + c.BaseURL()) })
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "www.example.com"
		req.Header.Set("X-Forwarded-Host", "evil.com")
		req.Header.Set("Forwarded", "host=evil.com;proto=https")

		resp, _ := srv.Test(req)

		if got := body(t, resp); got != "www.example.com http://www.example.com" {
			t.Errorf("got %q", got)
		}
	})
}

func TestRailsClientIPCases(t *testing.T) {
	// Rails trusts loopback and the private ranges by default. Cartridge
	// trusts nothing until the app names its proxies.
	ip := func(t *testing.T, peer string, headers map[string]string) string {
		t.Helper()
		srv := newTestServer(t, func(c *ServerConfig) {
			c.ProxyHeader = "X-Forwarded-For"
			c.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
		})
		srv.Get("/ip", func(c *Context) error { return c.SendString(c.IP()) })
		req := httptest.NewRequest("GET", "/ip", nil)
		req.RemoteAddr = peer + ":1234"
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	cases := []struct {
		name      string
		forwarded string
		want      string
	}{
		{"one address", "3.4.5.6", "3.4.5.6"},
		{"an entry that is not an address", "3.4.5.6,unknown", "3.4.5.6"},
		{"a private proxy after the client", "3.4.5.6,172.16.0.1", "3.4.5.6"},
		{"a port on the client", "3.4.5.6:1234,127.0.0.1", "3.4.5.6"},
		{"an address that the client added in front", "9.9.9.9, 3.4.5.6, 172.31.4.4, 10.0.0.1", "3.4.5.6"},
		{"IPv6 with brackets and a port", "[fe80::0202:b3ff:fe1e:8329]:3000,unknown", "fe80::202:b3ff:fe1e:8329"},
		{"no address at all", "not_ip_address", "127.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ip(t, "127.0.0.1", map[string]string{"X-Forwarded-For": c.forwarded})

			if got != c.want {
				t.Errorf("X-Forwarded-For %q: got %q, want %q", c.forwarded, got, c.want)
			}
		})
	}

	t.Run("a Client-IP header is never read", func(t *testing.T) {
		got := ip(t, "127.0.0.1", map[string]string{"X-Forwarded-For": "1.1.1.1", "Client-IP": "2.2.2.2", "X-Client-IP": "2.2.2.2"})

		if got != "1.1.1.1" {
			t.Errorf("got %q, want 1.1.1.1", got)
		}
	})

	t.Run("a peer that is not a trusted proxy cannot set its address", func(t *testing.T) {
		got := ip(t, "1.2.3.4", map[string]string{"X-Forwarded-For": "3.4.5.6"})

		if got != "1.2.3.4" {
			t.Errorf("got %q, want the peer 1.2.3.4", got)
		}
	})
}

// externalRedirects are locations that take a browser to another site.
var externalRedirects = []string{
	"http://www.rubyonrails.org/",
	"//www.rubyonrails.org/",
	"///www.rubyonrails.org/",
	"http:///www.rubyonrails.org/",
	"https:www.rubyonrails.org",
	"javascript:alert(document.domain)\b",
	"http://test.host@www.rubyonrails.org/",
	`\\www.rubyonrails.org/`,
	`/\www.rubyonrails.org/`,
	" //www.rubyonrails.org/",
	"\t//www.rubyonrails.org/",
}

func TestRailsRedirectCases(t *testing.T) {
	redirect := func(t *testing.T, block bool, handler HandlerFunc, to string) *http.Response {
		t.Helper()
		srv := newTestServer(t, func(c *ServerConfig) { c.BlockExternalRedirects = block })
		srv.Get("/go", handler)
		req := httptest.NewRequest("GET", "http://test.host/go?to="+url.QueryEscape(to), nil)
		resp, err := srv.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	plain := func(c *Context) error { return c.Redirect(c.Query("to")) }

	t.Run("with BlockExternalRedirects, Redirect refuses another host", func(t *testing.T) {
		for _, to := range append([]string{"/lol\r\nwat", "\x00/lol\r\nwat"}, externalRedirects...) {
			resp := redirect(t, true, plain, to)

			if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("Location") != "" {
				t.Errorf("%q: status = %d, Location = %q, want a 500", to, resp.StatusCode, resp.Header.Get("Location"))
			}
		}
	})

	t.Run("with BlockExternalRedirects, Redirect follows a location on this host", func(t *testing.T) {
		for _, to := range []string{"/things/stuff", "http://test.host/app", "?foo=bar", "edit", "@example.com"} {
			resp := redirect(t, true, plain, to)

			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != to {
				t.Errorf("%q: status = %d, Location = %q", to, resp.StatusCode, resp.Header.Get("Location"))
			}
		}
	})

	t.Run("with BlockExternalRedirects, RedirectExternal still leaves the site", func(t *testing.T) {
		resp := redirect(t, true, func(c *Context) error { return c.RedirectExternal(c.Query("to")) }, "http://www.rubyonrails.org/")

		if resp.Header.Get("Location") != "http://www.rubyonrails.org/" {
			t.Errorf("Location = %q", resp.Header.Get("Location"))
		}
	})

	t.Run("without BlockExternalRedirects, Redirect behaves as before", func(t *testing.T) {
		resp := redirect(t, false, plain, "http://www.rubyonrails.org/")

		if resp.Header.Get("Location") != "http://www.rubyonrails.org/" {
			t.Errorf("Location = %q", resp.Header.Get("Location"))
		}
	})

	t.Run("RedirectLocal never leaves the site", func(t *testing.T) {
		local := func(c *Context) error { return c.RedirectLocal(c.Query("to"), "/fallback") }
		for _, to := range append([]string{"/lol\r\nwat", "example.com", "@example.com"}, externalRedirects...) {
			resp := redirect(t, false, local, to)

			got := resp.Header.Get("Location")
			if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") || strings.HasPrefix(got, `/\`) {
				t.Errorf("%q: Location = %q, want a path on this host", to, got)
			}
		}
	})

	t.Run("a line break in a location cannot add a response header", func(t *testing.T) {
		port := freePort(t)
		srv := newTestServer(t, func(c *ServerConfig) { c.Config = &portConfig{port: port} })
		srv.Get("/go", func(c *Context) error { return c.Redirect("/lol\r\nSet-Cookie: evil=1\r\nX-Evil: 1") })
		if err := srv.StartAsync(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

		resp, err := client.Get("http://127.0.0.1:" + port + "/go")

		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("X-Evil") != "" {
			t.Errorf("the response has injected headers: %v", resp.Header)
		}
	})
}

func TestRailsCSRFCases(t *testing.T) {
	ok := func(c *Context) error { return c.SendString("ok") }
	status := func(t *testing.T, method string, headers map[string]string) int {
		t.Helper()
		srv := newTestServer(t, func(*ServerConfig) {})
		srv.Get("/x", ok)
		srv.Post("/x", ok)
		srv.Put("/x", ok)
		srv.Patch("/x", ok)
		srv.Delete("/x", ok)
		req := httptest.NewRequest(method, "http://test.host/x", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := srv.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	t.Run("GET and HEAD pass without any header", func(t *testing.T) {
		for _, method := range []string{"GET", "HEAD"} {
			if got := status(t, method, nil); got != http.StatusOK {
				t.Errorf("%s: status = %d, want 200", method, got)
			}
		}
	})

	t.Run("every method that changes state is blocked without a header", func(t *testing.T) {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			if got := status(t, method, nil); got != http.StatusForbidden {
				t.Errorf("%s: status = %d, want 403", method, got)
			}
		}
	})

	t.Run("an XMLHttpRequest header is not a pass", func(t *testing.T) {
		if got := status(t, "POST", map[string]string{"X-Requested-With": "XMLHttpRequest"}); got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	t.Run("the Origin of this server passes", func(t *testing.T) {
		if got := status(t, "POST", map[string]string{"Origin": "http://test.host"}); got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("a null Origin, another Origin, and another scheme are blocked", func(t *testing.T) {
		for _, origin := range []string{"null", "http://bad.host", "https://test.host", "http://test.host.bad.host", "http://test.host@bad.host"} {
			if got := status(t, "POST", map[string]string{"Origin": origin}); got != http.StatusForbidden {
				t.Errorf("Origin %q: status = %d, want 403", origin, got)
			}
		}
	})

	t.Run("a method override header does not turn a GET into a write", func(t *testing.T) {
		written := false
		srv := newTestServer(t, func(*ServerConfig) {})
		srv.Get("/x", ok)
		srv.Post("/x", func(c *Context) error { written = true; return c.SendString("written") })
		req := httptest.NewRequest("GET", "http://test.host/x?_method=POST", nil)
		req.Header.Set("X-HTTP-Method-Override", "POST")

		resp, _ := srv.Test(req)

		if written || body(t, resp) != "ok" {
			t.Error("the override reached the POST route")
		}
	})
}

// From the Rack::Static and Rack::Directory advisories: encoded dots and
// slashes must not reach a file outside the static folder, or a dotfile.
func TestStaticFilesStayInTheirFolder(t *testing.T) {
	dir := t.TempDir()
	public := filepath.Join(dir, "public")
	writeFile(t, filepath.Join(public, "app.js"), "console.log(1)")
	writeFile(t, filepath.Join(public, ".env"), "SECRET=1")
	writeFile(t, filepath.Join(dir, "secret.txt"), "TOP SECRET")
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(public, ".link")); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, func(c *ServerConfig) {
		c.EnableStaticAssets = true
		c.StaticDirectory = public
	})

	for _, target := range []string{
		"/assets/../secret.txt",
		"/assets/%2e%2e/secret.txt",
		"/assets/..%2fsecret.txt",
		"/assets/.%2e/secret.txt",
		"/assets/app.js/../../secret.txt",
		"/assets/%2e%2e%2f%2e%2e%2fsecret.txt",
		"/assets../secret.txt",
		"/assets/%2eenv",
		"/assets/.env",
		"/assets/.link",
		"/assets//secret.txt",
		`/assets/..\secret.txt`,
		"/assets/..%5csecret.txt",
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.URL = &url.URL{Path: mustUnescape(t, target), RawPath: target}

		resp, _ := srv.Test(req)

		got := body(t, resp)
		if resp.StatusCode == http.StatusOK || strings.Contains(got, "SECRET") {
			t.Errorf("%s: status = %d, body = %q", target, resp.StatusCode, got)
		}
	}
}

func mustUnescape(t *testing.T, path string) string {
	t.Helper()
	unescaped, err := url.PathUnescape(path)
	if err != nil {
		t.Fatal(err)
	}
	return unescaped
}

// From the Rack::CommonLogger and Active Record logging advisories: a
// request value must not forge a log line or send an escape sequence to
// the terminal.
func TestDevLogEscapesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(newColorHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))

	logger.Info("http request", "path", "/a\n12:00:00 ERROR forged\x1b[2J", "status", 200)

	got := out.String()
	if strings.Count(got, "\n") != 1 || strings.Contains(got, "\x1b[2J") {
		t.Errorf("log output = %q, want one line and no escape sequence from the path", got)
	}
	if !strings.Contains(got, `/a\n12:00:00 ERROR forged`) {
		t.Errorf("log output = %q, want the path with its line break escaped", got)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Run("a version turns on the defaults up to it", func(t *testing.T) {
		old, current := DefaultServerConfig(), DefaultServerConfig()

		errOld, errCurrent := old.LoadDefaults("1.6"), current.LoadDefaults("1.7")

		if errOld != nil || errCurrent != nil {
			t.Fatalf("errors: %v, %v", errOld, errCurrent)
		}
		if old.BlockExternalRedirects || !current.BlockExternalRedirects {
			t.Errorf("BlockExternalRedirects: 1.6 = %v, 1.7 = %v, want false and true", old.BlockExternalRedirects, current.BlockExternalRedirects)
		}
	})

	t.Run("an unknown version is an error", func(t *testing.T) {
		if err := DefaultServerConfig().LoadDefaults("1.70"); err == nil {
			t.Error("LoadDefaults returned nil, want an error")
		}
	})

	t.Run("WithDefaults sets them for NewApp, and WithServerConfig can turn one off", func(t *testing.T) {
		location := func(opts ...AppOption) string {
			opts = append(opts, WithRoutes(func(s *Server) {
				s.Get("/go", func(c *Context) error { return c.Redirect("http://other.test/") })
			}))
			app, err := NewApp(newAppTestConfig(t), opts...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
			rec := httptest.NewRecorder()
			app.Server.ServeHTTP(rec, httptest.NewRequest("GET", "/go", nil))
			return rec.Header().Get("Location")
		}

		blocked := location(WithDefaults("1.7"))
		off := location(WithDefaults("1.7"), WithServerConfig(func(c *ServerConfig) { c.BlockExternalRedirects = false }))

		if blocked != "" || off != "http://other.test/" {
			t.Errorf("Location with defaults = %q, with the default off = %q", blocked, off)
		}
	})

	t.Run("WithDefaults with an unknown version fails NewApp", func(t *testing.T) {
		if _, err := NewApp(newAppTestConfig(t), WithDefaults("9.9")); err == nil {
			t.Error("NewApp returned nil, want an error")
		}
	})
}
