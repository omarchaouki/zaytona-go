package view

import (
	"github.com/a-h/templ"

	"github.com/omarchaouki/zaytona-go/internal/i18n"
)

// AdminPage is what the admin layout needs on every page. Admin pages hold
// personal data: no-store and noindex are set by the middleware.
type AdminPage struct {
	Locale i18n.Locale
	Nonce  string
	Title  string
	Asset  func(string) string
	// CSRF is the session synchronizer token (hidden "csrf" field, X-CSRF-Token header).
	CSRF  string
	Email string
	Owner bool
	// Active is the current navigation entry ("dashboard", "orders", "security", "password").
	Active string
	// Unseen is the number of orders nobody opened yet (red badge).
	Unseen int64
	// PendingReviews waiting for moderation (gold badge).
	PendingReviews int64
	// Return is where the language switch comes back to (an /admin path).
	Return string
	// Flash is a translated one-line result of the last action.
	Flash      string
	FlashError bool
	// Bare hides the navigation (forced password change, 2FA step).
	Bare bool
}

// T translates in the admin's language.
func (p AdminPage) T(k i18n.Key, pairs ...string) string { return i18n.T(p.Locale, k, pairs...) }

// Meta returns the locale rendering metadata.
func (p AdminPage) Meta() i18n.Meta { return p.Locale.Meta() }

// adminNavItem is one entry of the admin navigation.
type adminNavItem struct {
	Key   string
	Href  string
	Label i18n.Key
	Short i18n.Key
	Icon  func(string) templ.Component
	// OwnerOnly entries are hidden from staff.
	OwnerOnly bool
}

// Daily work: bottom tabs on phones, top of the sidebar on computers. Only
// pages that exist are listed.
var adminPrimary = []adminNavItem{
	{Key: "dashboard", Href: "/admin", Label: i18n.AdminNavDashboard, Icon: IconLayoutDashboard},
	{Key: "orders", Href: "/admin/orders", Label: i18n.AdminNavOrders, Icon: IconPackage},
	{Key: "offers", Href: "/admin/offers", Label: i18n.AdminNavOffers, Short: i18n.AdminNavOffersShort, Icon: IconDroplets},
	{Key: "reviews", Href: "/admin/reviews", Label: i18n.AdminNavReviews, Short: i18n.AdminNavReviewsShort, Icon: IconMessageCircle},
}

// Occasional pages: the "More" sheet on phones.
var adminSecondary = []adminNavItem{
	{Key: "certificates", Href: "/admin/certificates", Label: i18n.AdminNavTrust, Icon: IconFileCheck2},
	{Key: "content", Href: "/admin/content", Label: i18n.AdminNavContent, Icon: IconImage},
	{Key: "faq", Href: "/admin/faq", Label: i18n.AdminNavFaq, Icon: IconHelpCircle},
	{Key: "settings", Href: "/admin/settings", Label: i18n.AdminNavSettings, Icon: IconSettings},
	{Key: "admins", Href: "/admin/admins", Label: i18n.AdminNavAdmins, Icon: IconUsers, OwnerOnly: true},
	{Key: "audit", Href: "/admin/audit", Label: i18n.AdminNavAudit, Icon: IconClipboardList, OwnerOnly: true},
	{Key: "security", Href: "/admin/security", Label: i18n.AdminNavSecurity, Icon: IconShieldCheck},
	{Key: "password", Href: "/admin/password", Label: i18n.AdminNavPassword, Icon: IconKeyRound},
}

// secondary lists the "More" entries this admin may open.
func (p AdminPage) secondary() []adminNavItem {
	var out []adminNavItem
	for _, n := range adminSecondary {
		if !n.OwnerOnly || p.Owner {
			out = append(out, n)
		}
	}
	return out
}

func (p AdminPage) secondaryActive() bool {
	for _, n := range adminSecondary {
		if n.Key == p.Active {
			return true
		}
	}
	return false
}

func (p AdminPage) navLabel(n adminNavItem) string {
	if n.Short != "" {
		return p.T(n.Short)
	}
	return p.T(n.Label)
}
