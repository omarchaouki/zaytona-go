// Package admin holds the admin panel handlers. Every handler goes through
// guard: session (with 2FA and forced password change steps), role, and on
// unsafe methods the per-session CSRF token. Mutations write an audit row.
package admin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // Africa/Casablanca without a system tz database

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/omarchaouki/zaytona-go/internal/assets"
	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/httpx"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/media"
	"github.com/omarchaouki/zaytona-go/internal/notify"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

var casablanca = func() *time.Location {
	loc, err := time.LoadLocation("Africa/Casablanca")
	if err != nil {
		panic(err)
	}
	return loc
}()

// Handlers serve /admin.
type Handlers struct {
	Config   *config.Config
	Store    *store.Store
	Site     *store.SiteCache
	Assets   *assets.Assets
	Logger   *slog.Logger
	Sessions *auth.Sessions
	Limiter  *security.Limiter
	IPHash   *security.Hasher
	// Box encrypts TOTP secrets at rest.
	Box *auth.SecretBox
	// Media processes photo uploads (offers, certificates, gallery).
	Media *media.Processor
	// Webhook validates the new-order webhook address (no internal hosts).
	Webhook *notify.Webhook
	Now     func() time.Time
}

// New wires the admin handlers from the shared services.
func New(cfg *config.Config, st *store.Store, site *store.SiteCache, as *assets.Assets, logger *slog.Logger) (*Handlers, error) {
	box, err := auth.NewSecretBox(cfg.DeriveKey(config.KeyTOTPEncryption))
	if err != nil {
		return nil, err
	}
	return &Handlers{
		Config:   cfg,
		Store:    st,
		Site:     site,
		Assets:   as,
		Logger:   logger,
		Sessions: &auth.Sessions{Q: st.Q, Secure: cfg.SiteURL.Scheme == "https"},
		Limiter:  security.NewLimiter(st.Q),
		IPHash:   security.NewHasher(cfg.DeriveKey(config.KeyIPHash)),
		Box:      box,
		Webhook:  notify.NewWebhook(cfg.DeriveKey(config.KeyWebhookSignature), !cfg.IsProduction()),
		Now:      time.Now,
	}, nil
}

// Handler serves every /admin path. It has its own mux, dispatched before the
// public one: "/admin/{x}" would overlap with "/{locale}/privacy" there.
func (h *Handlers) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", h.LoginPage)
	post(mux, "POST /admin/login", h.Login)
	mux.HandleFunc("GET /admin/2fa", h.guard(roleAny, stepMFA, h.TwoFactorPage))
	post(mux, "POST /admin/2fa", h.guard(roleAny, stepMFA, h.TwoFactor))
	post(mux, "POST /admin/logout", h.guard(roleAny, stepMFA|stepPassword, h.Logout))
	mux.HandleFunc("GET /admin/password", h.guard(roleAny, stepPassword, h.PasswordPage))
	post(mux, "POST /admin/password", h.guard(roleAny, stepPassword, h.ChangePassword))
	post(mux, "POST /admin/locale", h.guard(roleAny, stepPassword, h.SetLocale))
	mux.HandleFunc("GET /admin/more", h.guard(roleAny, 0, h.More))

	mux.HandleFunc("GET /admin/security", h.guard(roleAny, 0, h.SecurityPage))
	post(mux, "POST /admin/security/reauth", h.guard(roleAny, 0, h.Reauth))
	post(mux, "POST /admin/security/totp/start", h.guard(roleAny, 0, h.TOTPStart))
	post(mux, "POST /admin/security/totp/enable", h.guard(roleAny, 0, h.TOTPEnable))
	post(mux, "POST /admin/security/totp/cancel", h.guard(roleAny, 0, h.TOTPCancel))
	post(mux, "POST /admin/security/totp/disable", h.guard(roleAny, 0, h.TOTPDisable))
	post(mux, "POST /admin/security/recovery", h.guard(roleAny, 0, h.RecoveryRegenerate))
	post(mux, "POST /admin/security/signout-others", h.guard(roleAny, 0, h.SignOutOthers))

	h.orderRoutes(mux)
	h.catalogueRoutes(mux)
	mux.HandleFunc("/", h.guard(roleAny, 0, h.NotFound))
	return mux
}

type role int

const (
	roleAny role = iota
	roleOwner
)

// step flags let a handler run before the login is complete.
type step int

const (
	stepMFA      step = 1 << iota // reachable while the TOTP code is pending
	stepPassword                  // reachable while a new password is required
)

// mfaPendingMax is how long a password-only session may wait for its code.
const mfaPendingMax = 10 * time.Minute

type handlerFunc func(w http.ResponseWriter, r *http.Request, cur *auth.Current)

// guard loads the session and enforces, in order: signed in, 2FA done, new
// password chosen, CSRF token on unsafe methods, role.
func (h *Handlers) guard(need role, allowed step, fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		cur, err := h.Sessions.Load(ctx, r)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				h.serverError(w, r, err)
				return
			}
			h.Sessions.ClearCookie(w)
			h.toLogin(w, r, true)
			return
		}
		switch {
		case cur.Session.MfaPending:
			if h.Now().Sub(cur.Session.CreatedAt) > mfaPendingMax {
				_ = h.Sessions.End(ctx, w, cur)
				h.toLogin(w, r, false)
				return
			}
			if allowed&stepMFA == 0 {
				h.redirect(w, r, "/admin/2fa")
				return
			}
		case allowed&stepMFA != 0 && allowed&stepPassword == 0:
			// The 2FA page after the code was accepted.
			h.redirect(w, r, "/admin")
			return
		case cur.Admin.MustChangePassword && allowed&stepPassword == 0:
			h.redirect(w, r, "/admin/password")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				token = r.PostFormValue("csrf")
			}
			if !auth.CSRFValid(cur.Session.CsrfToken, token) {
				h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonExpired, r.Referer())
				return
			}
			if !formFieldsOK(r) {
				http.Error(w, "unexpected form field", http.StatusBadRequest)
				return
			}
		}
		if need == roleOwner && !cur.IsOwner() {
			h.message(w, r, cur, http.StatusForbidden, i18n.AdminCommonForbidden, "")
			return
		}
		fn(w, r, cur)
	}
}

// SignedIn reports a fully signed-in admin (private uploads such as
// certificate scans that are not public yet).
func (h *Handlers) SignedIn(r *http.Request) bool {
	cur, err := h.Sessions.Load(r.Context(), r)
	return err == nil && !cur.Session.MfaPending
}

// toLogin sends a visitor without a session to the login page, keeping the
// admin page they wanted (GET only) to come back to.
func (h *Handlers) toLogin(w http.ResponseWriter, r *http.Request, keepNext bool) {
	target := "/admin/login"
	if keepNext && r.Method == http.MethodGet && r.URL.Path != "/admin" && r.URL.Path != "/admin/" {
		target += "?next=" + url.QueryEscape(r.URL.RequestURI())
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.Header().Set("X-Redirect", target)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handlers) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.Header().Set("X-Redirect", target)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// safeNext keeps only same-site admin paths as a return target.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/admin") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil || u.Host != "" || u.Scheme != "" || strings.HasPrefix(u.Path, "/admin/login") {
		return ""
	}
	return next
}

// page fills the admin layout for a signed-in admin.
func (h *Handlers) page(r *http.Request, cur *auth.Current, title i18n.Key, active string) view.AdminPage {
	locale := i18n.Locale(cur.Admin.UiLocale)
	p := view.AdminPage{
		Locale: locale,
		Nonce:  httpx.Nonce(r.Context()),
		Title:  i18n.T(locale, title) + " | " + i18n.T(locale, i18n.AdminNavBrand),
		Asset:  h.Assets.URL,
		CSRF:   cur.CSRF(),
		Email:  cur.Admin.Email,
		Owner:  cur.IsOwner(),
		Active: active,
		Return: r.URL.RequestURI(),
	}
	if !cur.Session.MfaPending {
		if n, err := h.Store.Q.CountUnseenOrders(r.Context()); err == nil {
			p.Unseen = n
		}
		if n, err := h.Store.Q.CountPendingReviews(r.Context()); err == nil {
			p.PendingReviews = n
		}
	}
	if k, ok := flashes[r.URL.Query().Get("done")]; ok {
		p.Flash = p.T(k, "count", r.URL.Query().Get("n"))
	}
	return p
}

// flashes are the results shown after a redirect (?done=key, ?n=count).
var flashes = map[string]i18n.Key{
	"password":       i18n.AdminPasswordSaved,
	"totp-on":        i18n.AdminSecurityEnabled,
	"totp-off":       i18n.AdminSecurityDisabled,
	"signout-others": i18n.AdminPasswordOthersSignedOut,
	"saved":          i18n.AdminOrderSaved,
	"applied":        i18n.AdminOrdersApplied,
	"removed":        i18n.AdminCommonDeleted,
	"paid":           i18n.AdminOrdersMarkedPaid,
	"unpaid":         i18n.AdminOrdersMarkedUnpaid,
	"restored":       i18n.AdminOrdersRestored,
}

// guestPage fills the layout before sign-in (login page).
func (h *Handlers) guestPage(r *http.Request, locale i18n.Locale, title i18n.Key) view.AdminPage {
	return view.AdminPage{
		Locale: locale,
		Nonce:  httpx.Nonce(r.Context()),
		Title:  i18n.T(locale, title) + " | " + i18n.T(locale, i18n.AdminNavBrand),
		Asset:  h.Assets.URL,
		Bare:   true,
	}
}

// message renders a small page (forbidden, expired form) with a way back.
func (h *Handlers) message(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, msg i18n.Key, back string) {
	p := h.page(r, cur, msg, "")
	p.Title = p.T(msg)
	if back = safeNext(strings.TrimPrefix(back, h.Config.SiteURL.String())); back == "" {
		back = "/admin"
	}
	render(w, r, status, p.Nonce, view.AdminMessage(p, p.T(msg), back, p.T(i18n.AdminOrderBack)))
}

// NotFound is the 404 inside /admin.
func (h *Handlers) NotFound(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	p := h.page(r, cur, i18n.ErrorsNotFoundTitle, "")
	render(w, r, http.StatusNotFound, p.Nonce, view.AdminMessage(p, p.T(i18n.ErrorsNotFoundBody), "/admin", p.T(i18n.AdminNavDashboard)))
}

// More is the phone "More" sheet as a page (without JS).
func (h *Handlers) More(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	p := h.page(r, cur, i18n.AdminNavMore, "more")
	render(w, r, http.StatusOK, p.Nonce, view.AdminMore(p))
}

// audit writes one audit_logs row. meta must not hold personal data in clear.
func (h *Handlers) audit(ctx context.Context, r *http.Request, adminID *uuid.UUID, action, entity, entityID string, meta map[string]any) {
	if err := h.auditTo(ctx, h.Store.Q, r, adminID, action, entity, entityID, meta); err != nil {
		h.Logger.ErrorContext(ctx, "audit", "action", action, "err", err)
	}
}

func (h *Handlers) auditTo(ctx context.Context, q *dbq.Queries, r *http.Request, adminID *uuid.UUID, action, entity, entityID string, meta map[string]any) error {
	p := dbq.InsertAuditLogParams{Action: action, IpHash: h.ipHash(r)}
	if adminID != nil {
		p.AdminID = uuid.NullUUID{UUID: *adminID, Valid: true}
	}
	if entity != "" {
		p.Entity = &entity
	}
	if entityID != "" {
		p.EntityID = &entityID
	}
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		p.Meta = b
	}
	return q.InsertAuditLog(ctx, p)
}

func (h *Handlers) ipHash(r *http.Request) []byte { return h.IPHash.Sum(httpx.ClientIP(r.Context())) }

func (h *Handlers) ipKey(r *http.Request) string { return hex.EncodeToString(h.ipHash(r)) }

func (h *Handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.ErrorContext(r.Context(), "admin handler", "request_id", httpx.RequestID(r.Context()), "path", r.URL.Path, "err", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Internal error. Reference: "+httpx.RequestID(r.Context()), http.StatusInternalServerError)
}

func render(w http.ResponseWriter, r *http.Request, status int, nonce string, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(templ.WithNonce(r.Context(), nonce), w)
}
