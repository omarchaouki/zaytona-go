package domain

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func nullDec(s string) decimal.NullDecimal { return decimal.NewNullDecimal(dec(s)) }

func TestValidateOrder(t *testing.T) {
	ok := OrderInput{Choice: "huile-olive-30l", Name: "  Fatima  Zahra ", Phone: "06 12 34 56 78", City: "Marrakech"}
	v, errs := ok.Validate()
	if len(errs) != 0 {
		t.Fatalf("valid input rejected: %v", errs)
	}
	if v.PhoneE164 != "+212612345678" || v.Name != "Fatima  Zahra" {
		t.Fatalf("normalized: %+v", v)
	}

	for _, tc := range []struct {
		in    OrderInput
		field string
		key   string
	}{
		{OrderInput{Name: "Ali", Phone: "0612345678", City: "Rabat"}, "choice", "validation.itemsRequired"},
		{OrderInput{Choice: "custom", Name: "Ali", Phone: "0612345678", City: "Rabat"}, "customLiters", "validation.litersRequired"},
		{OrderInput{Choice: "custom", CustomLiters: "abc", Name: "Ali", Phone: "0612345678", City: "Rabat"}, "customLiters", "validation.litersRange"},
		{OrderInput{Choice: "x", Name: "A", Phone: "0612345678", City: "Rabat"}, "name", "validation.nameRequired"},
		{OrderInput{Choice: "x", Name: strings.Repeat("a", 81), Phone: "0612345678", City: "Rabat"}, "name", "validation.nameTooLong"},
		{OrderInput{Choice: "x", Name: "Ali", City: "Rabat"}, "phone", "validation.phoneRequired"},
		{OrderInput{Choice: "x", Name: "Ali", Phone: "12345", City: "Rabat"}, "phone", "validation.phoneInvalid"},
		{OrderInput{Choice: "x", Name: "Ali", Phone: "0612345678"}, "city", "validation.cityRequired"},
		{OrderInput{Choice: "x", Name: "Ali", Phone: "0612345678", City: "Rabat", Address: strings.Repeat("a", 301)}, "address", "validation.addressTooLong"},
		{OrderInput{Choice: "x", Name: "Ali", Phone: "0612345678", City: "Rabat", Note: strings.Repeat("a", 501)}, "note", "validation.noteTooLong"},
		{OrderInput{Choice: "x", Name: "Ali\xff", Phone: "0612345678", City: "Rabat"}, "name", "validation.nameRequired"},
	} {
		_, errs := tc.in.Validate()
		if errs[tc.field] != tc.key {
			t.Errorf("%+v: %s = %q, want %q (all: %v)", tc.in, tc.field, errs[tc.field], tc.key, errs)
		}
	}
}

func TestCleanText(t *testing.T) {
	got, ok := CleanText(" a\x00b\tc\r\nd ", true)
	if !ok || got != "ab c\nd" {
		t.Fatalf("%q %v", got, ok)
	}
	if got, _ := CleanText("a\nb", false); got != "a b" {
		t.Fatalf("single line: %q", got)
	}
	// NFC: e + combining acute becomes é.
	if got, _ := CleanText("e"+string(rune(0x301)), false); got != string(rune(0xe9)) {
		t.Fatalf("nfc: %q", got)
	}
}

func TestComputeTotals(t *testing.T) {
	lines := []OrderLine{{UnitPrice: dec("2700"), Quantity: 1}, {UnitPrice: dec("90"), Quantity: 2}}
	for _, tc := range []struct {
		name     string
		d        DeliverySettings
		fee      string // "" = confirmed by phone
		total    string
		subtotal string
	}{
		{"confirm", DeliverySettings{Mode: DeliveryConfirm}, "", "2880", "2880"},
		{"flat", DeliverySettings{Mode: DeliveryFlat, Fee: nullDec("30")}, "30", "2910", "2880"},
		{"free", DeliverySettings{Mode: DeliveryFlat, Fee: nullDec("0")}, "0", "2880", "2880"},
		{"free above, reached", DeliverySettings{Mode: DeliveryFreeAbove, Fee: nullDec("40"), FreeAbove: nullDec("2000")}, "0", "2880", "2880"},
		{"free above, below", DeliverySettings{Mode: DeliveryFreeAbove, Fee: nullDec("40"), FreeAbove: nullDec("5000")}, "40", "2920", "2880"},
		{"flat without fee", DeliverySettings{Mode: DeliveryFlat}, "", "2880", "2880"},
	} {
		got := ComputeTotals(lines, tc.d)
		if !got.Subtotal.Equal(dec(tc.subtotal)) || !got.Total.Equal(dec(tc.total)) {
			t.Errorf("%s: %+v", tc.name, got)
		}
		if (tc.fee == "") != !got.DeliveryFee.Valid || (tc.fee != "" && !got.DeliveryFee.Decimal.Equal(dec(tc.fee))) {
			t.Errorf("%s: fee %+v want %q", tc.name, got.DeliveryFee, tc.fee)
		}
	}
	if p := LitersPrice(45, dec("90.5")); !p.Equal(dec("4072.5")) {
		t.Fatalf("liters price %s", p)
	}
}

func TestCleanAttribution(t *testing.T) {
	got := CleanAttribution(map[string]string{"utm_source": " facebook ", "evil": "x", "fbclid": strings.Repeat("a", 400)})
	if got["utm_source"] != "facebook" || got["evil"] != "" || len(got["fbclid"]) != 300 {
		t.Fatalf("%v", got)
	}
	if CleanAttribution(map[string]string{"x": "y"}) != nil {
		t.Fatal("empty attribution should be nil")
	}
}

func TestTemplateItems(t *testing.T) {
	if got := TemplateItems([]string{"30 L", "60 L"}, []int{1, 2}, "ar"); got != "1 × 30 L، 2 × 60 L" {
		t.Fatalf("%q", got)
	}
}
