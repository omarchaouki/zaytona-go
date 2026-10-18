package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

func (h *Handlers) contentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/content", h.guard(roleAny, 0, h.Content))
	post(mux, "POST /admin/content/{block}", h.guard(roleOwner, 0, h.SaveContent))
	post(mux, "POST /admin/gallery", h.guard(roleOwner, 0, h.AddGalleryImage))
	post(mux, "POST /admin/gallery/{id}", h.guard(roleOwner, 0, h.SaveGalleryCaption))
	post(mux, "POST /admin/gallery/{id}/move", h.guard(roleOwner, 0, h.MoveGalleryImage))
	post(mux, "POST /admin/gallery/{id}/delete", h.guard(roleOwner, 0, h.DeleteGalleryImage))
	mux.HandleFunc("GET /admin/faq", h.guard(roleAny, 0, h.FAQ))
	post(mux, "POST /admin/faq", h.guard(roleOwner, 0, h.SaveFAQ))
	post(mux, "POST /admin/faq/{id}", h.guard(roleOwner, 0, h.SaveFAQ))
	post(mux, "POST /admin/faq/{id}/move", h.guard(roleOwner, 0, h.MoveFAQ))
	post(mux, "POST /admin/faq/{id}/delete", h.guard(roleOwner, 0, h.DeleteFAQ))
}

// block decodes a content block in both languages (missing: zero values).
func (h *Handlers) block(ctx context.Context, key string, ar, fr any) error {
	b, err := h.Store.Q.GetContentBlock(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal(b.ValueAr, ar)
	_ = json.Unmarshal(b.ValueFr, fr)
	return nil
}

func (h *Handlers) renderContent(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, s *view.FormState) {
	ctx := r.Context()
	d := view.ContentData{Form: s, UploadMB: h.uploadMB()}
	for _, b := range []struct {
		key    string
		ar, fr any
	}{
		{"hero", &d.HeroAr, &d.HeroFr},
		{"story", &d.StoryAr, &d.StoryFr},
		{"policies", &d.PoliciesAr, &d.PoliciesFr},
		{"page_privacy", &d.PrivacyAr, &d.PrivacyFr},
		{"page_terms", &d.TermsAr, &d.TermsFr},
	} {
		if err := h.block(ctx, b.key, b.ar, b.fr); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	gallery, err := h.Store.Q.ListGalleryImages(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	for _, g := range gallery {
		d.Gallery = append(d.Gallery, view.GalleryRow{
			ID: g.ID.String(), CaptionAr: deref(g.CaptionAr), CaptionFr: deref(g.CaptionFr),
			Image: view.MediaImage(g.MediaID, ints32(g.Widths), int(g.Width), int(g.Height)),
		})
	}
	p := h.page(r, cur, i18n.AdminContentTitle, "content")
	render(w, r, status, p.Nonce, view.AdminContent(p, d))
}

// Content renders the texts and the gallery (read-only for staff).
func (h *Handlers) Content(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderContent(w, r, cur, http.StatusOK, nil)
}

// SaveContent stores one block in both languages (empty fields fall back to
// the default texts on the site).
func (h *Handlers) SaveContent(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	key := r.PathValue("block")
	f := newForm(r)
	var ar, fr any
	switch key {
	case "hero":
		ar = domain.HeroContent{Eyebrow: f.text("eyebrow_ar", 120, false, false), Title: f.text("title_ar", 160, false, false), Subtitle: f.text("subtitle_ar", 400, false, true)}
		fr = domain.HeroContent{Eyebrow: f.text("eyebrow_fr", 120, false, false), Title: f.text("title_fr", 160, false, false), Subtitle: f.text("subtitle_fr", 400, false, true)}
	case "story":
		ar = domain.StoryContent{Title: f.text("title_ar", 160, false, false), Body: f.text("body_ar", 4000, false, true)}
		fr = domain.StoryContent{Title: f.text("title_fr", 160, false, false), Body: f.text("body_fr", 4000, false, true)}
	case "policies":
		ar = domain.PoliciesContent{COD: f.text("cod_ar", 600, false, true), Delivery: f.text("delivery_ar", 600, false, true), Returns: f.text("returns_ar", 600, false, true), Privacy: f.text("privacy_ar", 600, false, true)}
		fr = domain.PoliciesContent{COD: f.text("cod_fr", 600, false, true), Delivery: f.text("delivery_fr", 600, false, true), Returns: f.text("returns_fr", 600, false, true), Privacy: f.text("privacy_fr", 600, false, true)}
	case "page_privacy", "page_terms":
		reviewed := f.check("reviewed")
		ar = domain.PageContent{Title: f.text("title_ar", 160, true, false), HTML: view.CleanRichText(f.text("html_ar", 30000, true, true)), Reviewed: reviewed}
		fr = domain.PageContent{Title: f.text("title_fr", 160, true, false), HTML: view.CleanRichText(f.text("html_fr", 30000, true, true)), Reviewed: reviewed}
	default:
		http.NotFound(w, r)
		return
	}
	if !f.ok() {
		p := h.page(r, cur, i18n.AdminContentTitle, "content")
		h.renderContent(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	arJSON, err := json.Marshal(ar)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	frJSON, err := json.Marshal(fr)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	err = h.Store.InTx(ctx, func(q *dbq.Queries) error {
		if err := q.UpsertContentBlock(ctx, dbq.UpsertContentBlockParams{Key: key, ValueAr: arJSON, ValueFr: frJSON, UpdatedBy: uuidPtr(cur)}); err != nil {
			return err
		}
		return store.Audit(ctx, q, actor(r, h, cur), "content.update", "content", key, map[string]any{"size_ar": len(arJSON), "size_fr": len(frJSON)})
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/content?done=saved#"+key, http.StatusSeeOther) //nolint:gosec // G710: key is one of the fixed block names
}

// AddGalleryImage uploads a photo into the gallery (last position).
func (h *Handlers) AddGalleryImage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	p := h.page(r, cur, i18n.AdminContentTitle, "content")
	f := newForm(r)
	capAr := f.text("captionAr", 200, false, false)
	capFr := f.text("captionFr", 200, false, false)
	if !f.ok() {
		h.renderContent(w, r, cur, http.StatusUnprocessableEntity, formState("gallery", f, p))
		return
	}
	m, photoErr, err := h.upload(r, w, cur, "photo", dbq.MediaKindGallery)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if photoErr == "" && m == nil {
		photoErr = i18n.AdminCommonRequired
	}
	if photoErr != "" {
		f.fail("photo", photoErr)
		h.renderContent(w, r, cur, http.StatusUnprocessableEntity, formState("gallery", f, p))
		return
	}
	g, err := h.Store.Q.CreateGalleryImage(ctx, dbq.CreateGalleryImageParams{MediaID: m.ID, CaptionAr: optText(capAr), CaptionFr: optText(capFr)})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "gallery.add", "gallery", g.ID.String(), nil)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/content?done=saved#gallery", http.StatusSeeOther)
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h *Handlers) galleryItem(w http.ResponseWriter, r *http.Request) (dbq.GalleryImage, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return dbq.GalleryImage{}, false
	}
	g, err := h.Store.Q.GetGalleryImage(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return g, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return g, false
	}
	return g, true
}

// SaveGalleryCaption updates the two captions.
func (h *Handlers) SaveGalleryCaption(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	g, ok := h.galleryItem(w, r)
	if !ok {
		return
	}
	p := h.page(r, cur, i18n.AdminContentTitle, "content")
	f := newForm(r)
	capAr := f.text("captionAr", 200, false, false)
	capFr := f.text("captionFr", 200, false, false)
	if !f.ok() {
		h.renderContent(w, r, cur, http.StatusUnprocessableEntity, formState(g.ID.String(), f, p))
		return
	}
	if err := h.Store.Q.UpdateGalleryCaption(r.Context(), dbq.UpdateGalleryCaptionParams{ID: g.ID, CaptionAr: optText(capAr), CaptionFr: optText(capFr)}); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(r.Context(), r, &cur.Admin.ID, "gallery.caption", "gallery", g.ID.String(), nil)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/content?done=saved#gallery", http.StatusSeeOther)
}

// moved returns ids in their new order after moving id one place up or down.
func moved(ids []uuid.UUID, id uuid.UUID, up bool) []uuid.UUID {
	i := slices.Index(ids, id)
	if i < 0 {
		return ids
	}
	j := i + 1
	if up {
		j = i - 1
	}
	if j < 0 || j >= len(ids) {
		return ids
	}
	out := slices.Clone(ids)
	out[i], out[j] = out[j], out[i]
	return out
}

// MoveGalleryImage moves a photo one place up or down (positions renumbered).
func (h *Handlers) MoveGalleryImage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	g, ok := h.galleryItem(w, r)
	if !ok {
		return
	}
	err := h.Store.InTx(ctx, func(q *dbq.Queries) error {
		rows, err := q.ListGalleryImages(ctx)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, x := range rows {
			ids[i] = x.ID
		}
		for i, id := range moved(ids, g.ID, r.PostFormValue("dir") == "up") {
			if err := q.SetGallerySortOrder(ctx, dbq.SetGallerySortOrderParams{ID: id, SortOrder: int32(i)}); err != nil { //nolint:gosec // G115: a few dozen photos
				return err
			}
		}
		return store.Audit(ctx, q, actor(r, h, cur), "gallery.move", "gallery", g.ID.String(), map[string]any{"dir": r.PostFormValue("dir")})
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/content#gallery", http.StatusSeeOther)
}

// DeleteGalleryImage removes a photo and its files (confirmed).
func (h *Handlers) DeleteGalleryImage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	g, ok := h.galleryItem(w, r)
	if !ok {
		return
	}
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminCommonDeleteTitle, "content")
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminCommonDeleteTitle), p.T(i18n.AdminGalleryDeleteBody), "/admin/content#gallery", nil))
		return
	}
	if err := h.Store.Q.DeleteGalleryImage(r.Context(), g.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(r.Context(), r, &cur.Admin.ID, "gallery.delete", "gallery", g.ID.String(), nil)
	h.dropMedia(r, &g.MediaID)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/content?done=removed#gallery", http.StatusSeeOther)
}

// ---------------------------------------------------------------- FAQ

func (h *Handlers) renderFAQ(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, s *view.FormState) {
	rows, err := h.Store.Q.ListAllFaqs(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d := view.FAQData{Form: s}
	for _, f := range rows {
		d.Items = append(d.Items, view.FAQRow{
			ID: f.ID.String(), QuestionAr: f.QuestionAr, QuestionFr: f.QuestionFr, AnswerAr: f.AnswerAr, AnswerFr: f.AnswerFr,
			Active: f.IsActive, Demo: f.IsDemo,
		})
	}
	p := h.page(r, cur, i18n.AdminFaqTitle, "faq")
	render(w, r, status, p.Nonce, view.AdminFAQ(p, d))
}

// FAQ lists the questions (read-only for staff).
func (h *Handlers) FAQ(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderFAQ(w, r, cur, http.StatusOK, nil)
}

func (h *Handlers) faqItem(w http.ResponseWriter, r *http.Request) (dbq.Faq, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return dbq.Faq{}, false
	}
	f, err := h.Store.Q.GetFaq(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return f, false
	}
	if err != nil {
		h.serverError(w, r, err)
		return f, false
	}
	return f, true
}

// SaveFAQ creates (no id) or updates a question.
func (h *Handlers) SaveFAQ(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	var existing *dbq.Faq
	key := "new"
	if r.PathValue("id") != "" {
		f, ok := h.faqItem(w, r)
		if !ok {
			return
		}
		existing, key = &f, f.ID.String()
	}
	p := h.page(r, cur, i18n.AdminFaqTitle, "faq")
	f := newForm(r)
	qAr := f.text("questionAr", 300, true, true)
	qFr := f.text("questionFr", 300, true, true)
	aAr := f.text("answerAr", 3000, true, true)
	aFr := f.text("answerFr", 3000, true, true)
	active := f.check("isActive")
	if !f.ok() {
		h.renderFAQ(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	var id uuid.UUID
	if existing == nil {
		n, err := h.Store.Q.CountFaqs(ctx)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		row, err := h.Store.Q.CreateFaq(ctx, dbq.CreateFaqParams{
			QuestionAr: qAr, QuestionFr: qFr, AnswerAr: aAr, AnswerFr: aFr, SortOrder: int32(n), IsActive: active, //nolint:gosec // G115: a few dozen questions
		})
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		id = row.ID
	} else {
		id = existing.ID
		if err := h.Store.Q.UpdateFaq(ctx, dbq.UpdateFaqParams{ID: id, QuestionAr: qAr, QuestionFr: qFr, AnswerAr: aAr, AnswerFr: aFr, IsActive: active}); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	action := "faq.update"
	if existing == nil {
		action = "faq.create"
	}
	h.audit(ctx, r, &cur.Admin.ID, action, "faq", id.String(), map[string]any{"active": active})
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/faq?done=saved", http.StatusSeeOther)
}

// MoveFAQ moves a question one place up or down.
func (h *Handlers) MoveFAQ(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	item, ok := h.faqItem(w, r)
	if !ok {
		return
	}
	err := h.Store.InTx(ctx, func(q *dbq.Queries) error {
		rows, err := q.ListAllFaqs(ctx)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, x := range rows {
			ids[i] = x.ID
		}
		for i, id := range moved(ids, item.ID, r.PostFormValue("dir") == "up") {
			if err := q.SetFaqSortOrder(ctx, dbq.SetFaqSortOrderParams{ID: id, SortOrder: int32(i)}); err != nil { //nolint:gosec // G115: a few dozen questions
				return err
			}
		}
		return store.Audit(ctx, q, actor(r, h, cur), "faq.move", "faq", item.ID.String(), map[string]any{"dir": r.PostFormValue("dir")})
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/faq", http.StatusSeeOther)
}

// DeleteFAQ removes a question (confirmed).
func (h *Handlers) DeleteFAQ(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	item, ok := h.faqItem(w, r)
	if !ok {
		return
	}
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminCommonDeleteTitle, "faq")
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminCommonDeleteTitle), p.T(i18n.AdminFaqDeleteBody), "/admin/faq", nil))
		return
	}
	if err := h.Store.Q.DeleteFaq(r.Context(), item.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(r.Context(), r, &cur.Admin.ID, "faq.delete", "faq", item.ID.String(), nil)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/faq?done=removed", http.StatusSeeOther)
}
