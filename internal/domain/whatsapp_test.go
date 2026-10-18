package domain

import "testing"

func TestNormalizeWhatsAppNumber(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"0612345678", "212612345678", true},
		{"+212 6 12 34 56 78", "212612345678", true},
		{"00212712345678", "212712345678", true},
		{"212512345678", "212512345678", true},
		{"06-12-34-56-78", "212612345678", true},
		{"12345", "", false},
		{"", "", false},
		{"0812345678", "", false},
	} {
		got, ok := NormalizeWhatsAppNumber(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWhatsAppLinkAndTemplate(t *testing.T) {
	msg := FillTemplate("Salam {name}, commande {reference} {unknown}", map[string]string{"name": "Ali & Co", "reference": "OL-2026-010001"})
	if msg != "Salam Ali & Co, commande OL-2026-010001 {unknown}" {
		t.Fatalf("template: %q", msg)
	}
	if got := WhatsAppLink("212612345678", ""); got != "https://wa.me/212612345678" {
		t.Fatalf("bare link: %s", got)
	}
	if got := WhatsAppLink("212612345678", "a b&c"); got != "https://wa.me/212612345678?text=a+b%26c" {
		t.Fatalf("link: %s", got)
	}
}

func TestSettingsFromRowsKeepsDefaultsOnBadJSON(t *testing.T) {
	s := SettingsFromRows(map[string][]byte{
		"whatsapp": []byte(`{"number":"212612345678"}`),
		"identity": []byte(`not json`),
	})
	if s.WhatsApp.Number != "212612345678" {
		t.Fatalf("whatsapp: %+v", s.WhatsApp)
	}
	if s.Identity.NameFr != "Huilerie El Hadaji" {
		t.Fatalf("identity defaults lost: %+v", s.Identity)
	}
	if s.Pricing.CustomMinLiters != 5 || s.Delivery.Mode != DeliveryConfirm {
		t.Fatalf("defaults: %+v %+v", s.Pricing, s.Delivery)
	}
}
