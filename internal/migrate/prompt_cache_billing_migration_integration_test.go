//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F09PromptCacheBillingMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	assertUp := func() {
		t.Helper()
		var modelColumns, freezeColumns, logColumns, ledgerCount int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='model_prices'
   AND column_name IN ('cache_read_ratio','cache_creation_ratio')`).Scan(&modelColumns))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='freeze_idempotency'
   AND column_name IN (
       'effective_cache_read_price_cents','effective_cache_creation_price_cents',
       'cache_read_ratio','cache_creation_ratio','uncached_prompt_tokens',
       'cache_read_tokens','cache_creation_tokens','cache_usage_schema',
       'cache_usage_status','uncached_input_cost_cents','cache_read_cost_cents',
       'cache_creation_cost_cents','output_cost_cents'
   )`).Scan(&freezeColumns))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='call_logs'
   AND column_name IN (
       'uncached_prompt_tokens','cache_read_tokens','cache_creation_tokens',
       'cache_usage_schema','cache_usage_status','pricing_version',
       'effective_input_price_cents','effective_cache_read_price_cents',
       'effective_cache_creation_price_cents','effective_output_price_cents',
       'cache_read_cost_cents','cache_creation_cost_cents'
   )`).Scan(&logColumns))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version='000026_prd6_prompt_cache_billing'`,
		).Scan(&ledgerCount))
		require.Equal(t, 2, modelColumns)
		require.Equal(t, 13, freezeColumns)
		require.Equal(t, 12, logColumns)
		require.Equal(t, 1, ledgerCount)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000026_prd6_prompt_cache_billing.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "unactivated default ratios and empty receipts permit controlled rollback")

	var remainingColumns, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public'
   AND ((table_name='model_prices' AND column_name='cache_read_ratio')
     OR (table_name='freeze_idempotency' AND column_name='cache_read_tokens')
     OR (table_name='call_logs' AND column_name='cache_read_tokens'))`).Scan(&remainingColumns))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000026_prd6_prompt_cache_billing'`,
	).Scan(&ledgerCount))
	require.Zero(t, remainingColumns)
	require.Zero(t, ledgerCount)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	model := fmt.Sprintf("p6f09-migration-ratio-%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx,
		`INSERT INTO model_prices (
			model_name, provider, input_price_cents, output_price_cents,
			cache_read_ratio, cache_creation_ratio
		 ) VALUES ($1, 'test', 4, 20, 0.1, 1.25)`, model)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F09 down refused")
	assertUp()

	var readRatio float64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT cache_read_ratio::float8 FROM model_prices WHERE model_name=$1`, model,
	).Scan(&readRatio))
	require.InDelta(t, 0.1, readRatio, 0.000001, "refused rollback must preserve activated pricing")
	require.NoError(t, func() error {
		_, updateErr := pool.Exec(ctx,
			`UPDATE model_prices SET cache_read_ratio=1, cache_creation_ratio=1 WHERE model_name=$1`, model)
		return updateErr
	}())

	orgID := testutil.NewTestOrg(t, pool)
	require.NoError(t, func() error {
		_, insertErr := pool.Exec(ctx,
			`INSERT INTO wallets (org_id,balance,granted_balance,frozen,total_spent)
			 VALUES ($1,1000,0,0,0)`, orgID)
		return insertErr
	}())
	requestID := "op_p6f09_migration_pv2_guard"
	freeze, err := billing.NewBilling(pool, nil).Freeze(ctx, billing.FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1000,
		SafetyFactor:    1,
		MinFreezeAmount: 1,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	require.Contains(t, freeze.PricingVersion, "pv2_")

	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F09 down refused")
	assertUp()

	var storedVersion string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT pricing_version FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&storedVersion))
	require.Equal(t, freeze.PricingVersion, storedVersion, "refused rollback must preserve the financial receipt")
}
