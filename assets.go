package cartridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
)

// A digested name puts a hash of the file's content before its extension:
// "app.js" becomes "app-1a2b3c4d.js". The name changes when the content
// changes, so a browser can keep a digested file forever.

// immutableCacheControl is the Cache-Control of a digested embedded file.
const immutableCacheControl = "public, max-age=31536000, immutable"

// digestLength is the number of hex characters in a digest.
const digestLength = 8

// assetDigests maps static file names to digested names.
type assetDigests struct {
	fsys     fs.FS
	prefix   string
	embedded bool // the files never change: hash them once
	byName   map[string]string
	byDigest map[string]string
}

// newAssetDigests hashes every embedded file once. Files on disk can change,
// so they are hashed on each call instead.
func newAssetDigests(fsys fs.FS, prefix string, embedded bool) (*assetDigests, error) {
	a := &assetDigests{fsys: fsys, prefix: prefix, embedded: embedded}
	if !embedded {
		return a, nil
	}
	a.byName = map[string]string{}
	a.byDigest = map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && hasDotSegment(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		digested, err := digestName(fsys, p)
		if err != nil {
			return err
		}
		a.byName[p] = digested
		a.byDigest[digested] = p
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cartridge: digest static files: %w", err)
	}
	return a, nil
}

// url returns the digested URL of the named file.
func (a *assetDigests) url(name string) (string, error) {
	name = strings.TrimPrefix(name, "/")
	var digested string
	if a.embedded {
		digested = a.byName[name]
	} else if fs.ValidPath(name) && !hasDotSegment(name) {
		digested, _ = digestName(a.fsys, name)
	}
	if digested == "" {
		return "", fmt.Errorf("cartridge: asset %q not found", name)
	}
	return (&url.URL{Path: a.prefix + "/" + digested}).EscapedPath(), nil
}

// original returns the file that a digested name points at. On disk, it
// returns the current file even when the digest is old.
func (a *assetDigests) original(digested string) (string, bool) {
	if a.embedded {
		name, ok := a.byDigest[digested]
		return name, ok
	}
	name, ok := undigest(digested)
	if !ok {
		return "", false
	}
	info, err := fs.Stat(a.fsys, name)
	return name, err == nil && !info.IsDir()
}

// digestName returns the name with the digest of the file's content.
func digestName(fsys fs.FS, name string) (string, error) {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	ext := path.Ext(name)
	return strings.TrimSuffix(name, ext) + "-" + hex.EncodeToString(sum[:])[:digestLength] + ext, nil
}

// undigest removes the digest from a name: "app-1a2b3c4d.js" becomes "app.js".
func undigest(name string) (string, bool) {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	cut := len(base) - digestLength - 1
	if cut < 1 || base[cut] != '-' {
		return "", false
	}
	if _, err := hex.DecodeString(base[cut+1:]); err != nil {
		return "", false
	}
	return base[:cut] + ext, true
}

// staticFiles returns the static files, the URL prefix they are served
// under, and whether they are embedded. The files are nil when the server
// serves no static files.
func (s *Server) staticFiles() (fs.FS, string, bool) {
	if !s.cfg.EnableStaticAssets {
		return nil, "", false
	}
	prefix := strings.TrimSuffix(s.cfg.StaticPrefix, "/")
	if prefix == "" {
		prefix = "/assets"
	}
	if s.cfg.StaticFS != nil {
		return s.cfg.StaticFS, prefix, true
	}
	dir := s.cfg.StaticDirectory
	if dir == "" {
		dir = s.cfg.Config.GetPublicDirectory()
	}
	if dir == "" {
		return nil, "", false
	}
	return os.DirFS(dir), prefix, false
}

// assetDigests hashes the embedded static files on its first call.
func (s *Server) assetDigests() (*assetDigests, error) {
	s.digestsOnce.Do(func() {
		fsys, prefix, embedded := s.staticFiles()
		if fsys == nil {
			s.digestsErr = fmt.Errorf("cartridge: the server serves no static files")
			return
		}
		s.digests, s.digestsErr = newAssetDigests(fsys, prefix, embedded)
	})
	return s.digests, s.digestsErr
}

// Asset returns the URL of a static file with a digest of its content in
// the name, for example "/assets/app-1a2b3c4d.js" for "app.js". The server
// serves a digested URL with a one-year immutable cache, and a new deploy
// with a changed file gives a new URL. Embedded files are hashed once, at
// startup. Files on disk (development) are hashed on each call, and their
// digested URLs are sent with "Cache-Control: no-cache". Templates of an
// app from NewApp call it as {{asset "app.js"}}.
//
// Asset does not change the url() and @import paths inside a CSS file.
// Those files keep their plain URL.
func (s *Server) Asset(name string) (string, error) {
	digests, err := s.assetDigests()
	if err != nil {
		return "", err
	}
	return digests.url(name)
}

// Importmap returns a <script type="importmap"> that maps JavaScript module
// names to digested URLs, and a <link rel="modulepreload"> for each module.
// Each entry is one of:
//
//	"controllers/*.js"     a glob: each file is a module named by its path
//	                       without the extension; an index file is named by
//	                       its folder ("controllers/index.js" is "controllers")
//	"@hotwired/turbo=turbo.min.js"  a module name for one file
//
// A later entry for the same module name wins. A glob that matches no file
// is an error. Templates of an app from NewApp call it as
// {{importmap "application.js" "controllers/*.js"}}. Load the first module
// with <script type="module">import "application"</script>. The script tag
// has data-turbo-track="reload", so Turbo reloads the page after a deploy.
func (s *Server) Importmap(entries ...string) (template.HTML, error) {
	digests, err := s.assetDigests()
	if err != nil {
		return "", err
	}

	var names, urls []string
	index := map[string]int{}
	pin := func(name, file string) error {
		u, err := digests.url(file)
		if err != nil {
			return err
		}
		if i, ok := index[name]; ok {
			urls[i] = u
			return nil
		}
		index[name] = len(names)
		names = append(names, name)
		urls = append(urls, u)
		return nil
	}

	for _, entry := range entries {
		if name, file, ok := strings.Cut(entry, "="); ok {
			if err := pin(name, file); err != nil {
				return "", err
			}
			continue
		}
		matches, err := fs.Glob(digests.fsys, entry)
		if err != nil {
			return "", fmt.Errorf("cartridge: importmap %q: %w", entry, err)
		}
		pinned := 0
		for _, file := range matches {
			if info, err := fs.Stat(digests.fsys, file); err != nil || info.IsDir() || hasDotSegment(file) {
				continue
			}
			if err := pin(moduleName(file), file); err != nil {
				return "", err
			}
			pinned++
		}
		if pinned == 0 {
			return "", fmt.Errorf("cartridge: importmap: no file matches %q", entry)
		}
	}

	var b strings.Builder
	b.WriteString(`<script type="importmap" data-turbo-track="reload">{"imports": {`)
	for i, name := range names {
		if i > 0 {
			b.WriteString(",")
		}
		key, _ := json.Marshal(name) // escapes <, >, and &, so the script cannot end early
		value, _ := json.Marshal(urls[i])
		b.WriteString("\n  ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(value)
	}
	b.WriteString("\n}}</script>")
	for _, u := range urls {
		b.WriteString("\n<link rel=\"modulepreload\" href=\"" + template.HTMLEscapeString(u) + "\">")
	}
	return template.HTML(b.String()), nil
}

// moduleName is the file's path without its extension. An index file is
// named by its folder.
func moduleName(file string) string {
	name := strings.TrimSuffix(file, path.Ext(file))
	if path.Base(name) == "index" && path.Dir(name) != "." {
		return path.Dir(name)
	}
	return name
}
