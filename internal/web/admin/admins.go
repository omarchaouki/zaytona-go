package admin

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

func (h *Handlers) adminsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/admins", h.guard(roleOwner, 0, h.Admins))
	post(mux, "POST /admin/admins", h.guard(roleOwner, 0, h.AddAdmin))
	post(mux, "POST /admin/admins/{id}/{action}", h.guard(roleOwner, 0, h.AdminAction))
	mux.HandleFunc("GET /admin/audit", h.guard(roleOwner, 0, h.Audit))
}

// tempPassword is 20 random characters in groups of five (about 100 bits),
// unambiguous letters and digits only.
func tempPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for i, x := range b {
		if i > 0 && i%5 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(x)%len(alphabet)])
	}
	return sb.String()
}

func (h *Handlers) renderAdmins(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, edit func(p *view.AdminPage, d *view.AdminsData)) {
	rows, err := h.Store.Q.ListAdminsOverview(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminAdminsTitle, "admins")
	d := view.AdminsData{SelfID: cur.Admin.ID.String(), NeedsReauth: !cur.RecentAuth(h.Now())}
	if r.URL.Query().Get("reauth") == "failed" {
		d.ReauthError = p.T(i18n.AdminPasswordWrongCurrent)
	}
	for _, a := range rows {
		d.Admins = append(d.Admins, view.AdminRow{
			ID: a.ID.String(), Email: a.Email, Name: deref(a.Name), Role: string(a.Role),
			TwoFactor: a.TotpEnabledAt != nil, MustChange: a.MustChangePassword, Disabled: a.DisabledAt != nil, LastLogin: a.LastLoginAt,
		})
	}
	if edit != nil {
		edit(&p, &d)
	}
	render(w, r, status, p.Nonce, view.AdminAdmins(p, d))
}

// Admins lists the accounts (owner only).
func (h *Handlers) Admins(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderAdmins(w, r, cur, http.StatusOK, nil)
}

// AddAdmin creates an account with a temporary password shown once (recent password check required).
func (h *Handlers) AddAdmin(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	if !cur.RecentAuth(h.Now()) {
		http.Redirect(w, r, "/admin/admins", http.StatusSeeOther)
		return
	}
	f := newForm(r)
	email := f.email("email", true)
	name := f.text("name", 120, false, false)
	role := dbq.AdminRole(r.PostFormValue("role"))
	if role != dbq.AdminRoleOwner && role != dbq.AdminRoleStaff {
		f.fail("form", i18n.AdminCommonError)
	}
	if !f.ok() {
		h.renderAdmins(w, r, cur, http.StatusUnprocessableEntity, func(p *view.AdminPage, d *view.AdminsData) { d.Form = formState("new", f, *p) })
		return
	}
	temp := tempPassword()
	hash, err := auth.HashPassword(temp)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	var namePtr *string
	if name != "" {
		namePtr = &name
	}
	a, err := h.Store.AddAdmin(r.Context(), actor(r, h, cur), dbq.CreateAdminParams{
		Email: email, Name: namePtr, PasswordHash: hash, Role: role, UiLocale: cur.Admin.UiLocale,
	})
	if errors.Is(err, store.ErrEmailExists) {
		f.fail("email", i18n.AdminAdminsExists)
		h.renderAdmins(w, r, cur, http.StatusUnprocessableEntity, func(p *view.AdminPage, d *view.AdminsData) { d.Form = formState("new", f, *p) })
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// Shown once, in this response only (no redirect: the password is not stored).
	h.renderAdmins(w, r, cur, http.StatusOK, func(_ *view.AdminPage, d *view.AdminsData) {
		d.TempPassword, d.TempFor, d.TempIsNew = temp, a.Email, true
	})
}

// AdminAction changes the role, resets the password, disables or enables an account.
func (h *Handlers) AdminAction(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	if !cur.RecentAuth(h.Now()) {
		http.Redirect(w, r, "/admin/admins", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a := actor(r, h, cur)
	switch r.PathValue("action") {
	case "role":
		role := dbq.AdminRole(r.PostFormValue("role"))
		if role != dbq.AdminRoleOwner && role != dbq.AdminRoleStaff {
			http.Error(w, "bad role", http.StatusBadRequest)
			return
		}
		err = h.Store.SetAdminRole(ctx, a, id, role)
	case "disable":
		if r.PostFormValue("confirm") != "1" {
			p := h.page(r, cur, i18n.AdminAdminsDisable, "admins")
			render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminAdminsDisable), p.T(i18n.AdminAdminsDisableBody), "/admin/admins", nil))
			return
		}
		err = h.Store.SetAdminDisabled(ctx, a, id, true)
	case "enable":
		err = h.Store.SetAdminDisabled(ctx, a, id, false)
	case "reset":
		temp := tempPassword()
		hash, herr := auth.HashPassword(temp)
		if herr != nil {
			h.serverError(w, r, herr)
			return
		}
		if err = h.Store.ResetAdminPassword(ctx, a, id, hash); err == nil {
			target, gerr := h.Store.Q.GetAdminByID(ctx, id)
			if gerr != nil {
				h.serverError(w, r, gerr)
				return
			}
			h.renderAdmins(w, r, cur, http.StatusOK, func(_ *view.AdminPage, d *view.AdminsData) {
				d.TempPassword, d.TempFor = temp, target.Email
			})
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	var msg i18n.Key
	switch {
	case errors.Is(err, store.ErrLastOwner):
		msg = i18n.AdminAdminsLastOwner
	case errors.Is(err, store.ErrSelf):
		msg = i18n.AdminAdminsNotSelf
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	if msg != "" {
		h.renderAdmins(w, r, cur, http.StatusConflict, func(p *view.AdminPage, d *view.AdminsData) { d.Error = p.T(msg) })
		return
	}
	http.Redirect(w, r, "/admin/admins?done=saved", http.StatusSeeOther)
}

const auditPageSize = 50

// Audit is the audit log viewer (owner only), filtered by actor, action and dates.
func (h *Handlers) Audit(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	q := r.URL.Query()
	d := view.AuditData{Page: 1}
	params := dbq.ListAuditLogsParams{Limit: auditPageSize}
	if id, err := uuid.Parse(q.Get("actor")); err == nil {
		params.AdminID = uuid.NullUUID{UUID: id, Valid: true}
		d.Actor = id.String()
	}
	if a := q.Get("action"); auditActionRe.MatchString(a) {
		params.Action = &a
		d.Action = a
	}
	if t, err := time.ParseInLocation(time.DateOnly, q.Get("from"), casablanca); err == nil {
		params.FromDate = &t
		d.From = q.Get("from")
	}
	if t, err := time.ParseInLocation(time.DateOnly, q.Get("to"), casablanca); err == nil {
		next := t.AddDate(0, 0, 1)
		params.ToDate = &next
		d.To = q.Get("to")
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 && n <= 10000 {
		d.Page = n
	}
	params.Offset = int32((d.Page - 1) * auditPageSize) //nolint:gosec // G115: page capped at 10000
	rows, err := h.Store.Q.ListAuditLogs(ctx, params)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, l := range rows {
		d.Total = l.TotalCount
		row := view.AuditRow{At: l.CreatedAt, Actor: deref(l.AdminEmail), Action: l.Action}
		if l.Entity != nil {
			row.Target = *l.Entity
			if l.EntityID != nil {
				row.Target += " " + *l.EntityID
			}
		}
		if len(l.Meta) > 0 {
			var compact map[string]any
			if json.Unmarshal(l.Meta, &compact) == nil {
				b, _ := json.Marshal(compact)
				row.Details = string(b)
			}
		}
		d.Rows = append(d.Rows, row)
	}
	d.Pages = max(1, int((d.Total+auditPageSize-1)/auditPageSize))
	pageURL := func(n int) string {
		v := url.Values{}
		for _, k := range []string{"actor", "action", "from", "to"} {
			if q.Get(k) != "" {
				v.Set(k, q.Get(k))
			}
		}
		v.Set("page", strconv.Itoa(n))
		return "/admin/audit?" + v.Encode()
	}
	if d.Page > 1 {
		d.PrevURL = pageURL(d.Page - 1)
	}
	if d.Page < d.Pages {
		d.NextURL = pageURL(d.Page + 1)
	}
	admins, err := h.Store.Q.ListAdminsOverview(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, a := range admins {
		d.Actors = append(d.Actors, view.Option{Value: a.ID.String(), Label: a.Email})
	}
	if d.Actions, err = h.Store.Q.ListAuditActions(ctx); err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminAuditTitle, "audit")
	render(w, r, http.StatusOK, p.Nonce, view.AdminAudit(p, d))
}

var auditActionRe = regexp.MustCompile(`^[a-z_.]{1,64}$`)
