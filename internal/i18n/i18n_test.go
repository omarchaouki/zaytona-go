package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestLocalesHaveSameKeys(t *testing.T) {
	if d := Diff(messages[AR], messages[FR]); len(d) > 0 {
		t.Errorf("missing in fr.json: %v", d)
	}
	if d := Diff(messages[FR], messages[AR]); len(d) > 0 {
		t.Errorf("missing in ar.json: %v", d)
	}
}

func TestGeneratedKeysMatchFiles(t *testing.T) {
	if len(AllKeys) != len(messages[AR]) {
		t.Fatalf("keys_gen.go has %d keys, ar.json %d: run task generate", len(AllKeys), len(messages[AR]))
	}
	for _, k := range AllKeys {
		if _, ok := messages[AR][string(k)]; !ok {
			t.Errorf("generated key %q not in ar.json: run task generate", k)
		}
	}
}

func TestPlaceholdersMatchAcrossLocales(t *testing.T) {
	re := regexp.MustCompile(`\{(\w+)\}`)
	names := func(s string) string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			out = append(out, m[1])
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for k, ar := range messages[AR] {
		if a, f := names(ar), names(messages[FR][k]); a != f {
			t.Errorf("%s: ar {%s} vs fr {%s}", k, a, f)
		}
	}
}

func TestNoEasternArabicDigits(t *testing.T) {
	for k, v := range messages[AR] {
		if strings.ContainsAny(v, "٠١٢٣٤٥٦٧٨٩") {
			t.Errorf("%s uses Eastern Arabic digits; Western digits in both locales", k)
		}
	}
}

func TestT(t *testing.T) {
	if got := T(FR, HeroRating, "average", "4.8", "count", "12"); got != "4.8/5 · 12 avis" {
		t.Fatalf("got %q", got)
	}
	if T(AR, NavSwitchTo) != "Français" {
		t.Fatalf("ar switch label: %q", T(AR, NavSwitchTo))
	}
	if got := T(FR, Key("nope.missing")); got != "nope.missing" {
		t.Fatalf("missing key: %q", got)
	}
}

func TestParseAndMeta(t *testing.T) {
	if _, ok := Parse("en"); ok {
		t.Fatal("en accepted")
	}
	l, ok := Parse("ar")
	if !ok || l.Meta().Dir != "rtl" || l.Other() != FR {
		t.Fatal("ar meta")
	}
}
