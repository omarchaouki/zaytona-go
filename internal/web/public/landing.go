package public

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/db/dbq"
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
	"github.com/omarchaouki/zaytona-go/internal/store"
	"github.com/omarchaouki/zaytona-go/internal/view"
)

// offerImage is the photo of an offer without its own upload (same photo on
// every offer, as in the reference).
const offerImage = "oil-pour"

// buildLanding localises and formats SiteData for the landing view.
func (h *Handlers) buildLanding(p view.Page, d *store.SiteData) view.LandingData {
	l := p.Locale
	ar := l == i18n.AR
	loc := string(l)
	s := d.Settings
	t := func(k i18n.Key, pairs ...string) string { return i18n.T(l, k, pairs...) }
	pick := func(a, f string) string {
		if ar {
			return a
		}
		return f
	}
	pickPtr := func(a, f *string) string {
		if ar {
			return deref(a)
		}
		return deref(f)
	}

	var hero domain.HeroContent
	d.ContentFor("hero", ar, &hero)
	out := view.LandingData{
		HeroEyebrow:  orDefault(hero.Eyebrow, t(i18n.HeroEyebrow)),
		HeroTitle:    orDefault(hero.Title, t(i18n.HeroTitle)),
		HeroSubtitle: orDefault(hero.Subtitle, t(i18n.HeroSubtitle)),
		HeroImage:    view.StaticImage(p.Asset, "olives-golden-hour"),
		HasLab:       d.LabReports > 0,
	}

	// Rating: approved, non-example reviews only.
	if r := d.RealReviews; r.Total > 0 {
		avg := r.Average.Round(1)
		avgF, _ := avg.Float64()
		avgText := domain.FormatNumber(avg)
		count := strconv.FormatInt(r.Total, 10)
		out.Rating = &view.Rating{
			Average:      avgF,
			AverageText:  avgText,
			CountText:    count,
			StarsLabel:   t(i18n.ReviewsStarsLabel, "rating", avgText),
			SummaryLabel: t(i18n.HeroRating, "average", avgText, "count", count),
		}
	}

	// Offers and the price anchor (cheapest in stock).
	img := view.StaticImage(p.Asset, offerImage)
	unit := t(i18n.OrderFormLitersUnit)
	for _, o := range d.Offers {
		name := pick(o.NameAr, o.NameFr)
		size := domain.FormatNumber(o.Liters) + " " + unit
		card := view.OfferCard{
			Slug:       o.Slug,
			Name:       name,
			SizeLabel:  size,
			Tagline:    pickPtr(o.TaglineAr, o.TaglineFr),
			Price:      domain.FormatMAD(o.PriceMad, loc),
			Image:      img,
			ImageAlt:   name,
			OutOfStock: o.StockStatus == dbq.StockStatusOutOfStock,
			LowStock:   o.StockStatus == dbq.StockStatusLowStock,
			OrderLabel: t(i18n.ProductsOrderLabel, "name", name+" "+size),
		}
		if o.ImageID != nil && o.ImageWidth != nil && o.ImageHeight != nil {
			card.Image = view.MediaImage(*o.ImageID, ints(o.ImageWidths), int(*o.ImageWidth), int(*o.ImageHeight))
		}
		if o.Liters.IsPositive() {
			card.PerLiter = domain.FormatMAD(o.PriceMad.Div(o.Liters), loc)
		}
		out.Offers = append(out.Offers, card)
	}
	setOfferFill(out.Offers, d.Offers)
	if from, ok := cheapestOffer(d.Offers); ok {
		out.PriceFrom = domain.FormatMAD(from, loc)
		if ppl := s.Pricing.PricePerLiter; ppl.Valid {
			out.PricePerLiter = domain.FormatMAD(ppl.Decimal, loc)
		}
	}
	if pr := s.Pricing; pr.CustomEnabled && pr.PricePerLiter.Valid {
		out.Custom = &view.CustomCard{
			PerLiter:    domain.FormatMAD(pr.PricePerLiter.Decimal, loc),
			PerLiterRaw: pr.PricePerLiter.Decimal.StringFixed(2),
			Min:         pr.CustomMinLiters,
			Max:         pr.CustomMaxLiters,
			Body:        t(i18n.ProductsCustomBody, "min", strconv.Itoa(pr.CustomMinLiters), "max", strconv.Itoa(pr.CustomMaxLiters)),
			Hint:        t(i18n.ProductsGaugeHint, "min", strconv.Itoa(pr.CustomMinLiters), "max", strconv.Itoa(pr.CustomMaxLiters)),
		}
	}

	// Certificates: the public ONSSA authorisation and the others.
	onssa := d.PublicONSSA()
	for _, c := range d.Certifications {
		cert := view.Certificate{
			Label:  pickPtr(c.LabelAr, c.LabelFr),
			Number: c.Number,
		}
		if cert.Label == "" {
			cert.Label = strings.ToUpper(string(c.Type))
		}
		if c.IssuedAt != nil {
			cert.Issued = domain.FormatDayLong(*c.IssuedAt, loc)
		}
		if c.ExpiresAt != nil {
			cert.Expires = domain.FormatDayLong(*c.ExpiresAt, loc)
		}
		if c.ScanID != nil && c.ScanWidth != nil && c.ScanHeight != nil {
			cert.Scan = view.MediaImage(*c.ScanID, ints(c.ScanWidths), int(*c.ScanWidth), int(*c.ScanHeight))
			cert.ScanAlt = t(i18n.WhyUsCertificateAlt, "number", c.Number)
		}
		if onssa != nil && c.ID == onssa.ID {
			out.ONSSA = &cert
		} else if c.Number != "" {
			out.OtherCerts = append(out.OtherCerts, cert)
		}
	}

	// Why us: policy texts (admin content first), delivery rules, contact facts.
	var pol domain.PoliciesContent
	d.ContentFor("policies", ar, &pol)
	out.CODText = orDefault(pol.COD, t(i18n.WhyUsCodBody))
	out.ReturnsText = orDefault(pol.Returns, t(i18n.WhyUsReturnsBody))
	out.PrivacyText = orDefault(pol.Privacy, t(i18n.WhyUsPrivacyBody))
	out.DeliveryText = orDefault(pol.Delivery, view.DeliveryText(s.Delivery, l))
	out.DeliveryTime = pick(s.Delivery.TimeAr, s.Delivery.TimeFr)
	out.Hours = pick(s.Contact.HoursAr, s.Contact.HoursFr)
	out.ResponseTime = pick(s.Contact.ResponseTimeAr, s.Contact.ResponseTimeFr)
	if d.DeliveredOrders > 0 {
		out.Delivered = domain.FormatNumber(decimal.NewFromInt(d.DeliveredOrders))
	}
	if s.Identity.ShowLegalIDs {
		out.LegalName, out.ICE, out.RC = s.Identity.LegalName, s.Identity.ICE, s.Identity.RC
	}

	// Story and gallery.
	var story domain.StoryContent
	d.ContentFor("story", ar, &story)
	out.StoryTitle = orDefault(story.Title, t(i18n.StoryTitle))
	out.StoryParagraphs = splitParagraphs(orDefault(story.Body, t(i18n.StoryBody)))
	for _, g := range d.Gallery {
		out.Gallery = append(out.Gallery, view.GalleryItem{
			Image:   view.MediaImage(g.MediaID, ints(g.Widths), int(g.Width), int(g.Height)),
			Caption: pickPtr(g.CaptionAr, g.CaptionFr),
		})
	}

	for _, f := range d.FAQs {
		out.FAQs = append(out.FAQs, view.FAQItem{Question: pick(f.QuestionAr, f.QuestionFr), Answer: pick(f.AnswerAr, f.AnswerFr)})
	}

	out.JSONLD = landingJSONLD(h.Config.SiteURL.String(), p, d, out)
	return out
}

// cheapestOffer is the price anchor: the lowest price among offers in stock.
func cheapestOffer(offers []dbq.ListActiveOffersWithImageRow) (decimal.Decimal, bool) {
	var best decimal.Decimal
	found := false
	for _, o := range offers {
		if o.StockStatus == dbq.StockStatusOutOfStock || !o.PriceMad.IsPositive() {
			continue
		}
		if !found || o.PriceMad.LessThan(best) {
			best, found = o.PriceMad, true
		}
	}
	return best, found
}

func splitParagraphs(s string) []string {
	var out []string
	for _, block := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		if b := strings.TrimSpace(block); b != "" {
			out = append(out, b)
		}
	}
	return out
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ints(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// setOfferFill sets how full each offer's drawn jerrycan is: its litres
// against the largest offer, in steps of 10% (at least 20%, so it shows).
func setOfferFill(cards []view.OfferCard, offers []dbq.ListActiveOffersWithImageRow) {
	largest := decimal.Zero
	for _, o := range offers {
		if o.Liters.GreaterThan(largest) {
			largest = o.Liters
		}
	}
	if !largest.IsPositive() {
		return
	}
	for i := range cards {
		if i >= len(offers) {
			return
		}
		pct := int(offers[i].Liters.Div(largest).Mul(decimal.NewFromInt(10)).Round(0).IntPart()) * 10
		cards[i].Fill = min(max(pct, 20), 100)
	}
}
