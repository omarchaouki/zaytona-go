package view

import (
	"github.com/omarchaouki/zaytona-go/internal/domain"
	"github.com/omarchaouki/zaytona-go/internal/i18n"
)

// DeliveryText describes the delivery fee rule (same wording rules as the reference).
func DeliveryText(dl domain.DeliverySettings, l i18n.Locale) string {
	loc := string(l)
	switch {
	case dl.Mode == domain.DeliveryFlat && dl.Fee.Valid && dl.Fee.Decimal.IsZero():
		return i18n.T(l, i18n.WhyUsDeliveryFree)
	case dl.Mode == domain.DeliveryFlat && dl.Fee.Valid:
		return i18n.T(l, i18n.WhyUsDeliveryFlat, "fee", domain.FormatMAD(dl.Fee.Decimal, loc))
	case dl.Mode == domain.DeliveryFreeAbove && dl.Fee.Valid && dl.FreeAbove.Valid:
		return i18n.T(l, i18n.WhyUsDeliveryFreeAbove,
			"fee", domain.FormatMAD(dl.Fee.Decimal, loc), "threshold", domain.FormatMAD(dl.FreeAbove.Decimal, loc))
	}
	return i18n.T(l, i18n.WhyUsDeliveryConfirm)
}
