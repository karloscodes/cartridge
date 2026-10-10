package cartridge

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func render(t *testing.T, views *HTMLViews, name string, data any, layouts ...string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := views.Render(&buf, name, data, layouts...); err != nil {
		t.Fatalf("Render %s: %v", name, err)
	}
	return buf.String()
}

func TestHTMLViewsLayouts(t *testing.T) {
	t.Run("layout renders run at the same time", func(t *testing.T) {
		const renders = 4
		var arrived sync.WaitGroup
		arrived.Add(renders)
		allInside := make(chan struct{})
		go func() { arrived.Wait(); close(allInside) }()
		// Each layout render waits here until every render is inside a layout.
		together := func() (string, error) {
			arrived.Done()
			select {
			case <-allInside:
				return "", nil
			case <-time.After(5 * time.Second):
				return "", errors.New("layout renders ran one at a time")
			}
		}
		views := NewHTMLViews(fstest.MapFS{
			"layouts/main.html": {Data: []byte(`<main>{{together}}{{embed}}</main>`)},
			"notes/show.html":   {Data: []byte(`<p>{{.}}</p>`)},
		}, template.FuncMap{"together": together}, false)
		errs := make([]error, renders)
		var done sync.WaitGroup

		for i := range renders {
			done.Go(func() { errs[i] = views.Render(&bytes.Buffer{}, "notes/show", i, "layouts/main") })
		}
		done.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("render %d: %v", i, err)
			}
		}
	})

	t.Run("each parallel render gets its own page and data", func(t *testing.T) {
		views := NewHTMLViews(fstest.MapFS{
			"layouts/main.html":  {Data: []byte(`<main>{{render "partials/nav" .}}{{embed}}</main>`)},
			"layouts/admin.html": {Data: []byte(`<admin>{{embed}}</admin>`)},
			"partials/nav.html":  {Data: []byte(`<nav>{{.}}</nav>`)},
			"notes/show.html":    {Data: []byte(`<p>{{.}}</p>`)},
			"notes/index.html":   {Data: []byte(`<ul>{{.}}</ul>`)},
		}, nil, false)
		pages := []string{"notes/show", "notes/index"}
		layouts := []string{"layouts/main", "layouts/admin"}
		wants := map[string]string{
			"notes/show layouts/main":   "<main><nav>%d</nav><p>%d</p></main>",
			"notes/show layouts/admin":  "<admin><p>%d</p></admin>",
			"notes/index layouts/main":  "<main><nav>%d</nav><ul>%d</ul></main>",
			"notes/index layouts/admin": "<admin><ul>%d</ul></admin>",
		}
		const renders = 200
		got := make([]string, renders)
		errs := make([]error, renders)
		var done sync.WaitGroup

		for i := range renders {
			done.Go(func() {
				var buf bytes.Buffer
				errs[i] = views.Render(&buf, pages[i%2], i, layouts[(i/2)%2])
				got[i] = buf.String()
			})
		}
		done.Wait()

		for i := range renders {
			want := wants[pages[i%2]+" "+layouts[(i/2)%2]]
			if layouts[(i/2)%2] == "layouts/main" {
				want = fmt.Sprintf(want, i, i)
			} else {
				want = fmt.Sprintf(want, i)
			}
			if errs[i] != nil || got[i] != want {
				t.Errorf("render %d = %q, %v; want %q", i, got[i], errs[i], want)
			}
		}
	})

	t.Run("a layout can embed the page twice", func(t *testing.T) {
		views := NewHTMLViews(fstest.MapFS{
			"layouts/twice.html": {Data: []byte(`{{embed}}|{{embed}}`)},
			"notes/show.html":    {Data: []byte(`<p>{{.}}</p>`)},
		}, nil, false)

		got := render(t, views, "notes/show", "a", "layouts/twice")

		if got != "<p>a</p>|<p>a</p>" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a page that calls embed is an error, also after a layout render", func(t *testing.T) {
		views := NewHTMLViews(fstest.MapFS{
			"layouts/main.html": {Data: []byte(`<main>{{embed}}</main>`)},
			"notes/show.html":   {Data: []byte(`<p>{{.}}</p>`)},
			"notes/bad.html":    {Data: []byte(`{{embed}}`)},
		}, nil, false)
		render(t, views, "notes/show", "secret", "layouts/main")

		err := views.Render(&bytes.Buffer{}, "notes/bad", nil)

		if err == nil {
			t.Error("Render returned no error")
		}
	})

	t.Run("with reload, a changed layout shows on the next render", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "layouts", "main.html"), `<main>{{embed}}</main>`)
		writeFile(t, filepath.Join(dir, "notes", "show.html"), `<p>{{.}}</p>`)
		views := NewHTMLViews(os.DirFS(dir), nil, true)
		render(t, views, "notes/show", "a", "layouts/main")

		writeFile(t, filepath.Join(dir, "layouts", "main.html"), `<section>{{embed}}</section>`)
		got := render(t, views, "notes/show", "a", "layouts/main")

		if got != "<section><p>a</p></section>" {
			t.Errorf("got %q", got)
		}
	})
}
