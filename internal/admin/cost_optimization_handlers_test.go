package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/testutil"
)

func costOptimizationRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(42))
	return req.WithContext(ctx)
}

func TestCostOptimizationGroupByWhitelist(t *testing.T) {
	for _, value := range []string{"", "overall", "model", "project", "provider", "channel"} {
		group, expression, ok := costOptimizationGroupBy(value)
		if !ok || group.Key == "" || expression == "" {
			t.Fatalf("expected %q to be accepted, got %#v %q %t", value, group, expression, ok)
		}
	}
	if _, _, ok := costOptimizationGroupBy("model); DROP TABLE call_logs; --"); ok {
		t.Fatal("unexpected untrusted group expression to pass the whitelist")
	}
}

func TestCostOptimizationSummaryUsesTenantAndEvidenceConditions(t *testing.T) {
	var capturedQuery string
	var capturedArgs []any
	db := &testutil.MockDB{QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
		switch {
		case strings.Contains(query, "FROM invocation_optimization_policies"):
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				*dest[1].(*int) = 4
				return nil
			}}
		case strings.Contains(query, "FROM invocation_optimization_receipts"):
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 12
				*dest[1].(*int64) = 8
				*dest[2].(*int64) = 10
				*dest[3].(*int64) = 7
				*dest[4].(*int64) = 0
				*dest[5].(*int64) = 420
				return nil
			}}
		case strings.Contains(query, "FROM response_cache_commercial_receipts"):
			for _, required := range []string{
				"JOIN freeze_idempotency f",
				"f.status='settled'",
				"f.usage_source='response_cache'",
				"f.usage_authority='local_cache'",
				"f.usage_contract_version='unirouter.usage.v2'",
				"f.calculated_cost_micros=0",
				"f.charged_cost_micros=0",
			} {
				if !strings.Contains(query, required) {
					t.Fatalf("realized response-cache savings must require terminal zero-charge evidence %q: %s", required, query)
				}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 3
				*dest[1].(*string) = "125000"
				return nil
			}}
		default:
			capturedQuery = query
			capturedArgs = append([]any(nil), args...)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 10
				*dest[1].(*int64) = 8
				*dest[2].(*int64) = 6
				*dest[3].(*int64) = 5
				*dest[4].(*int64) = 4
				*dest[5].(*int64) = 1000
				*dest[6].(*int64) = 400
				*dest[7].(*int64) = 200
				*dest[8].(*string) = "1000000"
				*dest[9].(*string) = "700000"
				*dest[10].(*string) = "300000"
				*dest[11].(*string) = "350000"
				*dest[12].(*string) = "50000"
				*dest[13].(*string) = "0"
				return nil
			}}
		}
	}}
	h := newTestHandler(db)
	rec := httptest.NewRecorder()
	h.CostOptimizationSummary(rec, costOptimizationRequest("/cost-optimization/summary?provider=openai"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, expected := range []string{
		"org_id = $1",
		"provider = $2",
		"status_code > 0 AND status_code < 400",
		"cache_usage_status <> 'not_reported'",
		"pricing_version <> ''",
		"uncached_prompt_tokens + cache_read_tokens + cache_creation_tokens = prompt_tokens",
		"baseline_cost_micros - actual_cost_micros",
	} {
		if !strings.Contains(capturedQuery, expected) {
			t.Fatalf("summary query missing %q: %s", expected, capturedQuery)
		}
	}
	if len(capturedArgs) != 2 || capturedArgs[0] != int64(7) || capturedArgs[1] != "openai" {
		t.Fatalf("unexpected scoped query args: %#v", capturedArgs)
	}

	var response struct {
		Summary costOptimizationSummary `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Summary.NetSavingsMicros != "300000" || response.Summary.EvidenceCoverageRatio != 0.8 {
		t.Fatalf("unexpected exact summary: %#v", response.Summary)
	}
	if response.Summary.AmountAuthority != "request_time_frozen_customer_pricing" ||
		response.Summary.SupplierCOGSAuthority != "not_claimed_use_provider_reconciliation" ||
		response.Summary.ResponseCacheRuntime != "runtime_unavailable" ||
		response.Summary.CompressionRuntime != "shadow_active" ||
		response.Summary.ResponseCacheHits != 3 ||
		response.Summary.ResponseCacheAvoidedCostMicros != "125000" ||
		response.Summary.CompressionEstimatedSavedTokens != 420 {
		t.Fatalf("authority boundary drifted: %#v", response.Summary)
	}
}

func TestCostOptimizationBreakdownRejectsUnknownGroup(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		t.Fatal("invalid group must fail before querying")
		return nil, nil
	}})
	rec := httptest.NewRecorder()
	h.CostOptimizationBreakdown(rec, costOptimizationRequest("/cost-optimization/breakdown?group_by=raw_sql"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid group_by") {
		t.Fatalf("expected fail-closed group error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCostOptimizationOpportunitiesRemainEvidenceBounded(t *testing.T) {
	ratio := 0.5
	rows := []costOptimizationRow{
		{
			Key:                        "model-a",
			CandidateRequests:          10,
			EligibleRequests:           8,
			CacheReadTokens:            100,
			CacheCreationTokens:        200,
			NetSavingsMicros:           "-20",
			CacheReadSavingsMicros:     "30",
			CacheCreationPremiumMicros: "50",
			EvidenceCoverageRatio:      0.8,
			ReadToCreationTokenRatio:   &ratio,
		},
	}
	opportunities := costOptimizationOpportunities(costOptimizationGroup{Key: "model", Label: "模型"}, rows)
	if len(opportunities) != 3 {
		t.Fatalf("expected three bounded opportunities, got %#v", opportunities)
	}
	if opportunities[0].Kind != "negative_net_savings" || opportunities[0].Severity != "high" {
		t.Fatalf("expected high severity net-loss first, got %#v", opportunities)
	}
	for _, opportunity := range opportunities {
		if strings.Contains(strings.ToLower(opportunity.Rationale), "prompt text") || opportunity.NextAction == "" {
			t.Fatalf("opportunity escaped evidence boundary: %#v", opportunity)
		}
	}
}

func TestCanonicalAuthorityReportsResponseCacheBypassWithoutErasingHistoricalFacts(t *testing.T) {
	if got := responseCacheRuntimeLabel(modelrouting.ModeCanonical, 5, true); got != "canonical_bypass" {
		t.Fatalf("canonical response cache runtime=%q", got)
	}
	if got := responseCacheRuntimeLabel(modelrouting.ModeLegacy, 5, true); got != "canary_active" {
		t.Fatalf("legacy historical runtime label=%q", got)
	}
}
