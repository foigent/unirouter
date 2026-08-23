package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
)

func TestRateCardQuoteAdapterPreservesFrozenAuthorityIdentity(t *testing.T) {
	quote := enterprisefinance.RequestPriceQuote{
		PricingVersion: "rcq2_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Components: []enterprisefinance.RequestPriceComponent{
			{Meter: enterprisefinance.MeterInputTokens, PriceMicrosPerMillion: 10},
			{Meter: enterprisefinance.MeterOutputTokens, PriceMicrosPerMillion: 30},
			{Meter: enterprisefinance.MeterCacheReadTokens, PriceMicrosPerMillion: 2},
			{Meter: enterprisefinance.MeterCacheCreationTokens, PriceMicrosPerMillion: 12},
		},
	}
	converted, err := priceQuoteFromRateCardQuote(quote)
	require.NoError(t, err)
	require.Equal(t, quote.PricingVersion, converted.Version)
	require.Equal(t, int64(10), converted.InputPriceMicrosPerMillion)
	require.Equal(t, int64(30), converted.OutputPriceMicrosPerMillion)
	require.Equal(t, 0.2, converted.CacheReadRatio)
	require.Equal(t, 1.2, converted.CacheCreationRatio)
}

func TestRateCardQuoteAdapterRejectsUndefinedCacheRatio(t *testing.T) {
	_, err := requestPriceDisplayRatio(0, 1)
	require.Error(t, err)
	require.Equal(t, float64(0), mustDisplayRatio(t, 0, 0))
}

func TestRateCardQuoteAdapterPreservesExactNonTokenRational(t *testing.T) {
	quote := enterprisefinance.RequestPriceQuote{
		PricingVersion: "rcq3_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Components: []enterprisefinance.RequestPriceComponent{
			{Meter: enterprisefinance.MeterInputTokens, PriceMicrosPerMillion: 10},
			{Meter: enterprisefinance.MeterOutputTokens, PriceMicrosPerMillion: 30},
			{Meter: enterprisefinance.MeterCacheReadTokens, PriceMicrosPerMillion: 2},
			{Meter: enterprisefinance.MeterCacheCreationTokens, PriceMicrosPerMillion: 12},
			{
				Meter:        enterprisefinance.MeterBuiltinWebSearchCalls,
				UnitQuantity: "100", AmountMinor: "3000000", Currency: "CNY", AmountScale: 1_000_000,
				ComponentHash: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			},
		},
	}
	converted, err := priceQuoteFromRateCardQuote(quote)
	require.NoError(t, err)
	require.Equal(t, []UnitRate{{
		Meter:        enterprisefinance.MeterBuiltinWebSearchCalls,
		UnitQuantity: 100, AmountMinor: 3_000_000, Currency: "CNY", AmountScale: 1_000_000,
		ComponentHash: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}}, converted.UnitRates)
}

func TestRequestPricingAuthorityIsExplicitAndDefaultsToUpgradeCompatibility(t *testing.T) {
	service := NewBilling(nil, nil)
	require.Equal(t, RequestPricingAuthorityLegacy, service.requestPricingAuthority)
	require.NoError(t, ValidateRequestPricingAuthority(RequestPricingAuthorityLegacy))
	require.NoError(t, ValidateRequestPricingAuthority(RequestPricingAuthorityRateCard))
	require.Error(t, ValidateRequestPricingAuthority("auto"))

	service.WithRequestPricingAuthority(RequestPricingAuthorityRateCard)
	require.Equal(t, RequestPricingAuthorityRateCard, service.requestPricingAuthority)
}

func mustDisplayRatio(t *testing.T, input, cache int64) float64 {
	t.Helper()
	value, err := requestPriceDisplayRatio(input, cache)
	require.NoError(t, err)
	return value
}
