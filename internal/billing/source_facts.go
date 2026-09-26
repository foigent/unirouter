package billing

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// SourceBasis freezes explanatory inputs, not another amount ledger. All
// quantities cross the source contract as decimal strings.
type SourceBasis struct {
	BillingMode           string           `json:"billing_mode"`
	ModeSource            string           `json:"mode_source"`
	PricingAuthority      string           `json:"pricing_authority"`
	PricingVersion        string           `json:"pricing_version"`
	PricingState          string           `json:"pricing_state"`
	RateCardRef           string           `json:"rate_card_ref,omitempty"`
	RateCardVersion       string           `json:"rate_card_version,omitempty"`
	RateCardLayer         string           `json:"rate_card_layer,omitempty"`
	InputRate             string           `json:"input_rate_micros_per_million"`
	CacheReadRate         string           `json:"cache_read_rate_micros_per_million"`
	CacheCreationRate     string           `json:"cache_creation_rate_micros_per_million"`
	OutputRate            string           `json:"output_rate_micros_per_million"`
	Multiplier            string           `json:"price_multiplier"`
	Currency              string           `json:"currency"`
	AmountScale           string           `json:"amount_scale"`
	UnitRates             []SourceUnitRate `json:"unit_rates"`
	ExecutionSnapshotHash string           `json:"execution_snapshot_hash,omitempty"`
}

type SourceUnitRate struct {
	Meter         string `json:"meter"`
	UnitQuantity  string `json:"unit_quantity"`
	AmountMinor   string `json:"amount_minor"`
	Currency      string `json:"currency"`
	AmountScale   string `json:"amount_scale"`
	ComponentHash string `json:"component_hash"`
}

type ReservationMaterializer interface {
	MaterializeReservationTx(context.Context, pgx.Tx, FreezeRequest, SourceBasis) error
}

// ReadPersistedSourceBasis reads only values already frozen on this request.
// A legacy row did not record which authority or RateCard produced those rates;
// neither the current configuration nor an owner/price JOIN can recover that.
func ReadPersistedSourceBasis(ctx context.Context, tx pgx.Tx, org int64, request string) (*SourceBasis, error) {
	b := &SourceBasis{ModeSource: "persisted_reservation", PricingAuthority: "not_captured", PricingState: "unknown", Currency: "CNY", AmountScale: "1000000", UnitRates: []SourceUnitRate{}}
	var units []byte
	e := tx.QueryRow(ctx, `SELECT billing_mode,pricing_version,
	 effective_input_price_micros_per_million::text,effective_cache_read_price_micros_per_million::text,
	 effective_cache_creation_price_micros_per_million::text,effective_output_price_micros_per_million::text,
	 price_multiplier::text,effective_unit_rates,COALESCE(execution_snapshot_hash,'')
	 FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, org, request).Scan(&b.BillingMode, &b.PricingVersion, &b.InputRate, &b.CacheReadRate, &b.CacheCreationRate, &b.OutputRate, &b.Multiplier, &units, &b.ExecutionSnapshotHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	rates, e := decodeUnitRates(units)
	if e != nil {
		return nil, e
	}
	for _, r := range rates {
		b.UnitRates = append(b.UnitRates, SourceUnitRate{r.Meter, strconv.FormatInt(r.UnitQuantity, 10), strconv.FormatInt(r.AmountMinor, 10), r.Currency, strconv.FormatInt(r.AmountScale, 10), r.ComponentHash})
	}
	return b, nil
}

func (b *Billing) SetReservationMaterializer(m ReservationMaterializer) {
	if b != nil {
		b.reservationMaterializer = m
	}
}

func (b *Billing) materializeSourceReservation(ctx context.Context, tx pgx.Tx, req FreezeRequest, quote priceQuote, mode BillingMode) error {
	if b.reservationMaterializer == nil {
		return nil
	}
	number := func(value int64) string { return strconv.FormatInt(value, 10) }
	basis := SourceBasis{
		BillingMode: string(mode), ModeSource: "reservation", PricingAuthority: string(b.requestPricingAuthority),
		PricingVersion: quote.Version, PricingState: "priced", RateCardRef: quote.RateCardRef, RateCardLayer: quote.RateCardLayer,
		InputRate: number(quote.InputPriceMicrosPerMillion), CacheReadRate: number(quote.CacheReadPriceMicrosPerMillion),
		CacheCreationRate: number(quote.CacheCreationPriceMicrosPerMillion), OutputRate: number(quote.OutputPriceMicrosPerMillion),
		Multiplier: quote.Multiplier, Currency: "CNY", AmountScale: "1000000", UnitRates: []SourceUnitRate{},
		ExecutionSnapshotHash: req.ExecutionSnapshotHash,
	}
	if quote.RateCardVersion > 0 {
		basis.RateCardVersion = number(quote.RateCardVersion)
	}
	// Only the legacy observation quote writer owns this interpretation.
	if mode == BillingModeCostObservation && b.requestPricingAuthority == RequestPricingAuthorityLegacy && len(quote.Version) > 4 && quote.Version[:4] == "uo1_" {
		basis.PricingState = "unpriced"
	}
	for _, rate := range quote.UnitRates {
		basis.UnitRates = append(basis.UnitRates, SourceUnitRate{rate.Meter, number(rate.UnitQuantity), number(rate.AmountMinor), rate.Currency, number(rate.AmountScale), rate.ComponentHash})
	}
	return b.reservationMaterializer.MaterializeReservationTx(ctx, tx, req, basis)
}

// TerminalMaterializers preserves the existing V1-before-source lock order.
// An error rolls back the entire original journal/budget/outbox transaction.
type TerminalMaterializers []TerminalMaterializer

func (items TerminalMaterializers) MaterializeTerminalTx(ctx context.Context, tx pgx.Tx, record TerminalRecord) error {
	for _, item := range items {
		if item != nil {
			if err := item.MaterializeTerminalTx(ctx, tx, record); err != nil {
				return err
			}
		}
	}
	return nil
}
