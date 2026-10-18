package admin

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// Login throttling (SECURITY.md section 6): failures only, per email and per IP.
const (
	loginWindow      = 15 * time.Minute
	loginMaxPerEmail = 5
	loginMaxPerIP    = 10
	codeMaxPerAdmin  = 5
)

func loginLocale(r *http.Request) i18n.Locale {
	if l, ok := i18n.Parse(r.FormValue("lang")); ok {
		return l
	}
	return i18n.Default
}

// LoginPage renders /admin/login (or goes to /admin when already signed in).
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if cur, err := h.Sessions.Load(r.Context(), r); err == nil {
		target := "/admin"
		if cur.Session.MfaPending {
			target = "/admin/2fa"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	locale := loginLocale(r)
	p := h.guestPage(r, locale, i18n.AdminLoginTitle)
	d := view.LoginData{Next: safeNext(r.URL.Query().Get("next"))}
	if r.URL.Query().Get("expired") == "1" {
		d.Error = p.T(i18n.AdminLoginExpired)
	}
	render(w, r, http.StatusOK, p.Nonce, view.AdminLogin(p, d))
}

// Login checks the password. Timing-safe (unknown emails still run argon2),
// one generic error, failures counted per email and per IP and audited.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := loginLocale(r)
	p := h.guestPage(r, locale, i18n.AdminLoginTitle)
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	d := view.LoginData{Email: email, Next: safeNext(r.PostFormValue("next"))}
	if len(email) > 254 {
		email = email[:254]
		d.Email = ""
	}
	fail := func(key i18n.Key, status int) {
		d.Error = p.T(key)
		render(w, r, status, p.Nonce, view.AdminLogin(p, d))
	}

	emailKey := "login:email:" + hexSum(h.IPHash, "email:"+email)
	ipKey := "login:ip:" + h.ipKey(r)
	nEmail, err := h.Limiter.Count(ctx, emailKey, loginWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	nIP, err := h.Limiter.Count(ctx, ipKey, loginWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if nEmail >= loginMaxPerEmail || nIP >= loginMaxPerIP {
		auth.VerifyDummy(password)
		h.audit(ctx, r, nil, "auth.login_throttled", "", "", nil)
		fail(i18n.AdminLoginRateLimited, http.StatusTooManyRequests)
		return
	}

	admin, err := h.Store.Q.GetAdminByEmail(ctx, email)
	found := err == nil && admin.DisabledAt == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		h.serverError(w, r, err)
		return
	}
	ok, rehash := false, false
	if found && email != "" && len(password) <= auth.MaxPasswordLength {
		ok, rehash = auth.VerifyPassword(admin.PasswordHash, password)
	} else {
		auth.VerifyDummy(password)
	}
	if !ok {
		_, _ = h.Limiter.Hit(ctx, emailKey, loginWindow)
		_, _ = h.Limiter.Hit(ctx, ipKey, loginWindow)
		if found {
			h.audit(ctx, r, &admin.ID, "auth.login_failed", "admin", admin.ID.String(), nil)
		} else {
			h.audit(ctx, r, nil, "auth.login_failed", "", "", map[string]any{"known": false})
		}
		fail(i18n.AdminLoginInvalid, http.StatusUnauthorized)
		return
	}
	if rehash {
		if hash, err := auth.HashPassword(password); err == nil {
			_ = h.Store.Q.UpdateAdminPassword(ctx, dbq.UpdateAdminPasswordParams{ID: admin.ID, PasswordHash: hash, MustChangePassword: admin.MustChangePassword})
		}
	}

	mfa := admin.TotpEnabledAt != nil && len(admin.TotpSecretEnc) > 0
	if _, err := h.Sessions.Start(ctx, w, admin, mfa, h.ipHash(r), r.UserAgent()); err != nil {
		h.serverError(w, r, err)
		return
	}
	if mfa {
		h.audit(ctx, r, &admin.ID, "auth.password_ok", "admin", admin.ID.String(), nil)
		http.Redirect(w, r, "/admin/2fa", http.StatusSeeOther)
		return
	}
	h.completeLogin(r, admin, "password")
	target := "/admin"
	if admin.MustChangePassword {
		target = "/admin/password"
	} else if d.Next != "" {
		target = d.Next
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handlers) completeLogin(r *http.Request, admin dbq.Admin, method string) {
	ctx := r.Context()
	if err := h.Store.Q.UpdateAdminLastLogin(ctx, admin.ID); err != nil {
		h.Logger.ErrorContext(ctx, "last login", "err", err)
	}
	h.audit(ctx, r, &admin.ID, "auth.login", "admin", admin.ID.String(), map[string]any{"method": method})
}

// TwoFactorPage asks for the TOTP or a recovery code.
func (h *Handlers) TwoFactorPage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	p := h.page(r, cur, i18n.AdminLoginTwoFactorTitle, "")
	render(w, r, http.StatusOK, p.Nonce, view.AdminTwoFactor(p, ""))
}

// TwoFactor checks the code, then rotates the session out of the pending state.
func (h *Handlers) TwoFactor(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	p := h.page(r, cur, i18n.AdminLoginTwoFactorTitle, "")
	key := "2fa:admin:" + cur.Admin.ID.String()
	n, err := h.Limiter.Count(ctx, key, loginWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if n >= codeMaxPerAdmin {
		_ = h.Sessions.End(ctx, w, cur)
		h.audit(ctx, r, &cur.Admin.ID, "auth.login_throttled", "admin", cur.Admin.ID.String(), map[string]any{"step": "totp"})
		lp := h.guestPage(r, p.Locale, i18n.AdminLoginTitle)
		render(w, r, http.StatusTooManyRequests, lp.Nonce, view.AdminLogin(lp, view.LoginData{Error: lp.T(i18n.AdminLoginRateLimited)}))
		return
	}
	method, ok, err := h.checkSecondFactor(r, cur, r.PostFormValue("code"))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !ok {
		_, _ = h.Limiter.Hit(ctx, key, loginWindow)
		h.audit(ctx, r, &cur.Admin.ID, "auth.login_failed", "admin", cur.Admin.ID.String(), map[string]any{"step": "totp"})
		render(w, r, http.StatusUnauthorized, p.Nonce, view.AdminTwoFactor(p, p.T(i18n.AdminLoginCodeInvalid)))
		return
	}
	if _, err := h.Sessions.Rotate(ctx, w, cur, false); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.completeLogin(r, cur.Admin, method)
	target := "/admin"
	if cur.Admin.MustChangePassword {
		target = "/admin/password"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// checkSecondFactor accepts a TOTP code (each time step once) or an unused
// recovery code. It returns the method used.
func (h *Handlers) checkSecondFactor(r *http.Request, cur *auth.Current, code string) (string, bool, error) {
	ctx := r.Context()
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 20 || !cur.TOTPEnabled() {
		return "", false, nil
	}
	secret, err := h.Box.Open(cur.Admin.TotpSecretEnc)
	if err != nil {
		return "", false, err
	}
	if step, ok := auth.MatchTOTP(secret, code, h.Now()); ok {
		n, err := h.Store.Q.AdvanceTOTPStep(ctx, dbq.AdvanceTOTPStepParams{ID: cur.Admin.ID, Step: step})
		return "totp", n == 1, err
	}
	n, err := h.Store.Q.UseRecoveryCode(ctx, dbq.UseRecoveryCodeParams{AdminID: cur.Admin.ID, CodeHash: auth.HashRecoveryCode(code)})
	if err != nil || n != 1 {
		return "", false, err
	}
	h.audit(ctx, r, &cur.Admin.ID, "auth.recovery_code_used", "admin", cur.Admin.ID.String(), nil)
	return "recovery_code", true, nil
}

// Logout ends the session.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	if err := h.Sessions.End(r.Context(), w, cur); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(r.Context(), r, &cur.Admin.ID, "auth.logout", "admin", cur.Admin.ID.String(), nil)
	http.Redirect(w, r, "/admin/login?lang="+string(cur.Admin.UiLocale), http.StatusSeeOther) //nolint:gosec // G710: fixed path, locale is a DB enum
}

// PasswordPage renders the password form (forced on first login).
func (h *Handlers) PasswordPage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	p := h.page(r, cur, i18n.AdminPasswordTitle, "password")
	p.Bare = cur.Admin.MustChangePassword
	render(w, r, http.StatusOK, p.Nonce, view.AdminPassword(p, view.PasswordData{Forced: cur.Admin.MustChangePassword}))
}

var passwordProblems = map[int]i18n.Key{
	auth.PasswordTooShort:      i18n.AdminPasswordTooShort,
	auth.PasswordTooLong:       i18n.AdminPasswordTooLong,
	auth.PasswordCommon:        i18n.AdminPasswordCommon,
	auth.PasswordContainsEmail: i18n.AdminPasswordContainsEmail,
	auth.PasswordSame:          i18n.AdminPasswordSame,
}

// ChangePassword checks the current password and the new one, then signs out
// every other session and rotates this one.
func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	p := h.page(r, cur, i18n.AdminPasswordTitle, "password")
	p.Bare = cur.Admin.MustChangePassword
	d := view.PasswordData{Forced: cur.Admin.MustChangePassword}
	fail := func(k i18n.Key, status int) {
		d.Error = p.T(k)
		render(w, r, status, p.Nonce, view.AdminPassword(p, d))
	}
	current, next, confirm := r.PostFormValue("current"), r.PostFormValue("next"), r.PostFormValue("confirm")

	key := "password:admin:" + cur.Admin.ID.String()
	if n, err := h.Limiter.Count(ctx, key, loginWindow); err != nil || n >= loginMaxPerEmail {
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		fail(i18n.AdminLoginRateLimited, http.StatusTooManyRequests)
		return
	}
	if ok, _ := auth.VerifyPassword(cur.Admin.PasswordHash, current); len(current) > auth.MaxPasswordLength || !ok {
		_, _ = h.Limiter.Hit(ctx, key, loginWindow)
		h.audit(ctx, r, &cur.Admin.ID, "auth.password_change_failed", "admin", cur.Admin.ID.String(), nil)
		fail(i18n.AdminPasswordWrongCurrent, http.StatusUnprocessableEntity)
		return
	}
	if problem := auth.CheckNewPassword(next, cur.Admin.Email, current); problem != auth.PasswordOK {
		fail(passwordProblems[problem], http.StatusUnprocessableEntity)
		return
	}
	if next != confirm {
		fail(i18n.AdminPasswordMismatch, http.StatusUnprocessableEntity)
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.Store.Q.UpdateAdminPassword(ctx, dbq.UpdateAdminPasswordParams{ID: cur.Admin.ID, PasswordHash: hash, MustChangePassword: false}); err != nil {
		h.serverError(w, r, err)
		return
	}
	if _, err := h.Sessions.Rotate(ctx, w, cur, true); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "auth.password_changed", "admin", cur.Admin.ID.String(), map[string]any{"forced": cur.Admin.MustChangePassword})
	if cur.Admin.MustChangePassword {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/password?done=password", http.StatusSeeOther)
}

// SetLocale switches the admin language and comes back to the same page.
func (h *Handlers) SetLocale(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	locale := i18n.Locale(cur.Admin.UiLocale).Other()
	if err := h.Store.Q.UpdateAdminLocale(r.Context(), dbq.UpdateAdminLocaleParams{ID: cur.Admin.ID, UiLocale: dbq.Locale(locale)}); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(r.Context(), r, &cur.Admin.ID, "admin.locale", "admin", cur.Admin.ID.String(), map[string]any{"locale": string(locale)})
	next := safeNext(r.PostFormValue("next"))
	if next == "" {
		next = "/admin"
	}
	http.Redirect(w, r, next, http.StatusSeeOther) //nolint:gosec // G710: next passed safeNext (same-site /admin paths only)
}

func hexSum(h interface{ Sum(string) []byte }, v string) string { return hex.EncodeToString(h.Sum(v)) }
