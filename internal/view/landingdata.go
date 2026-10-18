package view

// LandingData is the landing page content, already localised and formatted
// by the handler. Views only lay it out.
type LandingData struct {
	// Hero texts (admin content or translations) and the price anchor ("" hides it).
	HeroEyebrow, HeroTitle, HeroSubtitle string
	PriceFrom, PricePerLiter             string
	HeroImage                            Image // full-bleed background, the LCP image
	// Rating from approved, non-example reviews; nil until there is one.
	Rating *Rating

	Offers []OfferCard
	Custom *CustomCard

	ONSSA      *Certificate
	OtherCerts []Certificate
	HasLab     bool

	// Why us
	CODText, DeliveryText, DeliveryTime, ReturnsText, PrivacyText string
	Hours, ResponseTime                                           string
	Delivered                                                     string // formatted count, "" hides
	LegalName, ICE, RC                                            string

	StoryTitle      string
	StoryParagraphs []string
	Gallery         []GalleryItem

	FAQs []FAQItem

	// Reviews section (nil when the owner hides it).
	Reviews *ReviewsData

	// Serialized JSON-LD documents (escaped for <script>).
	JSONLD []string
}

// Rating is a review aggregate.
type Rating struct {
	Average      float64
	AverageText  string // formatted for the locale
	CountText    string
	StarsLabel   string
	SummaryLabel string // "4.8/5 · 12 reviews"
}

// OfferCard is one litre offer.
type OfferCard struct {
	Slug, Name, SizeLabel, Tagline string
	Price, PerLiter                string
	Image                          Image
	ImageAlt                       string
	OutOfStock, LowStock           bool
	OrderLabel                     string // accessible name of the order link
	Fill                           int    // jerrycan fill against the largest offer: 20 to 100, steps of 10
}

// CustomCard is the "other quantity" offer and the litre gauge: PerLiterRaw
// ("90.00"), Min and Max feed the live total; the gauge posts nothing.
type CustomCard struct {
	PerLiter, PerLiterRaw string
	Min, Max              int
	Body, Hint            string
}

// Certificate is a public certificate.
type Certificate struct {
	Label, Number, Issued, Expires string
	Scan                           Image
	ScanAlt                        string
}

// GalleryItem is one story photo.
type GalleryItem struct {
	Image   Image
	Caption string
}

// FAQItem is one question; Answer is admin text, sanitized when rendered.
type FAQItem struct {
	Question, Answer string
}

// PolicyData is a privacy or terms page.
type PolicyData struct {
	Title string
	HTML  string // admin HTML (sanitized when rendered); "" shows the "not published" note
	Extra string // trusted HTML appended by the server (pixel notice), sanitized too
}
