package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
)

var requestMeterPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)

// UnitRate is the exact, immutable non-token Rate Card component frozen for a
// request. AmountMinor uses AmountScale micro-CNY units and applies to
// UnitQuantity meter units. ComponentHash binds it to reviewed source evidence.
type UnitRate struct {
	Meter         string `json:"meter"`
	UnitQuantity  int64  `json:"unit_quantity"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
	AmountScale   int64  `json:"amount_scale"`
	ComponentHash string `json:"component_hash"`
}

func normalizeMeterUsage(values []metering.MeterQuantity) ([]metering.MeterQuantity, error) {
	return metering.NormalizeMeterQuantities(values)
}

func normalizeUnitRates(values []UnitRate) ([]UnitRate, error) {
	if len(values) == 0 {
		return []UnitRate{}, nil
	}
	result := slices.Clone(values)
	slices.SortFunc(result, func(left, right UnitRate) int {
		return strings.Compare(strings.TrimSpace(left.Meter), strings.TrimSpace(right.Meter))
	})
	for index := range result {
		result[index].Meter = strings.TrimSpace(result[index].Meter)
		result[index].Currency = strings.TrimSpace(result[index].Currency)
		result[index].ComponentHash = strings.TrimSpace(result[index].ComponentHash)
		if !requestMeterPattern.MatchString(result[index].Meter) || result[index].UnitQuantity <= 0 ||
			result[index].AmountMinor < 0 || result[index].Currency != "CNY" ||
			result[index].AmountScale != money.MicrosPerCNY || result[index].ComponentHash == "" {
			return nil, fmt.Errorf("billing unit rate %q is invalid", result[index].Meter)
		}
		if index > 0 && result[index-1].Meter == result[index].Meter {
			return nil, fmt.Errorf("billing unit rate %s is duplicated", result[index].Meter)
		}
	}
	return result, nil
}

// CanonicalUnitRates validates and sorts a frozen non-token pricing envelope
// for receipt materialization outside Billing. Callers cannot create pricing
// authority with this helper; authority is established only by Rate Card
// selection during Freeze.
func CanonicalUnitRates(values []UnitRate) ([]UnitRate, error) {
	return normalizeUnitRates(values)
}

func unitRateFromQuoteComponent(meter, unitQuantity, amountMinor, currency string, amountScale int64, componentHash string) (UnitRate, error) {
	unit, err := strconv.ParseInt(unitQuantity, 10, 64)
	if err != nil || unit <= 0 {
		return UnitRate{}, fmt.Errorf("billing unit rate %s has unsupported unit quantity", meter)
	}
	amount, err := strconv.ParseInt(amountMinor, 10, 64)
	if err != nil || amount < 0 {
		return UnitRate{}, fmt.Errorf("billing unit rate %s has unsupported amount", meter)
	}
	rates, err := normalizeUnitRates([]UnitRate{{
		Meter: meter, UnitQuantity: unit, AmountMinor: amount,
		Currency: currency, AmountScale: amountScale, ComponentHash: componentHash,
	}})
	if err != nil {
		return UnitRate{}, err
	}
	return rates[0], nil
}

func encodeUnitRates(rates []UnitRate) ([]byte, error) {
	normalized, err := normalizeUnitRates(rates)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func decodeUnitRates(raw []byte) ([]UnitRate, error) {
	if len(raw) == 0 {
		return []UnitRate{}, nil
	}
	var rates []UnitRate
	if err := json.Unmarshal(raw, &rates); err != nil {
		return nil, fmt.Errorf("decode frozen unit rates: %w", err)
	}
	return normalizeUnitRates(rates)
}

func encodeMeterUsage(usage []metering.MeterQuantity) ([]byte, error) {
	normalized, err := normalizeMeterUsage(usage)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func decodeMeterUsage(raw []byte) ([]metering.MeterQuantity, error) {
	if len(raw) == 0 {
		return []metering.MeterQuantity{}, nil
	}
	var usage []metering.MeterQuantity
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, fmt.Errorf("decode meter usage: %w", err)
	}
	return normalizeMeterUsage(usage)
}

func calculateUnitMeterCost(rates []UnitRate, usage []metering.MeterQuantity) (int64, error) {
	normalizedRates, err := normalizeUnitRates(rates)
	if err != nil {
		return 0, err
	}
	normalizedUsage, err := normalizeMeterUsage(usage)
	if err != nil {
		return 0, err
	}
	byMeter := make(map[string]UnitRate, len(normalizedRates))
	for _, rate := range normalizedRates {
		byMeter[rate.Meter] = rate
	}
	components := make([]money.UnitComponent, 0, len(normalizedUsage))
	for _, item := range normalizedUsage {
		rate, ok := byMeter[item.Meter]
		if !ok {
			return 0, fmt.Errorf("billing meter %s has no frozen unit rate", item.Meter)
		}
		components = append(components, money.UnitComponent{
			Quantity: item.Quantity, AmountMicros: rate.AmountMinor, UnitQuantity: rate.UnitQuantity,
		})
	}
	calculation, err := money.CalculateUnits(components...)
	if err != nil {
		return 0, fmt.Errorf("calculate non-token meter cost: %w", err)
	}
	return calculation.TotalMicros, nil
}

func estimateUnitMeterHold(rates []UnitRate, usage []metering.MeterQuantity, safetyFactor string) (int64, error) {
	normalizedRates, err := normalizeUnitRates(rates)
	if err != nil {
		return 0, err
	}
	normalizedUsage, err := normalizeMeterUsage(usage)
	if err != nil {
		return 0, err
	}
	byMeter := make(map[string]UnitRate, len(normalizedRates))
	for _, rate := range normalizedRates {
		byMeter[rate.Meter] = rate
	}
	var total int64
	for _, item := range normalizedUsage {
		rate, ok := byMeter[item.Meter]
		if !ok {
			return 0, fmt.Errorf("billing meter %s has no frozen unit rate", item.Meter)
		}
		hold, err := money.EstimateUnitHold(money.UnitComponent{
			Quantity: item.Quantity, AmountMicros: rate.AmountMinor, UnitQuantity: rate.UnitQuantity,
		}, safetyFactor)
		if err != nil {
			return 0, fmt.Errorf("estimate non-token meter %s: %w", item.Meter, err)
		}
		total, err = money.Add(total, hold)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func meterUsageEqual(left, right []metering.MeterQuantity) bool {
	leftNormalized, leftErr := normalizeMeterUsage(left)
	rightNormalized, rightErr := normalizeMeterUsage(right)
	return leftErr == nil && rightErr == nil && slices.Equal(leftNormalized, rightNormalized)
}

func persistReservationMeterEvidenceTx(
	ctx context.Context,
	tx pgx.Tx,
	requestID string,
	orgID int64,
	usage []metering.MeterQuantity,
	nonTokenCostMicros int64,
) error {
	if nonTokenCostMicros < 0 {
		return fmt.Errorf("non-token meter cost must be nonnegative")
	}
	wire, err := encodeMeterUsage(usage)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET meter_usage=$3::jsonb, non_token_cost_micros=$4, updated_at=NOW()
		WHERE request_id=$1 AND org_id=$2
		  AND status IN ('frozen','settlement_pending')`,
		requestID, orgID, string(wire), nonTokenCostMicros,
	)
	if err != nil {
		return fmt.Errorf("persist non-token meter evidence: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("persist non-token meter evidence: reservation state changed")
	}
	return nil
}
