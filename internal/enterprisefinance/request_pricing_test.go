package enterprisefinance

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestPriceQuotePrefersContractAndBindsCompleteManifest(t *testing.T) {
	at := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	provider := requestPricingCard("provider", RateProviderList, "provider-a", "model-a", at.Add(-time.Hour), 3, 5)
	provider.SourceEvidenceRef = "source-provider"
	contract := requestPricingCard("contract", RateSupplierContract, "supplier-b", "model-a", at.Add(-time.Hour), 2, 4)

	quote, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: provider}, {Card: contract}}, 7, "model-a", at)
	require.NoError(t, err)
	require.Equal(t, RateSupplierContract, quote.Layer)
	require.Equal(t, "contract", quote.RateCardRef)
	require.Equal(t, "supplier-b", quote.ProviderRef)
	require.Equal(t, RequestPricingKey, quote.SelectionKey)
	require.Len(t, quote.Components, 4)
	require.Regexp(t, `^rcq2_[a-f0-9]{64}$`, quote.PricingVersion)
	require.Regexp(t, `^sha256:[a-f0-9]{64}$`, quote.ManifestSHA256)
	require.LessOrEqual(t, len(quote.PricingVersion), 80)

	input, err := RequestPriceMicros(quote, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(2), input)
	output, err := RequestPriceMicros(quote, MeterOutputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(4), output)

	changed := contract
	changed.Components[0].Amount.ValueMinor = "4"
	changedQuote, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: changed}}, 7, "model-a", at)
	require.NoError(t, err)
	require.NotEqual(t, quote.PricingVersion, changedQuote.PricingVersion)
}

func TestRequestPriceQuoteSelectsUTCMinuteBandAndBindsCompleteSchedule(t *testing.T) {
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	card := requestPricingCard("scheduled", RateSupplierContract, "provider-a", "model-a", day.Add(-time.Hour), 99, 199)
	card.DailyPriceBands = []DailyPriceBand{
		{BandRef: "peak", StartMinuteUTC: 12 * 60, EndMinuteUTC: 24 * 60, Components: requestPricingCard("x", RateSupplierContract, "p", "m", day, 20, 40).Components},
		{BandRef: "base", StartMinuteUTC: 0, EndMinuteUTC: 6 * 60, Components: requestPricingCard("x", RateSupplierContract, "p", "m", day, 10, 20).Components},
		{BandRef: "valley", StartMinuteUTC: 6 * 60, EndMinuteUTC: 12 * 60, Components: requestPricingCard("x", RateSupplierContract, "p", "m", day, 2, 4).Components},
	}

	valley, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, "model-a", day.Add(7*time.Hour+59*time.Second))
	require.NoError(t, err)
	require.Equal(t, "UTC", valley.PricingTimezone)
	require.Equal(t, "valley", valley.SelectedBand.BandRef)
	require.Equal(t, 6*60, valley.SelectedBand.StartMinuteUTC)
	require.Len(t, valley.DailyPriceBands, 3)
	require.Equal(t, []string{"base", "valley", "peak"}, []string{
		valley.DailyPriceBands[0].BandRef, valley.DailyPriceBands[1].BandRef, valley.DailyPriceBands[2].BandRef,
	})
	require.Regexp(t, `^sha256:[a-f0-9]{64}$`, valley.ScheduleSHA256)
	input, err := RequestPriceMicros(valley, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(2), input)

	peak, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, "model-a", day.Add(12*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "peak", peak.SelectedBand.BandRef)
	input, err = RequestPriceMicros(peak, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(20), input)
	require.NotEqual(t, valley.PricingVersion, peak.PricingVersion, "selected band is part of the frozen quote")

	changedUnselected := card
	changedUnselected.DailyPriceBands = canonicalDailyPriceBands(card.DailyPriceBands)
	changedUnselected.DailyPriceBands[0].Components[0].Amount.ValueMinor = "11"
	changed, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: changedUnselected}}, 7, "model-a", day.Add(7*time.Hour))
	require.NoError(t, err)
	require.NotEqual(t, valley.ScheduleSHA256, changed.ScheduleSHA256)
	require.NotEqual(t, valley.PricingVersion, changed.PricingVersion, "unselected bands remain bound into the manifest")
}

func TestRequestPriceQuoteSelectsExactUTCWeekdaySchedule(t *testing.T) {
	monday := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	require.Equal(t, time.Monday, monday.Weekday())
	base := requestPricingCard("x", RateSupplierContract, "p", "m", monday, 5, 45).Components
	peak := requestPricingCard("x", RateSupplierContract, "p", "m", monday, 10, 90).Components
	card := requestPricingCard("weekly", RateSupplierContract, "deepseek", "DeepSeek V4 Flash", monday.Add(-7*24*time.Hour), 5, 45)
	weekdays := []int{1, 2, 3, 4, 5}
	card.DailyPriceBands = []DailyPriceBand{
		{BandRef: "weekend", WeekdaysUTC: []int{0, 6}, StartMinuteUTC: 0, EndMinuteUTC: 1440, Components: base},
		{BandRef: "weekday-base-0", WeekdaysUTC: weekdays, StartMinuteUTC: 0, EndMinuteUTC: 60, Components: base},
		{BandRef: "weekday-peak-1", WeekdaysUTC: weekdays, StartMinuteUTC: 60, EndMinuteUTC: 240, Components: peak},
		{BandRef: "weekday-base-1", WeekdaysUTC: weekdays, StartMinuteUTC: 240, EndMinuteUTC: 360, Components: base},
		{BandRef: "weekday-peak-2", WeekdaysUTC: weekdays, StartMinuteUTC: 360, EndMinuteUTC: 600, Components: peak},
		{BandRef: "weekday-base-2", WeekdaysUTC: weekdays, StartMinuteUTC: 600, EndMinuteUTC: 1440, Components: base},
	}

	weekdayPeak, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, card.ModelRef, monday.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "weekday-peak-1", weekdayPeak.SelectedBand.BandRef)
	require.Equal(t, weekdays, weekdayPeak.SelectedBand.WeekdaysUTC)
	require.Regexp(t, `^rcq4_[a-f0-9]{64}$`, weekdayPeak.PricingVersion)
	input, err := RequestPriceMicros(weekdayPeak, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(10), input)

	weekendOffPeak, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, card.ModelRef, monday.Add(-24*time.Hour+2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, time.Sunday, monday.Add(-24*time.Hour).Weekday())
	require.Equal(t, "weekend", weekendOffPeak.SelectedBand.BandRef)
	input, err = RequestPriceMicros(weekendOffPeak, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(5), input)
	require.NotEqual(t, weekdayPeak.PricingVersion, weekendOffPeak.PricingVersion)

	broken := card
	broken.DailyPriceBands = canonicalDailyPriceBands(card.DailyPriceBands)
	broken.DailyPriceBands[0].WeekdaysUTC = []int{0}
	require.ErrorIs(t, ValidateRequestPricingRateCard(broken), ErrInvalid)

	broken = card
	broken.DailyPriceBands = canonicalDailyPriceBands(card.DailyPriceBands)
	broken.DailyPriceBands[0].WeekdaysUTC = []int{0, 7}
	require.ErrorIs(t, ValidateRequestPricingRateCard(broken), ErrInvalid)
}

func TestRequestPricingDailyBandsFailClosedOnGapOverlapCrossMidnightAndIncompletePrice(t *testing.T) {
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	components := requestPricingCard("x", RateSupplierContract, "p", "m", day, 10, 20).Components
	tests := []struct {
		name  string
		bands []DailyPriceBand
		want  error
	}{
		{name: "gap", bands: []DailyPriceBand{
			{BandRef: "first", StartMinuteUTC: 0, EndMinuteUTC: 60, Components: components},
			{BandRef: "second", StartMinuteUTC: 61, EndMinuteUTC: 1440, Components: components},
		}, want: ErrInvalid},
		{name: "overlap", bands: []DailyPriceBand{
			{BandRef: "first", StartMinuteUTC: 0, EndMinuteUTC: 61, Components: components},
			{BandRef: "second", StartMinuteUTC: 60, EndMinuteUTC: 1440, Components: components},
		}, want: ErrInvalid},
		{name: "cross-midnight", bands: []DailyPriceBand{
			{BandRef: "night", StartMinuteUTC: 22 * 60, EndMinuteUTC: 6 * 60, Components: components},
		}, want: ErrInvalid},
		{name: "incomplete", bands: []DailyPriceBand{
			{BandRef: "whole-day", StartMinuteUTC: 0, EndMinuteUTC: 1440, Components: components[:3]},
		}, want: ErrRequestPricingNotReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			card := requestPricingCard("scheduled", RateSupplierContract, "provider-a", "model-a", day, 10, 20)
			card.DailyPriceBands = test.bands
			err := ValidateRequestPricingRateCard(card)
			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestRequestPriceQuoteFailsClosedOnSamePriorityProviderAmbiguity(t *testing.T) {
	at := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	left := requestPricingCard("contract-a", RateSupplierContract, "provider-a", "model-a", at.Add(-time.Hour), 2, 4)
	right := requestPricingCard("contract-b", RateSupplierContract, "provider-b", "model-a", at.Add(-time.Hour), 1, 3)

	_, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: left}, {Card: right}}, 7, "model-a", at)
	require.ErrorIs(t, err, ErrRequestPricingAmbiguous)
	require.ErrorContains(t, err, "providers=provider-a,provider-b")
}

func TestRequestPriceQuoteNeverUsesReportingOrBilledEffectiveLayers(t *testing.T) {
	at := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	for _, layer := range []string{RateSupplierEffective, RateInternalShowback, RateInternalChargeback} {
		card := requestPricingCard("ignored", layer, "provider-a", "model-a", at.Add(-time.Hour), 2, 4)
		card.SourceEvidenceRef = "required-for-billed-effective"
		_, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, "model-a", at)
		require.ErrorIs(t, err, ErrRequestPricingNotReady, layer)
	}
}

func TestRequestPricingActivationRequiresExplicitFourMetersAndMicroCNY(t *testing.T) {
	at := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	card := requestPricingCard("contract", RateSupplierContract, "provider-a", "model-a", at, 2, 4)
	require.NoError(t, ValidateRequestPricingRateCard(card))

	card.Components = card.Components[:2]
	err := ValidateRequestPricingRateCard(card)
	require.ErrorIs(t, err, ErrRequestPricingNotReady)
	require.ErrorContains(t, err, MeterCacheReadTokens)

	card = requestPricingCard("contract", RateSupplierContract, "provider-a", "model-a", at, 2, 4)
	card.Components[0].Amount.Currency = "USD"
	err = ValidateRequestPricingRateCard(card)
	require.ErrorIs(t, err, ErrRequestPricingNotReady)

	provider := requestPricingCard("provider", RateProviderList, "provider-a", "model-a", at, 2, 4)
	err = ValidateRequestPricingRateCard(provider)
	require.ErrorContains(t, err, "provider_list_source_evidence")
}

func TestRequestComponentConversionUsesBigIntegerAndCheckedHalfUpRounding(t *testing.T) {
	component := RateComponent{
		Meter: MeterInputTokens, UnitQuantity: "3000000",
		Amount: Amount{ValueMinor: "2", Currency: "CNY", Scale: 1_000_000},
	}
	price, err := requestComponentPrice(component)
	require.NoError(t, err)
	require.Equal(t, int64(1), price)

	component.UnitQuantity = "1"
	component.Amount.ValueMinor = strings.Repeat("9", 70)
	_, err = requestComponentPrice(component)
	require.ErrorContains(t, err, "request_price_overflow")

	component.Amount.ValueMinor = "1"
	component.UnitQuantity = "0"
	_, err = requestComponentPrice(component)
	require.True(t, errors.Is(err, ErrInvalid))

	component.UnitQuantity = "1000000"
	component.Amount.ValueMinor = "9223372036854775807"
	price, err = requestComponentPrice(component)
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), price)
}

func TestRequestPriceQuotePreservesOfficialAbsoluteCachePricesWithoutRatios(t *testing.T) {
	at := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		input      string
		cacheRead  string
		wantInput  int64
		wantCached int64
	}{
		{name: "DeepSeek 1 over 30", input: "30000000", cacheRead: "1000000", wantInput: 30_000_000, wantCached: 1_000_000},
		{name: "Kimi K2.6 1.1 over 6.5", input: "6500000", cacheRead: "1100000", wantInput: 6_500_000, wantCached: 1_100_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			card := requestPricingCard("official", RateProviderList, "provider", "model", at, 1, 1)
			card.SourceEvidenceRef = "official-source"
			card.Components[0].Amount.ValueMinor = test.input
			card.Components[2].Amount.ValueMinor = test.cacheRead
			quote, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, "model", at.Add(time.Minute))
			require.NoError(t, err)
			input, err := RequestPriceMicros(quote, MeterInputTokens)
			require.NoError(t, err)
			cached, err := RequestPriceMicros(quote, MeterCacheReadTokens)
			require.NoError(t, err)
			require.Equal(t, test.wantInput, input)
			require.Equal(t, test.wantCached, cached)
		})
	}
}

func TestRequestPriceQuoteBindsSupportedPerCallMeterAsExactRational(t *testing.T) {
	at := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	card := requestPricingCard("kimi-tools", RateSupplierContract, "kimi", "kimi-k2.6", at, 6_500_000, 26_000_000)
	card.Components = append(card.Components, RateComponent{
		Meter: MeterBuiltinWebSearchCalls, UnitQuantity: "1",
		Amount: Amount{ValueMinor: "30000", Currency: "CNY", Scale: 1_000_000},
	})
	quote, err := ResolveRequestPriceQuote([]RateCardRecord{{Card: card}}, 7, "kimi-k2.6", at.Add(time.Minute))
	require.NoError(t, err)
	require.Regexp(t, `^rcq3_[a-f0-9]{64}$`, quote.PricingVersion)
	unitRates := RequestUnitPriceComponents(quote)
	require.Len(t, unitRates, 1)
	require.Equal(t, MeterBuiltinWebSearchCalls, unitRates[0].Meter)
	require.Equal(t, "30000", unitRates[0].AmountMinor)
	require.Equal(t, "1", unitRates[0].UnitQuantity)
	require.Equal(t, int64(30_000), unitRates[0].PriceMicrosPerUnit)
}

func requestPricingCard(ref, layer, provider, model string, from time.Time, input, output int64) RateCard {
	values := []struct {
		meter string
		value int64
	}{
		{MeterInputTokens, input},
		{MeterOutputTokens, output},
		{MeterCacheReadTokens, input},
		{MeterCacheCreationTokens, input},
	}
	components := make([]RateComponent, 0, len(values))
	for _, value := range values {
		components = append(components, RateComponent{
			Meter: value.meter, UnitQuantity: "1000000",
			Amount: Amount{ValueMinor: stringInt(value.value), Currency: "CNY", Scale: 1_000_000},
		})
	}
	return RateCard{
		ContractVersion: ContractVersion, RateCardRef: ref, OrgID: 7,
		ProviderRef: provider, ModelRef: model, Layer: layer, Version: 3, State: RateActive,
		EffectiveFrom: from.UTC(), Components: components, CreatedBy: "user:1", ApprovedBy: "user:2",
	}
}

func stringInt(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
