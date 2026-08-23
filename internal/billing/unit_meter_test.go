package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
)

func TestUnitMeterCostAndFreezeUseExactFrozenRate(t *testing.T) {
	rate := UnitRate{
		Meter: "builtin_web_search_calls", UnitQuantity: 1, AmountMinor: 30_000,
		Currency: "CNY", AmountScale: money.MicrosPerCNY,
		ComponentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	usage := []metering.MeterQuantity{{Meter: rate.Meter, Quantity: 1}}

	cost, err := calculateUnitMeterCost([]UnitRate{rate}, usage)
	require.NoError(t, err)
	require.Equal(t, int64(30_000), cost)

	hold, err := estimateFreezeAmountWithMeters(priceQuote{UnitRates: []UnitRate{rate}}, 0, usage, "1.1", 0)
	require.NoError(t, err)
	require.Equal(t, int64(33_000), hold)
}

func TestUnitMeterSettlementFailsClosedWithoutFrozenLiability(t *testing.T) {
	usage := []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}}
	_, err := calculateUnitMeterCost(nil, usage)
	require.ErrorContains(t, err, "no frozen unit rate")

	record := reservationRecord{
		ReservedMeterUsage: []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 1}},
		EffectiveUnitRates: []UnitRate{{
			Meter: "builtin_web_search_calls", UnitQuantity: 1, AmountMinor: 30_000,
			Currency: "CNY", AmountScale: money.MicrosPerCNY,
			ComponentHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}},
	}
	_, _, err = calculateReservationCost(record, SettleRequest{
		MeterUsage: []metering.MeterQuantity{{Meter: "builtin_web_search_calls", Quantity: 2}},
	})
	require.ErrorContains(t, err, "exceeds its frozen reservation")
}

func TestUnitRateCanonicalContractRejectsDuplicatesAndUnsupportedMoney(t *testing.T) {
	valid := UnitRate{
		Meter: "builtin_web_search_calls", UnitQuantity: 1, AmountMinor: 30_000,
		Currency: "CNY", AmountScale: money.MicrosPerCNY,
		ComponentHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	_, err := normalizeUnitRates([]UnitRate{valid, valid})
	require.ErrorContains(t, err, "duplicated")
	valid.Currency = "USD"
	_, err = normalizeUnitRates([]UnitRate{valid})
	require.Error(t, err)
}
