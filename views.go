package cartridge

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// HTMLViews renders html/template files. A file's template name is its path
// without ".html", for example "admin/index". Inside a template:
//
//	{{render "partials/nav" .}}  renders another template
//	{{embed}}                    in a layout, renders the page
type HTMLViews struct {
	fsys   fs.FS
	funcs  template.FuncMap
	reload bool // parse the files on every render (development)

	mu       sync.Mutex // guards tmpl
	layoutMu sync.Mutex // one layout render at a time, because embed is swapped per render
	tmpl     *template.Template
}

// NewHTMLViews creates a view engine for the .html files in fsys.
func NewHTMLViews(fsys fs.FS, funcs template.FuncMap, reload bool) *HTMLViews {
	return &HTMLViews{fsys: fsys, funcs: funcs, reload: reload}
}

// Render executes the named template into w. With a layout, the layout
// renders and {{embed}} inserts the page.
func (v *HTMLViews) Render(w io.Writer, name string, data any, layouts ...string) error {
	t, err := v.templates()
	if err != nil {
		return err
	}
	page := t.Lookup(name)
	if page == nil {
		return fmt.Errorf("cartridge: template %q not found", name)
	}
	if len(layouts) == 0 || layouts[0] == "" {
		return page.Execute(w, data)
	}

	layout := t.Lookup(layouts[0])
	if layout == nil {
		return fmt.Errorf("cartridge: layout %q not found", layouts[0])
	}
	var body bytes.Buffer
	if err := page.Execute(&body, data); err != nil {
		return err
	}

	v.layoutMu.Lock()
	defer v.layoutMu.Unlock()
	layout.Funcs(template.FuncMap{"embed": func() template.HTML { return template.HTML(body.String()) }})
	return layout.Execute(w, data)
}

// templates returns the parsed templates, parsing them when needed.
func (v *HTMLViews) templates() (*template.Template, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tmpl != nil && !v.reload {
		return v.tmpl, nil
	}
	t, err := v.parse()
	if err != nil {
		return nil, err
	}
	v.tmpl = t
	return t, nil
}

func (v *HTMLViews) parse() (*template.Template, error) {
	root := template.New("")
	funcs := template.FuncMap{
		"embed": func() (template.HTML, error) {
			return "", errors.New("embed used outside a layout")
		},
		"render": func(name string, data any) (template.HTML, error) {
			t := root.Lookup(name)
			if t == nil {
				return "", fmt.Errorf("template %q not found", name)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return "", err
			}
			return template.HTML(buf.String()), nil
		},
	}
	for name, fn := range v.funcs {
		funcs[name] = fn
	}
	root.Funcs(funcs)

	err := fs.WalkDir(v.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		content, err := fs.ReadFile(v.fsys, p)
		if err != nil {
			return err
		}
		_, err = root.New(strings.TrimSuffix(p, ".html")).Parse(string(content))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("cartridge: parse templates: %w", err)
	}
	return root, nil
}
