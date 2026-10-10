// Package web holds the templates and static files of the notes app.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates are named by their path under templates/, without ".html".
func Templates() fs.FS { return sub(templatesFS, "templates") }

// Static files are served under /assets.
func Static() fs.FS { return sub(staticFS, "static") }

func sub(fsys embed.FS, dir string) fs.FS {
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return s
}
