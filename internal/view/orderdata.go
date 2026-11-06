package view

// OrderFormData is the order form state: blank on first render, filled with the
// submitted values and errors when a plain (no-JS) submission is re-rendered.
type OrderFormData struct {
	FormToken string
	// Submitted (or draft) values, raw strings.
	Choice, CustomLiters, Name, Phone, City, Address, Note string
	// Errors maps a field to its translated message; FormError is the box above
	// the buttons ("" for none).
	Errors    map[string]string
	FormError string
	// Duplicate warning (text already translated), "" when none.
	Duplicate string
	// Step shown first (0 offers, 1 details, 2 review).
	Step int

	Offers []FormOffer // oil, sold by the litre
	Custom *FormCustom
	Jars   []FormJar // jars (amlou...), any quantity alongside the oil
	// Delivery rule for the live total, as plain numbers for the script.
	DeliveryMode, DeliveryFee, DeliveryFreeAbove string
	// Totals as rendered by the server for the current choice.
	Subtotal, DeliveryText, Total string
	TotalLabel                    string
	Lines                         []FormLine

	TurnstileSiteKey string
	// JSON with the messages the script needs (validation, statuses).
	ScriptMessages string
}

// FormOffer is one radio option.
type FormOffer struct {
	Slug, SizeLabel, Name string
	Price                 string // formatted
	PriceRaw              string // "2700.00", for the live total
	Unavailable           bool
}

// FormCustom is the "other quantity" option.
type FormCustom struct {
	Min, Max            int
	PerLiter            string // formatted
	PerLiterRaw         string
	Hint, PerLiterLabel string
	Tiers               string // volume prices for the script: [{"min":30,"price":"85.00"}]
}

// FormJar is a jar product with the quantity chosen ("" for none).
type FormJar struct {
	Slug, Name, SizeLabel string
	Price                 string // formatted, one jar
	PriceRaw              string
	Unavailable           bool
	Qty                   string
}

// QtyName is the form field of a jar's quantity.
func (j FormJar) QtyName() string { return "qty_" + j.Slug }

// FormLine is a line of the review step and the confirmation summary.
type FormLine struct {
	Label, Amount string
	Quantity      int
}

// HasError reports whether field has an error.
func (f OrderFormData) HasError(field string) bool { return f.Errors[field] != "" }

// ConfirmationData is the confirmation page of a placed order.
type ConfirmationData struct {
	Found                      bool
	Reference, FirstName, City string
	Lines                      []FormLine
	DeliveryText, Total        string
	TotalLabel, PayText        string
	WhatsAppText               string // message prefilled with the order template
	// For the Meta Pixel Lead event (once per reference on the device).
	LeadValue, LeadContent string
}
