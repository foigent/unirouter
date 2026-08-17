//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCostOptimizationExactFormulaAndTenantScopeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO call_logs (
			request_id, org_id, model, provider, channel,
			prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
			completion_tokens, total_tokens, cost_micros, status_code, usage_source,
			cache_usage_schema, cache_usage_status, pricing_version,
			effective_input_price_micros_per_million,
			effective_cache_read_price_micros_per_million,
			effective_cache_creation_price_micros_per_million,
			effective_output_price_micros_per_million
		) VALUES
			('co-eligible-a', 7, 'model-a', 'provider-a', 'channel-a',
			 100, 20, 60, 20, 10, 110, 76, 200, 'upstream',
			 'provider.cache.v1', 'cache_read_write_reported', 'price-v1',
			 1000000, 100000, 1500000, 2000000),
			('co-reported-no-cache-a', 7, 'model-a', 'provider-a', 'channel-a',
			 100, 100, 0, 0, 10, 110, 120, 200, 'upstream',
			 'provider.cache.v1', 'cache_hit_miss_reported', 'price-v1',
			 1000000, 100000, 1500000, 2000000),
			('co-gap-a', 7, 'model-a', 'provider-a', 'channel-a',
			 100, 0, 0, 0, 10, 110, 120, 200, 'upstream',
			 '', 'not_reported', 'price-v1',
			 1000000, 100000, 1500000, 2000000),
			('co-other-tenant', 8, 'private-model-b', 'private-provider-b', 'private-channel-b',
			 100, 20, 60, 20, 10, 110, 0, 200, 'upstream',
			 'provider.cache.v1', 'cache_read_write_reported', 'price-v1',
			 1000000, 100000, 1500000, 2000000)
	`)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)
	request := func(target string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		reqCtx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(7))
		reqCtx = context.WithValue(reqCtx, middleware.CtxUserRole, auth.RoleOrgAdmin)
		reqCtx = context.WithValue(reqCtx, middleware.CtxUserID, int64(42))
		return req.WithContext(reqCtx)
	}
	rec := httptest.NewRecorder()
	h.CostOptimizationSummary(rec, request("/api/v1/cost-optimization/summary"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Summary costOptimizationSummary `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, int64(3), response.Summary.CandidateRequests)
	require.Equal(t, int64(2), response.Summary.EligibleRequests)
	require.Equal(t, int64(1), response.Summary.CacheUsedRequests)
	require.Equal(t, "240", response.Summary.OrdinaryInputBaselineCostMicros)
	require.Equal(t, "196", response.Summary.ActualCostMicros)
	require.Equal(t, "44", response.Summary.NetSavingsMicros)
	require.Equal(t, "54", response.Summary.CacheReadSavingsMicros)
	require.Equal(t, "10", response.Summary.CacheCreationPremiumMicros)
	require.Equal(t, "0", response.Summary.RoundingAdjustmentMicros)
	require.InDelta(t, 2.0/3.0, response.Summary.EvidenceCoverageRatio, 0.000001)

	trendRecorder := httptest.NewRecorder()
	h.CostOptimizationTrend(trendRecorder, request("/api/v1/cost-optimization/trend"))
	require.Equal(t, http.StatusOK, trendRecorder.Code, trendRecorder.Body.String())
	var trendResponse struct {
		Data []costOptimizationTrendRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(trendRecorder.Body.Bytes(), &trendResponse))
	require.Len(t, trendResponse.Data, 1)
	require.Equal(t, "44", trendResponse.Data[0].NetSavingsMicros)

	breakdownRecorder := httptest.NewRecorder()
	h.CostOptimizationBreakdown(breakdownRecorder, request("/api/v1/cost-optimization/breakdown?group_by=model"))
	require.Equal(t, http.StatusOK, breakdownRecorder.Code, breakdownRecorder.Body.String())
	var breakdownResponse struct {
		Data []costOptimizationRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(breakdownRecorder.Body.Bytes(), &breakdownResponse))
	require.Len(t, breakdownResponse.Data, 1)
	require.Equal(t, "model-a", breakdownResponse.Data[0].Key)
	require.Equal(t, "44", breakdownResponse.Data[0].NetSavingsMicros)

	opportunityRecorder := httptest.NewRecorder()
	h.CostOptimizationOpportunities(opportunityRecorder, request("/api/v1/cost-optimization/opportunities?group_by=model"))
	require.Equal(t, http.StatusOK, opportunityRecorder.Code, opportunityRecorder.Body.String())
	var opportunityResponse struct {
		Data []costOptimizationOpportunity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(opportunityRecorder.Body.Bytes(), &opportunityResponse))
	require.Len(t, opportunityResponse.Data, 1)
	require.Equal(t, "evidence_gap", opportunityResponse.Data[0].Kind)
	require.Equal(t, "model-a", opportunityResponse.Data[0].GroupKey)
}
