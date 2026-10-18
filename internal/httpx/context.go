// Package httpx is the middleware chain: request id, recover, real IP, security
// headers with a per-request CSP nonce, cross-origin protection, body limits,
// gzip for HTML and request logging with PII redaction.
package httpx

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyNonce
	keyClientIP
)

// RequestID returns the request id set by the RequestID middleware.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

// Nonce returns the CSP nonce for this request; every <script> must carry it.
func Nonce(ctx context.Context) string {
	v, _ := ctx.Value(keyNonce).(string)
	return v
}

// ClientIP returns the client IP resolved by the RealIP middleware.
func ClientIP(ctx context.Context) string {
	v, _ := ctx.Value(keyClientIP).(string)
	return v
}

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so the first one listed is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusWriter records the status code and bytes written.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
