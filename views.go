package cartridge

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"maps"
	"path"
	"strings"
	"sync"
)

// HTMLViews renders html/template files. A file's template name is its path
// without ".html", for example "admin/index". Inside a template:
//
//	{{render "partials/nav" .}}  renders another template
//	{{embed}}                    in a layout, renders the page
//
// They also have timeAgo, pluralize, truncate, squish, and dict. See
// defaultViewFuncs.
//
// Renders run in parallel. Each file is parsed once, or on every render
// with reload.
type HTMLViews struct {
	fsys   fs.FS
	funcs  template.FuncMap
	reload bool // parse the files on every render (development)

	mu  sync.Mutex // guards set
	set *viewSet
}

// viewSet holds one parse of the files, twice. A page renders from pages,
// where {{embed}} is an error. A layout renders from layouts, where
// {{embed}} writes embedMarker, and Render puts the page in its place. So
// no render changes a template that another render uses.
type viewSet struct {
	pages   *template.Template
	layouts *template.Template
}

// embedMarker stands for the page in the output of a layout. It is random,
// so a page or its data cannot contain it.
var embedMarker = []byte("<!--cartridge-embed-" + rand.Text() + "-->")

// NewHTMLViews creates a view engine for the .html files in fsys.
func NewHTMLViews(fsys fs.FS, funcs template.FuncMap, reload bool) *HTMLViews {
	return &HTMLViews{fsys: fsys, funcs: funcs, reload: reload}
}

// Render executes the named template into w. With a layout, the layout
// renders and {{embed}} inserts the page.
func (v *HTMLViews) Render(w io.Writer, name string, data any, layouts ...string) error {
	set, err := v.templates()
	if err != nil {
		return err
	}
	page := set.pages.Lookup(name)
	if page == nil {
		return fmt.Errorf("cartridge: template %q not found", name)
	}
	if len(layouts) == 0 || layouts[0] == "" {
		return page.Execute(w, data)
	}

	layout := set.layouts.Lookup(layouts[0])
	if layout == nil {
		return fmt.Errorf("cartridge: layout %q not found", layouts[0])
	}
	var body bytes.Buffer
	if err := page.Execute(&body, data); err != nil {
		return err
	}
	var out bytes.Buffer
	if err := layout.Execute(&out, data); err != nil {
		return err
	}
	return writeEmbedded(w, out.Bytes(), body.Bytes())
}

// writeEmbedded writes the layout output with the page in place of each
// embedMarker.
func writeEmbedded(w io.Writer, layout, page []byte) error {
	for {
		i := bytes.Index(layout, embedMarker)
		if i < 0 {
			break
		}
		if _, err := w.Write(layout[:i]); err != nil {
			return err
		}
		if _, err := w.Write(page); err != nil {
			return err
		}
		layout = layout[i+len(embedMarker):]
	}
	_, err := w.Write(layout)
	return err
}

// templates returns the parsed templates, parsing them when needed.
func (v *HTMLViews) templates() (*viewSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.set != nil && !v.reload {
		return v.set, nil
	}
	set, err := v.parse()
	if err != nil {
		return nil, err
	}
	v.set = set
	return set, nil
}

func (v *HTMLViews) parse() (*viewSet, error) {
	pages := template.New("")
	pages.Funcs(v.setFuncs(pages, func() (template.HTML, error) {
		return "", errors.New("embed used outside a layout")
	}))

	err := fs.WalkDir(v.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		content, err := fs.ReadFile(v.fsys, p)
		if err != nil {
			return err
		}
		_, err = pages.New(strings.TrimSuffix(p, ".html")).Parse(string(content))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("cartridge: parse templates: %w", err)
	}

	// html/template can clone a set only before it executes, so clone now.
	layouts, err := pages.Clone()
	if err != nil {
		return nil, fmt.Errorf("cartridge: parse templates: %w", err)
	}
	layouts.Funcs(v.setFuncs(layouts, func() (template.HTML, error) {
		return template.HTML(embedMarker), nil
	}))
	return &viewSet{pages: pages, layouts: layouts}, nil
}

// setFuncs returns the functions of one template set: the default
// functions, embed, and render. The app's functions win over them.
func (v *HTMLViews) setFuncs(set *template.Template, embed func() (template.HTML, error)) template.FuncMap {
	funcs := defaultViewFuncs()
	funcs["embed"] = embed
	funcs["render"] = func(name string, data any) (template.HTML, error) {
		t := set.Lookup(name)
		if t == nil {
			return "", fmt.Errorf("template %q not found", name)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	}
	maps.Copy(funcs, v.funcs)
	return funcs
}
