package httpx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// RequestIDMiddleware assigns a random id, exposed as X-Request-Id.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyRequestID, id)))
	})
}

// Recover turns a panic into a generic 500 with the request id, never a stack trace.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					handlePanic(w, r, logger, v)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func handlePanic(w http.ResponseWriter, r *http.Request, logger *slog.Logger, v any) {
	if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
		panic(v)
	}
	logger.ErrorContext(r.Context(), "panic", "request_id", RequestID(r.Context()), "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = fmt.Fprintf(w, "Internal error. Reference: %s\n", RequestID(r.Context()))
}

// RealIP resolves the client IP. Proxy headers are read only when trustProxy is
// set, and then only the right-most X-Forwarded-For hop (the one our proxy added).
func RealIP(trustProxy bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trustProxy)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyClientIP, ip)))
		})
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			hops := strings.Split(xff[len(xff)-1], ",")
			if ip := net.ParseIP(strings.TrimSpace(hops[len(hops)-1])); ip != nil {
				return ip.String()
			}
		}
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return ""
}

// IsHTTPS reports whether the request reached us over TLS (directly or via a trusted proxy).
func IsHTTPS(r *http.Request, trustProxy bool) bool {
	if r.TLS != nil {
		return true
	}
	return trustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// HeadersConfig configures SecurityHeaders.
type HeadersConfig struct {
	TrustProxy bool
	// Features returns the optional CSP origins for this request (settings-driven).
	Features func(*http.Request) CSPFeatures
}

// SecurityHeaders sets the CSP with a fresh 128-bit nonce and the fixed security headers.
func SecurityHeaders(cfg HeadersConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			nonce := base64.RawStdEncoding.EncodeToString(b)

			var f CSPFeatures
			if cfg.Features != nil {
				f = cfg.Features(r)
			}
			https := IsHTTPS(r, cfg.TrustProxy)
			f.HTTPS = https

			h := w.Header()
			h.Set("Content-Security-Policy", BuildCSP(nonce, f))
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
			if https {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			if strings.HasPrefix(r.URL.Path, "/admin") {
				h.Set("X-Robots-Tag", "noindex, nofollow")
				h.Set("Cache-Control", "no-store")
				h.Set("Referrer-Policy", "no-referrer")
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyNonce, nonce)))
		})
	}
}

// CrossOrigin rejects cross-site unsafe requests from browsers (Sec-Fetch-Site / Origin).
func CrossOrigin() Middleware {
	cop := http.NewCrossOriginProtection()
	return cop.Handler
}

// Body limits from SECURITY.md section 4.
const (
	FormBodyLimit    int64 = 16 << 10
	WAClickBodyLimit int64 = 2 << 10
)

// LimitBodyFor caps request bodies with a per-request limit (uploads get more).
func LimitBodyFor(limit func(r *http.Request) int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				r.Body = http.MaxBytesReader(w, r.Body, limit(r))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitBody caps the request body for every method that may carry one.
func LimitBody(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Logger logs one line per request. Query strings are dropped on admin and
// confirmation routes (tokens); IPs are logged as a keyed-hash prefix only.
func Logger(logger *slog.Logger, ipHash func(string) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			path := r.URL.Path
			if r.URL.RawQuery != "" && !sensitivePath(path) {
				path += "?" + r.URL.RawQuery
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			attrs := []any{
				"method", r.Method,
				"path", path,
				"status", status,
				"bytes", sw.bytes,
				"ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()),
			}
			if ip := ClientIP(r.Context()); ip != "" && ipHash != nil {
				attrs = append(attrs, "ip", ipHash(ip))
			}
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			}
			logger.Log(r.Context(), level, "request", attrs...)
		})
	}
}

func sensitivePath(p string) bool {
	return strings.HasPrefix(p, "/admin") || strings.Contains(p, "/order/")
}
