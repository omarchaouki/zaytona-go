package domain

import (
	"strings"
	"testing"
)

func TestReviewValidate(t *testing.T) {
	v, errs := ReviewInput{Name: " Fatima  Zahra ", Rating: "5", Comment: "زيت ممتاز وطعم رائع جداً", City: "Rabat", Phone: "06 12 34 56 78"}.Validate()
	if len(errs) != 0 || v.Rating != 5 || v.PhoneE164 != "+212612345678" || v.Language != "ar" || v.Name != "Fatima  Zahra" && v.Name != "Fatima Zahra" {
		t.Fatalf("%+v %v", v, errs)
	}
	_, errs = ReviewInput{Name: "F", Rating: "6", Comment: "court", City: strings.Repeat("x", 81), Phone: "12"}.Validate()
	for _, f := range []string{"name", "rating", "comment", "city", "phone"} {
		if errs[f] == "" {
			t.Errorf("%s accepted", f)
		}
	}
	_, errs = ReviewInput{Name: "Karim", Rating: "4", Comment: strings.Repeat("a", 601)}.Validate()
	if errs["comment"] != "reviews.errors.commentLong" {
		t.Fatalf("long comment: %v", errs)
	}
	if _, errs := (ReviewInput{Name: "Karim", Rating: "4", Comment: "Très bonne huile, merci."}).Validate(); len(errs) != 0 {
		t.Fatalf("optional fields: %v", errs)
	}
}

func TestDisplayNameAndLanguage(t *testing.T) {
	for in, want := range map[string]string{"Fatima Zahra Bennani": "Fatima Zahra B.", "Karim": "Karim", "  ": "", "محمد العلوي": "محمد ا."} {
		if got := DisplayName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if DetectLanguage("Très bonne huile") != "fr" || DetectLanguage("زيت ممتاز") != "ar" || DetectLanguage("Top زيت ok bien") != "fr" || DetectLanguage("123") != "fr" {
		t.Fatal("language")
	}
}
