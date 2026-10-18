package domain

import (
	"strconv"
	"time"
)

// Month names from CLDR (Go has no CLDR): ar-MA uses the Moroccan names.
// This is locale data, not copy, so it lives here rather than in the i18n files.
var (
	monthsAr = [12]string{"يناير", "فبراير", "مارس", "أبريل", "ماي", "يونيو", "يوليوز", "غشت", "شتنبر", "أكتوبر", "نونبر", "دجنبر"}
	monthsFr = [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

// FormatDayLong prints a calendar day as "9 octobre 2026" / "9 أكتوبر 2026"
// (Western digits in both locales). Only the date part of t is used.
func FormatDayLong(t time.Time, locale string) string {
	months := monthsFr
	if locale == "ar" {
		months = monthsAr
	}
	return strconv.Itoa(t.Day()) + " " + months[t.Month()-1] + " " + strconv.Itoa(t.Year())
}
