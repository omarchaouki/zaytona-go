// Package public holds the handlers of the public site.
package public

import (
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Africa/Casablanca without a system tz database

	"github.com/a-h/templ"

	"github.com/omarchaouki/zaytona-go/internal/assets"
	"github.com/omarchaouki/zaytona-go/internal/config"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
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

// Handlers serve the public pages.
type Handlers struct {
	Config  *config.Config
	Store   *store.Store
	Site    *store.SiteCache
	Assets  *assets.Assets
	Logger  *slog.Logger
	Limiter *security.Limiter
	IPHash  *security.Hasher
	// Orders
	PhoneHash  *security.Hasher
	FormTokens *security.FormTokens
	Pow        *security.Pow
	Turnstile  *security.Turnstile // nil when TURNSTILE_* is not set
	Confirm    *security.ConfirmationTokens
	Notifier   OrderNotifier // nil disables notifications
	// Media re-encodes review photos.
	Media *media.Processor
}

// OrderNotifier receives placed orders (notify.Notifier).
type OrderNotifier interface {
	Enqueue(o notify.Order)
}

// CSPFeatures enables third-party origins only for features that are on.
func (h *Handlers) CSPFeatures(r *http.Request) httpx.CSPFeatures {
	path := r.URL.Path
	admin := strings.HasPrefix(path, "/admin")
	f := httpx.CSPFeatures{Turnstile: h.Config.TurnstileSite != "" && !admin}
	if h.Config.UmamiWebsiteID != "" && !admin {
		f.UmamiHost = "https://cloud.umami.is"
	}
	if admin || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/api/") {
		return f
	}
	if d, err := h.Site.Get(r.Context()); err == nil {
		m := d.Settings.Marketing
		f.MetaPixel = m.PixelEnabled && m.PixelID != ""
		f.MapEmbed = d.Settings.Contact.MapEmbedURL != ""
	}
	return f
}

// Landing renders /{locale}.
func (h *Handlers) Landing(w http.ResponseWriter, r *http.Request) {
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
	p, data := h.landingPage(r, locale, d)
	if data.Reviews, err = h.reviewsSection(r, locale, d); err != nil {
		h.serverError(w, r, err)
		return
	}
	f := h.orderForm(locale, d, domain.OrderInput{})
	// "Order" on an offer card without JS: /ar?offer=<slug>#order preselects it;
	// the litre gauge sends ?offer=custom&liters=<n> (digits only, the form validates the range).
	if slug := r.URL.Query().Get("offer"); slug != "" && len(slug) <= 64 {
		in := domain.OrderInput{Choice: slug}
		if liters := r.URL.Query().Get("liters"); slug == domain.CustomChoice && isDigits(liters, 3) {
			in.CustomLiters = liters
		}
		f = h.orderForm(locale, d, in)
	}
	render(w, r, http.StatusOK, p.Nonce, view.Landing(p, data, f))
}

// landingPage builds the layout and section data of the landing page.
func (h *Handlers) landingPage(r *http.Request, locale i18n.Locale, d *store.SiteData) (view.Page, view.LandingData) {
	p := h.page(r, locale, d, "")
	p.Title = i18n.T(locale, i18n.MetaTitle)
	p.Description = i18n.T(locale, i18n.MetaDescription)
	p.OGImage = h.ogImage(locale)
	p.Script = "js/landing.js"
	p.FloatingWA = true
	p.MobileBar = true
	data := h.buildLanding(p, d)
	p.Preload = &data.HeroImage
	p.PreloadSizes = view.HeroImageSizes
	return p, data
}

// privacyNotice is appended to the privacy page while the Meta Pixel is on
// (the draft says the site uses no tracking cookies, true until then).
var privacyNotice = map[i18n.Locale]string{
	i18n.AR: "<h2>قياس الإعلانات (Meta)</h2><p>نستعمل أداة Meta Pixel لقياس فعالية إعلاناتنا على فيسبوك وإنستغرام: تسجّل زيارة الصفحات وإرسال الطلب (دون الاسم أو الهاتف أو العنوان)، وتضع ملفات تعريف من Meta في متصفحك. يمكنك رفضها في الشريط الذي يظهر عند أول زيارة، أو حذفها من إعدادات متصفحك.</p>",
	i18n.FR: "<h2>Mesure des publicités (Meta)</h2><p>Nous utilisons le Meta Pixel pour mesurer nos publicités sur Facebook et Instagram : il enregistre les pages visitées et l’envoi d’une commande (sans nom, téléphone ni adresse) et dépose des cookies de Meta dans votre navigateur. Vous pouvez les refuser dans le bandeau affiché lors de votre première visite, ou les supprimer dans les réglages de votre navigateur.</p>",
}

// Policy renders /{locale}/privacy and /{locale}/terms.
func (h *Handlers) Policy(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		key, title := "page_terms", i18n.MetaTermsTitle
		if page == "privacy" {
			key, title = "page_privacy", i18n.MetaPrivacyTitle
		}
		var c domain.PageContent
		d.ContentFor(key, locale == i18n.AR, &c)
		data := view.PolicyData{Title: orDefault(c.Title, i18n.T(locale, title)), HTML: c.HTML}
		if m := d.Settings.Marketing; page == "privacy" && m.PixelEnabled && m.PixelID != "" {
			data.Extra = privacyNotice[locale]
		}
		p := h.page(r, locale, d, "/"+page)
		p.Title = data.Title
		p.FloatingWA = true
		p.Script = "js/landing.js"
		render(w, r, http.StatusOK, p.Nonce, view.Policy(p, data))
	}
}

// NotFound renders the 404 page in the locale of the path (Arabic otherwise).
func (h *Handlers) NotFound(w http.ResponseWriter, r *http.Request) {
	locale := i18n.Default
	if seg, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/"); seg != "" {
		if l, ok := i18n.Parse(seg); ok {
			locale = l
		}
	}
	d, err := h.Site.Get(r.Context())
	if err != nil {
		d = &store.SiteData{Settings: domain.DefaultSettings()}
	}
	p := h.page(r, locale, d, "")
	p.Title = i18n.T(locale, i18n.ErrorsNotFoundTitle)
	p.NoIndex = true
	p.Canonical = ""
	p.Alternates = nil
	p.SwitchHref = "/" + string(locale.Other())
	render(w, r, http.StatusNotFound, p.Nonce, view.NotFound(p))
}

// waPlacements are the places a WhatsApp button sits (stored with each click).
var waPlacements = []string{
	"hero", "floating", "mobile_bar", "products_help", "product_unavailable", "why_us",
	"onssa", "order_section", "faq_more", "final_cta", "footer", "confirmation",
}

// WAClick counts an anonymous WhatsApp click: placement, locale and time only.
// Rate limited to 30 per minute per IP (keyed hash), always answers 204.
func (h *Handlers) WAClick(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Placement string `json:"placement"`
		Locale    string `json:"locale"`
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusRequestEntityTooLarge)
		return
	}
	if json.Unmarshal(raw, &body) != nil || !slices.Contains(waPlacements, body.Placement) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	locale, ok := i18n.Parse(body.Locale)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := "wa-click:" + hex.EncodeToString(h.IPHash.Sum(httpx.ClientIP(r.Context())))
	allowed, err := h.Limiter.Allow(r.Context(), key, 30, time.Minute)
	if err != nil {
		h.Logger.ErrorContext(r.Context(), "wa-click rate limit", "err", err)
	}
	if allowed {
		if err := h.Store.Q.InsertWhatsAppClick(r.Context(), dbq.InsertWhatsAppClickParams{
			Placement: body.Placement,
			Locale:    dbq.Locale(locale),
		}); err != nil {
			h.Logger.ErrorContext(r.Context(), "wa-click insert", "err", err)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// page fills the layout data. rest is the path after /{locale} (e.g. "/privacy").
func (h *Handlers) page(r *http.Request, locale i18n.Locale, d *store.SiteData, rest string) view.Page {
	site := h.Config.SiteURL.String()
	alternates := map[i18n.Locale]string{}
	for _, l := range i18n.Locales {
		alternates[l] = site + "/" + string(l) + rest
	}
	switchHref := "/" + string(locale.Other()) + rest
	if r.URL.RawQuery != "" {
		switchHref += "?" + r.URL.RawQuery
	}
	p := view.Page{
		Locale:     locale,
		Nonce:      httpx.Nonce(r.Context()),
		Canonical:  alternates[locale],
		Alternates: alternates,
		SwitchHref: switchHref,
		Asset:      h.Assets.URL,
		Settings:   d.Settings,
		Year:       time.Now().In(casablanca).Year(),
	}
	if c := d.PublicONSSA(); c != nil {
		p.ONSSANumber = c.Number
	}
	return p
}

func (h *Handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.ErrorContext(r.Context(), "handler", "request_id", httpx.RequestID(r.Context()), "path", r.URL.Path, "err", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Internal error. Reference: "+httpx.RequestID(r.Context()), http.StatusInternalServerError)
}

func render(w http.ResponseWriter, r *http.Request, status int, nonce string, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(templ.WithNonce(r.Context(), nonce), w)
}

func xmlEscapeText(w io.Writer, s string) error { return xml.EscapeText(w, []byte(s)) }

// isDigits reports whether s is 1 to n ASCII digits.
func isDigits(s string, n int) bool {
	if s == "" || len(s) > n {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
