package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestPerLiterForTiers(t *testing.T) {
	p := PricingSettings{
		PricePerLiter: decimal.NewNullDecimal(d("90")),
		// Out of order on purpose: the largest threshold reached wins.
		Tiers: []PriceTier{{MinLiters: 60, PricePerLiter: d("80")}, {MinLiters: 30, PricePerLiter: d("85")}},
	}
	for _, tc := range []struct {
		liters, want string
	}{{"1", "90"}, {"29", "90"}, {"30", "85"}, {"59.5", "85"}, {"60", "80"}, {"500", "80"}} {
		got, ok := p.PerLiterFor(d(tc.liters))
		if !ok || !got.Equal(d(tc.want)) {
			t.Errorf("%s L: %s, want %s", tc.liters, got, tc.want)
		}
	}
	if price, _ := p.OfferPrice(d("30")); !price.Equal(d("2550")) {
		t.Errorf("30 L offer: %s", price)
	}
	if _, ok := (PricingSettings{}).PerLiterFor(d("10")); ok {
		t.Error("price without a base price")
	}
	if s := p.SortedTiers(); s[0].MinLiters != 30 || s[1].MinLiters != 60 {
		t.Errorf("sorted: %+v", s)
	}
}

func TestValidateTiers(t *testing.T) {
	ok := []PriceTier{{MinLiters: 30, PricePerLiter: d("85")}, {MinLiters: 60, PricePerLiter: d("80")}}
	if k := ValidateTiers(ok); k != "" {
		t.Fatal(k)
	}
	for name, tiers := range map[string][]PriceTier{
		"admin.offers.tierMinRange":   {{MinLiters: 1, PricePerLiter: d("85")}},
		"admin.offers.tierDuplicate":  {{MinLiters: 30, PricePerLiter: d("85")}, {MinLiters: 30, PricePerLiter: d("80")}},
		"admin.offers.tierPriceRange": {{MinLiters: 30, PricePerLiter: d("0")}},
		"admin.offers.tierTooMany":    {{MinLiters: 2, PricePerLiter: d("1")}, {MinLiters: 3, PricePerLiter: d("1")}, {MinLiters: 4, PricePerLiter: d("1")}, {MinLiters: 5, PricePerLiter: d("1")}, {MinLiters: 6, PricePerLiter: d("1")}},
	} {
		if k := ValidateTiers(tiers); k != name {
			t.Errorf("want %s, got %q", name, k)
		}
	}
}
