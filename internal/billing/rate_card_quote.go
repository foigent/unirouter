package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uniroute/unirouter/internal/enterprisefinance"
)

type RequestPricingAuthority string

const (
	RequestPricingAuthorityLegacy   RequestPricingAuthority = "legacy"
	RequestPricingAuthorityRateCard RequestPricingAuthority = "rate_card"
)

func ValidateRequestPricingAuthority(authority RequestPricingAuthority) error {
	switch authority {
	case RequestPricingAuthorityLegacy, RequestPricingAuthorityRateCard:
		return nil
	default:
		return fmt.Errorf("request pricing authority must be legacy or rate_card")
	}
}

func (b *Billing) WithRequestPricingAuthority(authority RequestPricingAuthority) *Billing {
	if b != nil {
		b.requestPricingAuthority = authority
	}
	return b
}

// queryRateCardPriceQuote is kept outside billing.go so the settlement state
// machine only needs a small resolver switch. It deliberately has no fallback
// to model_prices: the authenticated legacy bootstrap converts those rows to
// reviewable drafts, and missing/ambiguous active authority fails closed.
func (b *Billing) queryRateCardPriceQuote(ctx context.Context, orgID int64, model string, at time.Time) (priceQuote, error) {
	store, err := enterprisefinance.NewPostgresStore(b.db)
	if err != nil {
		return priceQuote{}, err
	}
	quote, err := store.ResolveRequestPriceQuote(ctx, orgID, model, at.UTC().Truncate(time.Microsecond))
	if err != nil {
		if errors.Is(err, enterprisefinance.ErrRequestPricingNotReady) {
			return priceQuote{}, fmt.Errorf("%w: %w", ErrModelPriceNotConfigured, err)
		}
		return priceQuote{}, fmt.Errorf("resolve versioned Rate Card for org %d model %s: %w", orgID, model, err)
	}
	return priceQuoteFromRateCardQuote(quote)
}

func priceQuoteFromRateCardQuote(quote enterprisefinance.RequestPriceQuote) (priceQuote, error) {
	input, err := enterprisefinance.RequestPriceMicros(quote, enterprisefinance.MeterInputTokens)
	if err != nil {
		return priceQuote{}, err
	}
	output, err := enterprisefinance.RequestPriceMicros(quote, enterprisefinance.MeterOutputTokens)
	if err != nil {
		return priceQuote{}, err
	}
	cacheRead, err := enterprisefinance.RequestPriceMicros(quote, enterprisefinance.MeterCacheReadTokens)
	if err != nil {
		return priceQuote{}, err
	}
	cacheCreation, err := enterprisefinance.RequestPriceMicros(quote, enterprisefinance.MeterCacheCreationTokens)
	if err != nil {
		return priceQuote{}, err
	}
	cacheReadRatio, err := requestPriceDisplayRatio(input, cacheRead)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-read display ratio: %w", err)
	}
	cacheCreationRatio, err := requestPriceDisplayRatio(input, cacheCreation)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-creation display ratio: %w", err)
	}
	unitComponents := enterprisefinance.RequestUnitPriceComponents(quote)
	unitRates := make([]UnitRate, 0, len(unitComponents))
	for _, component := range unitComponents {
		rate, err := unitRateFromQuoteComponent(
			component.Meter, component.UnitQuantity, component.AmountMinor,
			component.Currency, component.AmountScale, component.ComponentHash,
		)
		if err != nil {
			return priceQuote{}, err
		}
		unitRates = append(unitRates, rate)
	}
	unitRates, err = normalizeUnitRates(unitRates)
	if err != nil {
		return priceQuote{}, err
	}
	return priceQuote{
		InputPriceMicrosPerMillion: input, CacheReadPriceMicrosPerMillion: cacheRead,
		CacheCreationPriceMicrosPerMillion: cacheCreation, OutputPriceMicrosPerMillion: output,
		CacheReadRatio: cacheReadRatio, CacheCreationRatio: cacheCreationRatio,
		Multiplier: "1", Version: quote.PricingVersion,
		SelectionKey: quote.SelectionKey, RateCardRef: quote.RateCardRef,
		RateCardVersion: quote.RateCardVersion, RateCardLayer: quote.Layer,
		ProviderRef: quote.ProviderRef, UnitRates: unitRates,
	}, nil
}

func requestPriceDisplayRatio(input, cache int64) (float64, error) {
	if input == 0 {
		if cache == 0 {
			return 0, nil
		}
		return 0, fmt.Errorf("nonzero cache price with zero input price")
	}
	return float64(cache) / float64(input), nil
}
