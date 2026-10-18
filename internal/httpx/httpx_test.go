package httpx

import (
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const base = "default-src 'none'; script-src 'self' 'nonce-N' 'strict-dynamic'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; manifest-src 'self'; worker-src 'self'"

func TestBuildCSPPerFeature(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    CSPFeatures
		want string
	}{
		{"none, http", CSPFeatures{}, base},
		{"none, https", CSPFeatures{HTTPS: true}, base + "; upgrade-insecure-requests"},
		{
			"pixel",
			CSPFeatures{MetaPixel: true},
			"default-src 'none'; script-src 'self' 'nonce-N' 'strict-dynamic' https://connect.facebook.net; style-src 'self'; img-src 'self' data: https://www.facebook.com; font-src 'self'; connect-src 'self' https://www.facebook.com https://connect.facebook.net; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; manifest-src 'self'; worker-src 'self'",
		},
		{
			"umami",
			CSPFeatures{UmamiHost: "https://cloud.umami.is"},
			"default-src 'none'; script-src 'self' 'nonce-N' 'strict-dynamic' https://cloud.umami.is; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self' https://cloud.umami.is; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; manifest-src 'self'; worker-src 'self'",
		},
		{
			"turnstile + map",
			CSPFeatures{Turnstile: true, MapEmbed: true},
			"default-src 'none'; script-src 'self' 'nonce-N' 'strict-dynamic' https://challenges.cloudflare.com; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src https://challenges.cloudflare.com https://www.google.com; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'; manifest-src 'self'; worker-src 'self'",
		},
	} {
		if got := BuildCSP("N", tc.f); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}

func TestSecurityHeadersAndNonce(t *testing.T) {
	var seen string
	h := SecurityHeaders(HeadersConfig{})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = Nonce(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ar", nil))
	if len(seen) < 22 {
		t.Fatalf("nonce too short: %q", seen)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'nonce-"+seen+"'") {
		t.Fatalf("CSP does not carry the request nonce: %s", csp)
	}
	for k, v := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q", k, got)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS over plain http")
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ar", nil))
	if rec2.Header().Get("Content-Security-Policy") == csp {
		t.Fatal("nonce reused across requests")
	}
}

func TestAdminHeaders(t *testing.T) {
	h := SecurityHeaders(HeadersConfig{TrustProxy: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("admin headers: %v", rec.Header())
	}
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("no HSTS behind a trusted https proxy")
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := clientIP(req, false); got != "10.0.0.1" {
		t.Errorf("untrusted proxy: %s", got)
	}
	if got := clientIP(req, true); got != "5.6.7.8" {
		t.Errorf("trusted proxy should take the right-most hop: %s", got)
	}
	req.Header.Set("X-Forwarded-For", "garbage")
	if got := clientIP(req, true); got != "10.0.0.1" {
		t.Errorf("bad header should fall back to RemoteAddr: %s", got)
	}
}

func TestCrossOriginRejectsCrossSitePost(t *testing.T) {
	h := CrossOrigin()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/ar/order", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST got %d", rec.Code)
	}
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/ar/order", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("same-origin POST got %d", rec.Code)
	}
}

func TestLimitBody(t *testing.T) {
	h := LimitBody(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("0123456789")))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestRecoverHidesPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret detail") }),
		RequestIDMiddleware, Recover(logger))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), rec.Header().Get("X-Request-Id")) {
		t.Fatal("500 page does not show the request id")
	}
}

func TestGzipOnlyHTML(t *testing.T) {
	page := strings.Repeat("<p>زيت الزيتون</p>", 50)
	h := GzipHTML(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/css" {
			w.Header().Set("Content-Type", "text/css")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = io.WriteString(w, page)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ar", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("HTML not compressed")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if string(body) != page {
		t.Fatal("round trip mismatch")
	}

	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("non-HTML compressed")
	}
}

func TestLoggerDropsSensitiveQuery(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := Logger(logger, func(string) string { return "hash" })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ar/order/OL-2026-010001?t=SECRET", nil))
	if strings.Contains(buf.String(), "SECRET") {
		t.Fatalf("token logged: %s", buf.String())
	}
}
