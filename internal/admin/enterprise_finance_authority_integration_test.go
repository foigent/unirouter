//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseRateCardAuthorityStatusReportsConfiguredProcessModeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','customer') RETURNING id`,
		fmt.Sprintf("rate-authority-status-%d", time.Now().UnixNano()),
	).Scan(&orgID))

	handler := NewHandler(pool, "rate-authority-secret", []byte("0123456789abcdef01234567"), nil)
	handler.SetRequestPricingAuthority("rate_card")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/enterprise-finance/rate-cards?org_id=%d", orgID), nil)
	req = req.WithContext(rateAuthorityActorContext(req.Context(), orgID))
	rec := httptest.NewRecorder()

	handler.ListEnterpriseRateCards(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Authority struct {
			CurrentMode  string `json:"current_mode"`
			StrictActive bool   `json:"strict_active"`
			State        string `json:"state"`
		} `json:"request_pricing_authority"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "rate_card", response.Authority.CurrentMode)
	require.True(t, response.Authority.StrictActive)
	require.Equal(t, "no_active_rate", response.Authority.State)
}

func TestEnterpriseRateCardPreflightReportsMissingModelWithoutChangingModeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','customer') RETURNING id`,
		fmt.Sprintf("rate-authority-preflight-%d", time.Now().UnixNano()),
	).Scan(&orgID))

	handler := NewHandler(pool, "rate-authority-secret", []byte("0123456789abcdef01234567"), nil)
	handler.SetRequestPricingAuthority("legacy")
	body := fmt.Sprintf(`{"org_id":%d,"models":[{"model_ref":"missing-model"}]}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enterprise-finance/rate-cards/preflight", strings.NewReader(body))
	req = req.WithContext(rateAuthorityActorContext(req.Context(), orgID))
	rec := httptest.NewRecorder()

	handler.PreflightEnterpriseRateCardAuthority(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Ready  bool `json:"ready"`
		Models []struct {
			ModelRef string `json:"model_ref"`
			State    string `json:"state"`
			Error    string `json:"error"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.False(t, response.Ready)
	require.Len(t, response.Models, 1)
	require.Equal(t, "missing-model", response.Models[0].ModelRef)
	require.Equal(t, "not_ready", response.Models[0].State)
	require.Contains(t, response.Models[0].Error, "no active quote")
	require.Equal(t, "legacy", handler.effectiveRequestPricingAuthority())
}

func TestEnterpriseRateCardCreateAPIStoresCompleteUTCDailyScheduleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','customer') RETURNING id`,
		fmt.Sprintf("rate-authority-api-schedule-%d", time.Now().UnixNano()),
	).Scan(&orgID))
	base := adminRequestRateComponents(20)
	payload, err := json.Marshal(map[string]interface{}{
		"org_id": orgID, "provider_ref": "provider-a", "model_ref": "scheduled-model",
		"layer":          enterprisefinance.RateSupplierContract,
		"effective_from": time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond),
		"components":     base,
		"daily_price_bands": []enterprisefinance.DailyPriceBand{
			{BandRef: "base-before", StartMinuteUTC: 0, EndMinuteUTC: 90, Components: base},
			{BandRef: "valley", StartMinuteUTC: 90, EndMinuteUTC: 360, Components: adminRequestRateComponents(5)},
			{BandRef: "base-after", StartMinuteUTC: 360, EndMinuteUTC: 1440, Components: base},
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enterprise-finance/rate-cards", strings.NewReader(string(payload)))
	actorCtx := rateAuthorityActorContext(req.Context(), orgID)
	actorCtx = context.WithValue(actorCtx, middleware.CtxUserID, int64(301))
	req = req.WithContext(actorCtx)
	rec := httptest.NewRecorder()
	handler := NewHandler(pool, "rate-authority-secret", []byte("0123456789abcdef01234567"), nil)

	handler.CreateEnterpriseRateCardDraft(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var response enterprisefinance.RateCardRecord
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Card.DailyPriceBands, 3)
	require.Equal(t, "valley", response.Card.DailyPriceBands[1].BandRef)

	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	listed, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, 1440, listed[0].Card.DailyPriceBands[2].EndMinuteUTC)
}

func TestEnterpriseRateCardActivationAPIRequiresAndAppliesExactSupersedeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','customer') RETURNING id`,
		fmt.Sprintf("rate-authority-api-supersede-%d", time.Now().UnixNano()),
	).Scan(&orgID))
	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	effectiveFrom := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	createApproved := func(ref string, input int64) enterprisefinance.RateCardRecord {
		t.Helper()
		draft, createErr := store.CreateRateCardDraft(ctx, enterprisefinance.RateCard{
			RateCardRef: ref, OrgID: orgID, ProviderRef: "provider-a", ModelRef: "model-a",
			Layer: enterprisefinance.RateSupplierContract, EffectiveFrom: effectiveFrom,
			Components: adminRequestRateComponents(input),
		}, "user:101")
		require.NoError(t, createErr)
		approved, approveErr := store.ApproveRateCard(ctx, orgID, ref, draft.Card.Version, "user:102")
		require.NoError(t, approveErr)
		return approved
	}
	oldApproved := createApproved("api-old-rate", 10)
	oldActive, err := store.ActivateRateCard(ctx, orgID, oldApproved.Card.RateCardRef, oldApproved.Card.Version, "user:102", time.Now().UTC().Truncate(time.Microsecond))
	require.NoError(t, err)
	candidate := createApproved("api-new-rate", 20)

	body := fmt.Sprintf(
		`{"org_id":%d,"expected_version":%d,"supersedes_rate_card_ref":%q,"expected_superseded_version":%d}`,
		orgID, candidate.Card.Version, oldActive.Card.RateCardRef, oldActive.Card.Version,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enterprise-finance/rate-cards/api-new-rate/activate", strings.NewReader(body))
	req.SetPathValue("rate_card_ref", candidate.Card.RateCardRef)
	actorCtx := rateAuthorityActorContext(req.Context(), orgID)
	actorCtx = context.WithValue(actorCtx, middleware.CtxUserID, int64(102))
	req = req.WithContext(actorCtx)
	rec := httptest.NewRecorder()
	handler := NewHandler(pool, "rate-authority-secret", []byte("0123456789abcdef01234567"), nil)

	handler.ActivateEnterpriseRateCard(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	latest, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	states := make(map[string]string, len(latest))
	for _, record := range latest {
		states[record.Card.RateCardRef] = record.Card.State
	}
	require.Equal(t, enterprisefinance.RateRetired, states[oldActive.Card.RateCardRef])
	require.Equal(t, enterprisefinance.RateActive, states[candidate.Card.RateCardRef])
}

func adminRequestRateComponents(input int64) []enterprisefinance.RateComponent {
	values := []struct {
		meter string
		value int64
	}{
		{enterprisefinance.MeterInputTokens, input},
		{enterprisefinance.MeterOutputTokens, input * 2},
		{enterprisefinance.MeterCacheReadTokens, input / 2},
		{enterprisefinance.MeterCacheCreationTokens, input},
	}
	components := make([]enterprisefinance.RateComponent, 0, len(values))
	for _, value := range values {
		components = append(components, enterprisefinance.RateComponent{
			Meter: value.meter, UnitQuantity: "1000000",
			Amount: enterprisefinance.Amount{ValueMinor: fmt.Sprintf("%d", value.value), Currency: "CNY", Scale: 1_000_000},
		})
	}
	return components
}

func rateAuthorityActorContext(ctx context.Context, orgID int64) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, auth.RoleOrgAdmin)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	return ctx
}
