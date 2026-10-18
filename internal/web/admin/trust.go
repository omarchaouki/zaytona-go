package admin

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

func (h *Handlers) trustRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/certificates", h.guard(roleAny, 0, h.Certificates))
	post(mux, "POST /admin/certificates", h.guard(roleOwner, 0, h.SaveCertificate))
	post(mux, "POST /admin/certificates/{id}", h.guard(roleOwner, 0, h.SaveCertificate))
	post(mux, "POST /admin/certificates/{id}/delete", h.guard(roleOwner, 0, h.DeleteCertificate))
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (h *Handlers) renderTrust(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, s *view.FormState) {
	rows, err := h.Store.Q.ListAllCertificationsWithScan(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d := view.TrustData{Form: s, UploadMB: h.uploadMB()}
	for _, c := range rows {
		row := view.CertRow{
			ID: c.ID.String(), Type: string(c.Type), LabelAr: deref(c.LabelAr), LabelFr: deref(c.LabelFr), Number: c.Number,
			Issued: dateString(c.IssuedAt), Expires: dateString(c.ExpiresAt), Public: c.IsPublic, SortOrder: c.SortOrder,
		}
		if c.ScanID != nil && c.ScanWidth != nil && c.ScanHeight != nil {
			row.Scan = view.MediaImage(*c.ScanID, ints32(c.ScanWidths), int(*c.ScanWidth), int(*c.ScanHeight))
			row.HasScan = true
		}
		d.Certs = append(d.Certs, row)
	}
	p := h.page(r, cur, i18n.AdminTrustTitle, "certificates")
	render(w, r, status, p.Nonce, view.AdminTrust(p, d))
}

// Certificates lists the certificates (read-only for staff).
func (h *Handlers) Certificates(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderTrust(w, r, cur, http.StatusOK, nil)
}

var certTypeValues = []string{"onssa", "organic", "igp", "iso", "other"}

// SaveCertificate creates or updates a certificate and its scan (owner).
func (h *Handlers) SaveCertificate(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	var existing *dbq.Certification
	key := "new"
	if raw := r.PathValue("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		c, err := h.Store.Q.GetCertification(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		existing, key = &c, c.ID.String()
	}
	p := h.page(r, cur, i18n.AdminTrustTitle, "certificates")
	f := newForm(r)
	typ := r.PostFormValue("type")
	if !slices.Contains(certTypeValues, typ) {
		f.fail("type", i18n.AdminTrustErrorInvalid)
	}
	number := f.text("number", 80, true, false)
	labelAr := f.text("labelAr", 120, false, false)
	labelFr := f.text("labelFr", 120, false, false)
	issued := f.date("issued")
	expires := f.date("expires")
	if issued != nil && expires != nil && expires.Before(*issued) {
		f.fail("expires", i18n.AdminTrustErrorInvalid)
	}
	if !f.ok() {
		h.renderTrust(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	scan, scanErr, err := h.upload(r, w, cur, "scan", dbq.MediaKindCertificateScan)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if scanErr != "" {
		f.fail("scan", i18n.AdminTrustErrorImage)
		h.renderTrust(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	var scanID, oldScan *string
	if existing != nil {
		scanID, oldScan = existing.ScanID, existing.ScanID
	}
	if scan != nil {
		scanID = &scan.ID
	}
	public := f.check("isPublic")
	var id uuid.UUID
	if existing == nil {
		c, err := h.Store.Q.CreateCertification(ctx, dbq.CreateCertificationParams{
			Type: dbq.CertificationType(typ), LabelAr: opt(labelAr), LabelFr: opt(labelFr), Number: number,
			IssuedAt: issued, ExpiresAt: expires, ScanID: scanID, IsPublic: public,
		})
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		id = c.ID
	} else {
		id = existing.ID
		if err := h.Store.Q.UpdateCertification(ctx, dbq.UpdateCertificationParams{
			ID: id, Type: dbq.CertificationType(typ), LabelAr: opt(labelAr), LabelFr: opt(labelFr), Number: number,
			IssuedAt: issued, ExpiresAt: expires, ScanID: scanID, IsPublic: public, SortOrder: existing.SortOrder,
		}); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	action := "certificate.update"
	if existing == nil {
		action = "certificate.create"
	}
	h.audit(ctx, r, &cur.Admin.ID, action, "certificate", id.String(), map[string]any{
		"type": typ, "number": number, "public": public, "scan": scanID != nil, "new_scan": scan != nil,
	})
	if scan != nil && oldScan != nil {
		h.dropMedia(r, oldScan)
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/certificates?done=saved", http.StatusSeeOther)
}

// DeleteCertificate removes a certificate and its scan (owner, confirmed).
func (h *Handlers) DeleteCertificate(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := h.Store.Q.GetCertification(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminCommonDeleteTitle, "certificates")
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminCommonDeleteTitle), p.T(i18n.AdminTrustDeleteConfirm), "/admin/certificates", nil))
		return
	}
	if err := h.Store.Q.DeleteCertification(ctx, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "certificate.delete", "certificate", id.String(), map[string]any{"type": string(c.Type), "number": c.Number})
	h.dropMedia(r, c.ScanID)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/certificates?done=removed", http.StatusSeeOther)
}
