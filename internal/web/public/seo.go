package public

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

type jsonObj = map[string]any

// landingJSONLD builds the structured data from real data only: demo offers
// are excluded, ratings come from approved non-example reviews, and fields the
// owner has not filled in are omitted.
func landingJSONLD(origin string, p view.Page, d *store.SiteData, v view.LandingData) []string {
	s := d.Settings
	pageURL := origin + "/" + string(p.Locale)
	orgID := origin + "/#organization"

	org := jsonObj{
		"@context":           "https://schema.org",
		"@type":              "LocalBusiness",
		"@id":                orgID,
		"name":               p.SiteName(),
		"url":                pageURL,
		"image":              origin + p.Asset("images/olives-harvest-750.webp"),
		"areaServed":         jsonObj{"@type": "Country", "name": "Morocco"},
		"paymentAccepted":    "Cash",
		"currenciesAccepted": "MAD",
	}
	if s.Identity.ShowLegalIDs && s.Identity.LegalName != "" {
		org["legalName"] = s.Identity.LegalName
	}
	if phone := p.FirstPhone(); phone != "" {
		org["telephone"] = phone
	}
	if s.Contact.Email != "" {
		org["email"] = s.Contact.Email
	}
	if addr := p.Address(); addr != "" {
		org["address"] = jsonObj{"@type": "PostalAddress", "streetAddress": addr, "addressRegion": "Marrakech-Safi", "addressCountry": "MA"}
	}
	if s.Contact.MapURL != "" {
		org["hasMap"] = s.Contact.MapURL
	}
	if socials := p.Socials(); len(socials) > 0 {
		urls := make([]string, len(socials))
		for i, x := range socials {
			urls[i] = x.URL
		}
		org["sameAs"] = urls
	}
	if r := d.RealReviews; r.Total > 0 {
		avg, _ := r.Average.Round(1).Float64()
		org["aggregateRating"] = jsonObj{"@type": "AggregateRating", "ratingValue": avg, "reviewCount": r.Total, "bestRating": 5, "worstRating": 1}
		// Published reviews as shown on the page (first name + initial), never examples.
		reviews := make([]any, 0, len(d.TopReviews))
		for _, rv := range d.TopReviews {
			reviews = append(reviews, jsonObj{
				"@type":         "Review",
				"author":        jsonObj{"@type": "Person", "name": domain.DisplayName(rv.AuthorName)},
				"datePublished": rv.CreatedAt.In(casablanca).Format("2006-01-02"),
				"reviewBody":    rv.Comment,
				"inLanguage":    string(rv.Language),
				"reviewRating":  jsonObj{"@type": "Rating", "ratingValue": rv.Rating, "bestRating": 5, "worstRating": 1},
			})
		}
		if len(reviews) > 0 {
			org["review"] = reviews
		}
	}
	docs := []any{org}

	for i, o := range d.Offers {
		if o.IsDemo {
			continue
		}
		availability := "https://schema.org/InStock"
		switch o.StockStatus {
		case "out_of_stock":
			availability = "https://schema.org/OutOfStock"
		case "low_stock":
			availability = "https://schema.org/LimitedAvailability"
		}
		card := v.Offers[i]
		product := jsonObj{
			"@context": "https://schema.org",
			"@type":    "Product",
			"name":     card.Name + " " + card.SizeLabel,
			"image":    origin + card.Image.Fallback,
			"brand":    jsonObj{"@id": orgID},
			"offers": jsonObj{
				"@type":         "Offer",
				"price":         o.PriceMad.StringFixed(2),
				"priceCurrency": "MAD",
				"availability":  availability,
				"url":           pageURL + "#order",
				"seller":        jsonObj{"@id": orgID},
			},
		}
		if card.Tagline != "" {
			product["description"] = card.Tagline
		}
		docs = append(docs, product)
	}

	if len(v.FAQs) > 0 {
		qs := make([]any, len(v.FAQs))
		for i, f := range v.FAQs {
			qs[i] = jsonObj{"@type": "Question", "name": f.Question, "acceptedAnswer": jsonObj{"@type": "Answer", "text": f.Answer}}
		}
		docs = append(docs, jsonObj{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": qs})
	}

	out := make([]string, 0, len(docs))
	for _, doc := range docs {
		if b, err := marshalJSONLD(doc); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// marshalJSONLD encodes for a <script> element: encoding/json already escapes
// <, >, & and U+2028/U+2029, so the payload can never close the tag.
func marshalJSONLD(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

var sitemapPaths = []string{"", "/privacy", "/terms"}

// Sitemap lists every public page in both languages with hreflang alternates.
func (h *Handlers) Sitemap(w http.ResponseWriter, _ *http.Request) {
	origin := h.Config.SiteURL.String()
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">`)
	for _, path := range sitemapPaths {
		for _, l := range i18n.Locales {
			b.WriteString("<url><loc>" + xmlEscape(origin+"/"+string(l)+path) + "</loc>")
			for _, alt := range i18n.Locales {
				b.WriteString(`<xhtml:link rel="alternate" hreflang="` + string(alt) + `" href="` + xmlEscape(origin+"/"+string(alt)+path) + `"/>`)
			}
			b.WriteString(`<xhtml:link rel="alternate" hreflang="x-default" href="` + xmlEscape(origin+"/ar"+path) + `"/>`)
			priority := "1.0"
			if path != "" {
				priority = "0.3"
			}
			b.WriteString("<priority>" + priority + "</priority></url>")
		}
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(b.String()))
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xmlEscapeText(&buf, s)
	return buf.String()
}

// Robots serves robots.txt (admin never crawled).
func (h *Handlers) Robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /admin\nAllow: /\n\nSitemap: " + h.Config.SiteURL.String() + "/sitemap.xml\n"))
}

// ogImage is the absolute Open Graph image of a locale.
func (h *Handlers) ogImage(l i18n.Locale) string {
	return h.Config.SiteURL.String() + h.Assets.URL("images/og-"+string(l)+".jpg")
}
