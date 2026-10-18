package domain

import (
	"strings"

	"github.com/shopspring/decimal"
)

// FormatNumber prints a number the Moroccan way with Western digits in both
// locales: "." groups thousands, "," marks decimals, at most 2 decimals, no
// trailing zeros (1250.5 -> "1.250,5"). Rounds half away from zero like Intl.
func FormatNumber(v decimal.Decimal) string {
	v = v.Round(2)
	neg := v.IsNegative()
	s := v.Abs().StringFixed(2)
	intPart, frac, _ := strings.Cut(s, ".")
	frac = strings.TrimRight(frac, "0")

	var b strings.Builder
	if neg && (intPart != "0" || frac != "") {
		b.WriteByte('-')
	}
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteByte(',')
		b.WriteString(frac)
	}
	return b.String()
}

// FormatMAD formats an amount: "1.250,5 درهم" (ar) / "1.250,5 MAD" (fr).
func FormatMAD(v decimal.Decimal, locale string) string {
	if locale == "ar" {
		return FormatNumber(v) + " درهم"
	}
	return FormatNumber(v) + " MAD"
}

// EmphasisPart is one run of a "plain *highlighted* plain" title.
type EmphasisPart struct {
	Text        string
	Highlighted bool
}

// SplitEmphasis splits admin-editable titles where *words* get the accent style.
func SplitEmphasis(s string) []EmphasisPart {
	var parts []EmphasisPart
	for {
		start := strings.IndexByte(s, '*')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start+1:], '*')
		if end <= 0 { // no closing star, or "**"
			break
		}
		if start > 0 {
			parts = append(parts, EmphasisPart{Text: s[:start]})
		}
		parts = append(parts, EmphasisPart{Text: s[start+1 : start+1+end], Highlighted: true})
		s = s[start+end+2:]
	}
	if s != "" {
		parts = append(parts, EmphasisPart{Text: s})
	}
	return parts
}

// StripEmphasis removes the *stars* (page titles, meta descriptions).
func StripEmphasis(s string) string {
	var b strings.Builder
	for _, p := range SplitEmphasis(s) {
		b.WriteString(p.Text)
	}
	return b.String()
}
