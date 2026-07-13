package billing

import "math"

const (
	// PublicPriceBasisTokens is the only token basis exposed by the V1 admin API
	// and UI. It matches the convention used by upstream model vendors.
	PublicPriceBasisTokens = 1_000_000

	// internalPriceBasisTokens is retained for the existing decimal database
	// columns and calculation path. It is deliberately not an API contract.
	internalPriceBasisTokens = 1_000

	publicPriceScale = PublicPriceBasisTokens / internalPriceBasisTokens / 100
)

// CNYPerMillionToInternalPriceCents converts the public CNY-per-million-token
// rate to the database's decimal CNY-cent-per-thousand-token rate.
func CNYPerMillionToInternalPriceCents(price float64) float64 {
	return price / publicPriceScale
}

// InternalPriceCentsToCNYPerMillion converts the database calculation rate to
// the public CNY-per-million-token contract.
func InternalPriceCentsToCNYPerMillion(price float64) float64 {
	return price * publicPriceScale
}

// NormalizeCNYPerMillionPrice validates the public rate and rounds it only to
// the precision that NUMERIC(18,6) can represent after conversion. A caller
// can reject a changed value to avoid silent price rounding.
func NormalizeCNYPerMillionPrice(price float64) (float64, bool) {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price >= 1e13 {
		return 0, false
	}
	normalized := math.Round(price*1e5) / 1e5
	if math.Abs(price-normalized) > 1e-9 {
		return 0, false
	}
	return normalized, true
}
