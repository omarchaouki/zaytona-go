package admin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/auth"
	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/security"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

func (h *Handlers) settingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/settings", h.guard(roleAny, 0, h.SettingsPage))
	post(mux, "POST /admin/settings/{group}", h.guard(roleOwner, 0, h.SaveSettings))
}

// maskSettings returns a settings group as a map with phone numbers masked
// (for the audit log).
func maskSettings(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	for k, val := range m {
		switch k {
		case "number":
			if s, ok := val.(string); ok && s != "" {
				m[k] = security.RedactPhone("+" + s)
			}
		case "phones":
			if list, ok := val.([]any); ok {
				for i, x := range list {
					if s, ok := x.(string); ok {
						list[i] = security.RedactPhone(s)
					}
				}
			}
		}
	}
	return m
}

func (h *Handlers) renderSettings(w http.ResponseWriter, r *http.Request, cur *auth.Current, status int, s *view.FormState) {
	st, err := h.settings(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p := h.page(r, cur, i18n.AdminSettingsTitle, "settings")
	d := view.SettingsData{S: st, Form: s, NeedsReauth: !cur.RecentAuth(h.Now())}
	if r.URL.Query().Get("reauth") == "failed" {
		d.ReauthError = p.T(i18n.AdminPasswordWrongCurrent)
	}
	render(w, r, status, p.Nonce, view.AdminSettings(p, d))
}

// SettingsPage renders every group (read-only for staff).
func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	h.renderSettings(w, r, cur, http.StatusOK, nil)
}

var pixelIDRe = regexp.MustCompile(`^[0-9]{8,20}$`)

// SaveSettings validates and stores one group, then back to its section.
func (h *Handlers) SaveSettings(w http.ResponseWriter, r *http.Request, cur *auth.Current) {
	ctx := r.Context()
	group := r.PathValue("group")
	cur2, err := h.settings(ctx)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	f := newForm(r)
	type change struct {
		key           string
		before, after any
	}
	var changes []change
	switch group {
	case "identity":
		id := cur2.Identity
		id.NameAr = f.text("nameAr", 120, true, false)
		id.NameFr = f.text("nameFr", 120, true, false)
		id.LegalName = f.text("legalName", 120, false, false)
		id.ICE = f.text("ice", 20, false, false)
		id.RC = f.text("rc", 40, false, false)
		id.ShowLegalIDs = f.check("showLegalIds")
		ct := cur2.Contact
		ct.AddressAr = f.text("addressAr", 300, false, true)
		ct.AddressFr = f.text("addressFr", 300, false, true)
		changes = []change{{"identity", cur2.Identity, id}, {"contact", cur2.Contact, ct}}
	case "contact":
		wa := cur2.WhatsApp
		if raw := strings.TrimSpace(r.PostFormValue("whatsapp")); raw == "" {
			wa.Number = ""
		} else if n, ok := domain.NormalizeWhatsAppNumber(raw); ok {
			wa.Number = n
		} else {
			f.fail("whatsapp", i18n.AdminSettingsInvalid)
		}
		ct := cur2.Contact
		if raw := strings.TrimSpace(r.PostFormValue("phone")); raw == "" {
			ct.Phones = nil
		} else if e164, ok := domain.NormalizeMoroccanPhone(raw); ok {
			ct.Phones = append([]string{e164}, without(cur2.Contact.Phones, e164, 1)...)
		} else {
			f.fail("phone", i18n.AdminSettingsInvalid)
		}
		ct.Email = f.email("email", false)
		ct.HoursAr = f.text("hoursAr", 120, false, false)
		ct.HoursFr = f.text("hoursFr", 120, false, false)
		ct.ResponseTimeAr = f.text("responseAr", 120, false, false)
		ct.ResponseTimeFr = f.text("responseFr", 120, false, false)
		ct.MapURL = f.httpsURL("mapUrl", "")
		ct.MapEmbedURL = f.httpsURL("mapEmbed", "https://www.google.com/maps/embed")
		ct.Socials.Facebook = f.httpsURL("facebook", "")
		ct.Socials.Instagram = f.httpsURL("instagram", "")
		ct.Socials.TikTok = f.httpsURL("tiktok", "")
		ct.Socials.YouTube = f.httpsURL("youtube", "")
		numbersChanged := wa.Number != cur2.WhatsApp.Number || firstOf(ct.Phones) != firstOf(cur2.Contact.Phones)
		if f.ok() && numbersChanged && !cur.RecentAuth(h.Now()) {
			f.fail("form", i18n.AdminCommonReauthFirst)
		}
		changes = []change{{"whatsapp", cur2.WhatsApp, wa}, {"contact", cur2.Contact, ct}}
	case "delivery":
		dl := cur2.Delivery
		dl.TimeAr = f.text("timeAr", 120, false, false)
		dl.TimeFr = f.text("timeFr", 120, false, false)
		fee := f.decimal("fee", 0, 100000, false)
		above := f.decimal("freeAbove", 0, 10000000, false)
		null := func(d *decimal.Decimal) decimal.NullDecimal {
			if d == nil {
				return decimal.NullDecimal{}
			}
			return decimal.NullDecimal{Decimal: *d, Valid: true}
		}
		switch r.PostFormValue("mode") {
		case "flat":
			if fee == nil {
				f.fail("fee", i18n.AdminDeliveryRequired)
			}
			dl.Mode, dl.Fee, dl.FreeAbove = domain.DeliveryFlat, null(fee), decimal.NullDecimal{}
		case "free_above":
			if fee == nil {
				f.fail("fee", i18n.AdminDeliveryRequired)
			}
			if above == nil {
				f.fail("freeAbove", i18n.AdminDeliveryRequired)
			}
			dl.Mode, dl.Fee, dl.FreeAbove = domain.DeliveryFreeAbove, null(fee), null(above)
		case "free":
			zero := decimal.Zero
			dl.Mode, dl.Fee, dl.FreeAbove = domain.DeliveryFlat, null(&zero), decimal.NullDecimal{}
		case "confirm":
			dl.Mode, dl.Fee, dl.FreeAbove = domain.DeliveryConfirm, decimal.NullDecimal{}, decimal.NullDecimal{}
		default:
			f.fail("form", i18n.AdminCommonError)
		}
		changes = []change{{"delivery", cur2.Delivery, dl}}
	case "templates":
		t := domain.TemplateSettings{
			OrderAr: f.text("orderAr", 1000, true, true), OrderFr: f.text("orderFr", 1000, true, true),
			CustomerAr: f.text("customerAr", 1000, true, true), CustomerFr: f.text("customerFr", 1000, true, true),
			GreetingAr: f.text("greetingAr", 500, true, true), GreetingFr: f.text("greetingFr", 500, true, true),
		}
		changes = []change{{"templates", cur2.Templates, t}}
	case "notifications":
		n := domain.NotificationSettings{Email: f.email("email", false)}
		if raw := strings.TrimSpace(r.PostFormValue("webhook")); raw != "" {
			if len(raw) > 500 || h.Webhook.ValidateURL(raw) != nil {
				f.fail("webhook", i18n.AdminSettingsWebhookInvalid)
			}
			n.WebhookURL = raw
		}
		changes = []change{{"notifications", cur2.Notifications, n}}
	case "toggles":
		t := domain.ToggleSettings{ShowReviews: f.check("showReviews"), ShowGallery: f.check("showGallery"), ShowCounters: f.check("showCounters")}
		changes = []change{{"toggles", cur2.Toggles, t}}
	case "marketing":
		m := domain.MarketingSettings{
			PixelEnabled: f.check("pixelEnabled"), PixelID: strings.TrimSpace(r.PostFormValue("pixelId")),
			RequireConsent: f.check("requireConsent"),
		}
		switch {
		case m.PixelID != "" && !pixelIDRe.MatchString(m.PixelID):
			f.fail("pixelId", i18n.AdminMarketingInvalidId)
		case m.PixelEnabled && m.PixelID == "":
			f.fail("pixelId", i18n.AdminMarketingMissingId)
		}
		changes = []change{{"marketing", cur2.Marketing, m}}
	default:
		http.NotFound(w, r)
		return
	}
	if !f.ok() {
		p := h.page(r, cur, i18n.AdminSettingsTitle, "settings")
		h.renderSettings(w, r, cur, http.StatusUnprocessableEntity, formState(group, f, p))
		return
	}
	err = h.Store.InTx(ctx, func(q *dbq.Queries) error {
		for _, c := range changes {
			if err := h.saveSetting(ctx, q, r, cur, c.key, c.before, c.after); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.Site.Invalidate()
	http.Redirect(w, r, "/admin/settings?done=saved#"+group, http.StatusSeeOther) //nolint:gosec // G710: group is one of the fixed names above
}

func firstOf(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// without drops v from list, starting at index from.
func without(list []string, v string, from int) []string {
	var out []string
	for i, x := range list {
		if i >= from && x != v {
			out = append(out, x)
		}
	}
	return out
}
