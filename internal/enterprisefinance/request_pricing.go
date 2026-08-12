package enterprisefinance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	MeterInputTokens         = "input_tokens"
	MeterOutputTokens        = "output_tokens"
	MeterCacheReadTokens     = "cache_read_tokens"
	MeterCacheCreationTokens = "cache_creation_tokens"

	RequestPricingAuthority = "versioned_rate_card"
	RequestPricingKey       = "model_ref"
	requestPricingVersion   = "rcq2"
	requestPricingScale     = int64(1_000_000)
	requestPricingBasis     = int64(1_000_000)
)

var (
	ErrRequestPricingNotReady  = errors.New("enterprisefinance: request pricing authority is not ready")
	ErrRequestPricingAmbiguous = errors.New("enterprisefinance: request pricing authority is ambiguous")
)

var requiredRequestMeters = []string{
	MeterInputTokens,
	MeterOutputTokens,
	MeterCacheReadTokens,
	MeterCacheCreationTokens,
}

// RequestPriceComponent is the exact, content-addressed request-time rate used
// by Billing. PriceMicrosPerMillion is rounded half-up exactly once from the
// CNY-micro Rate Card amount and its explicit unit_quantity.
type RequestPriceComponent struct {
	Meter                 string `json:"meter"`
	UnitQuantity          string `json:"unit_quantity"`
	AmountMinor           string `json:"amount_minor"`
	Currency              string `json:"currency"`
	AmountScale           int64  `json:"amount_scale"`
	ComponentHash         string `json:"component_hash"`
	PriceMicrosPerMillion int64  `json:"price_micros_per_million"`
}

// RequestPriceBand is the canonical, fully priced form of one UTC band. Every
// band contains all four request meters; no ratio or inherited price is used.
type RequestPriceBand struct {
	BandRef        string                  `json:"band_ref"`
	StartMinuteUTC int                     `json:"start_minute_utc"`
	EndMinuteUTC   int                     `json:"end_minute_utc"`
	Components     []RequestPriceComponent `json:"components"`
}

type RequestPriceBandSelection struct {
	BandRef        string `json:"band_ref"`
	StartMinuteUTC int    `json:"start_minute_utc"`
	EndMinuteUTC   int    `json:"end_minute_utc"`
}

// RequestPriceQuote is the sole request-pricing authority contract. The short
// PricingVersion is a SHA-256 content address over the complete Manifest, so it
// binds the card ref/version, provider, effective window and every component
// hash without exceeding the existing 80-byte ledger column.
type RequestPriceQuote struct {
	Authority         string                    `json:"authority"`
	SelectionKey      string                    `json:"selection_key"`
	OrgID             int64                     `json:"org_id"`
	ModelRef          string                    `json:"model_ref"`
	ProviderRef       string                    `json:"provider_ref"`
	Layer             string                    `json:"layer"`
	RateCardRef       string                    `json:"rate_card_ref"`
	RateCardVersion   int64                     `json:"rate_card_version"`
	EffectiveFrom     time.Time                 `json:"effective_from"`
	EffectiveTo       *time.Time                `json:"effective_to,omitempty"`
	SourceEvidenceRef string                    `json:"source_evidence_ref,omitempty"`
	PricingTimezone   string                    `json:"pricing_timezone"`
	DailyPriceBands   []RequestPriceBand        `json:"daily_price_bands"`
	SelectedBand      RequestPriceBandSelection `json:"selected_band"`
	ScheduleSHA256    string                    `json:"schedule_sha256"`
	Components        []RequestPriceComponent   `json:"components"`
	ManifestSHA256    string                    `json:"manifest_sha256"`
	PricingVersion    string                    `json:"pricing_version"`
}

type requestPriceManifest struct {
	Authority         string                    `json:"authority"`
	SelectionKey      string                    `json:"selection_key"`
	OrgID             int64                     `json:"org_id"`
	ModelRef          string                    `json:"model_ref"`
	ProviderRef       string                    `json:"provider_ref"`
	Layer             string                    `json:"layer"`
	RateCardRef       string                    `json:"rate_card_ref"`
	RateCardVersion   int64                     `json:"rate_card_version"`
	EffectiveFrom     string                    `json:"effective_from"`
	EffectiveTo       string                    `json:"effective_to"`
	SourceEvidenceRef string                    `json:"source_evidence_ref"`
	PricingTimezone   string                    `json:"pricing_timezone"`
	DailyPriceBands   []RequestPriceBand        `json:"daily_price_bands"`
	SelectedBand      RequestPriceBandSelection `json:"selected_band"`
	ScheduleSHA256    string                    `json:"schedule_sha256"`
	Components        []RequestPriceComponent   `json:"components"`
}

// ValidateRequestPricingRateCard applies the stricter activation contract used
// by supplier cost prediction. Cache prices must be explicit; UniRouter does
// not invent cache ratios from input price.
func ValidateRequestPricingRateCard(card RateCard) error {
	if card.Layer != RateSupplierContract && card.Layer != RateProviderList {
		return nil
	}
	if err := ValidateRateCard(card); err != nil {
		return err
	}
	if card.Layer == RateProviderList && strings.TrimSpace(card.SourceEvidenceRef) == "" {
		return invalid("provider_list_source_evidence")
	}
	if err := validateRequestPriceComponents(card.Components, "base"); err != nil {
		return err
	}
	for _, band := range card.DailyPriceBands {
		if err := validateRequestPriceComponents(band.Components, "band "+strings.TrimSpace(band.BandRef)); err != nil {
			return err
		}
	}
	return nil
}

func validateRequestPriceComponents(rateComponents []RateComponent, contextRef string) error {
	components := make(map[string]RateComponent, len(rateComponents))
	for _, component := range rateComponents {
		components[component.Meter] = component
	}
	for _, meter := range requiredRequestMeters {
		component, ok := components[meter]
		if !ok {
			return fmt.Errorf("%w: %s missing request-pricing meter %s", ErrRequestPricingNotReady, contextRef, meter)
		}
		if component.Amount.Currency != "CNY" || component.Amount.Scale != requestPricingScale {
			return fmt.Errorf("%w: %s meter %s must use CNY micro precision", ErrRequestPricingNotReady, contextRef, meter)
		}
		if _, err := requestComponentPrice(component); err != nil {
			return fmt.Errorf("%w: %s meter %s: %v", ErrRequestPricingNotReady, contextRef, meter, err)
		}
	}
	return nil
}

// ResolveRequestPriceQuote selects a single active supplier-cost Rate Card by
// model_ref. supplier_contracted wins over provider_list. Because the request
// path does not yet carry a stable provider/connection identity, more than one
// candidate at the winning layer fails closed rather than guessing a provider.
func ResolveRequestPriceQuote(records []RateCardRecord, orgID int64, modelRef string, pricedAt time.Time) (RequestPriceQuote, error) {
	modelRef = strings.TrimSpace(modelRef)
	if orgID <= 0 || modelRef == "" || !validTime(pricedAt) {
		return RequestPriceQuote{}, invalid("request_price_identity")
	}
	pricedAt = pricedAt.UTC()
	byLayer := map[string][]RateCardRecord{
		RateSupplierContract: {},
		RateProviderList:     {},
	}
	for _, record := range records {
		card := record.Card
		if card.OrgID != orgID || strings.TrimSpace(card.ModelRef) != modelRef || card.State != RateActive {
			continue
		}
		if card.Layer != RateSupplierContract && card.Layer != RateProviderList {
			continue
		}
		if pricedAt.Before(card.EffectiveFrom) || (card.EffectiveTo != nil && !pricedAt.Before(*card.EffectiveTo)) {
			continue
		}
		byLayer[card.Layer] = append(byLayer[card.Layer], record)
	}

	var candidates []RateCardRecord
	for _, layer := range []string{RateSupplierContract, RateProviderList} {
		if len(byLayer[layer]) > 0 {
			candidates = byLayer[layer]
			break
		}
	}
	if len(candidates) == 0 {
		return RequestPriceQuote{}, fmt.Errorf("%w: no active quote for model_ref %s", ErrRequestPricingNotReady, modelRef)
	}
	if len(candidates) != 1 {
		providers := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			providers = append(providers, strings.TrimSpace(candidate.Card.ProviderRef))
		}
		sort.Strings(providers)
		return RequestPriceQuote{}, fmt.Errorf("%w: model_ref %s has %d candidates at layer %s (providers=%s)",
			ErrRequestPricingAmbiguous, modelRef, len(candidates), candidates[0].Card.Layer, strings.Join(providers, ","))
	}

	card := candidates[0].Card
	if err := ValidateRequestPricingRateCard(card); err != nil {
		return RequestPriceQuote{}, err
	}
	selectedComponents := card.Components
	selectedBand := RequestPriceBandSelection{BandRef: "base", StartMinuteUTC: 0, EndMinuteUTC: minutesPerUTCDay}
	dailyPriceBands := make([]RequestPriceBand, 0, len(card.DailyPriceBands))
	for _, band := range canonicalDailyPriceBands(card.DailyPriceBands) {
		components, err := requestQuoteComponents(band.Components)
		if err != nil {
			return RequestPriceQuote{}, err
		}
		dailyPriceBands = append(dailyPriceBands, RequestPriceBand{
			BandRef: band.BandRef, StartMinuteUTC: band.StartMinuteUTC,
			EndMinuteUTC: band.EndMinuteUTC, Components: components,
		})
	}
	if band, scheduled, err := selectDailyPriceBand(card.DailyPriceBands, pricedAt); err != nil {
		return RequestPriceQuote{}, fmt.Errorf("%w: invalid UTC daily price schedule: %v", ErrRequestPricingNotReady, err)
	} else if scheduled {
		selectedComponents = band.Components
		selectedBand = RequestPriceBandSelection{
			BandRef: band.BandRef, StartMinuteUTC: band.StartMinuteUTC, EndMinuteUTC: band.EndMinuteUTC,
		}
	}
	components, err := requestQuoteComponents(selectedComponents)
	if err != nil {
		return RequestPriceQuote{}, err
	}
	scheduleWire, err := json.Marshal(struct {
		Timezone string             `json:"timezone"`
		Bands    []RequestPriceBand `json:"bands"`
	}{Timezone: "UTC", Bands: dailyPriceBands})
	if err != nil {
		return RequestPriceQuote{}, fmt.Errorf("marshal request price schedule: %w", err)
	}
	scheduleDigest := sha256.Sum256(scheduleWire)
	scheduleSHA256 := "sha256:" + hex.EncodeToString(scheduleDigest[:])
	effectiveTo := ""
	if card.EffectiveTo != nil {
		effectiveTo = card.EffectiveTo.UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	manifest := requestPriceManifest{
		Authority: RequestPricingAuthority, SelectionKey: RequestPricingKey,
		OrgID: card.OrgID, ModelRef: strings.TrimSpace(card.ModelRef), ProviderRef: strings.TrimSpace(card.ProviderRef),
		Layer: card.Layer, RateCardRef: strings.TrimSpace(card.RateCardRef), RateCardVersion: card.Version,
		EffectiveFrom: card.EffectiveFrom.UTC().Format("2006-01-02T15:04:05.000000Z"), EffectiveTo: effectiveTo,
		SourceEvidenceRef: strings.TrimSpace(card.SourceEvidenceRef), PricingTimezone: "UTC",
		DailyPriceBands: dailyPriceBands, SelectedBand: selectedBand, ScheduleSHA256: scheduleSHA256,
		Components: components,
	}
	wire, err := json.Marshal(manifest)
	if err != nil {
		return RequestPriceQuote{}, fmt.Errorf("marshal request price manifest: %w", err)
	}
	digest := sha256.Sum256(wire)
	digestHex := hex.EncodeToString(digest[:])
	quote := RequestPriceQuote{
		Authority: RequestPricingAuthority, SelectionKey: RequestPricingKey,
		OrgID: card.OrgID, ModelRef: manifest.ModelRef, ProviderRef: manifest.ProviderRef,
		Layer: card.Layer, RateCardRef: manifest.RateCardRef, RateCardVersion: card.Version,
		EffectiveFrom: card.EffectiveFrom.UTC(), SourceEvidenceRef: manifest.SourceEvidenceRef,
		PricingTimezone: "UTC", DailyPriceBands: dailyPriceBands, SelectedBand: selectedBand,
		ScheduleSHA256: scheduleSHA256, Components: components, ManifestSHA256: "sha256:" + digestHex,
		PricingVersion: requestPricingVersion + "_" + digestHex,
	}
	if card.EffectiveTo != nil {
		value := card.EffectiveTo.UTC()
		quote.EffectiveTo = &value
	}
	return quote, nil
}

func requestQuoteComponents(rateComponents []RateComponent) ([]RequestPriceComponent, error) {
	byMeter := make(map[string]RateComponent, len(rateComponents))
	for _, component := range rateComponents {
		byMeter[component.Meter] = component
	}
	components := make([]RequestPriceComponent, 0, len(requiredRequestMeters))
	for _, meter := range requiredRequestMeters {
		component, ok := byMeter[meter]
		if !ok {
			return nil, fmt.Errorf("%w: missing quote meter %s", ErrRequestPricingNotReady, meter)
		}
		componentHash, err := RateComponentIdentity(component)
		if err != nil {
			return nil, err
		}
		price, err := requestComponentPrice(component)
		if err != nil {
			return nil, err
		}
		components = append(components, RequestPriceComponent{
			Meter: meter, UnitQuantity: component.UnitQuantity,
			AmountMinor: component.Amount.ValueMinor, Currency: component.Amount.Currency,
			AmountScale: component.Amount.Scale, ComponentHash: componentHash,
			PriceMicrosPerMillion: price,
		})
	}
	return components, nil
}

// ResolveRequestPriceQuote loads latest Rate Card versions and applies the
// fail-closed pure resolver above.
func (s *PostgresStore) ResolveRequestPriceQuote(ctx context.Context, orgID int64, modelRef string, pricedAt time.Time) (RequestPriceQuote, error) {
	records, err := s.ListLatestRateCards(ctx, orgID)
	if err != nil {
		return RequestPriceQuote{}, err
	}
	return ResolveRequestPriceQuote(records, orgID, modelRef, pricedAt)
}

func requestComponentPrice(component RateComponent) (int64, error) {
	if component.Amount.Currency != "CNY" || component.Amount.Scale != requestPricingScale {
		return 0, invalid("request_price_currency_scale")
	}
	amount, ok := new(big.Int).SetString(component.Amount.ValueMinor, 10)
	if !ok || amount.Sign() < 0 {
		return 0, invalid("request_price_amount")
	}
	unit, ok := new(big.Int).SetString(component.UnitQuantity, 10)
	if !ok || unit.Sign() <= 0 {
		return 0, invalid("request_price_unit")
	}
	// value_minor is micro-CNY because activation requires scale=1e6.
	// Multiply before dividing so no precision is lost; ties round half-up.
	numerator := new(big.Int).Mul(amount, big.NewInt(requestPricingBasis))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, unit, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(unit) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() < 0 || quotient.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return 0, invalid("request_price_overflow")
	}
	return quotient.Int64(), nil
}

func requestPriceByMeter(quote RequestPriceQuote, meter string) (int64, error) {
	for _, component := range quote.Components {
		if component.Meter == meter {
			return component.PriceMicrosPerMillion, nil
		}
	}
	return 0, fmt.Errorf("%w: missing quote meter %s", ErrRequestPricingNotReady, meter)
}

// RequestPriceMicros returns a component price without exposing positional
// assumptions to Billing.
func RequestPriceMicros(quote RequestPriceQuote, meter string) (int64, error) {
	return requestPriceByMeter(quote, meter)
}
