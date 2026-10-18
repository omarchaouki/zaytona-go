package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestFormatNumber(t *testing.T) {
	for in, want := range map[string]string{
		"0":          "0",
		"90":         "90",
		"1250.5":     "1.250,5",
		"2700":       "2.700",
		"8100.00":    "8.100",
		"1234567.89": "1.234.567,89",
		"0.005":      "0,01",
		"999.999":    "1.000",
		"-1250.5":    "-1.250,5",
		"-0.001":     "0",
	} {
		if got := FormatNumber(decimal.RequireFromString(in)); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestFormatMAD(t *testing.T) {
	v := decimal.RequireFromString("1250.5")
	if got := FormatMAD(v, "ar"); got != "1.250,5 درهم" {
		t.Errorf("ar: %q", got)
	}
	if got := FormatMAD(v, "fr"); got != "1.250,5 MAD" {
		t.Errorf("fr: %q", got)
	}
}

func TestSplitEmphasis(t *testing.T) {
	got := SplitEmphasis("L’une des *meilleures huiles du Maroc*, à votre porte")
	if len(got) != 3 || !got[1].Highlighted || got[1].Text != "meilleures huiles du Maroc" || got[2].Text != ", à votre porte" {
		t.Fatalf("%+v", got)
	}
	if s := StripEmphasis("a *b* c *d"); s != "a b c *d" {
		t.Fatalf("strip: %q", s)
	}
	if p := SplitEmphasis("plain"); len(p) != 1 || p[0].Highlighted {
		t.Fatalf("%+v", p)
	}
}
