// Package domain holds pure business logic: no I/O, no net/http, no store.
package domain

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

// Settings groups stored in the settings table (key -> JSON object). Every
// group has defaults, so a missing or malformed row never breaks the page.

// WhatsAppSettings holds the number in international digits only, e.g. "212612345678". Empty hides every WhatsApp button.
type WhatsAppSettings struct {
	Number string `json:"number"`
}

// IdentitySettings is the business name and optional legal ids.
type IdentitySettings struct {
	NameAr       string `json:"nameAr"`
	NameFr       string `json:"nameFr"`
	LegalName    string `json:"legalName"`
	ICE          string `json:"ice"`
	RC           string `json:"rc"`
	ShowLegalIDs bool   `json:"showLegalIds"`
}

// Socials holds optional https links.
type Socials struct {
	Facebook  string `json:"facebook"`
	Instagram string `json:"instagram"`
	TikTok    string `json:"tiktok"`
	YouTube   string `json:"youtube"`
}

// ContactSettings is what the footer and contact blocks show.
type ContactSettings struct {
	Phones         []string `json:"phones"` // E.164
	Email          string   `json:"email"`
	AddressAr      string   `json:"addressAr"`
	AddressFr      string   `json:"addressFr"`
	MapURL         string   `json:"mapUrl"`
	MapEmbedURL    string   `json:"mapEmbedUrl"`
	HoursAr        string   `json:"hoursAr"`
	HoursFr        string   `json:"hoursFr"`
	ResponseTimeAr string   `json:"responseTimeAr"`
	ResponseTimeFr string   `json:"responseTimeFr"`
	Socials        Socials  `json:"socials"`
}

// Delivery fee modes.
const (
	DeliveryFlat      = "flat"       // fixed fee (0 = always free)
	DeliveryFreeAbove = "free_above" // fee unless subtotal >= threshold
	DeliveryConfirm   = "confirm"    // told by phone
)

// DeliverySettings drives the delivery fee rules.
type DeliverySettings struct {
	Mode      string              `json:"mode"`
	Fee       decimal.NullDecimal `json:"fee"`
	FreeAbove decimal.NullDecimal `json:"freeAbove"`
	TimeAr    string              `json:"timeAr"`
	TimeFr    string              `json:"timeFr"`
}

// TemplateSettings are the WhatsApp message templates ({name} {reference} {city} {items} {total}).
type TemplateSettings struct {
	OrderAr    string `json:"orderAr"`
	OrderFr    string `json:"orderFr"`
	CustomerAr string `json:"customerAr"`
	CustomerFr string `json:"customerFr"`
	GreetingAr string `json:"greetingAr"`
	GreetingFr string `json:"greetingFr"`
}

// PricingSettings drives litre pricing (olive oil is sold by the litre). A null price hides custom quantities.
type PricingSettings struct {
	PricePerLiter   decimal.NullDecimal `json:"pricePerLiter"`
	CustomEnabled   bool                `json:"customEnabled"`
	CustomMinLiters int                 `json:"customMinLiters"`
	CustomMaxLiters int                 `json:"customMaxLiters"`
	// Seeded placeholder price, cleared when the owner saves a real one.
	IsDemo bool `json:"isDemo"`
}

// MarketingSettings configures the Meta Pixel, off by default.
type MarketingSettings struct {
	PixelEnabled   bool   `json:"pixelEnabled"`
	PixelID        string `json:"pixelId"`
	RequireConsent bool   `json:"requireConsent"`
}

// NotificationSettings holds the optional new-order email and webhook.
type NotificationSettings struct {
	Email      string `json:"email"`
	WebhookURL string `json:"webhookUrl"`
}

// ToggleSettings switch optional sections.
type ToggleSettings struct {
	ShowReviews  bool `json:"showReviews"`
	ShowGallery  bool `json:"showGallery"`
	ShowCounters bool `json:"showCounters"`
}

// Settings is every group, defaults filled in.
type Settings struct {
	WhatsApp      WhatsAppSettings     `json:"whatsapp"`
	Identity      IdentitySettings     `json:"identity"`
	Contact       ContactSettings      `json:"contact"`
	Delivery      DeliverySettings     `json:"delivery"`
	Templates     TemplateSettings     `json:"templates"`
	Pricing       PricingSettings      `json:"pricing"`
	Marketing     MarketingSettings    `json:"marketing"`
	Notifications NotificationSettings `json:"notifications"`
	Toggles       ToggleSettings       `json:"toggles"`
}

// SettingsKeys lists the stored groups in a stable order.
var SettingsKeys = []string{
	"whatsapp", "identity", "contact", "delivery", "templates",
	"pricing", "marketing", "notifications", "toggles",
}

// DefaultSettings mirrors the reference defaults.
func DefaultSettings() Settings {
	return Settings{
		Identity: IdentitySettings{
			NameAr: "معصرة الهداجي لزيت الزيتون",
			NameFr: "Huilerie El Hadaji",
		},
		Contact: ContactSettings{
			Phones:    []string{},
			AddressAr: "الوناسدة، قلعة السراغنة 43000، المغرب",
			AddressFr: "Louanasda, El Kelaâ des Sraghna 43000, Maroc",
		},
		Delivery: DeliverySettings{Mode: DeliveryConfirm},
		Templates: TemplateSettings{
			OrderAr:    "السلام عليكم، أريد تأكيد طلبي رقم {reference}.\nالاسم: {name}\nالمدينة: {city}\nالطلب: {items}\nالمجموع: {total}",
			OrderFr:    "Bonjour, je souhaite confirmer ma commande {reference}.\nNom : {name}\nVille : {city}\nCommande : {items}\nTotal : {total}",
			CustomerAr: "السلام عليكم {name}، نتواصل معكم من المعصرة بخصوص طلبكم رقم {reference}.",
			CustomerFr: "Bonjour {name}, nous vous contactons au sujet de votre commande {reference}.",
			GreetingAr: "السلام عليكم، أريد الاستفسار عن منتجاتكم.",
			GreetingFr: "Bonjour, je souhaite me renseigner sur vos produits.",
		},
		Pricing: PricingSettings{
			CustomEnabled:   true,
			CustomMinLiters: 5,
			CustomMaxLiters: 500,
		},
		Marketing: MarketingSettings{RequireConsent: true},
		Toggles:   ToggleSettings{ShowReviews: true, ShowGallery: true},
	}
}

// SettingsGroup returns a pointer to the group stored under key, or nil.
func (s *Settings) SettingsGroup(key string) any {
	switch key {
	case "whatsapp":
		return &s.WhatsApp
	case "identity":
		return &s.Identity
	case "contact":
		return &s.Contact
	case "delivery":
		return &s.Delivery
	case "templates":
		return &s.Templates
	case "pricing":
		return &s.Pricing
	case "marketing":
		return &s.Marketing
	case "notifications":
		return &s.Notifications
	case "toggles":
		return &s.Toggles
	}
	return nil
}

// SettingsFromRows overlays stored JSON on the defaults. A malformed group keeps
// its defaults instead of failing the page.
func SettingsFromRows(rows map[string][]byte) Settings {
	s := DefaultSettings()
	for _, key := range SettingsKeys {
		raw, ok := rows[key]
		if !ok {
			continue
		}
		group := s.SettingsGroup(key)
		backup, _ := json.Marshal(group)
		if err := json.Unmarshal(raw, group); err != nil {
			_ = json.Unmarshal(backup, group)
		}
	}
	return s
}
