package security

import (
	"context"
	"time"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
)

// Limiter is a fixed-window rate limiter stored in Postgres (rate_limits), so
// limits survive restarts and hold across processes.
type Limiter struct {
	q   *dbq.Queries
	now func() time.Time
}

// NewLimiter uses the given queries (pool or transaction).
func NewLimiter(q *dbq.Queries) *Limiter { return &Limiter{q: q, now: time.Now} }

// Allow counts one hit for key in the current window and reports whether the
// count is still within limit.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	start := l.now().UTC().Truncate(window)
	n, err := l.q.HitRateLimit(ctx, dbq.HitRateLimitParams{Key: key, WindowStart: start})
	if err != nil {
		return false, err
	}
	return int(n) <= limit, nil
}

// Count returns the hits of key in the current window without adding one
// (login throttling counts failures only).
func (l *Limiter) Count(ctx context.Context, key string, window time.Duration) (int, error) {
	start := l.now().UTC().Truncate(window)
	n, err := l.q.PeekRateLimit(ctx, dbq.PeekRateLimitParams{Key: key, WindowStart: start})
	return int(n), err
}

// Hit adds one hit to key in the current window and returns the new count.
func (l *Limiter) Hit(ctx context.Context, key string, window time.Duration) (int, error) {
	start := l.now().UTC().Truncate(window)
	n, err := l.q.HitRateLimit(ctx, dbq.HitRateLimitParams{Key: key, WindowStart: start})
	return int(n), err
}
