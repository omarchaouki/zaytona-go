// Command zaytona is the single binary: serve | migrate | seed | admin reset-password | images | healthcheck | version.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "time/tzdata" // Africa/Casablanca in the static container

	"github.com/omarchaouki/zaytona-go/internal/assets"
	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/db"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/media"
	"github.com/omarchaouki/zaytona-go/internal/notify"
	"github.com/omarchaouki/zaytona-go/internal/seed"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/web"
	webassets "github.com/omarchaouki/zaytona-go/web"
)

var version = "dev"

const usage = `usage: zaytona <command>

commands:
  serve                  run the web server
  images [dir]           re-encode photos of assets/source into web/static/images (AVIF + WebP)
  migrate                apply database migrations
  seed                   fill missing starting content (idempotent, demo rows flagged)
  admin reset-password   reset ADMIN_EMAIL to ADMIN_PASSWORD, sign out its sessions, force a new password
  admin webhook-key      print the new-order webhook signing key (hex) for the receiving system
  healthcheck            exit 0 when the local server answers /healthz
  version                print the version`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "healthcheck":
		err = healthcheck(ctx)
	case "migrate":
		err = withStore(ctx, func(_ *config.Config, s *store.Store) error {
			v, err := db.Migrate(ctx, s.Pool)
			if err == nil {
				fmt.Printf("database at version %d\n", v)
			}
			return err
		})
	case "seed":
		err = withStore(ctx, func(cfg *config.Config, s *store.Store) error {
			err := seed.Run(ctx, s, seed.Options{
				AdminEmail:     cfg.AdminEmail,
				AdminPassword:  cfg.AdminPassword,
				WhatsAppNumber: cfg.SeedWhatsApp,
				DemoReviews:    true,
				Log:            func(m string) { fmt.Println("  " + m) },
			})
			if err == nil {
				fmt.Println("Seed complete. Demo rows are flagged in the admin until replaced.")
			}
			return err
		})
	case "admin":
		switch {
		case len(os.Args) >= 3 && os.Args[2] == "reset-password":
			err = withStore(ctx, resetPassword)
		case len(os.Args) >= 3 && os.Args[2] == "webhook-key":
			err = webhookKey()
		default:
			err = errors.New(usage)
		}
	case "images":
		err = imagesCmd(os.Args[2:])
	case "serve":
		err = withStore(ctx, func(cfg *config.Config, s *store.Store) error { return serve(ctx, cfg, s) })
	default:
		err = errors.New(usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func withStore(ctx context.Context, fn func(*config.Config, *store.Store) error) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, cfg.DatabasePoolMax)
	if err != nil {
		return err
	}
	defer pool.Close()
	return fn(cfg, store.New(pool))
}

func resetPassword(cfg *config.Config, s *store.Store) error {
	ctx := context.Background()
	if cfg.AdminEmail == "" || len(cfg.AdminPassword) < 12 {
		return errors.New("set ADMIN_EMAIL and an ADMIN_PASSWORD of 12+ characters")
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	return s.InTx(ctx, func(q *dbq.Queries) error {
		a, err := q.GetAdminByEmail(ctx, cfg.AdminEmail)
		if err != nil {
			return fmt.Errorf("no admin with that email: %w", store.NotFound(err))
		}
		if err := q.UpdateAdminPassword(ctx, dbq.UpdateAdminPasswordParams{ID: a.ID, PasswordHash: hash, MustChangePassword: true}); err != nil {
			return err
		}
		if err := q.DeleteAdminSessions(ctx, a.ID); err != nil {
			return err
		}
		if err := q.InsertAuditLog(ctx, dbq.InsertAuditLogParams{Action: "admin.password_reset_cli", Entity: ptr("admin"), EntityID: ptr(a.ID.String())}); err != nil {
			return err
		}
		fmt.Println("Password reset. All sessions signed out; a new password is required at next login.")
		return nil
	})
}

// serve runs the web server with graceful shutdown (SIGTERM: drain 20 s).
func serve(ctx context.Context, cfg *config.Config, s *store.Store) error {
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	a, err := assets.Load(webassets.Dist())
	if err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	if !a.Built() {
		logger.Warn("assets not built: run `go run ./tools/assetbuild` (task assets)")
	}
	site := store.NewSiteCache(s, time.Minute)
	// New-order email/webhook: targets read from Settings on every order.
	notifier := notify.New(notify.Config{
		SMTPURL:       cfg.SMTPURL,
		EmailFrom:     cfg.NotifyEmailFrom,
		WebhookURL:    cfg.OrderWebhookURL,
		WebhookSecret: cfg.DeriveKey(config.KeyWebhookSignature),
		Production:    cfg.IsProduction(),
	}, func(ctx context.Context) domain.NotificationSettings {
		d, err := site.Get(ctx)
		if err != nil {
			return domain.NotificationSettings{}
		}
		return d.Settings.Notifications
	}, logger)
	go media.Warm()                              // compile the WASM image encoders before the first upload
	go s.RunRetention(ctx, 24*time.Hour, logger) // expired sessions, old rate limits, spent PoW
	h := web.NewHandler(web.Deps{
		Config:   cfg,
		Store:    s,
		Site:     site,
		Assets:   a,
		Logger:   logger,
		Notifier: notifier,
	})
	srv := web.NewServer(fmt.Sprintf(":%d", cfg.Port), h)
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("listening", "port", cfg.Port, "env", cfg.Env)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// SIGTERM: stop accepting, drain requests (20 s), then flush notifications.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	notifier.Close(shutdownCtx)
	return err
}

func healthcheck(ctx context.Context) error {
	port := 3000
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("PORT %q is not a valid port", v)
		}
		port = p
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Loopback only, numeric port: not attacker-controlled.
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil) // #nosec G704
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- loopback health probe
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func ptr(s string) *string { return &s }

// webhookKey prints the HMAC key of X-Zaytona-Signature, hex encoded: the
// receiver decodes the hex and computes HMAC-SHA256 of the raw request body.
func webhookKey() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	fmt.Println(hex.EncodeToString(cfg.DeriveKey(config.KeyWebhookSignature))) //nolint:forbidigo // CLI output asked for by the operator
	return nil
}
