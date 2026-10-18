// Package assets resolves logical asset names to their content-hashed URLs
// (manifest.json written by tools/assetbuild) and serves them.
package assets

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Prefix is where assets are served.
const Prefix = "/static/"

// ManifestName is written by tools/assetbuild next to the hashed files.
const ManifestName = "manifest.json"

// Assets maps logical names ("css/app.css") to hashed files ("css/app.3f9a1c0b2d.css").
type Assets struct {
	fsys   fs.FS
	files  map[string]string
	hashed map[string]bool
}

// Load reads the manifest. A missing manifest (assets not built yet) yields
// unhashed URLs, so the server still starts; run `task assets`.
func Load(fsys fs.FS) (*Assets, error) {
	a := &Assets{fsys: fsys, files: map[string]string{}, hashed: map[string]bool{}}
	raw, err := fs.ReadFile(fsys, ManifestName)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &a.files); err != nil {
		return nil, err
	}
	for _, h := range a.files {
		a.hashed[h] = true
	}
	return a, nil
}

// Built reports whether a manifest was found.
func (a *Assets) Built() bool { return len(a.files) > 0 }

// URL returns the public URL of a logical asset name.
func (a *Assets) URL(name string) string {
	if h, ok := a.files[name]; ok {
		return Prefix + h
	}
	return Prefix + name
}

var contentTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
	".avif":  "image/avif",
	".webp":  "image/webp",
	".jpg":   "image/jpeg",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".txt":   "text/plain; charset=utf-8",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
}

// Handler serves /static/<name>: hashed files are immutable for a year,
// precompressed .gz variants are used when the client accepts gzip.
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		ct, ok := contentTypes[path.Ext(name)]
		if !ok || name == ManifestName || strings.HasSuffix(name, ".gz") || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		if a.hashed[name] {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "public, max-age=3600")
		}
		h.Set("Content-Type", ct)
		h.Add("Vary", "Accept-Encoding")

		file := name
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			if _, err := fs.Stat(a.fsys, name+".gz"); err == nil {
				file = name + ".gz"
				h.Set("Content-Encoding", "gzip")
			}
		}
		data, err := fs.ReadFile(a.fsys, file)
		if err != nil {
			h.Del("Content-Encoding")
			h.Del("Cache-Control")
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data) // #nosec G705 -- embedded build output, explicit Content-Type, nosniff
	})
}
