package domain

import (
	"regexp"
	"strings"
)

var moroccanPhone = regexp.MustCompile(`^(?:\+212|00212|0)([5-7]\d{8})$`)

// NormalizeMoroccanPhone accepts Moroccan mobile and landline numbers in any
// common format (spaces, dots, dashes, parentheses, 00212, Arabic-Indic digits)
// and returns E.164 (+212XXXXXXXXX). ok is false otherwise.
func NormalizeMoroccanPhone(input string) (string, bool) {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= '٠' && r <= '٩': // Arabic-Indic
			b.WriteRune('0' + (r - '٠'))
		case r >= '۰' && r <= '۹': // Eastern Arabic-Indic
			b.WriteRune('0' + (r - '۰'))
		case r == ' ' || r == '.' || r == '-' || r == '(' || r == ')' || r == '\u200e' || r == '\u200f' || r == '\t' || r == '\u00a0':
		default:
			b.WriteRune(r)
		}
	}
	m := moroccanPhone.FindStringSubmatch(b.String())
	if m == nil {
		return "", false
	}
	return "+212" + m[1], true
}

// FormatPhoneDisplay shows a stored E.164 number: Moroccan ones locally
// (+212612345678 -> "06 12 34 56 78"), others unchanged.
func FormatPhoneDisplay(e164 string) string {
	if len(e164) != 13 || !strings.HasPrefix(e164, "+212") {
		return e164
	}
	if _, ok := NormalizeMoroccanPhone(e164); !ok {
		return e164
	}
	l := "0" + e164[4:]
	return l[0:2] + " " + l[2:4] + " " + l[4:6] + " " + l[6:8] + " " + l[8:10]
}
