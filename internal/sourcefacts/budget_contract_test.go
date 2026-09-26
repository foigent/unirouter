package sourcefacts

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSourceBudgetNullableDefinitionAndCheckedConversion(t *testing.T) {
	monthly, none := "monthly", "none"
	zero, positive, overflow := int64(0), int64(100000), int64(math.MaxInt64)
	for _, tc := range []struct {
		name   string
		cents  *int64
		period *string
		state  string
		amount string
	}{
		{"unset", nil, &monthly, "unset", ""},
		{"explicit_zero", &zero, &monthly, "inactive_zero", "0"},
		{"period_none", &positive, &none, "inactive_period_none", "1000000000"},
		{"missing_period", &positive, nil, "unsupported", "1000000000"},
		{"active", &positive, &monthly, "active", "1000000000"},
		{"conversion_overflow", &overflow, &monthly, "unsupported", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := gatewayBudgetDefinition(tc.cents, tc.period, 1, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), "observed_baseline", "projects:synthetic:budget:1")
			require.NoError(t, err)
			require.Equal(t, tc.state, d.SettingState)
			if tc.amount == "" {
				require.Nil(t, d.Amount)
			} else {
				require.Equal(t, tc.amount, d.Amount.ValueMinor)
				require.Equal(t, "CNY", d.Amount.Currency)
				require.Equal(t, "1000000", d.Amount.Scale)
			}
			require.Equal(t, "report_only", d.RuntimeEffect)
			if tc.cents == nil {
				require.Nil(t, d.OriginalCents)
			}
		})
	}
}

func TestSourceBudgetExactComparisonKeepsCoverageUnitsAndReservationsSeparate(t *testing.T) {
	cents, period := int64(100000), "monthly"
	d, err := gatewayBudgetDefinition(&cents, &period, 1, time.Now(), "created", "projects:synthetic:budget:1")
	require.NoError(t, err)
	for _, tc := range []struct{ spent, remaining, threshold string }{
		{"600000000", "400000000", "below"},
		{"1200000000", "-200000000", "above"},
		{"999999999", "1", "below"},
		{"1000000000", "0", "at"},
		{"0", "1000000000", "below"},
	} {
		result := compareBudget(d, BudgetMoney{ValueMinor: tc.spent, Currency: "CNY", Scale: "1000000"}, true)
		require.Equal(t, "comparable", result.State)
		require.Equal(t, tc.remaining, result.Remaining.ValueMinor)
		require.Equal(t, tc.threshold, result.ThresholdState)
		require.Equal(t, tc.spent, result.UtilizationNumerator)
		require.Equal(t, "1000000000", result.UtilizationDenominator)
	}
	partial := compareBudget(d, BudgetMoney{ValueMinor: "200000000", Currency: "CNY", Scale: "1000000"}, false)
	require.Equal(t, "coverage_gap", partial.State)
	require.Nil(t, partial.Remaining)
	for _, amount := range []BudgetMoney{{"600000000", "USD", "1000000"}, {"60000", "CNY", "100"}} {
		result := compareBudget(d, amount, true)
		require.Equal(t, "unit_mismatch", result.State)
		require.Nil(t, result.Remaining)
	}
	// Neither calculated cost nor reservations nor local detail sums enter the
	// comparator; it accepts only the chosen authoritative settled subtotal.
	old := compareBudget(d, BudgetMoney{"1200000000", "CNY", "1000000"}, true)
	cents = 150000
	updated, err := gatewayBudgetDefinition(&cents, &period, 2, time.Now(), "value_changed", "projects:synthetic:budget:2")
	require.NoError(t, err)
	require.Equal(t, "300000000", compareBudget(updated, BudgetMoney{"1200000000", "CNY", "1000000"}, true).Remaining.ValueMinor)
	require.Equal(t, "-200000000", old.Remaining.ValueMinor)
}

func TestSourceBudgetUTCPeriodIgnoresDisplayZoneAndShortFilter(t *testing.T) {
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	cutoff := time.Date(2026, 9, 10, 13, 0, 0, 0, shanghai)
	start, end, err := gatewayBudgetPeriod("monthly", cutoff)
	require.NoError(t, err)
	require.Equal(t, "2026-09-01T00:00:00Z", start.Format(time.RFC3339))
	require.Equal(t, "2026-10-01T00:00:00Z", end.Format(time.RFC3339))
	require.Equal(t, 8, start.In(shanghai).Hour())
	start, end, err = gatewayBudgetPeriod("daily", time.Date(2026, 9, 10, 7, 59, 59, 999999000, shanghai))
	require.NoError(t, err)
	require.Equal(t, "2026-09-09T00:00:00Z", start.Format(time.RFC3339))
	require.Equal(t, "2026-09-10T00:00:00Z", end.Format(time.RFC3339))
}
