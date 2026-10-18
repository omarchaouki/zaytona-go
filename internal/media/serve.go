package media

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Prefix is the URL path of uploaded images.
const Prefix = "/uploads/"

var contentTypes = map[string]string{"avif": "image/avif", "webp": "image/webp", "jpg": "image/jpeg"}

// Handler serves /uploads/<id>-<width>.<ext>. public reports whether an image
// may be shown to anyone (certificate scans only while their certificate is
// public); admin reports a signed-in admin, who sees every image.
func (p *Processor) Handler(public func(ctx context.Context, id string) (bool, error), admin func(r *http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		id, ext, ok := ParseName(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		isPublic, err := public(r.Context(), id)
		if err != nil || (!isPublic && !admin(r)) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(p.Dir, name)) //nolint:gosec // G304: name matched fileRe (no separators, no dots)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", contentTypes[ext])
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'")
		if isPublic {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "private, no-store")
		}
		http.ServeContent(w, r, "", st.ModTime(), f)
	})
}
