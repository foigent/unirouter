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
		INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		VALUES ($1, 1000, 0, 0, 0)`, orgID)
	require.NoError(t, err)

	service := NewBilling(pool, nil)
	inputPrice, outputPrice, err := service.QueryModelPrice(ctx, "unpriced-v1-model")
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)

	inputPrice, outputPrice, err = service.QueryModelPriceForOrg(ctx, orgID, "unpriced-v1-model")
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)

	freeze, err := service.Freeze(ctx, FreezeRequest{
		RequestID:       "v1-unpriced-freeze",
		OrgID:           orgID,
		Model:           "unpriced-v1-model",
		EstimatedTokens: 1000,
		SafetyFactor:    1,
		MinFreezeAmount: 1,
	})
	require.Nil(t, freeze)
	require.ErrorIs(t, err, ErrModelPriceNotConfigured)

	var balance, frozen int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance, frozen FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balance, &frozen))
	require.Equal(t, 1000, balance)
	require.Zero(t, frozen)
}

func TestV1PricingAuthorityExplicitZeroPriceIsNotMissing(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	const model = "explicit-free-v1-model"
	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		VALUES ($1, 0, 0)`, model)
	require.NoError(t, err)

	inputPrice, outputPrice, err := NewBilling(pool, nil).QueryModelPrice(ctx, model)
	require.NoError(t, err)
	require.Zero(t, inputPrice)
	require.Zero(t, outputPrice)
}
