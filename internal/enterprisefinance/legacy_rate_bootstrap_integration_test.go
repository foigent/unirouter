//go:build integration

package enterprisefinance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestLegacyRateBootstrapCreatesFreshIdempotentDraftsWithoutActivation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('legacy-rate-bootstrap','active','customer') RETURNING id`,
	).Scan(&orgID))
	oldUpdatedAt := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `
		INSERT INTO model_prices (
		  model_name,provider,input_price_cents,output_price_cents,updated_at
		) VALUES
		  ('bootstrap-ready','provider-a',1.25,2.50,$1),
		  ('bootstrap-missing-provider','',1.00,2.00,$1)`, oldUpdatedAt)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO org_model_prices (org_id,model_name,multiplier,updated_at)
		VALUES ($1,'bootstrap-ready',1.200,$2)`, orgID, oldUpdatedAt)
	require.NoError(t, err)

	startedAt := time.Now().UTC().Add(-time.Second)
	result, err := store.BootstrapLegacyProviderListDrafts(ctx, orgID, "user:1001")
	require.NoError(t, err)
	require.Equal(t, 1, result.CreatedDraftCount)
	require.Equal(t, 1, result.SkippedModelCount)
	require.True(t, result.ApprovalRequired)
	require.True(t, result.ActivationRequired)
	require.False(t, result.LegacyRuntimeFallback)

	records, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, RateDraft, records[0].Card.State)
	require.Equal(t, RateProviderList, records[0].Card.Layer)
	require.NotNil(t, records[0].SourceEvidence)
	require.False(t, records[0].SourceEvidence.LastSuccessAt.Before(startedAt))
	require.True(t, records[0].SourceEvidence.StaleAfter.After(time.Now().UTC().Add(23*time.Hour)))

	replayed, err := store.BootstrapLegacyProviderListDrafts(ctx, orgID, "user:1001")
	require.NoError(t, err)
	require.Zero(t, replayed.CreatedDraftCount)
	require.Equal(t, 1, replayed.ExistingDraftCount)

	approved, err := store.ApproveRateCard(ctx, orgID, records[0].Card.RateCardRef, records[0].Card.Version, "user:1002")
	require.NoError(t, err)
	active, err := store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:1002", time.Now().UTC())
	require.NoError(t, err, "bootstrap evidence freshness must be measured when the controlled read occurs, not from the historical row update")
	require.Equal(t, RateActive, active.Card.State)
}
