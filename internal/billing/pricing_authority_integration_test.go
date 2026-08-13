//go:build integration

package billing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1PricingAuthorityUnpricedModelFailsClosed(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		VALUES ($1, 10000000, 0, 0, 0)`, orgID)
	require.NoError(t, err)

	service := NewBilling(pool, nil)
	inputPrice, outputPrice, err := service.QueryModelPriceMicrosForOrg(ctx, orgID, "unpriced-v1-model")
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)

	freeze, err := service.Freeze(ctx, FreezeRequest{
		RequestID:       "v1-unpriced-freeze",
		OrgID:           orgID,
		Model:           "unpriced-v1-model",
		EstimatedTokens: 1000,
		SafetyFactor:    "1",
		MinFreezeCents:  1,
	})
	require.Nil(t, freeze)
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)

	var balanceMicros, frozenMicros int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, frozen_micros FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balanceMicros, &frozenMicros))
	require.Equal(t, testMicros(1000), balanceMicros)
	require.Zero(t, frozenMicros)
}

func TestV1PricingAuthorityExplicitZeroPriceIsNotMissing(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	const model = "explicit-free-v1-model"
	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		VALUES ($1, 0, 0)`, model)
	require.NoError(t, err)

	inputPrice, outputPrice, err := NewBilling(pool, nil).QueryModelPriceMicrosForOrg(ctx, 0, model)
	require.NoError(t, err)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)
}

func TestCostObservationUnpricedModelDoesNotBecomeTrafficAdmission(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	service := NewBilling(pool, nil).WithMode(BillingModeCostObservation)

	inputPrice, outputPrice, err := service.QueryModelPriceMicrosForOrg(ctx, orgID, "unpriced-observation-model")
	require.NoError(t, err)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)

	freeze, err := service.Freeze(ctx, FreezeRequest{
		RequestID: "unpriced-observation-freeze", OrgID: orgID,
		Model: "unpriced-observation-model", EstimatedTokens: 1000,
		SafetyFactor: "1", MinFreezeCents: 1,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	require.Zero(t, freeze.AmountMicros)
	require.Zero(t, freeze.CalculatedCostMicros)
	require.Regexp(t, `^uo1_[a-f0-9]{64}$`, freeze.PricingVersion)
	require.Equal(t, BillingModeCostObservation, freeze.BillingMode)
}
