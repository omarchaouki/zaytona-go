package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"golang.org/x/text/unicode/norm"
)

// Order rules shared by the handler (authoritative validation, prices from the
// DB only) and mirrored by the form script (live feedback). Error values are
// i18n keys under validation.*.

// CustomChoice is the offer value for "other quantity" (litres typed by the customer).
const CustomChoice = "custom"

// OrderInput is what the customer submits. Prices never come from the client.
type OrderInput struct {
	Choice       string // offer slug, or CustomChoice
	CustomLiters string // digits, only with CustomChoice
	Name         string
	Phone        string // any common format; normalized to E.164 by Validate
	City         string
	Address      string
	Note         string
}

// ValidOrder is a validated, normalized input.
type ValidOrder struct {
	Choice       string
	CustomLiters int // 0 unless Choice == CustomChoice
	Name         string
	PhoneE164    string
	City         string
	Address      string
	Note         string
}

// FieldErrors maps a form field to an i18n key.
type FieldErrors map[string]string

// CleanText normalizes user text: NFC, control characters removed (newlines
// kept when multiline), surrounding space trimmed. ok is false for invalid UTF-8.
func CleanText(s string, multiline bool) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	s = norm.NFC.String(strings.ReplaceAll(s, "\r\n", "\n"))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\t' || r == '\n':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String()), true
}

// Validate checks an order input; a non-empty FieldErrors means rejection.
// Limits mirror the reference (name 2..80, city 2..80, address 300, note 500).
func (in OrderInput) Validate() (ValidOrder, FieldErrors) {
	errs := FieldErrors{}
	var v ValidOrder
	text := func(field, s string, multiline bool) string {
		out, ok := CleanText(s, multiline)
		if !ok {
			errs[field] = "validation.invalid"
		}
		return out
	}
	runes := utf8.RuneCountInString

	v.Choice = strings.TrimSpace(in.Choice)
	switch {
	case v.Choice == "":
		errs["choice"] = "validation.itemsRequired"
	case v.Choice == CustomChoice:
		n, err := strconv.Atoi(strings.TrimSpace(in.CustomLiters))
		switch {
		case strings.TrimSpace(in.CustomLiters) == "":
			errs["customLiters"] = "validation.litersRequired"
		case err != nil || n < 1 || n > 999:
			errs["customLiters"] = "validation.litersRange"
		default:
			v.CustomLiters = n
		}
	case len(v.Choice) > 64:
		errs["choice"] = "validation.invalid"
	}

	v.Name = text("name", in.Name, false)
	switch n := runes(v.Name); {
	case n < 2:
		errs["name"] = "validation.nameRequired"
	case n > 80:
		errs["name"] = "validation.nameTooLong"
	}

	if strings.TrimSpace(in.Phone) == "" {
		errs["phone"] = "validation.phoneRequired"
	} else if p, ok := NormalizeMoroccanPhone(in.Phone); ok && len(in.Phone) <= 40 {
		v.PhoneE164 = p
	} else {
		errs["phone"] = "validation.phoneInvalid"
	}

	v.City = text("city", in.City, false)
	switch n := runes(v.City); {
	case n < 2:
		errs["city"] = "validation.cityRequired"
	case n > 80:
		errs["city"] = "validation.cityTooLong"
	}

	v.Address = text("address", in.Address, true)
	if runes(v.Address) > 300 {
		errs["address"] = "validation.addressTooLong"
	}
	v.Note = text("note", in.Note, true)
	if runes(v.Note) > 500 {
		errs["note"] = "validation.noteTooLong"
	}
	return v, errs
}

// FieldStep is the form step (0, 1, 2) that holds a field, for routing errors.
func FieldStep(field string) int {
	switch field {
	case "choice", "customLiters":
		return 0
	case "name", "phone", "city", "address", "note":
		return 1
	}
	return 2
}

// OrderLine is one priced line of an order.
type OrderLine struct {
	OfferID   string // empty for "other quantity"
	NameAr    string
	NameFr    string
	Liters    decimal.Decimal // per unit
	UnitPrice decimal.Decimal
	Quantity  int
}

// Totals of an order. DeliveryFee is invalid (null) when confirmed by phone.
type Totals struct {
	Subtotal    decimal.Decimal
	DeliveryFee decimal.NullDecimal
	Total       decimal.Decimal
}

// ComputeTotals applies the delivery rules from Settings to the lines.
func ComputeTotals(lines []OrderLine, d DeliverySettings) Totals {
	sub := decimal.Zero
	for _, l := range lines {
		sub = sub.Add(l.UnitPrice.Mul(decimal.NewFromInt(int64(l.Quantity))))
	}
	sub = sub.Round(2)
	var fee decimal.NullDecimal
	switch d.Mode {
	case DeliveryFlat:
		fee = d.Fee
	case DeliveryFreeAbove:
		if d.Fee.Valid {
			fee = d.Fee
			if d.FreeAbove.Valid && sub.GreaterThanOrEqual(d.FreeAbove.Decimal) {
				fee = decimal.NewNullDecimal(decimal.Zero)
			}
		}
	}
	total := sub
	if fee.Valid {
		total = total.Add(fee.Decimal)
	}
	return Totals{Subtotal: sub, DeliveryFee: fee, Total: total.Round(2)}
}

// LitersPrice is the price of liters litres at pricePerLiter, to the centime.
func LitersPrice(liters int, pricePerLiter decimal.Decimal) decimal.Decimal {
	return pricePerLiter.Mul(decimal.NewFromInt(int64(liters))).Round(2)
}

// CSVCell neutralises spreadsheet formulas in exported text: a cell starting
// with =, +, -, @, tab or carriage return gets a leading quote.
func CSVCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// AttributionKeys are the first-touch ad tags kept with an order.
var AttributionKeys = []string{"utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term", "fbclid"}

// CleanAttribution keeps known keys only, trimmed and capped at 300 characters.
// It returns nil when nothing is left.
func CleanAttribution(raw map[string]string) map[string]string {
	out := map[string]string{}
	for _, k := range AttributionKeys {
		v, ok := CleanText(raw[k], false)
		if !ok || v == "" {
			continue
		}
		if r := []rune(v); len(r) > 300 {
			v = string(r[:300])
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TemplateItems lists lines for the WhatsApp templates: "1 × name, 1 × name"
// (Arabic comma in Arabic).
func TemplateItems(names []string, quantities []int, locale string) string {
	sep := ", "
	if locale == "ar" {
		sep = "، "
	}
	parts := make([]string, len(names))
	for i := range names {
		parts[i] = strconv.Itoa(quantities[i]) + " × " + names[i]
	}
	return strings.Join(parts, sep)
}
