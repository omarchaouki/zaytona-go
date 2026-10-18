// Package view holds the templ components. Pages receive everything they show
// through Page and a page-specific data struct; views never touch the database.
package view

import (
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
)

// Page is what the layout needs on every public page.
type Page struct {
	Locale      i18n.Locale
	Nonce       string
	Title       string
	Description string
	// Canonical is the absolute URL of this page; Alternates maps locale -> absolute URL.
	Canonical  string
	Alternates map[i18n.Locale]string
	// SwitchHref is this page in the other language (relative, keeps the query).
	SwitchHref string
	NoIndex    bool
	Asset      func(string) string
	Settings   domain.Settings
	// Year in Morocco time, for the footer.
	Year int
	// ONSSANumber of the public authorisation ("" hides it in the footer).
	ONSSANumber string
	// Preload is the LCP image (landing hero background), preloaded in <head>.
	Preload      *Image
	PreloadSizes string
	// OGImage is the absolute Open Graph image URL ("" omits it).
	OGImage string
	// Script is the logical name of the page's JS bundle ("" for none).
	Script string
	// FloatingWA shows the floating WhatsApp button (not on the confirmation page).
	FloatingWA bool
	// MobileBar renders the phone-only sticky order bar (landing page).
	MobileBar bool
	// NoReferrer sets <meta name="referrer" content="no-referrer"> (confirmation page).
	NoReferrer bool
}

// PixelID is the Meta Pixel to load, "" when the owner has not enabled it.
func (p Page) PixelID() string {
	m := p.Settings.Marketing
	if m.PixelEnabled && m.PixelID != "" {
		return m.PixelID
	}
	return ""
}

// LocalHref is a path inside the current locale ("/privacy" -> "/fr/privacy").
func (p Page) LocalHref(rest string) string { return "/" + string(p.Locale) + rest }

// Hours is the opening hours text in the page language.
func (p Page) Hours() string {
	if p.Locale == i18n.AR {
		return p.Settings.Contact.HoursAr
	}
	return p.Settings.Contact.HoursFr
}

// Social is a footer social link.
type Social struct{ Label, URL string }

// Socials lists the configured social links in a stable order.
func (p Page) Socials() []Social {
	s := p.Settings.Contact.Socials
	var out []Social
	for _, x := range []Social{{"Facebook", s.Facebook}, {"Instagram", s.Instagram}, {"TikTok", s.TikTok}, {"YouTube", s.YouTube}} {
		if x.URL != "" {
			out = append(out, x)
		}
	}
	return out
}

// WAHref is a wa.me link with text (the greeting when text is empty), or "".
func (p Page) WAHref(text string) string {
	n := p.Settings.WhatsApp.Number
	if n == "" {
		return ""
	}
	if text == "" {
		text = p.Settings.Templates.GreetingFr
		if p.Locale == i18n.AR {
			text = p.Settings.Templates.GreetingAr
		}
	}
	return domain.WhatsAppLink(n, text)
}

// FirstPhone is the first contact phone (E.164) or "".
func (p Page) FirstPhone() string {
	if len(p.Settings.Contact.Phones) == 0 {
		return ""
	}
	return p.Settings.Contact.Phones[0]
}

// T translates in the page locale.
func (p Page) T(k i18n.Key, pairs ...string) string { return i18n.T(p.Locale, k, pairs...) }

// Meta returns the locale rendering metadata.
func (p Page) Meta() i18n.Meta { return p.Locale.Meta() }

// SiteName is the business name in the page language (admin-editable).
func (p Page) SiteName() string {
	if p.Locale == i18n.AR {
		return p.Settings.Identity.NameAr
	}
	return p.Settings.Identity.NameFr
}

// Address is the business address in the page language.
func (p Page) Address() string {
	if p.Locale == i18n.AR {
		return p.Settings.Contact.AddressAr
	}
	return p.Settings.Contact.AddressFr
}

// HomeHref is the landing page of the current locale.
func (p Page) HomeHref() string { return "/" + string(p.Locale) }

// SectionHref links to a landing section: a bare fragment on the landing
// page itself (no reload), the home page plus the fragment elsewhere.
func (p Page) SectionHref(fragment string) string {
	if p.MobileBar {
		return fragment
	}
	return p.HomeHref() + fragment
}

// WhatsAppHref opens a chat with the greeting template, or "" when no number is set
// (every WhatsApp button is then hidden).
func (p Page) WhatsAppHref() string {
	n := p.Settings.WhatsApp.Number
	if n == "" {
		return ""
	}
	greeting := p.Settings.Templates.GreetingFr
	if p.Locale == i18n.AR {
		greeting = p.Settings.Templates.GreetingAr
	}
	return domain.WhatsAppLink(n, greeting)
}

// PreloadFonts lists the fonts preloaded per locale (max 2): one family per
// locale, Archivo (variable) in French, IBM Plex Sans Arabic in Arabic.
func (p Page) PreloadFonts() []string {
	if p.Locale == i18n.AR {
		return []string{p.Asset("fonts/ibm-plex-sans-arabic-arabic-400-normal.woff2"), p.Asset("fonts/ibm-plex-sans-arabic-arabic-500-normal.woff2")}
	}
	return []string{p.Asset("fonts/archivo-latin-wght-normal.woff2")}
}
