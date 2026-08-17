//go:build integration

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/modelreadiness"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRateCardReadinessPriceAuthorityIgnoresLegacyRowsAndReflectsStrictResolverIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('readiness-rate-card','active','customer') RETURNING id`,
	).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name,provider,input_price_cents,output_price_cents)
		VALUES ('legacy-only-readiness','legacy-provider',1,2)`)
	require.NoError(t, err)

	handler := NewHandler(pool, "readiness-rate-card-secret", []byte("0123456789abcdef01234567"), nil)
	handler.SetRequestPricingAuthority(string(billing.RequestPricingAuthorityRateCard))
	billingService := billing.NewBilling(pool, nil).WithRequestPricingAuthority(billing.RequestPricingAuthorityRateCard)
	handler.SetBillingReadinessProvider(billingService)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-readiness", nil)

	missingState, missingErr := billingService.InspectAdmissionReadiness(ctx, orgID, "legacy-only-readiness")
	missing := handler.readinessPriceAuthorityAxis(request, "legacy-only-readiness", missingState, missingErr)
	require.Equal(t, modelreadiness.StateBlocked, missing.State)
	require.Equal(t, "rate_card_authority_not_ready", missing.ReasonCode)
	require.Equal(t, "rate_card", missing.SafeEvidence["request_pricing_mode"])

	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	effectiveFrom := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	draft, err := store.CreateRateCardDraft(ctx, enterprisefinance.RateCard{
		RateCardRef: "readiness-active-rate", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "strict-only-readiness",
		Layer: enterprisefinance.RateSupplierContract, EffectiveFrom: effectiveFrom,
		Components: adminRequestRateComponents(10),
	}, "user:201")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, draft.Card.Version, "user:202")
	require.NoError(t, err)
	active, err := store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:202", time.Now().UTC().Truncate(time.Microsecond))
	require.NoError(t, err)

	readyState, readyErr := billingService.InspectAdmissionReadiness(ctx, orgID, "strict-only-readiness")
	ready := handler.readinessPriceAuthorityAxis(request, "strict-only-readiness", readyState, readyErr)
	require.Equal(t, modelreadiness.StateReady, ready.State)
	require.Equal(t, "rate_card_authority_resolved", ready.ReasonCode)
	require.Equal(t, active.Card.RateCardRef, ready.SafeEvidence["rate_card_ref"])
	require.Equal(t, active.Card.Version, ready.SafeEvidence["rate_card_version"])
}
