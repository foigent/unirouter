//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1PricingUnitContractMigrationDocumentsWithoutRepricing(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	const model = "v1-pricing-unit-contract"
	_, err := pool.Exec(ctx, `
		INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		VALUES ($1, 0.2, 0.1)
		ON CONFLICT (model_name) DO UPDATE
		SET input_price_cents=EXCLUDED.input_price_cents,
		    output_price_cents=EXCLUDED.output_price_cents`, model)
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var inputComment, outputComment string
		var inputPrice, outputPrice float64
		var ledger int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT col_description('model_prices'::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid='model_prices'::regclass AND attname='input_price_cents'`,
		).Scan(&inputComment))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT col_description('model_prices'::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid='model_prices'::regclass AND attname='output_price_cents'`,
		).Scan(&outputComment))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT input_price_cents::float8, output_price_cents::float8
			FROM model_prices WHERE model_name=$1`, model,
		).Scan(&inputPrice, &outputPrice))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM schema_migrations
			WHERE version='000035_v1_pricing_unit_contract'`,
		).Scan(&ledger))
		require.Contains(t, inputComment, "CNY cents per 1,000 input tokens")
		require.Contains(t, outputComment, "CNY cents per 1,000 output tokens")
		require.InDelta(t, 0.2, inputPrice, 0.000001)
		require.InDelta(t, 0.1, outputPrice, 0.000001)
		require.Equal(t, 1, ledger)
	}

	assertUp()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000035_v1_pricing_unit_contract.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000035_v1_pricing_unit_contract'`,
	).Scan(&ledger))
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
}
