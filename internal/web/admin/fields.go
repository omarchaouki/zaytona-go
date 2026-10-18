package admin

import (
	"net/http"
	"slices"
	"strings"
)

// allowedFields lists the form fields every admin POST route accepts (the
// CSRF token is always allowed). The guard refuses a request carrying any
// other field or file (SECURITY.md sections 4 and 7: unknown fields rejected,
// one expected file at most). A name ending in "*" is a prefix.
var allowedFields = map[string][]string{
	"POST /admin/login":                   {"email", "password", "next", "lang"},
	"POST /admin/2fa":                     {"code"},
	"POST /admin/logout":                  {},
	"POST /admin/password":                {"username", "current", "next", "confirm"},
	"POST /admin/locale":                  {"next"},
	"POST /admin/security/reauth":         {"password", "username", "next"},
	"POST /admin/security/totp/start":     {},
	"POST /admin/security/totp/enable":    {"code"},
	"POST /admin/security/totp/cancel":    {},
	"POST /admin/security/totp/disable":   {},
	"POST /admin/security/recovery":       {},
	"POST /admin/security/signout-others": {},

	"POST /admin/orders/bulk":        {"action", "status", "ids", "return", "confirm"},
	"POST /admin/orders/seen-all":    {"return"},
	"POST /admin/orders/restore":     {"ids", "return"},
	"POST /admin/orders/{id}/seen":   {},
	"POST /admin/orders/{id}/status": {"status", "note"},
	"POST /admin/orders/{id}/paid":   {"paid"},
	"POST /admin/orders/{id}/edit":   {"fee", "q_*"},
	"POST /admin/orders/{id}/note":   {"note"},
	"POST /admin/orders/{id}/delete": {"return", "confirm"},

	"POST /admin/offers/pricing":     {"pricePerLiter", "customEnabled", "customMin", "customMax"},
	"POST /admin/offers":             offerFields,
	"POST /admin/offers/{id}":        offerFields,
	"POST /admin/offers/{id}/delete": {"confirm"},

	"POST /admin/settings/{group}": {
		"nameAr", "nameFr", "addressAr", "addressFr", "legalName", "ice", "rc", "showLegalIds",
		"whatsapp", "phone", "email", "hoursAr", "hoursFr", "responseAr", "responseFr", "mapUrl", "mapEmbed",
		"facebook", "instagram", "tiktok", "youtube",
		"mode", "fee", "freeAbove", "timeAr", "timeFr",
		"orderAr", "orderFr", "customerAr", "customerFr", "greetingAr", "greetingFr",
		"webhook", "showReviews", "showGallery", "showCounters",
		"pixelEnabled", "pixelId", "requireConsent",
	},

	"POST /admin/certificates":             certFields,
	"POST /admin/certificates/{id}":        certFields,
	"POST /admin/certificates/{id}/delete": {"confirm"},

	"POST /admin/content/{block}": {
		"eyebrow_ar", "eyebrow_fr", "title_ar", "title_fr", "subtitle_ar", "subtitle_fr", "body_ar", "body_fr",
		"cod_ar", "cod_fr", "delivery_ar", "delivery_fr", "returns_ar", "returns_fr", "privacy_ar", "privacy_fr",
		"html_ar", "html_fr", "reviewed",
	},
	"POST /admin/gallery":             {"captionAr", "captionFr", "file:photo"},
	"POST /admin/gallery/{id}":        {"captionAr", "captionFr"},
	"POST /admin/gallery/{id}/move":   {"dir"},
	"POST /admin/gallery/{id}/delete": {"confirm"},

	"POST /admin/faq":             faqFields,
	"POST /admin/faq/{id}":        faqFields,
	"POST /admin/faq/{id}/move":   {"dir"},
	"POST /admin/faq/{id}/delete": {"confirm"},

	"POST /admin/admins":               {"email", "name", "role"},
	"POST /admin/admins/{id}/{action}": {"role", "confirm"},

	"POST /admin/reviews/bulk":          {"action", "ids", "return"},
	"POST /admin/reviews/{id}/delete":   {"return", "confirm"},
	"POST /admin/reviews/{id}/{action}": {"return", "reason", "comment", "replyAr", "replyFr"},
}

var (
	offerFields = []string{"liters", "sortOrder", "nameAr", "nameFr", "mode", "price", "taglineAr", "taglineFr", "removePhoto", "isActive", "file:photo"}
	certFields  = []string{"type", "number", "labelAr", "labelFr", "issued", "expires", "isPublic", "file:scan"}
	faqFields   = []string{"questionAr", "questionFr", "answerAr", "answerFr", "isActive"}
)

// post registers a POST route; it panics at startup when the route has no
// allow-list, so a new form cannot ship without one.
func post(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	if _, ok := allowedFields[pattern]; !ok {
		panic("admin: no allowed form fields declared for " + pattern)
	}
	mux.HandleFunc(pattern, h)
}

func fieldAllowed(allowed []string, name string) bool {
	if name == "csrf" {
		return true
	}
	for _, a := range allowed {
		if a == name || (strings.HasSuffix(a, "*") && strings.HasPrefix(name, strings.TrimSuffix(a, "*"))) {
			return true
		}
	}
	return false
}

// formFieldsOK checks a parsed POST against its route's allow-list: no
// unknown field, and at most one file in the declared file field.
func formFieldsOK(r *http.Request) bool {
	allowed, ok := allowedFields[r.Pattern]
	if !ok {
		return false
	}
	for name, values := range r.PostForm {
		if fieldAllowed(allowed, name) {
			continue
		}
		// A file input left empty is sent as a part with filename="", which
		// Go reads as an empty value: "no file chosen" for a declared file field.
		if slices.Contains(allowed, "file:"+name) && emptyValues(values) {
			continue
		}
		return false
	}
	if r.MultipartForm != nil {
		for name, files := range r.MultipartForm.File {
			if len(files) > 1 || !slices.Contains(allowed, "file:"+name) {
				return false
			}
		}
	}
	return true
}

// emptyValues reports whether every value is empty.
func emptyValues(values []string) bool {
	for _, v := range values {
		if v != "" {
			return false
		}
	}
	return true
}
