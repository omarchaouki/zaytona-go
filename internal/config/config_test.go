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

func TestDatabaseURLRules(t *testing.T) {
	cases := []struct {
		name, env, url, want string
	}{
		{"local without TLS", "production", "postgres://zaytona@127.0.0.1:5432/zaytona", ""},
		{"socket", "production", "postgres://zaytona@/zaytona?host=/var/run/postgresql", ""},
		{"remote without TLS", "production", "postgres://u:p@db.abcd.supabase.co:5432/postgres", "needs TLS"},
		{"remote prefer", "production", "postgres://u:p@db.abcd.supabase.co:5432/postgres?sslmode=prefer", "needs TLS"},
		{"remote require", "production", "postgres://u:p@db.abcd.supabase.co:5432/postgres?sslmode=require", ""},
		{"session pooler verify-full", "production", "postgres://postgres.abcd:p@aws-0-eu-west-3.pooler.supabase.com:5432/postgres?sslmode=verify-full&sslrootcert=/etc/zaytona/supabase-ca.crt", ""},
		{"transaction pooler", "production", "postgres://postgres.abcd:p@aws-0-eu-west-3.pooler.supabase.com:6543/postgres?sslmode=require", "transaction pooler"},
		{"remote in development", "development", "postgres://u:p@db.example.com/x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromLookup(lookup(map[string]string{
				"APP_ENV": tc.env, "DATABASE_URL": tc.url, "APP_SECRET": goodSecret, "SITE_URL": "https://example.com",
			}))
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("want an error about %q, got %v", tc.want, err)
			}
		})
	}
}

func TestHostMustBeAnIP(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://localhost/db", "APP_SECRET": goodSecret, "SITE_URL": "https://example.com"}
	base["HOST"] = "127.0.0.1"
	if c, err := FromLookup(lookup(base)); err != nil || c.Host != "127.0.0.1" {
		t.Fatalf("127.0.0.1 refused: %v", err)
	}
	base["HOST"] = "localhost"
	if _, err := FromLookup(lookup(base)); err == nil || !strings.Contains(err.Error(), "HOST") {
		t.Fatalf("host name accepted: %v", err)
	}
}
