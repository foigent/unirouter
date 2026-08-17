//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

const dashboardZeroWalletJWTSecret = "dashboard-zero-wallet-integration-secret"

func TestDashboardStatsZeroWalletPostgresIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	h := NewHandler(pool, dashboardZeroWalletJWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/dashboard/stats",
		middleware.AdminAuth(pool, dashboardZeroWalletJWTSecret)(
			middleware.RequireExactRoles(auth.RoleLegacyAdmin, auth.RolePlatformAdmin)(http.HandlerFunc(h.DashboardStats)),
		),
	)

	testCases := []struct {
		name    string
		orgType string
		role    string
	}{
		{name: "platform administrator partner organization", orgType: "partner", role: auth.RolePlatformAdmin},
		{name: "legacy administrator customer organization", orgType: "customer", role: auth.RoleLegacyAdmin},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var orgID, userID int64
			require.NoError(t, pool.QueryRow(ctx,
				`INSERT INTO orgs (name, status, org_type) VALUES ($1, 'active', $2) RETURNING id`,
				fmt.Sprintf("dashboard-zero-wallet-%d", index), testCase.orgType,
			).Scan(&orgID))
			require.NoError(t, pool.QueryRow(ctx,
				`INSERT INTO users (org_id, username, password_hash, role, status)
				 VALUES ($1, $2, 'unchanged-test-verifier', $3, 'active') RETURNING id`,
				orgID, fmt.Sprintf("dashboard-zero-wallet-user-%d", index), testCase.role,
			).Scan(&userID))

			token := testutil.MustAdminSessionJWT(t, ctx, pool, userID, orgID, testCase.role, dashboardZeroWalletJWTSecret)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/stats", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var response struct {
				TotalRequests         int           `json:"total_requests"`
				TodayCostMicros       string        `json:"today_cost_micros"`
				AverageLatencyMS      float64       `json:"avg_latency_ms"`
				P95LatencyMS          float64       `json:"p95_latency_ms"`
				P99LatencyMS          float64       `json:"p99_latency_ms"`
				SuccessRate           float64       `json:"success_rate"`
				WalletBalanceMicros   string        `json:"wallet_balance_micros"`
				WalletGrantedMicros   string        `json:"wallet_granted_micros"`
				WalletFrozenMicros    string        `json:"wallet_frozen_micros"`
				WalletThresholdMicros string        `json:"wallet_threshold_micros"`
				RecentLogs            []interface{} `json:"recent_logs"`
				ModelDistribution     []interface{} `json:"model_distribution"`
				Delta                 struct {
					CostMicros  string  `json:"cost_micros"`
					LatencyMS   float64 `json:"latency_ms"`
					SuccessRate float64 `json:"success_rate"`
				} `json:"delta_vs_yesterday"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Zero(t, response.TotalRequests)
			require.Equal(t, "0", response.TodayCostMicros)
			require.Zero(t, response.AverageLatencyMS)
			require.Zero(t, response.P95LatencyMS)
			require.Zero(t, response.P99LatencyMS)
			require.Zero(t, response.SuccessRate)
			require.Equal(t, "0", response.WalletBalanceMicros)
			require.Equal(t, "0", response.WalletGrantedMicros)
			require.Equal(t, "0", response.WalletFrozenMicros)
			require.Equal(t, "0", response.WalletThresholdMicros)
			require.Empty(t, response.RecentLogs)
			require.Empty(t, response.ModelDistribution)
			require.Equal(t, "0", response.Delta.CostMicros)
			require.Zero(t, response.Delta.LatencyMS)
			require.Zero(t, response.Delta.SuccessRate)

			var walletCount, callCount int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets WHERE org_id = $1`, orgID).Scan(&walletCount))
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs WHERE org_id = $1`, orgID).Scan(&callCount))
			require.Zero(t, walletCount, "dashboard GET must not create a wallet")
			require.Zero(t, callCount)
		})
	}
}
