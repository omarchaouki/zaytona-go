package httpx

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
	return w
}}

// GzipHTML compresses text/html responses for clients that accept gzip.
// Static assets are precompressed at build time and skip this.
func GzipHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.TrimSpace(q) != "q=0" {
			return true
		}
	}
	return false
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	if h.Get("Content-Encoding") != "" || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		return
	}
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	gz, _ := gzipPool.Get().(*gzip.Writer)
	gz.Reset(g.ResponseWriter)
	g.gz = gz
}

func (g *gzipWriter) WriteHeader(code int) {
	if code != http.StatusNoContent && code != http.StatusNotModified && code >= 200 {
		g.decide()
	} else {
		g.decided = true
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide()
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
	}
}
