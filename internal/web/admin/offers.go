package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// catalogueRoutes registers offers, settings, certificates, FAQ, content,
// admins and the audit log. Reading is open to staff where the reference
// allowed it; every change is owner-only and audited.
func (h *Handlers) catalogueRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/offers", h.guard(roleAny, 0, h.Offers))
	post(mux, "POST /admin/offers/pricing", h.guard(roleOwner, 0, h.SavePricing))
	post(mux, "POST /admin/offers", h.guard(roleOwner, 0, h.SaveOffer))
	post(mux, "POST /admin/offers/{id}", h.guard(roleOwner, 0, h.SaveOffer))
	post(mux, "POST /admin/offers/{id}/delete", h.guard(roleOwner, 0, h.DeleteOffer))
	h.settingsRoutes(mux)
	h.trustRoutes(mux)
	h.contentRoutes(mux)
	h.adminsRoutes(mux)
	h.reviewsRoutes(mux)
}

// settings reads every group fresh from the database (the site cache may be a minute old).
func (h *Handlers) settings(ctx context.Context) (domain.Settings, error) {
	rows, err := h.Store.Q.ListSettings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	m := map[string][]byte{}
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return domain.SettingsFromRows(m), nil
}

// saveSetting stores one group in q and writes the audit row with the old and
// new values (phone numbers masked).
func (h *Handlers) saveSetting(ctx context.Context, q *dbq.Queries, r *http.Request, cur *auth.Current, key string, before, after any) error {
	value, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if err := q.UpsertSetting(ctx, dbq.UpsertSettingParams{Key: key, Value: value, UpdatedBy: uuidPtr(cur)}); err != nil {
		return err
	}
	return store.Audit(ctx, q, actor(r, h, cur), "settings."+key, "settings", key, map[string]any{"before": maskSettings(before), "after": maskSettings(after)})
}

func (h *Handlers) offersData(r *http.Request) (view.OffersData, error) {
	ctx := r.Context()
	s, err := h.settings(ctx)
	if err != nil {
		return view.OffersData{}, err
	}
	rows, err := h.Store.Q.ListAllOffersWithImage(ctx)
	if err != nil {
		return view.OffersData{}, err
	}
	pr := s.Pricing
	d := view.OffersData{
		CustomEnabled: pr.CustomEnabled, CustomMin: strconv.Itoa(pr.CustomMinLiters), CustomMax: strconv.Itoa(pr.CustomMaxLiters),
		PriceIsDemo: pr.IsDemo, UploadMB: h.uploadMB(),
	}
	if pr.PricePerLiter.Valid {
		v := pr.PricePerLiter.Decimal
		d.PerLiter = &v
		d.PricePerLiter = v.StringFixed(2)
	}
	for _, o := range rows {
		row := view.OfferRow{
			ID: o.ID.String(), NameAr: o.NameAr, NameFr: o.NameFr, TaglineAr: deref(o.TaglineAr), TaglineFr: deref(o.TaglineFr),
			Liters: o.Liters, Price: o.PriceMad, AutoPrice: o.AutoPrice, Active: o.IsActive, Demo: o.IsDemo, SortOrder: o.SortOrder,
		}
		if o.ImageID != nil && o.ImageWidth != nil && o.ImageHeight != nil {
			row.Image = view.MediaImage(*o.ImageID, ints32(o.ImageWidths), int(*o.ImageWidth), int(*o.ImageHeight))
			row.HasImage = true
		}
		d.Offers = append(d.Offers, row)
	}
	return d, nil
}

func ints32(v []int32) []int {
	out := make([]int, len(v))
	for i, x := range v {
		out[i] = int(x)
	}
	return out
}

// Offers renders the page.
func (h *Handlers) Offers(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderOffers(w, r, cur, http.StatusOK, nil)
}

func (h *Handlers) renderOffers(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, s *view.FormState) {
	d, err := h.offersData(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Form = s
	p := h.page(r, cur, i18n.AdminOffersTitle, "offers")
	if q := r.URL.Query(); q.Get("done") == "pricing" {
		p.Flash = p.T(i18n.AdminOffersSaved, "count", q.Get("n"))
	}
	render(w, r, status, p.Nonce, view.AdminOffers(p, d))
}

// failForm shows the page again with the posted values and the errors.
func formState(key string, f *form, p view.AdminPage) *view.FormState {
	return &view.FormState{Key: key, Values: f.values(), Errors: f.messages(p)}
}

// SavePricing stores the price per litre and the "other quantity" limits,
// then re-prices every automatic offer.
func (h *Handlers) SavePricing(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	f := newForm(r)
	price := f.decimal("pricePerLiter", 0, 100000, true)
	if price != nil && !price.IsPositive() {
		f.fail("pricePerLiter", i18n.AdminCommonNumberInvalid)
	}
	minL := f.integer("customMin", 1, 999, 0)
	maxL := f.integer("customMax", 1, 999, 0)
	if f.r.PostFormValue("customMin") == "" {
		f.fail("customMin", i18n.AdminCommonRequired)
	}
	if f.r.PostFormValue("customMax") == "" {
		f.fail("customMax", i18n.AdminCommonRequired)
	}
	if f.ok() && minL > maxL {
		f.fail("customMax", i18n.AdminOffersRangeError)
	}
	if !f.ok() {
		p := h.page(r, cur, i18n.AdminOffersTitle, "offers")
		h.renderOffers(w, r, cur, http.StatusUnprocessableEntity, formState("pricing", f, p))
		return
	}
	s, err := h.settings(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	before := s.Pricing
	after := domain.PricingSettings{
		PricePerLiter: decimal.NullDecimal{Decimal: *price, Valid: true}, CustomEnabled: f.check("customEnabled"),
		CustomMinLiters: minL, CustomMaxLiters: maxL,
	}
	var repriced int64
	err = h.Store.InTx(ctx, func(q *dbq.Queries) error {
		if err := h.saveSetting(ctx, q, r, cur, "pricing", before, after); err != nil {
			return err
		}
		repriced, err = q.RepriceAutoOffers(ctx, *price)
		return err
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/offers?done=pricing&n="+strconv.FormatInt(repriced, 10), http.StatusSeeOther)
}

// SaveOffer creates (no id) or updates an offer, with an optional photo.
func (h *Handlers) SaveOffer(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	var existing *dbq.Offer
	key := "new"
	if raw := r.PathValue("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		o, err := h.Store.Q.GetOffer(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		existing, key = &o, o.ID.String()
	}
	p := h.page(r, cur, i18n.AdminOffersTitle, "offers")
	f := newForm(r)
	liters := f.decimal("liters", 0, 999, true)
	if liters != nil && !liters.IsPositive() {
		f.fail("liters", i18n.AdminCommonNumberInvalid)
	}
	sortOrder := f.integer("sortOrder", 0, 1000, 100)
	nameAr := f.text("nameAr", 120, true, false)
	nameFr := f.text("nameFr", 120, true, false)
	taglineAr := f.text("taglineAr", 200, false, false)
	taglineFr := f.text("taglineFr", 200, false, false)
	auto := r.PostFormValue("mode") != "fixed"
	var price decimal.Decimal
	s, err := h.settings(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if auto {
		if !s.Pricing.PricePerLiter.Valid {
			f.fail("form", i18n.AdminOffersErrorNoPrice)
		} else if liters != nil {
			// Same rounding as RepriceAutoOffers.
			price = liters.Mul(s.Pricing.PricePerLiter.Decimal).Round(2)
		}
	} else if v := f.decimal("price", 0, 1000000, true); v != nil {
		price = *v
	}
	if !f.ok() {
		h.renderOffers(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	photo, photoErr, err := h.upload(r, w, cur, "photo", dbq.MediaKindOffer)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if photoErr != "" {
		f.fail("photo", photoErr)
		h.renderOffers(w, r, cur, http.StatusUnprocessableEntity, formState(key, f, p))
		return
	}
	var imageID *string
	if existing != nil {
		imageID = existing.ImageID
	}
	oldImage := imageID
	if photo != nil {
		imageID = &photo.ID
	} else if f.check("removePhoto") {
		imageID = nil
	}
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	var saved dbq.Offer
	if existing == nil {
		slug := "huile-olive-" + strings.ReplaceAll(liters.String(), ".", "-") + "l-" + strconv.FormatInt(time.Now().UnixNano()/1e6, 36)
		saved, err = h.Store.Q.CreateOffer(ctx, dbq.CreateOfferParams{
			Slug: slug, NameAr: nameAr, NameFr: nameFr, TaglineAr: opt(taglineAr), TaglineFr: opt(taglineFr),
			Liters: *liters, AutoPrice: auto, PriceMad: price, ImageID: imageID, StockStatus: dbq.StockStatusInStock,
			IsActive: f.check("isActive"), SortOrder: int32(sortOrder), //nolint:gosec // G115: 0..1000
		})
	} else {
		saved, err = h.Store.Q.UpdateOffer(ctx, dbq.UpdateOfferParams{
			ID: existing.ID, Slug: existing.Slug, NameAr: nameAr, NameFr: nameFr, TaglineAr: opt(taglineAr), TaglineFr: opt(taglineFr),
			Liters: *liters, AutoPrice: auto, PriceMad: price, ImageID: imageID, StockStatus: existing.StockStatus,
			IsActive: f.check("isActive"), SortOrder: int32(sortOrder), //nolint:gosec // G115: 0..1000
		})
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	action := "offer.update"
	if existing == nil {
		action = "offer.create"
	}
	h.audit(ctx, r, &cur.Admin.ID, action, "offer", saved.ID.String(), map[string]any{
		"liters": saved.Liters.String(), "price": saved.PriceMad.StringFixed(2), "auto": saved.AutoPrice,
		"active": saved.IsActive, "photo": imageID != nil,
	})
	if oldImage != nil && (imageID == nil || *imageID != *oldImage) {
		h.dropMedia(r, oldImage)
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/offers?done=saved", http.StatusSeeOther)
}

// DeleteOffer removes an offer (past orders keep their copied lines).
func (h *Handlers) DeleteOffer(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	o, err := h.Store.Q.GetOffer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if r.PostFormValue("confirm") != "1" {
		p := h.page(r, cur, i18n.AdminCommonDeleteTitle, "offers")
		render(w, r, http.StatusOK, p.Nonce, view.AdminConfirm(p, r.URL.Path, p.T(i18n.AdminCommonDeleteTitle), p.T(i18n.AdminOffersDeleteConfirm), "/admin/offers", nil))
		return
	}
	if err := h.Store.Q.DeleteOffer(ctx, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.audit(ctx, r, &cur.Admin.ID, "offer.delete", "offer", id.String(), map[string]any{"liters": o.Liters.String()})
	h.dropMedia(r, o.ImageID)
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/offers?done=removed", http.StatusSeeOther)
}
