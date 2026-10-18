package config

import (
	"strings"
	"testing"
)

const goodSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestProductionRefusesExampleSecretAndHTTP(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{
		"DATABASE_URL": "postgres://localhost/db",
		"APP_SECRET":   ExampleSecret,
		"SITE_URL":     "http://example.com",
	}))
	if err == nil {
		t.Fatal("accepted example secret in production")
	}
	for _, want := range []string{"APP_SECRET", "https"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestDevelopmentAcceptsExampleSecret(t *testing.T) {
	c, err := FromLookup(lookup(map[string]string{
		"APP_ENV":      "development",
		"DATABASE_URL": "postgres://localhost/db",
		"APP_SECRET":   ExampleSecret,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.IsProduction() || c.Port != 3000 || c.MaxUploadMB != 5 {
		t.Fatalf("%+v", c)
	}
}

func TestDerivedKeysDifferPerPurpose(t *testing.T) {
	c, err := FromLookup(lookup(map[string]string{
		"DATABASE_URL": "postgres://localhost/db",
		"APP_SECRET":   goodSecret,
		"SITE_URL":     "https://example.com/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	a, b := c.DeriveKey(KeyFormToken), c.DeriveKey(KeyPhoneHash)
	if len(a) != 32 || string(a) == string(b) {
		t.Fatal("derived keys must be 32 bytes and distinct")
	}
	if string(a) != string(c.DeriveKey(KeyFormToken)) {
		t.Fatal("derivation not deterministic")
	}
	if c.SiteURL.String() != "https://example.com" {
		t.Fatalf("site url %s", c.SiteURL)
	}
}

func TestShortSecretRefused(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{
		"DATABASE_URL": "postgres://localhost/db",
		"APP_SECRET":   "abcd",
		"SITE_URL":     "https://example.com",
	}))
	if err == nil {
		t.Fatal("short secret accepted")
	}
}
