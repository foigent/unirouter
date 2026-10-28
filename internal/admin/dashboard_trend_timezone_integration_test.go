//go:build integration

package admin

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
	"testing"
)

func TestDashboardTrendUTCSessionTimezoneIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status) VALUES('UTC trend boundary','active') RETURNING id`).Scan(&orgID))
	for i, row := range []struct {
		at   string
		cost int64
	}{
		{"2026-03-01T00:15:00Z", 1}, {"2026-03-01T23:45:00Z", 2}, {"2026-03-02T00:05:00Z", 4},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO call_logs(request_id,org_id,model,status_code,cost_micros,created_at) VALUES($1,$2,'utc-trend',200,$3,$4::timestamptz)`, fmt.Sprintf("utc-trend-%d", i), orgID, row.cost, row.at)
		require.NoError(t, err)
	}
	for _, zone := range []string{"UTC", "Asia/Shanghai", "Asia/Kathmandu"} {
		t.Run(zone, func(t *testing.T) {
			cfg := pool.Config()
			cfg.MaxConns = 1
			cfg.ConnConfig.RuntimeParams["timezone"] = zone
			scoped, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			defer scoped.Close()
			h := NewHandler(scoped, "utc-fixture", []byte("0123456789abcdef01234567"), nil)
			for _, window := range []struct {
				name, start, end string
				counts           []int
				costs            []string
			}{
				{"days", "2026-03-01T00:00:00Z", "2026-03-04T00:00:00Z", []int{2, 1, 0, 0}, []string{"3", "4", "0", "0"}},
				{"hours", "2026-03-01T23:00:00Z", "2026-03-02T01:00:00Z", []int{1, 1, 0}, []string{"2", "4", "0"}},
			} {
				points, err := h.dashboardDailyTrend(ctx, orgID, window.start, window.end)
				require.NoError(t, err)
				t.Logf("zone=%s window=%s actual=%v", zone, window.name, points)
				require.Len(t, points, len(window.counts))
				for i, point := range points {
					require.Equal(t, window.counts[i], point["requests"], "zone=%s window=%s index=%d", zone, window.name, i)
					require.Equal(t, window.costs[i], point["cost_micros"])
				}
			}
		})
	}
}
