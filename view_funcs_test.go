package cartridge

import (
	"html/template"
	"testing"
	"testing/fstest"
	"time"
)

// renderInline renders one template source with the default functions.
func renderInline(t *testing.T, source string, data any) string {
	t.Helper()
	views := NewHTMLViews(fstest.MapFS{
		"page.html":     {Data: []byte(source)},
		"partials.html": {Data: []byte(`{{define "chip"}}<b>{{.Label}} {{.Count}}</b>{{end}}`)},
	}, nil, false)
	return render(t, views, "page", data)
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-time.Minute - time.Second), "1 minute ago"},
		{now.Add(-5*time.Minute - time.Second), "5 minutes ago"},
		{now.Add(-3*time.Hour - time.Second), "3 hours ago"},
		{now.Add(-49 * time.Hour), "2 days ago"},
		{now.Add(-61 * 24 * time.Hour), "2 months ago"},
		{now.Add(-400 * 24 * time.Hour), "1 year ago"},
		{now.Add(3*24*time.Hour + time.Minute), "in 3 days"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		t.Run("says "+c.want, func(t *testing.T) {
			got, err := timeAgo(c.at)

			if err != nil || got != c.want {
				t.Errorf("timeAgo = %q, %v; want %q", got, err, c.want)
			}
		})
	}

	t.Run("a nil time is empty, so a missing date prints nothing", func(t *testing.T) {
		var deletedAt *time.Time

		got := renderInline(t, `[{{timeAgo .}}]`, deletedAt)

		if got != "[]" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a value that is not a time is an error", func(t *testing.T) {
		if _, err := timeAgo("yesterday"); err == nil {
			t.Error("timeAgo returned no error")
		}
	})
}

func TestPluralize(t *testing.T) {
	t.Run("one is singular, any other count is plural", func(t *testing.T) {
		got := renderInline(t, `{{pluralize 0 "reply"}}, {{pluralize 1 "reply"}}, {{pluralize .N "reply"}}`, map[string]int64{"N": 2})

		if got != "0 replies, 1 reply, 2 replies" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("makes the plural of the last word, also an irregular one", func(t *testing.T) {
		got := renderInline(t, `{{pluralize 3 "spam rule"}}, {{pluralize 2 "person"}}`, nil)

		if got != "3 spam rules, 2 people" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("takes the plural it is given", func(t *testing.T) {
		got := renderInline(t, `{{pluralize 2 "octopus" "octopodes"}}`, nil)

		if got != "2 octopodes" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a count that is not an integer is an error", func(t *testing.T) {
		if _, err := pluralize("2", "reply"); err == nil {
			t.Error("pluralize returned no error")
		}
	})
}

func TestTruncate(t *testing.T) {
	t.Run("keeps a short text as it is", func(t *testing.T) {
		got := renderInline(t, `{{truncate 5 "Hello"}}`, nil)

		if got != "Hello" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("cuts a long text to the length, with an ellipsis", func(t *testing.T) {
		got := renderInline(t, `{{truncate 5 "Hello world"}}`, nil)

		if got != "Hell…" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("counts characters, not bytes", func(t *testing.T) {
		got := renderInline(t, `{{"Ünïcödé text" | truncate 5}}`, nil)

		if got != "Ünïc…" {
			t.Errorf("got %q", got)
		}
	})
}

func TestSquish(t *testing.T) {
	got := renderInline(t, `[{{squish .}}]`, "  Hi,\n\n\tthere  you go ")

	if got != "[Hi, there you go]" {
		t.Errorf("got %q", got)
	}
}

func TestDict(t *testing.T) {
	t.Run("gives a template more than one value", func(t *testing.T) {
		got := renderInline(t, `{{template "chip" (dict "Label" "Open" "Count" .)}}`, 3)

		if got != "<b>Open 3</b>" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("an odd number of arguments or a key that is not a string is an error", func(t *testing.T) {
		if _, err := dict("Label"); err == nil {
			t.Error("dict with one argument returned no error")
		}
		if _, err := dict(1, "x"); err == nil {
			t.Error("dict with an int key returned no error")
		}
	})
}

func TestAppTemplateFuncsWin(t *testing.T) {
	templates := fstest.MapFS{"home.html": {Data: []byte(`{{truncate 3 "Hello"}}`)}}
	app, err := NewApp(newAppTestConfig(t),
		WithAssets(templates, nil),
		WithTemplateFuncs(template.FuncMap{"truncate": func(n int, s string) string { return "mine" }}),
		WithRoutes(func(s *Server) {
			s.Get("/", func(c *Context) error { return c.Render("home", nil) })
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	got := get(t, app, "/")

	if got != "mine" {
		t.Errorf("got %q, want the app's truncate", got)
	}
}
