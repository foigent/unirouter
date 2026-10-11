package admin

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDashboardModelCostExactSerialization(t *testing.T) {
	rows := &testutil.MockRows{Rows: []func(...any) error{func(dest ...any) error {
		*dest[0].(*string) = "original-model"
		*dest[1].(*int), *dest[2].(*int), *dest[3].(*int) = 3, 2, 1
		*dest[4].(*float64), *dest[5].(*float64) = 10, 20
		*dest[6].(*int64) = 9007199254740993
		return nil
	}}}
	h := newTestHandler(&testutil.MockDB{QueryFn: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
		require.Equal(t, []any{int64(42), "start", "end"}, args)
		return rows, nil
	}})
	data, err := h.dashboardModelDistribution(context.Background(), 42, "start", "end")
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.True(t, rows.Closed)
	require.Equal(t, "9007199254740993", data[0]["cost_micros"])
	require.Equal(t, "original-model", data[0]["model"])
	require.InDelta(t, 2.0/3, data[0]["success_rate"], 0.000001)
}
