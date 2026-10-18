package domain

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Review rules shared by the form and the server (same as the reference).
const (
	ReviewCommentMin = 10
	ReviewCommentMax = 600
	ReviewNameMax    = 60
	ReviewCityMax    = 80
	// ReviewPhotoMaxBytes caps a customer photo (5 MB, PROMPT.md section 12).
	ReviewPhotoMaxBytes = 5 << 20
)

// ReviewInput is the raw review form.
type ReviewInput struct {
	Name, Rating, Comment, City, Phone string
}

// ValidReview is a review ready to store.
type ValidReview struct {
	Name, Comment, City string
	Rating              int
	// PhoneE164 is empty when no phone was given (never displayed, only hashed).
	PhoneE164 string
	Language  string // "ar" or "fr", from the text
}

// Validate cleans the fields and returns i18n keys for the bad ones.
func (in ReviewInput) Validate() (ValidReview, FieldErrors) {
	errs := FieldErrors{}
	var v ValidReview
	name, ok := CleanText(in.Name, false)
	if n := utf8.RuneCountInString(name); !ok || n < 2 || n > ReviewNameMax {
		errs["name"] = "reviews.errors.name"
	}
	v.Name = name
	if r, err := strconv.Atoi(strings.TrimSpace(in.Rating)); err != nil || r < 1 || r > 5 {
		errs["rating"] = "reviews.errors.rating"
	} else {
		v.Rating = r
	}
	comment, ok := CleanText(in.Comment, true)
	switch n := utf8.RuneCountInString(comment); {
	case !ok || n < ReviewCommentMin:
		errs["comment"] = "reviews.errors.commentShort"
	case n > ReviewCommentMax:
		errs["comment"] = "reviews.errors.commentLong"
	}
	v.Comment = comment
	city, ok := CleanText(in.City, false)
	if !ok || utf8.RuneCountInString(city) > ReviewCityMax {
		errs["city"] = "reviews.errors.city"
	}
	v.City = city
	if p := strings.TrimSpace(in.Phone); p != "" {
		if e164, ok := NormalizeMoroccanPhone(p); ok {
			v.PhoneE164 = e164
		} else {
			errs["phone"] = "validation.phoneInvalid"
		}
	}
	v.Language = DetectLanguage(comment)
	return v, errs
}

// DisplayName shows the first names and the initial of the last one
// ("Fatima Zahra Bennani" -> "Fatima Zahra B."): no full surname online.
func DisplayName(name string) string {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	last, _ := utf8.DecodeRuneInString(parts[len(parts)-1])
	return strings.Join(parts[:len(parts)-1], " ") + " " + string(last) + "."
}

// DetectLanguage is "ar" when more than 30% of the letters are Arabic, "fr" otherwise.
func DetectLanguage(text string) string {
	letters, arabic := 0, 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if r >= 0x0600 && r <= 0x06FF {
			arabic++
		}
	}
	if letters > 0 && float64(arabic)/float64(letters) > 0.3 {
		return "ar"
	}
	return "fr"
}
