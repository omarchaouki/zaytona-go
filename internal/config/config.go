// Package config reads and validates the environment once at startup.
package config

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// ExampleSecret is the APP_SECRET value shipped in .env.example. It is refused
// outside development.
const ExampleSecret = "change-me-change-me-change-me-change-me-change-me-0000"

// Config is the validated application configuration.
type Config struct {
	Env             string // "development" or "production"
	Port            int
	Host            string // listen address; "" = every interface, 127.0.0.1 behind a local proxy
	LogLevel        string
	DatabaseURL     string
	DatabasePoolMax int32
	AppSecret       []byte
	SiteURL         *url.URL
	AdminEmail      string
	AdminPassword   string
	StorageDriver   string
	UploadDir       string
	MaxUploadMB     int
	SMTPURL         string
	NotifyEmailFrom string
	OrderWebhookURL string
	TurnstileSite   string
	TurnstileSecret string
	TrustProxy      bool
	SeedWhatsApp    string
	UmamiWebsiteID  string
}

// IsProduction reports whether production rules apply (Secure cookies, real secret).
func (c *Config) IsProduction() bool { return c.Env != "development" }

// Load reads the configuration from the process environment.
func Load() (*Config, error) { return FromLookup(os.LookupEnv) }

// FromLookup builds a Config from any lookup function (tests pass a map).
func FromLookup(lookup func(string) (string, bool)) (*Config, error) {
	get := func(k, def string) string {
		if v, ok := lookup(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	var errs []error

	c := &Config{
		// The release binary defaults to production rules when APP_ENV is unset.
		Env:             get("APP_ENV", "production"),
		LogLevel:        get("LOG_LEVEL", "info"),
		DatabaseURL:     get("DATABASE_URL", ""),
		AdminEmail:      get("ADMIN_EMAIL", ""),
		AdminPassword:   get("ADMIN_PASSWORD", ""),
		StorageDriver:   get("STORAGE_DRIVER", "local"),
		UploadDir:       get("UPLOAD_DIR", "./uploads"),
		SMTPURL:         get("SMTP_URL", ""),
		NotifyEmailFrom: get("NOTIFY_EMAIL_FROM", ""),
		OrderWebhookURL: get("ORDER_WEBHOOK_URL", ""),
		TurnstileSite:   get("TURNSTILE_SITE_KEY", ""),
		TurnstileSecret: get("TURNSTILE_SECRET_KEY", ""),
		SeedWhatsApp:    get("SEED_WHATSAPP_NUMBER", ""),
		UmamiWebsiteID:  get("UMAMI_WEBSITE_ID", ""),
	}

	if c.Env != "development" && c.Env != "production" {
		errs = append(errs, fmt.Errorf("APP_ENV must be development or production, got %q", c.Env))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error"))
	}

	var err error
	if c.Port, err = intInRange(get("PORT", "3000"), 1, 65535); err != nil {
		errs = append(errs, fmt.Errorf("PORT: %w", err))
	}
	if c.Host = get("HOST", ""); c.Host != "" && net.ParseIP(c.Host) == nil {
		errs = append(errs, fmt.Errorf("HOST must be an IP address such as 127.0.0.1, got %q", c.Host))
	}
	pool, err := intInRange(get("DATABASE_POOL_MAX", "10"), 1, 200)
	if err != nil {
		errs = append(errs, fmt.Errorf("DATABASE_POOL_MAX: %w", err))
	}
	c.DatabasePoolMax = int32(pool) // #nosec G115 -- bounded to 1..200 above
	if c.MaxUploadMB, err = intInRange(get("MAX_UPLOAD_MB", "5"), 1, 50); err != nil {
		errs = append(errs, fmt.Errorf("MAX_UPLOAD_MB: %w", err))
	}
	if c.TrustProxy, err = strconv.ParseBool(get("TRUST_PROXY", "false")); err != nil {
		errs = append(errs, errors.New("TRUST_PROXY must be true or false"))
	}

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	} else if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		errs = append(errs, errors.New("DATABASE_URL must be a postgres:// URL"))
	} else {
		errs = append(errs, checkDatabaseURL(u, c.IsProduction())...)
	}

	secret := get("APP_SECRET", "")
	switch {
	case secret == "":
		errs = append(errs, errors.New("APP_SECRET is required (openssl rand -hex 32)"))
	case secret == ExampleSecret && c.IsProduction():
		errs = append(errs, errors.New("APP_SECRET still has the example value; generate one with openssl rand -hex 32"))
	case secret == ExampleSecret:
		c.AppSecret = []byte(secret)
	default:
		b, err := hex.DecodeString(secret)
		if err != nil || len(b) < 32 {
			errs = append(errs, errors.New("APP_SECRET must be at least 32 random bytes encoded as hex"))
		}
		c.AppSecret = b
	}

	site := get("SITE_URL", "http://localhost:3000")
	u, err := url.Parse(site)
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
		errs = append(errs, errors.New("SITE_URL must be an absolute http(s) URL"))
	case c.IsProduction() && u.Scheme != "https":
		errs = append(errs, errors.New("SITE_URL must use https in production"))
	default:
		u.Path = strings.TrimRight(u.Path, "/")
		c.SiteURL = u
	}

	switch c.StorageDriver {
	case "local":
	default:
		errs = append(errs, fmt.Errorf("STORAGE_DRIVER %q is not supported (local)", c.StorageDriver))
	}

	if c.SeedWhatsApp != "" && c.IsProduction() {
		errs = append(errs, errors.New("SEED_WHATSAPP_NUMBER is for development only"))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// checkDatabaseURL refuses settings that would work badly or unsafely:
//   - a remote database in production without TLS (the URL must say
//     sslmode=require, or better verify-full with the provider's CA);
//   - Supabase's transaction pooler (port 6543): it hands each statement to
//     any server connection, which breaks the prepared statements and session
//     settings this app relies on. Use the session pooler or a direct connection.
func checkDatabaseURL(u *url.URL, production bool) []error {
	var errs []error
	host := u.Hostname()
	if host == "" {
		host = u.Query().Get("host") // unix socket in the query string
	}
	local := host == "" || host == "localhost" || strings.HasPrefix(host, "/") || net.ParseIP(host).IsLoopback()
	if production && !local {
		switch u.Query().Get("sslmode") {
		case "require", "verify-ca", "verify-full":
		default:
			errs = append(errs, errors.New("DATABASE_URL: a remote database needs TLS: add ?sslmode=verify-full&sslrootcert=/path/to/ca.crt (or at least sslmode=require)"))
		}
	}
	if strings.HasSuffix(host, ".pooler.supabase.com") && u.Port() == "6543" {
		errs = append(errs, errors.New("DATABASE_URL: Supabase transaction pooler (port 6543) is not supported; use the session pooler (same host, port 5432) or the direct connection"))
	}
	return errs
}

func intInRange(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%d is outside %d..%d", n, lo, hi)
	}
	return n, nil
}

// Key purposes for DeriveKey. One label per use, never reused.
const (
	KeySessionCSRF       = "session-csrf"
	KeyFormToken         = "form-token"
	KeyConfirmationToken = "confirmation-token"
	KeyAltchaHMAC        = "altcha-hmac"
	KeyTOTPEncryption    = "totp-encryption"
	KeyPhoneHash         = "phone-hash"
	KeyIPHash            = "ip-hash"
	KeyWebhookSignature  = "webhook-signature"
)

// DeriveKey returns a 32-byte key for one purpose, derived from APP_SECRET with HKDF-SHA256.
func (c *Config) DeriveKey(purpose string) []byte {
	k, err := hkdf.Key(sha256.New, c.AppSecret, nil, "zaytona:"+purpose, 32)
	if err != nil {
		// Only fails for an output length above 255*32 bytes.
		panic(err)
	}
	return k
}
