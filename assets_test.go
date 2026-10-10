package cartridge

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func newAssetServer(t *testing.T, static fstest.MapFS) *Server {
	t.Helper()
	app := newTestApp(t)
	app.cfg.EnableStaticAssets = true
	app.cfg.StaticFS = static
	return app
}

func newDiskAssetServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	app := newTestApp(t)
	app.cfg.EnableStaticAssets = true
	app.cfg.StaticDirectory = dir
	return app, dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

var digestedApp = regexp.MustCompile(`^/assets/app-[0-9a-f]{8}\.js$`)

func TestAsset(t *testing.T) {
	t.Run("an embedded file gets a digested URL, served with a one-year immutable cache", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{"app.js": {Data: []byte("console.log(1)")}})

		url, err := app.Asset("app.js")
		resp, _ := app.Test(httptest.NewRequest("GET", url, nil))

		if err != nil || !digestedApp.MatchString(url) {
			t.Fatalf("Asset = %q, %v", url, err)
		}
		if got := body(t, resp); resp.StatusCode != http.StatusOK || got != "console.log(1)" {
			t.Fatalf("GET %s = %d %q", url, resp.StatusCode, got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Errorf("Cache-Control = %q", got)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
			t.Errorf("Content-Type = %q", got)
		}
	})

	t.Run("a nested file keeps its folder", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{"controllers/hello.js": {Data: []byte("x")}})

		url, _ := app.Asset("controllers/hello.js")

		if !regexp.MustCompile(`^/assets/controllers/hello-[0-9a-f]{8}\.js$`).MatchString(url) {
			t.Errorf("Asset = %q", url)
		}
	})

	t.Run("other content gets another digest", func(t *testing.T) {
		one, _ := newAssetServer(t, fstest.MapFS{"app.js": {Data: []byte("1")}}).Asset("app.js")
		two, _ := newAssetServer(t, fstest.MapFS{"app.js": {Data: []byte("2")}}).Asset("app.js")

		if one == two {
			t.Errorf("both contents got %q", one)
		}
	})

	t.Run("an embedded file with an old digest is not found", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{"app.js": {Data: []byte("new")}})

		resp, _ := app.Test(httptest.NewRequest("GET", "/assets/app-00000000.js", nil))

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("a missing file or a dotfile is an error", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{".env": {Data: []byte("SECRET=1")}})

		for _, name := range []string{"missing.js", ".env"} {
			if url, err := app.Asset(name); err == nil {
				t.Errorf("Asset(%q) = %q, want an error", name, url)
			}
		}
	})

	t.Run("a plain embedded file with a hashed name keeps its one-year cache", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{"app.js": {Data: []byte("x")}})
		app.cfg.StaticNamesHashed = true

		resp, _ := app.Test(httptest.NewRequest("GET", "/assets/app.js", nil))

		if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000" {
			t.Errorf("Cache-Control = %q", got)
		}
	})

	t.Run("a plain embedded file without a hashed name is checked on each use", func(t *testing.T) {
		app := newAssetServer(t, fstest.MapFS{"font.woff2": {Data: []byte("x")}})
		first, _ := app.Test(httptest.NewRequest("GET", "/assets/font.woff2", nil))
		again := httptest.NewRequest("GET", "/assets/font.woff2", nil)
		again.Header.Set("If-None-Match", first.Header.Get("ETag"))

		resp, _ := app.Test(again)

		if got := first.Header.Get("Cache-Control"); got != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", got)
		}
		if first.Header.Get("ETag") == "" || resp.StatusCode != http.StatusNotModified {
			t.Errorf("ETag = %q, second status = %d, want an ETag and 304", first.Header.Get("ETag"), resp.StatusCode)
		}
	})

	t.Run("a changed embedded file gets another ETag", func(t *testing.T) {
		etag := func(content string) string {
			app := newAssetServer(t, fstest.MapFS{"font.woff2": {Data: []byte(content)}})
			resp, _ := app.Test(httptest.NewRequest("GET", "/assets/font.woff2", nil))
			return resp.Header.Get("ETag")
		}

		if etag("one") == etag("two") {
			t.Error("two contents share an ETag")
		}
	})

	t.Run("a file on disk gets a new URL when it changes", func(t *testing.T) {
		app, dir := newDiskAssetServer(t)
		writeFile(t, filepath.Join(dir, "app.js"), "old")
		before, _ := app.Asset("app.js")

		writeFile(t, filepath.Join(dir, "app.js"), "new")
		after, err := app.Asset("app.js")

		if err != nil || before == after || !digestedApp.MatchString(after) {
			t.Errorf("before %q, after %q, %v", before, after, err)
		}
	})

	t.Run("a digested file on disk is the current file, and the browser must check it", func(t *testing.T) {
		app, dir := newDiskAssetServer(t)
		writeFile(t, filepath.Join(dir, "app.js"), "old")
		old, _ := app.Asset("app.js")
		writeFile(t, filepath.Join(dir, "app.js"), "new")

		resp, _ := app.Test(httptest.NewRequest("GET", old, nil))

		if got := body(t, resp); got != "new" {
			t.Errorf("body = %q, want the current file", got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", got)
		}
	})

	t.Run("a plain file on disk is sent with no-cache", func(t *testing.T) {
		app, dir := newDiskAssetServer(t)
		writeFile(t, filepath.Join(dir, "app.css"), "body{}")

		resp, _ := app.Test(httptest.NewRequest("GET", "/assets/app.css", nil))

		if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", got)
		}
	})

	t.Run("without static files, it is an error", func(t *testing.T) {
		app := newTestApp(t)

		if _, err := app.Asset("app.js"); err == nil {
			t.Error("Asset returned no error")
		}
	})
}

func TestImportmap(t *testing.T) {
	static := fstest.MapFS{
		"application.js":                  {Data: []byte("import 'controllers'")},
		"turbo.min.js":                    {Data: []byte("turbo")},
		"controllers/index.js":            {Data: []byte("index")},
		"controllers/hello_controller.js": {Data: []byte("hello")},
		"controllers/README.md":           {Data: []byte("docs")},
	}

	t.Run("maps each module of a folder to its digested URL, and preloads it", func(t *testing.T) {
		app := newAssetServer(t, static)

		html, err := app.Importmap("controllers/*.js")

		if err != nil {
			t.Fatal(err)
		}
		hello, _ := app.Asset("controllers/hello_controller.js")
		index, _ := app.Asset("controllers/index.js")
		for _, want := range []string{
			`<script type="importmap" data-turbo-track="reload">`,
			`"controllers/hello_controller": "` + hello + `"`,
			`"controllers": "` + index + `"`,
			`<link rel="modulepreload" href="` + hello + `">`,
		} {
			if !strings.Contains(string(html), want) {
				t.Errorf("import map lacks %s:\n%s", want, html)
			}
		}
		if strings.Contains(string(html), "README") {
			t.Errorf("import map holds a file the glob does not match:\n%s", html)
		}
	})

	t.Run("pins a module under another name", func(t *testing.T) {
		app := newAssetServer(t, static)

		html, _ := app.Importmap("application.js", "@hotwired/turbo=turbo.min.js")

		turbo, _ := app.Asset("turbo.min.js")
		if !strings.Contains(string(html), `"application": "/assets/application-`) ||
			!strings.Contains(string(html), `"@hotwired/turbo": "`+turbo+`"`) {
			t.Errorf("import map:\n%s", html)
		}
	})

	t.Run("a later entry for the same module name wins", func(t *testing.T) {
		app := newAssetServer(t, static)

		html, _ := app.Importmap("application.js", "application=turbo.min.js")

		turbo, _ := app.Asset("turbo.min.js")
		if strings.Count(string(html), `"application"`) != 1 || !strings.Contains(string(html), `"application": "`+turbo+`"`) {
			t.Errorf("import map:\n%s", html)
		}
	})

	t.Run("a glob that matches no file is an error", func(t *testing.T) {
		app := newAssetServer(t, static)

		if _, err := app.Importmap("missing/*.js"); err == nil {
			t.Error("Importmap returned no error")
		}
	})
}
