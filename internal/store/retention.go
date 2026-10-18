package store

import (
	"context"
	"log/slog"
	"time"
)

// RateLimitRetention is how long fixed-window counters are kept (SECURITY.md section 12).
const RateLimitRetention = 48 * time.Hour

// CleanupResult counts the rows removed by Cleanup.
type CleanupResult struct {
	Sessions, RateLimits, Pow int64
}

// Cleanup deletes expired sessions, rate-limit windows older than two days
// and spent proof-of-work signatures past their expiry.
func (s *Store) Cleanup(ctx context.Context, now time.Time) (CleanupResult, error) {
	var r CleanupResult
	var err error
	if r.Sessions, err = s.Q.DeleteExpiredSessions(ctx); err != nil {
		return r, err
	}
	if r.RateLimits, err = s.Q.DeleteOldRateLimits(ctx, now.Add(-RateLimitRetention)); err != nil {
		return r, err
	}
	if r.Pow, err = s.Q.DeleteExpiredPow(ctx); err != nil {
		return r, err
	}
	return r, nil
}

// RunRetention runs Cleanup at start and then every interval until ctx ends.
func (s *Store) RunRetention(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	run := func() {
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		r, err := s.Cleanup(cctx, time.Now())
		if err != nil {
			logger.ErrorContext(ctx, "retention cleanup", "err", err)
			return
		}
		logger.InfoContext(ctx, "retention cleanup", "sessions", r.Sessions, "rate_limits", r.RateLimits, "pow", r.Pow)
	}
	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
