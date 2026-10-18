package domain

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	waNonDigits   = regexp.MustCompile(`[^\d+]`)
	waLocalMobile = regexp.MustCompile(`^0[5-7]\d{8}$`)
	waValid       = regexp.MustCompile(`^[1-9]\d{7,14}$`)
	templateVar   = regexp.MustCompile(`\{(\w+)\}`)
)

// NormalizeWhatsAppNumber turns any common input into international digits
// ("0612345678", "+212 6 12 34 56 78", "00212..." -> "212612345678"). ok is false
// when the result is not a plausible number.
func NormalizeWhatsAppNumber(input string) (string, bool) {
	n := waNonDigits.ReplaceAllString(input, "")
	n = strings.TrimPrefix(n, "+")
	n = strings.TrimPrefix(n, "00")
	if waLocalMobile.MatchString(n) {
		n = "212" + n[1:]
	}
	if !waValid.MatchString(n) {
		return "", false
	}
	return n, true
}

// WhatsAppLink builds https://wa.me/<digits>?text=<encoded>.
func WhatsAppLink(number, text string) string {
	link := "https://wa.me/" + number
	if text != "" {
		link += "?text=" + url.QueryEscape(text)
	}
	return link
}

// FillTemplate replaces {placeholders}; unknown ones are left as they are.
func FillTemplate(template string, values map[string]string) string {
	return templateVar.ReplaceAllStringFunc(template, func(m string) string {
		if v, ok := values[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}
