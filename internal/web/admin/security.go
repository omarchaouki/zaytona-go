package admin

import (
	"context"
	"net/http"
	"net/url"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// securityData describes the 2FA state of the signed-in admin.
func (h *Handlers) securityData(r *http.Request, cur *auth.Current) (view.SecurityData, error) {
	d := view.SecurityData{TOTPEnabled: cur.TOTPEnabled(), NeedsReauth: !cur.RecentAuth(h.Now())}
	if d.TOTPEnabled {
		n, err := h.Store.Q.CountUnusedRecoveryCodes(r.Context(), cur.Admin.ID)
		if err != nil {
			return d, err
		}
		d.RecoveryLeft = n
	} else if len(cur.Admin.TotpSecretEnc) > 0 && !d.NeedsReauth {
		secret, err := h.Box.Open(cur.Admin.TotpSecretEnc)
		if err != nil {
			return d, err
		}
		e, err := auth.EnrollmentFor(secret, cur.Admin.Email)
		if err != nil {
			return d, err
		}
		d.Enrollment = &view.SecurityEnrollment{Secret: e.Secret, QRData: e.QRData}
	}
	return d, nil
}

func (h *Handlers) renderSecurity(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, edit func(*view.AdminPage, *view.SecurityData)) {
	d, err := h.securityData(r, cur)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminSecurityTitle, "security")
	if edit != nil {
		edit(&p, &d)
	}
	render(w, r, status, p.Nonce, view.AdminSecurity(p, d))
}

// SecurityPage shows 2FA and sessions.
func (h *Handlers) SecurityPage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderSecurity(w, r, cur, http.StatusOK, nil)
}

// Reauth confirms the password before 2FA changes (valid 10 minutes).
func (h *Handlers) Reauth(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	key := "password:admin:" + cur.Admin.ID.String()
	n, err := h.Limiter.Count(ctx, key, loginWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	pw := r.PostFormValue("password")
	ok := false
	if n < loginMaxPerEmail && len(pw) <= auth.MaxPasswordLength {
		ok, _ = auth.VerifyPassword(cur.Admin.PasswordHash, pw)
	}
	next := safeNext(r.PostFormValue("next"))
	if !ok {
		_, _ = h.Limiter.Hit(ctx, key, loginWindow)
		h.audit(ctx, r, &cur.Admin.ID, "auth.reauth_failed", "admin", cur.Admin.ID.String(), nil)
		if next != "" {
			// Back to the page that asked, which shows the prompt with the error.
			http.Redirect(w, r, withQuery(next, "reauth", "failed"), http.StatusSeeOther) //nolint:gosec // G710: next passed safeNext
			return
		}
		msg := i18n.AdminPasswordWrongCurrent
		if n >= loginMaxPerEmail {
			msg = i18n.AdminLoginRateLimited
		}
		h.renderSecurity(w, r, cur, http.StatusUnauthorized, func(p *view.AdminPage, d *view.SecurityData) { d.Error = p.T(msg) })
		return
	}
	if err := h.Store.Q.MarkSessionRecentAuth(ctx, cur.Session.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	if next == "" {
		next = "/admin/security"
	}
	http.Redirect(w, r, next, http.StatusSeeOther) //nolint:gosec // G710: next passed safeNext
}

// requireRecent sends the admin back to the password prompt when the last
// password or code check is older than 10 minutes.
func (h *Handlers) requireRecent(w http.ResponseWriter, r *http.Request, cur *auth.Current) bool {
	if cur.RecentAuth(h.Now()) {
		return true
	}
	http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
	return false
}

// TOTPStart stores a new pending secret (not active until a code is confirmed).
func (h *Handlers) TOTPStart(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	if !h.requireRecent(w, r, cur) {
		return
	}
	if cur.TOTPEnabled() {
		http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
		return
	}
	e, err := auth.NewEnrollment(cur.Admin.Email)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	sealed, err := h.Box.Seal(e.Secret)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.Store.Q.SetAdminTOTP(r.Context(), dbq.SetAdminTOTPParams{ID: cur.Admin.ID, SecretEnc: sealed}); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
}

// TOTPCancel drops a pending secret.
func (h *Handlers) TOTPCancel(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	if !cur.TOTPEnabled() {
		if err := h.Store.Q.SetAdminTOTP(r.Context(), dbq.SetAdminTOTPParams{ID: cur.Admin.ID}); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
}

// TOTPEnable activates 2FA after a valid code, creates 10 recovery codes
// (shown once, in this response) and rotates the session.
func (h *Handlers) TOTPEnable(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	if !h.requireRecent(w, r, cur) {
		return
	}
	if cur.TOTPEnabled() || len(cur.Admin.TotpSecretEnc) == 0 {
		http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
		return
	}
	key := "2fa:admin:" + cur.Admin.ID.String()
	n, err := h.Limiter.Count(ctx, key, loginWindow)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	secret, err := h.Box.Open(cur.Admin.TotpSecretEnc)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	step, ok := auth.MatchTOTP(secret, r.PostFormValue("code"), h.Now())
	if !ok || n >= codeMaxPerAdmin {
		_, _ = h.Limiter.Hit(ctx, key, loginWindow)
		msg := i18n.AdminSecurityCodeInvalid
		if n >= codeMaxPerAdmin {
			msg = i18n.AdminLoginRateLimited
		}
		h.renderSecurity(w, r, cur, http.StatusUnprocessableEntity, func(p *view.AdminPage, d *view.SecurityData) { d.Error = p.T(msg) })
		return
	}
	codes, hashes, err := auth.NewRecoveryCodes()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	now := h.Now()
	err = h.Store.InTx(ctx, func(q *dbq.Queries) error {
		if err := q.SetAdminTOTP(ctx, dbq.SetAdminTOTPParams{ID: cur.Admin.ID, SecretEnc: cur.Admin.TotpSecretEnc, EnabledAt: &now}); err != nil {
			return err
		}
		if _, err := q.AdvanceTOTPStep(ctx, dbq.AdvanceTOTPStepParams{ID: cur.Admin.ID, Step: step}); err != nil {
			return err
		}
		if err := replaceRecoveryCodes(ctx, q, cur, hashes); err != nil {
			return err
		}
		return h.auditTo(ctx, q, r, &cur.Admin.ID, "auth.totp_enabled", "admin", cur.Admin.ID.String(), nil)
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.showCodes(w, r, cur, codes, i18n.AdminSecurityEnabled)
}

// TOTPDisable turns 2FA off (recent password check required) and rotates the session.
func (h *Handlers) TOTPDisable(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	if !h.requireRecent(w, r, cur) {
		return
	}
	err := h.Store.InTx(ctx, func(q *dbq.Queries) error {
		if err := q.SetAdminTOTP(ctx, dbq.SetAdminTOTPParams{ID: cur.Admin.ID}); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, cur.Admin.ID); err != nil {
			return err
		}
		return h.auditTo(ctx, q, r, &cur.Admin.ID, "auth.totp_disabled", "admin", cur.Admin.ID.String(), nil)
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if _, err := h.Sessions.Rotate(ctx, w, cur, false); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/security?done=totp-off", http.StatusSeeOther)
}

// RecoveryRegenerate replaces the recovery codes and shows the new ones once.
func (h *Handlers) RecoveryRegenerate(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	if !h.requireRecent(w, r, cur) {
		return
	}
	if !cur.TOTPEnabled() {
		http.Redirect(w, r, "/admin/security", http.StatusSeeOther)
		return
	}
	codes, hashes, err := auth.NewRecoveryCodes()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	err = h.Store.InTx(ctx, func(q *dbq.Queries) error {
		if err := replaceRecoveryCodes(ctx, q, cur, hashes); err != nil {
			return err
		}
		return h.auditTo(ctx, q, r, &cur.Admin.ID, "auth.recovery_codes_regenerated", "admin", cur.Admin.ID.String(), nil)
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.showCodes(w, r, cur, codes, "")
}

// showCodes renders the security page with the new recovery codes, after
// rotating the session (2FA state changed).
func (h *Handlers) showCodes(w http.ResponseWriter, r *http.Request, cur *auth.Current, codes []string, flash i18n.Key) {
	next, err := h.Sessions.Rotate(r.Context(), w, cur, false)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	admin, err := h.Store.Q.GetAdminByID(r.Context(), cur.Admin.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	next.Admin = admin
	h.renderSecurity(w, r, next, http.StatusOK, func(p *view.AdminPage, d *view.SecurityData) {
		d.RecoveryCodes = codes
		if flash != "" {
			p.Flash = p.T(flash)
		}
	})
}

func replaceRecoveryCodes(ctx context.Context, q *dbq.Queries, cur *auth.Current, hashes [][]byte) error {
	if err := q.DeleteRecoveryCodes(ctx, cur.Admin.ID); err != nil {
		return err
	}
	for _, hsh := range hashes {
		if err := q.InsertRecoveryCode(ctx, dbq.InsertRecoveryCodeParams{AdminID: cur.Admin.ID, CodeHash: hsh}); err != nil {
			return err
		}
	}
	return nil
}

// SignOutOthers ends every other session of this admin.
func (h *Handlers) SignOutOthers(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	if err := h.Store.Q.DeleteOtherAdminSessions(ctx, dbq.DeleteOtherAdminSessionsParams{AdminID: cur.Admin.ID, ID: cur.Session.ID}); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "auth.signout_others", "admin", cur.Admin.ID.String(), nil)
	http.Redirect(w, r, "/admin/security?done=signout-others", http.StatusSeeOther)
}

// withQuery adds one parameter to a same-site path.
func withQuery(path, key, value string) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}
