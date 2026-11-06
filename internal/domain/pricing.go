package domain

import (
	"sort"

	"github.com/shopspring/decimal"
)

// PriceTier is a volume price: from MinLiters litres in one line, every litre
// costs PricePerLiter instead of the base price (a promotion for large orders).
type PriceTier struct {
	MinLiters     int             `json:"minLiters"`
	PricePerLiter decimal.Decimal `json:"pricePerLiter"`
}

// MaxPriceTiers is how many volume tiers the admin can set.
const MaxPriceTiers = 4

// PerLiterFor is the price of one litre in a line of liters litres: the tier
// with the largest MinLiters not above liters, else the base price. ok is
// false when no base price is set.
func (p PricingSettings) PerLiterFor(liters decimal.Decimal) (decimal.Decimal, bool) {
	if !p.PricePerLiter.Valid {
		return decimal.Zero, false
	}
	price := p.PricePerLiter.Decimal
	best := 0
	for _, t := range p.Tiers {
		if t.MinLiters > best && liters.GreaterThanOrEqual(decimal.NewFromInt(int64(t.MinLiters))) {
			best, price = t.MinLiters, t.PricePerLiter
		}
	}
	return price, true
}

// OfferPrice is the price of an oil offer of liters litres that follows the
// price per litre (tiers included), to the centime.
func (p PricingSettings) OfferPrice(liters decimal.Decimal) (decimal.Decimal, bool) {
	ppl, ok := p.PerLiterFor(liters)
	if !ok {
		return decimal.Zero, false
	}
	return ppl.Mul(liters).Round(2), true
}

// SortedTiers returns the tiers by increasing MinLiters (for display and the script).
func (p PricingSettings) SortedTiers() []PriceTier {
	out := append([]PriceTier(nil), p.Tiers...)
	sort.Slice(out, func(i, j int) bool { return out[i].MinLiters < out[j].MinLiters })
	return out
}

// ValidateTiers checks volume tiers: at most MaxPriceTiers, each from 2 to
// 10000 litres, no two with the same threshold, a positive price below 100000.
// The error is an i18n key under admin.offers.
func ValidateTiers(tiers []PriceTier) string {
	if len(tiers) > MaxPriceTiers {
		return "admin.offers.tierTooMany"
	}
	seen := map[int]bool{}
	for _, t := range tiers {
		switch {
		case t.MinLiters < 2 || t.MinLiters > 10000:
			return "admin.offers.tierMinRange"
		case seen[t.MinLiters]:
			return "admin.offers.tierDuplicate"
		case !t.PricePerLiter.IsPositive() || t.PricePerLiter.GreaterThanOrEqual(decimal.NewFromInt(100000)):
			return "admin.offers.tierPriceRange"
		}
		seen[t.MinLiters] = true
	}
	return ""
}
