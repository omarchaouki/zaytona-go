package domain

import (
	"strings"
	"testing"
)

func TestNormalizeMoroccanPhone(t *testing.T) {
	for in, want := range map[string]string{
		"0612345678":         "+212612345678",
		"06 12 34 56 78":     "+212612345678",
		"06.12.34.56.78":     "+212612345678",
		"(06) 12-34-56-78":   "+212612345678",
		"+212 6 12 34 56 78": "+212612345678",
		"00212712345678":     "+212712345678",
		"0522123456":         "+212522123456",
		"٠٦١٢٣٤٥٦٧٨":         "+212612345678",
		"۰۶۱۲۳۴۵۶۷۸":         "+212612345678",
		"\u200e0612345678":   "+212612345678",
		"0812345678":         "",
		"061234567":          "",
		"+33612345678":       "",
		"":                   "",
		"06123456789":        "",
	} {
		got, ok := NormalizeMoroccanPhone(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q,%v want %q", in, got, ok, want)
		}
	}
}

func TestFormatPhoneDisplay(t *testing.T) {
	if got := FormatPhoneDisplay("+212612345678"); got != "06 12 34 56 78" {
		t.Fatalf("got %q", got)
	}
	if got := FormatPhoneDisplay("+33612345678"); got != "+33612345678" {
		t.Fatalf("foreign: %q", got)
	}
}

func FuzzNormalizePhone(f *testing.F) {
	for _, s := range []string{"0612345678", "+212 6 12 34 56 78", "٠٦١٢٣٤٥٦٧٨", "", "(", "00212"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := NormalizeMoroccanPhone(in)
		if !ok {
			return
		}
		if len(out) != 13 || !strings.HasPrefix(out, "+212") || !strings.ContainsAny(out[4:5], "567") {
			t.Fatalf("%q -> %q", in, out)
		}
		again, ok := NormalizeMoroccanPhone(out)
		if !ok || again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}

func TestNormalizeContactPhone(t *testing.T) {
	for in, want := range map[string]string{
		"0524 11 22 33":      "+212524112233",
		"+212 6 61 08 50 09": "+212661085009",
		"+39 388 188 48 56":  "+393881884856",
		"0039 388 188 4856":  "+393881884856",
	} {
		if got, ok := NormalizeContactPhone(in); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "388 188 48 56", "+12", "+39 388 188 48 56 77 88 99", "call me"} {
		if got, ok := NormalizeContactPhone(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}
