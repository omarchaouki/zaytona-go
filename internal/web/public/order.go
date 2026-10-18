package public

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/httpx"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/notify"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// Order limits (SECURITY.md section 10): per IP and per phone.
const (
	orderLimit       = 5
	orderLimitWindow = 10 * time.Minute
	powLimit         = 30
	confirmationTTL  = 30 * 24 * time.Hour
)

var referenceRE = regexp.MustCompile(`^OL-[0-9]{4}-[0-9]{6,}$`)

// orderOutcome is what a submission produced, rendered as JSON (fetch) or a page.
type orderOutcome struct {
	Status    string            `json:"status"` // created, invalid, duplicate, unavailable, rate_limited, rejected
	Redirect  string            `json:"redirect,omitempty"`
	Errors    map[string]string `json:"errors,omitempty"` // field -> translated message
	FormError string            `json:"formError,omitempty"`
	Duplicate string            `json:"duplicate,omitempty"`
	Step      int               `json:"step"`
}

// PlaceOrder handles POST /{locale}/order. A plain form post gets a full page
// (303 to the confirmation, or the landing page with the form and its errors);
// the form script asks for JSON (Accept: application/json).
func (h *Handlers) PlaceOrder(w http.ResponseWriter, r *http.Request) {
	locale, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "", http.StatusRequestEntityTooLarge)
		return
	}
	d, err := h.Site.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	in := domain.OrderInput{
		Choice:       r.PostForm.Get("offer"),
		CustomLiters: r.PostForm.Get("liters"),
		Name:         r.PostForm.Get("name"),
		Phone:        r.PostForm.Get("phone"),
		City:         r.PostForm.Get("city"),
		Address:      r.PostForm.Get("address"),
		Note:         r.PostForm.Get("note"),
	}
	out := h.placeOrder(r, locale, d, in)

	w.Header().Set("Cache-Control", "no-store")
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	if out.Status == "created" {
		http.Redirect(w, r, out.Redirect, http.StatusSeeOther)
		return
	}
	// Full page again, with the values, the errors and the step to fix.
	p, data := h.landingPage(r, locale, d)
	f := h.orderForm(locale, d, in)
	f.Errors, f.FormError, f.Duplicate, f.Step = out.Errors, out.FormError, out.Duplicate, out.Step
	render(w, r, http.StatusUnprocessableEntity, p.Nonce, view.Landing(p, data, f))
}

func (h *Handlers) placeOrder(r *http.Request, l i18n.Locale, d *store.SiteData, in domain.OrderInput) orderOutcome {
	ctx := r.Context()
	reject := func(status string, key i18n.Key, step int) orderOutcome {
		return orderOutcome{Status: status, FormError: i18n.T(l, key), Step: step}
	}

	v, ferrs := in.Validate()
	if len(ferrs) > 0 {
		return h.invalid(l, ferrs)
	}

	// Bots: honeypot filled, or posted faster than a person can type.
	if r.PostForm.Get("website") != "" || h.FormTokens.Check(r.PostForm.Get("form_token")) != security.FormTokenOK {
		return reject("rejected", i18n.OrderFormErrorRejected, 2)
	}
	if h.Turnstile != nil && !h.Turnstile.Verify(ctx, r.PostForm.Get("cf-turnstile-response"), httpx.ClientIP(ctx)) {
		return reject("rejected", i18n.OrderFormErrorRejected, 2)
	}
	if pow, err := h.Pow.Check(ctx, r.PostForm.Get("altcha")); err != nil || pow != security.PowOK {
		if err != nil {
			h.Logger.ErrorContext(ctx, "pow check", "err", err)
		}
		return reject("rejected", i18n.OrderFormErrorRejected, 2)
	}

	ipKey := "order:ip:" + hex.EncodeToString(h.IPHash.Sum(httpx.ClientIP(ctx)))
	phoneKey := "order:phone:" + hex.EncodeToString(h.PhoneHash.Sum(v.PhoneE164))
	for _, key := range []string{ipKey, phoneKey} {
		allowed, err := h.Limiter.Allow(ctx, key, orderLimit, orderLimitWindow)
		if err != nil {
			h.Logger.ErrorContext(ctx, "order rate limit", "err", err)
			return reject("rejected", i18n.OrderFormErrorRejected, 2)
		}
		if !allowed {
			return reject("rate_limited", i18n.OrderFormErrorRateLimited, 2)
		}
	}

	// Prices from the database only.
	lines, dupOffer, ferr, unavailable := h.priceOrder(r, d, v)
	switch {
	case unavailable:
		return reject("unavailable", i18n.OrderFormErrorUnavailable, 0)
	case ferr != nil:
		return h.invalid(l, ferr)
	}
	totals := domain.ComputeTotals(lines, d.Settings.Delivery)

	var attribution map[string]string
	if raw := r.PostForm.Get("attribution"); raw != "" && len(raw) < 4096 {
		var m map[string]string
		if json.Unmarshal([]byte(raw), &m) == nil {
			attribution = domain.CleanAttribution(m)
		}
	}
	res, err := h.Store.PlaceOrder(ctx, store.NewOrder{
		Order:            v,
		Lines:            lines,
		Totals:           totals,
		Locale:           dbq.Locale(l),
		Attribution:      attribution,
		IPHash:           h.IPHash.Sum(httpx.ClientIP(ctx)),
		DuplicateOfferID: dupOffer,
		ConfirmDuplicate: r.PostForm.Get("confirm_duplicate") == "1",
	})
	if err != nil {
		h.Logger.ErrorContext(ctx, "place order", "request_id", httpx.RequestID(ctx), "err", err)
		return reject("rejected", i18n.OrderFormErrorRejected, 2)
	}
	if res.Duplicate != nil {
		return orderOutcome{
			Status: "duplicate",
			Step:   2,
			Duplicate: i18n.T(l, i18n.OrderFormDuplicateBody,
				"minutes", strconv.Itoa(res.Duplicate.MinutesAgo), "reference", res.Duplicate.Reference),
		}
	}

	o := res.Created
	h.Logger.InfoContext(ctx, "order placed", "reference", o.Reference, "phone", security.RedactPhone(o.PhoneE164))
	if h.Notifier != nil {
		h.Notifier.Enqueue(notifyOrder(o, lines, l))
	}
	return orderOutcome{
		Status:   "created",
		Redirect: "/" + string(l) + "/order/" + o.Reference + "?t=" + url.QueryEscape(h.Confirm.For(o.Reference)),
	}
}

func (h *Handlers) invalid(l i18n.Locale, ferrs domain.FieldErrors) orderOutcome {
	out := orderOutcome{Status: "invalid", Errors: map[string]string{}, Step: 2, FormError: i18n.T(l, i18n.OrderFormErrorSummary)}
	for field, key := range ferrs {
		out.Errors[field] = i18n.T(l, i18n.Key(key))
		out.Step = min(out.Step, domain.FieldStep(field))
	}
	return out
}

// priceOrder turns the choice into a priced line. dupOffer scopes the duplicate
// guard (an offer id, or "" for a custom quantity: any order from that phone).
func (h *Handlers) priceOrder(r *http.Request, d *store.SiteData, v domain.ValidOrder) (lines []domain.OrderLine, dupOffer string, ferr domain.FieldErrors, unavailable bool) {
	pr := d.Settings.Pricing
	if v.Choice == domain.CustomChoice {
		if !pr.CustomEnabled || !pr.PricePerLiter.Valid || v.CustomLiters < pr.CustomMinLiters || v.CustomLiters > pr.CustomMaxLiters {
			return nil, "", domain.FieldErrors{"customLiters": "validation.litersRange"}, false
		}
		n := strconv.Itoa(v.CustomLiters)
		return []domain.OrderLine{{
			NameAr:    i18n.T(i18n.AR, i18n.OrderFormCustomLine, "liters", n),
			NameFr:    i18n.T(i18n.FR, i18n.OrderFormCustomLine, "liters", n),
			Liters:    decimal.NewFromInt(int64(v.CustomLiters)),
			UnitPrice: domain.LitersPrice(v.CustomLiters, pr.PricePerLiter.Decimal),
			Quantity:  1,
		}}, "", nil, false
	}
	offer, err := h.Store.Q.GetOfferBySlug(r.Context(), v.Choice)
	if err != nil {
		if !errors.Is(store.NotFound(err), store.ErrNotFound) {
			h.Logger.ErrorContext(r.Context(), "offer lookup", "err", err)
		}
		return nil, "", nil, true
	}
	if !offer.IsActive || offer.StockStatus == dbq.StockStatusOutOfStock {
		return nil, "", nil, true
	}
	size := func(loc i18n.Locale) string {
		return domain.FormatNumber(offer.Liters) + " " + i18n.T(loc, i18n.OrderFormLitersUnit)
	}
	return []domain.OrderLine{{
		OfferID:   offer.ID.String(),
		NameAr:    offer.NameAr + " " + size(i18n.AR),
		NameFr:    offer.NameFr + " " + size(i18n.FR),
		Liters:    offer.Liters,
		UnitPrice: offer.PriceMad,
		Quantity:  1,
	}}, offer.ID.String(), nil, false
}

// orderForm builds the form state (blank, or with submitted values).
func (h *Handlers) orderForm(l i18n.Locale, d *store.SiteData, in domain.OrderInput) view.OrderFormData {
	loc := string(l)
	s := d.Settings
	f := view.OrderFormData{
		FormToken:    h.FormTokens.Issue(),
		Choice:       in.Choice,
		CustomLiters: in.CustomLiters,
		Name:         in.Name,
		Phone:        in.Phone,
		City:         in.City,
		Address:      in.Address,
		Note:         in.Note,
		DeliveryMode: s.Delivery.Mode,
	}
	if s.Delivery.Fee.Valid {
		f.DeliveryFee = s.Delivery.Fee.Decimal.String()
	}
	if s.Delivery.FreeAbove.Valid {
		f.DeliveryFreeAbove = s.Delivery.FreeAbove.Decimal.String()
	}
	if h.Config.TurnstileSite != "" {
		f.TurnstileSiteKey = h.Config.TurnstileSite
	}
	unit := i18n.T(l, i18n.OrderFormLitersUnit)
	var available []string
	for _, o := range d.Offers {
		name := o.NameFr
		if l == i18n.AR {
			name = o.NameAr
		}
		out := o.StockStatus == dbq.StockStatusOutOfStock
		f.Offers = append(f.Offers, view.FormOffer{
			Slug:        o.Slug,
			SizeLabel:   domain.FormatNumber(o.Liters) + " " + unit,
			Name:        name,
			Price:       domain.FormatMAD(o.PriceMad, loc),
			PriceRaw:    o.PriceMad.StringFixed(2),
			Unavailable: out,
		})
		if !out {
			available = append(available, o.Slug)
		}
	}
	pr := s.Pricing
	if pr.CustomEnabled && pr.PricePerLiter.Valid {
		ppl := domain.FormatMAD(pr.PricePerLiter.Decimal, loc)
		f.Custom = &view.FormCustom{
			Min: pr.CustomMinLiters, Max: pr.CustomMaxLiters,
			PerLiter: ppl, PerLiterRaw: pr.PricePerLiter.Decimal.StringFixed(2),
			PerLiterLabel: i18n.T(l, i18n.ProductsPerLiter, "price", ppl),
			Hint: i18n.T(l, i18n.OrderFormLitersHint, "min", strconv.Itoa(pr.CustomMinLiters),
				"max", strconv.Itoa(pr.CustomMaxLiters), "price", ppl),
		}
	}
	// A single option is selected by default.
	if f.Choice == "" && len(available) == 1 && f.Custom == nil {
		f.Choice = available[0]
	}

	// Server-side totals for the current choice (the script keeps them live).
	var lines []domain.OrderLine
	for _, o := range d.Offers {
		if o.Slug == f.Choice && o.StockStatus != dbq.StockStatusOutOfStock {
			lines = append(lines, domain.OrderLine{UnitPrice: o.PriceMad, Quantity: 1})
			f.Lines = append(f.Lines, view.FormLine{Label: domain.FormatNumber(o.Liters) + " " + unit, Amount: domain.FormatMAD(o.PriceMad, loc), Quantity: 1})
		}
	}
	if f.Choice == domain.CustomChoice && f.Custom != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(f.CustomLiters)); err == nil && n > 0 && n <= 999 {
			price := domain.LitersPrice(n, pr.PricePerLiter.Decimal)
			lines = append(lines, domain.OrderLine{UnitPrice: price, Quantity: 1})
			f.Lines = append(f.Lines, view.FormLine{Label: i18n.T(l, i18n.OrderFormCustomLine, "liters", strconv.Itoa(n)), Amount: domain.FormatMAD(price, loc), Quantity: 1})
		}
	}
	tot := domain.ComputeTotals(lines, s.Delivery)
	f.Subtotal = domain.FormatMAD(tot.Subtotal, loc)
	f.Total = domain.FormatMAD(tot.Total, loc)
	f.DeliveryText, f.TotalLabel = deliveryLabels(l, tot.DeliveryFee)
	f.ScriptMessages = scriptMessages(l)
	return f
}

// deliveryLabels: the delivery value and the label of the total.
func deliveryLabels(l i18n.Locale, fee decimal.NullDecimal) (delivery, totalLabel string) {
	switch {
	case !fee.Valid:
		return i18n.T(l, i18n.OrderFormDeliveryConfirm), i18n.T(l, i18n.OrderFormTotalWithoutDelivery)
	case fee.Decimal.IsZero():
		return i18n.T(l, i18n.OrderFormDeliveryFree), i18n.T(l, i18n.OrderFormTotal)
	}
	return domain.FormatMAD(fee.Decimal, string(l)), i18n.T(l, i18n.OrderFormTotal)
}

// scriptMessages are the translated strings the form script needs.
func scriptMessages(l i18n.Locale) string {
	keys := map[string]i18n.Key{
		"nameRequired": i18n.ValidationNameRequired, "nameTooLong": i18n.ValidationNameTooLong,
		"phoneRequired": i18n.ValidationPhoneRequired, "phoneInvalid": i18n.ValidationPhoneInvalid,
		"cityRequired": i18n.ValidationCityRequired, "cityTooLong": i18n.ValidationCityTooLong,
		"addressTooLong": i18n.ValidationAddressTooLong, "noteTooLong": i18n.ValidationNoteTooLong,
		"itemsRequired": i18n.ValidationItemsRequired, "litersRequired": i18n.ValidationLitersRequired,
		"litersRange": i18n.ValidationLitersRange, "errorNetwork": i18n.OrderFormErrorNetwork,
		"errorRejected": i18n.OrderFormErrorRejected, "customLine": i18n.OrderFormCustomLine,
		"whatsappOrder": i18n.OrderFormWhatsappOrder, "total": i18n.OrderFormTotal,
		"totalWithoutDelivery": i18n.OrderFormTotalWithoutDelivery, "deliveryConfirm": i18n.OrderFormDeliveryConfirm,
		"deliveryFree": i18n.OrderFormDeliveryFree, "stepOf": i18n.OrderFormStepOf,
		"step1": i18n.OrderFormStep1, "step2": i18n.OrderFormStep2, "step3": i18n.OrderFormStep3,
		"currency": i18n.CurrencyMad,
	}
	m := make(map[string]string, len(keys))
	for k, key := range keys {
		m[k] = i18n.T(l, key)
	}
	b, err := json.Marshal(m) // goes into a data attribute (templ escapes it)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// OrderRedirect handles GET /{locale}/order (a refresh after a failed post): back to the form.
func (h *Handlers) OrderRedirect(w http.ResponseWriter, r *http.Request) {
	locale, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/"+string(locale)+"#order", http.StatusSeeOther) // #nosec G710 -- locale is "ar" or "fr" (i18n.Parse)
}

// Confirmation renders /{locale}/order/{reference}?t=: only with the signed
// token (references are sequential), for 30 days; noindex and no-referrer.
func (h *Handlers) Confirmation(w http.ResponseWriter, r *http.Request) {
	locale, ok := i18n.Parse(r.PathValue("locale"))
	if !ok {
		h.NotFound(w, r)
		return
	}
	d, err := h.Site.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	ref := r.PathValue("reference")
	p := h.page(r, locale, d, "/order/"+ref)
	p.Title = i18n.T(locale, i18n.ConfirmationMetaTitle)
	p.NoIndex, p.NoReferrer = true, true
	p.Canonical, p.Alternates = "", nil
	p.SwitchHref = "/" + string(locale.Other()) + "/order/" + ref + "?t=" + url.QueryEscape(r.URL.Query().Get("t"))
	p.Script = "js/landing.js"
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")

	data, status := h.confirmation(r, locale, d, ref)
	render(w, r, status, p.Nonce, view.Confirmation(p, data))
}

func (h *Handlers) confirmation(r *http.Request, l i18n.Locale, d *store.SiteData, ref string) (view.ConfirmationData, int) {
	notFound := view.ConfirmationData{}
	if !referenceRE.MatchString(ref) || !h.Confirm.Valid(ref, r.URL.Query().Get("t")) {
		return notFound, http.StatusNotFound
	}
	o, err := h.Store.Q.GetOrderByReference(r.Context(), ref)
	if err != nil || time.Since(o.CreatedAt) > confirmationTTL {
		return notFound, http.StatusNotFound
	}
	items, err := h.Store.Q.ListOrderItems(r.Context(), o.ID)
	if err != nil {
		return notFound, http.StatusNotFound
	}
	loc := string(l)
	total := domain.FormatMAD(o.Total, loc)
	data := view.ConfirmationData{
		Found:     true,
		Reference: o.Reference,
		FirstName: strings.Fields(o.CustomerName + " ")[0],
		City:      o.City,
		Total:     total,
		LeadValue: o.Total.StringFixed(2),
	}
	var names []string
	var qty []int
	for _, it := range items {
		name := it.NameFr
		if l == i18n.AR {
			name = it.NameAr
		}
		data.Lines = append(data.Lines, view.FormLine{Label: name, Quantity: int(it.Quantity)})
		names = append(names, name)
		qty = append(qty, int(it.Quantity))
	}
	data.LeadContent = domain.TemplateItems(names, qty, "fr")
	data.DeliveryText, data.TotalLabel = deliveryLabels(l, o.DeliveryFee)
	if o.DeliveryFee.Valid {
		data.PayText = i18n.T(l, i18n.ConfirmationPayOnDelivery, "total", total)
	} else {
		data.PayText = i18n.T(l, i18n.ConfirmationPayOnDeliveryTbc, "total", total)
	}
	tpl := d.Settings.Templates.OrderFr
	if l == i18n.AR {
		tpl = d.Settings.Templates.OrderAr
	}
	data.WhatsAppText = domain.FillTemplate(tpl, map[string]string{
		"reference": o.Reference, "name": o.CustomerName, "city": o.City,
		"items": domain.TemplateItems(names, qty, loc), "total": total,
	})
	return data, http.StatusOK
}

// PowChallenge serves GET /api/pow: a fresh proof-of-work challenge (30 per minute per IP).
func (h *Handlers) PowChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key := "pow:ip:" + hex.EncodeToString(h.IPHash.Sum(httpx.ClientIP(r.Context())))
	allowed, err := h.Limiter.Allow(r.Context(), key, powLimit, time.Minute)
	if err != nil || !allowed {
		http.Error(w, "", http.StatusTooManyRequests)
		return
	}
	ch, err := h.Pow.Issue()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ch)
}

func notifyOrder(o *dbq.Order, lines []domain.OrderLine, l i18n.Locale) notify.Order {
	n := notify.Order{
		Reference: o.Reference, Name: o.CustomerName, PhoneE164: o.PhoneE164, City: o.City,
		Locale: string(l), Subtotal: o.Subtotal, Total: o.Total, DeliveryFee: o.DeliveryFee,
		CreatedAt: o.CreatedAt,
	}
	if o.Address != nil {
		n.Address = *o.Address
	}
	if o.Note != nil {
		n.Note = *o.Note
	}
	for _, line := range lines {
		n.Items = append(n.Items, notify.Item{Name: line.NameFr, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	return n
}
