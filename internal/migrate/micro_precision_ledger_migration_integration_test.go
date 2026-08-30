//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1MicroPrecisionLedgerMigrationConvertsAndGuardsDowngrade(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeV1MicroLedger(t, pool, migrationsDir)

	const orgID = int64(3601)
	_, err := pool.Exec(ctx, `
		INSERT INTO orgs (id, name, status, org_type)
		VALUES ($1, 'v1-micro-ledger-history', 'active', 'standard')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		VALUES ($1, 123, 4, 0, 7)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (request_id, org_id, type, amount, balance_after)
		VALUES ('v1_micro_history', $1, 'topup', 2, 125)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO call_logs (
			request_id, org_id, cost_cents,
			effective_input_price_cents, effective_cache_read_price_cents,
			effective_cache_creation_price_cents, effective_output_price_cents,
			cache_read_cost_cents, cache_creation_cost_cents
		) VALUES (
			'v1_micro_history', $1, 1,
			0.2, 0.02, 0.25, 0.1, 0.0002, 0.0003
		)`, orgID)
	require.NoError(t, err)

	microLedgerDir := migrationsThrough(t, migrationsDir, "000036_v1_micro_precision_ledger.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, microLedgerDir))
	var balance, granted, spent, transactionAmount, callCost int64
	var inputPrice, cacheReadPrice, cacheReadCost, cacheCreationCost int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT balance_micros, granted_balance_micros, total_spent_micros
		FROM wallets WHERE org_id=$1`, orgID,
	).Scan(&balance, &granted, &spent))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT amount_micros FROM transactions
		WHERE org_id=$1 AND request_id='v1_micro_history'`, orgID,
	).Scan(&transactionAmount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT cost_micros, effective_input_price_micros_per_million,
		       effective_cache_read_price_micros_per_million,
		       cache_read_cost_micros, cache_creation_cost_micros
		FROM call_logs WHERE org_id=$1 AND request_id='v1_micro_history'`, orgID,
	).Scan(&callCost, &inputPrice, &cacheReadPrice, &cacheReadCost, &cacheCreationCost))
	require.Equal(t, int64(1_230_000), balance)
	require.Equal(t, int64(40_000), granted)
	require.Equal(t, int64(70_000), spent)
	require.Equal(t, int64(20_000), transactionAmount)
	require.Equal(t, int64(10_000), callCost)
	require.Equal(t, int64(2_000_000), inputPrice)
	require.Equal(t, int64(200_000), cacheReadPrice)
	require.Equal(t, int64(2), cacheReadCost)
	require.Equal(t, int64(3), cacheCreationCost)
	var nextOrgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('v1-micro-ledger-sequence-check', 'active', 'standard')
		RETURNING id`,
	).Scan(&nextOrgID))
	require.Greater(t, nextOrgID, orgID, "V1 migration must repair the historical explicit-id org sequence")
	var exactPriceConstraints int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
			'model_prices_input_price_nonnegative',
			'model_prices_output_price_nonnegative',
			'org_model_prices_multiplier_bounds'
		)`).Scan(&exactPriceConstraints))
	require.Equal(t, 3, exactPriceConstraints)

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000036_v1_micro_precision_ledger.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "converted cent history permits a lossless immediate downgrade")

	var oldBalance int
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE org_id=$1`, orgID).Scan(&oldBalance))
	require.Equal(t, 123, oldBalance)
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationsDir, "000036_v1_micro_precision_ledger"))

	_, err = pool.Exec(ctx, `UPDATE wallets SET balance_micros=balance_micros+10000 WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "post-activation micro-ledger evidence exists")

	var ledger, microColumn int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000036_v1_micro_precision_ledger'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance_micros'`).Scan(&microColumn))
	require.Equal(t, 1, ledger)
	require.Equal(t, 1, microColumn)
}

func TestV1MicroPrecisionLedgerMigrationRefusesActiveFinancialState(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeV1MicroLedger(t, pool, migrationsDir)

	const orgID = int64(3602)
	_, err := pool.Exec(ctx, `
		INSERT INTO orgs (id, name, status, org_type)
		VALUES ($1, 'v1-micro-ledger-active', 'active', 'standard')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		VALUES ($1, 99, 0, 1, 0)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
			request_id, org_id, frozen_amount, reserved_balance_amount,
			status, pricing_version
		) VALUES ('v1_micro_active', $1, 1, 1, 'frozen', 'pv2_active')`, orgID)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, migrationsDir, "000036_v1_micro_precision_ledger")
	require.ErrorContains(t, err, "active billing reservations exist")

	var ledger, oldColumn, newColumn int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000036_v1_micro_precision_ledger'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance'`).Scan(&oldColumn))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance_micros'`).Scan(&newColumn))
	require.Zero(t, ledger)
	require.Equal(t, 1, oldColumn)
	require.Zero(t, newColumn)
}

func TestV1MicroPrecisionLedgerMigrationRefusesInvalidPriceAuthority(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeV1MicroLedger(t, pool, migrationsDir)

	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		VALUES ('v1-micro-invalid-price', -0.1, 1)`)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, migrationsDir, "000036_v1_micro_precision_ledger")
	require.ErrorContains(t, err, "model price authority is outside the exact supported range")

	var ledger, oldColumn, newColumn int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000036_v1_micro_precision_ledger'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance'`).Scan(&oldColumn))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance_micros'`).Scan(&newColumn))
	require.Zero(t, ledger)
	require.Equal(t, 1, oldColumn)
	require.Zero(t, newColumn)
}

func TestV1MicroPrecisionLedgerMigrationRefusesUnrepresentableEffectivePrice(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	runMigrationsBeforeV1MicroLedger(t, pool, migrationsDir)

	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		VALUES ('v1-micro-overflow-price', 999999999999.999999, 1)`)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, migrationsDir, "000036_v1_micro_precision_ledger")
	require.ErrorContains(t, err, "effective model price exceeds micro-CNY int64 range")

	var ledger, oldColumn, newColumn int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000036_v1_micro_precision_ledger'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance'`).Scan(&oldColumn))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='wallets' AND column_name='balance_micros'`).Scan(&newColumn))
	require.Zero(t, ledger)
	require.Equal(t, 1, oldColumn)
	require.Zero(t, newColumn)
}

func runMigrationsBeforeV1MicroLedger(t *testing.T, pool *pgxpool.Pool, migrationsDir string) {
	t.Helper()
	preMicroDir := t.TempDir()
	entries, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") || name >= "000036_" {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(migrationsDir, name))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(preMicroDir, name), body, 0o600))
	}
	require.NoError(t, migrate.Run(context.Background(), pool, preMicroDir))
}
