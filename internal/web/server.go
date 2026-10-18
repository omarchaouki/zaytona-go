// Package web builds the router and the middleware chain.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/assets"
	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/httpx"
	"github.com/omarchaouki/zaytona-go/internal/media"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/web/admin"
	"github.com/omarchaouki/zaytona-go/internal/web/public"
)

// Deps are the shared services handed to handlers.
type Deps struct {
	Config *config.Config
	Store  *store.Store
	Site   *store.SiteCache
	Assets *assets.Assets
	Logger *slog.Logger
	// Notifier sends new-order email/webhook (nil: none).
	Notifier public.OrderNotifier
}

// NewHandler returns the full application handler.
func NewHandler(d Deps) http.Handler {
	ipHasher := security.NewHasher(d.Config.DeriveKey(config.KeyIPHash))
	pub := &public.Handlers{
		Config:  d.Config,
		Store:   d.Store,
		Site:    d.Site,
		Assets:  d.Assets,
		Logger:  d.Logger,
		Limiter: security.NewLimiter(d.Store.Q),
		IPHash:  ipHasher,

		PhoneHash:  security.NewHasher(d.Config.DeriveKey(config.KeyPhoneHash)),
		FormTokens: security.NewFormTokens(d.Config.DeriveKey(config.KeyFormToken)),
		Pow:        security.NewPow(d.Config.DeriveKey(config.KeyAltchaHMAC), d.Store.Q),
		Turnstile:  security.NewTurnstile(d.Config.TurnstileSecret),
		Confirm:    security.NewConfirmationTokens(d.Config.DeriveKey(config.KeyConfirmationToken)),
		Notifier:   d.Notifier,
	}

	uploadLimit := int64(d.Config.MaxUploadMB) << 20
	proc, err := media.New(d.Config.UploadDir, uploadLimit)
	if err != nil {
		panic(fmt.Errorf("UPLOAD_DIR: %w", err))
	}
	adm, err := admin.New(d.Config, d.Store, d.Site, d.Assets, d.Logger)
	if err != nil {
		panic(err) // the derived key always has the AES-256 length
	}
	adm.Media = proc
	pub.Media = proc
	uploads := proc.Handler(func(ctx context.Context, id string) (bool, error) {
		ok, err := d.Store.Q.IsMediaPublic(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return ok, err
	}, adm.SignedIn)

	adminHandler := adm.Handler()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Store.Pool.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /robots.txt", pub.Robots)
	mux.HandleFunc("GET /sitemap.xml", pub.Sitemap)
	mux.Handle("POST /api/wa-click", httpx.LimitBody(httpx.WAClickBodyLimit)(http.HandlerFunc(pub.WAClick)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ar", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /{locale}", pub.Landing)
	mux.HandleFunc("GET /{locale}/privacy", pub.Policy("privacy"))
	mux.HandleFunc("GET /{locale}/terms", pub.Policy("terms"))
	mux.HandleFunc("POST /{locale}/order", pub.PlaceOrder)
	mux.HandleFunc("GET /{locale}/order", pub.OrderRedirect)
	mux.HandleFunc("GET /{locale}/order/{reference}", pub.Confirmation)
	mux.HandleFunc("GET /api/pow", pub.PowChallenge)
	mux.HandleFunc("GET /{locale}/reviews", pub.ReviewsFragment)
	mux.HandleFunc("GET /{locale}/reviews/new", pub.ReviewForm)
	mux.HandleFunc("POST /{locale}/reviews", pub.SubmitReview)
	mux.HandleFunc("/", pub.NotFound)

	// /static/ and /admin are dispatched before the mux: its patterns would overlap with
	// /{locale}/{page} ("/static/privacy"), which ServeMux rejects as a conflict.
	static := d.Assets.Handler()
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, assets.Prefix) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			static.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, media.Prefix) {
			uploads.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			adminHandler.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	return httpx.Chain(root,
		httpx.RequestIDMiddleware,
		httpx.Logger(d.Logger, ipHasher.LogPrefix),
		httpx.Recover(d.Logger),
		httpx.RealIP(d.Config.TrustProxy),
		httpx.SecurityHeaders(httpx.HeadersConfig{
			TrustProxy: d.Config.TrustProxy,
			Features:   pub.CSPFeatures,
		}),
		httpx.CrossOrigin(),
		// Forms get 16 KiB; admin photo uploads get MAX_UPLOAD_MB and review photos 5 MB, plus room for the fields.
		httpx.LimitBodyFor(func(r *http.Request) int64 {
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				switch {
				case strings.HasPrefix(r.URL.Path, "/admin/"):
					return uploadLimit + 64<<10
				case r.URL.Path == "/ar/reviews" || r.URL.Path == "/fr/reviews":
					return domain.ReviewPhotoMaxBytes + 64<<10
				}
			}
			return httpx.FormBodyLimit
		}),
		httpx.GzipHTML,
	)
}

// NewServer wraps the handler with the timeouts from SECURITY.md section 4.
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
