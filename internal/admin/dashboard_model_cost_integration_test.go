//go:build integration

package admin

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDashboardModelCostDistributionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, otherOrgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status) VALUES ('composition','active') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status) VALUES ('other-composition','active') RETURNING id`).Scan(&otherOrgID))
	insert := func(id string, org int64, model, requested, cost, date string) {
		_, err := pool.Exec(ctx, `INSERT INTO call_logs (request_id,org_id,model,requested_model,status_code,latency_ms,cost_micros,created_at) VALUES ($1,$2,$3,$4,200,10,$5::bigint,$6::timestamptz)`, id, org, model, requested, cost, date)
		require.NoError(t, err)
	}
	for i := 0; i < 61; i++ {
		insert(fmt.Sprintf("composition-%d", i), orgID, fmt.Sprintf("model-%d", i), "original-alias", "1", "2026-10-10T12:00:00Z")
	}
	insert("exact-cost", orgID, "model-0", "alias", "9007199254740993", "2026-10-10T00:00:00Z")
	insert("fallback", orgID, " ", " original-name ", "7", "2026-10-10T12:00:00Z")
	insert("other-org", otherOrgID, "model-0", "alias", "999", "2026-10-10T12:00:00Z")
	insert("at-exclusive-end", orgID, "model-0", "alias", "999", "2026-10-11T00:00:00Z")
	insert("before-start", orgID, "model-0", "alias", "999", "2026-10-09T23:59:59Z")
	h := NewHandler(pool, "dashboard-composition-fixture", []byte("0123456789abcdef01234567"), nil)
	rows, err := h.dashboardModelDistribution(ctx, orgID, "2026-10-10T00:00:00Z", "2026-10-11T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, rows, 62, "composition cannot use a top-50 analytics projection")
	byModel := make(map[string]map[string]interface{})
	for _, row := range rows {
		byModel[row["model"].(string)] = row
	}
	require.Equal(t, "9007199254740994", byModel["model-0"]["cost_micros"])
	require.Equal(t, 2, byModel["model-0"]["requests"])
	require.Equal(t, "7", byModel["original-name"]["cost_micros"])
	require.Equal(t, "1", byModel["model-60"]["cost_micros"])
}
