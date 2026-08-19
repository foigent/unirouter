//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestOnPremBillingModeMigrationDefaultsHistoricalReservationsToPrepaid(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000062_xinsight_connector_redeem_lifecycle.up.sql")
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (
    request_id,org_id,status,pricing_version,
    frozen_amount_micros,reserved_balance_micros)
VALUES (
    'op_billing_mode_historical',960001,'frozen','historical-price-v1',
    120000,120000)`)
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	var mode string
	var admissionCalculatedCost int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT billing_mode,admission_calculated_cost_micros
FROM freeze_idempotency
WHERE org_id=960001 AND request_id='op_billing_mode_historical'`).Scan(&mode, &admissionCalculatedCost))
	require.Equal(t, "prepaid_enforced", mode)
	require.Equal(t, int64(120000), admissionCalculatedCost)

	var receiptTable, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema='public' AND table_name='billing_observation_receipts'`).Scan(&receiptTable))
	require.Equal(t, 1, receiptTable)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000063_onprem_billing_mode'`).Scan(&ledger))
	require.Equal(t, 1, ledger)

	var reviewedResetConstraints int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM pg_constraint
WHERE conname IN (
    'billing_observation_receipt_reservation_fk',
    'freeze_idempotency_observation_receipt_fk'
)
  AND condeferrable
  AND NOT condeferred`).Scan(&reviewedResetConstraints))
	require.Equal(t, 2, reviewedResetConstraints,
		"the reviewed observation receipt cycle must be deferrable but immediate by default")
}

func TestOnPremBillingModeMigrationCleanRollbackAndReapply(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000062_xinsight_connector_redeem_lifecycle.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	execMigrationDown(t, ctx, pool, "000068_provider_reported_model_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000065_model_endpoint_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000064_rate_card_request_pricing_authority.down.sql")
	execMigrationDown(t, ctx, pool, "000063_onprem_billing_mode.down.sql")

	var billingModeColumns, admissionColumns, receiptTables, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema='public'
  AND table_name IN ('freeze_idempotency','usage_finalization_journal')
  AND column_name='billing_mode'`).Scan(&billingModeColumns))
	require.Zero(t, billingModeColumns)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='freeze_idempotency'
  AND column_name='admission_calculated_cost_micros'`).Scan(&admissionColumns))
	require.Zero(t, admissionColumns)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema='public' AND table_name='billing_observation_receipts'`).Scan(&receiptTables))
	require.Zero(t, receiptTables)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000063_onprem_billing_mode'`).Scan(&ledger))
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000063_onprem_billing_mode'`).Scan(&ledger))
	require.Equal(t, 1, ledger)
}

func TestOnPremBillingModeMigrationRefusesRollbackWhileLaterMigrationIsApplied(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000063_onprem_billing_mode.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")

	var columns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='freeze_idempotency'
  AND column_name IN (
    'billing_mode','admission_calculated_cost_micros','terminal_observation_receipt_id'
  )`).Scan(&columns))
	require.Equal(t, 3, columns, "a refused out-of-order rollback must preserve migration-63 columns")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version IN ('000063_onprem_billing_mode','000064_rate_card_request_pricing_authority')`).Scan(&ledger))
	require.Equal(t, 2, ledger, "a refused out-of-order rollback must preserve both ledger rows")
}

func TestOnPremBillingModeMigrationRefusesObservationEvidenceRollbackAtomically(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000062_xinsight_connector_redeem_lifecycle.up.sql")
	ctx := context.Background()
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (
    request_id,org_id,status,pricing_version,billing_mode)
VALUES (
    'op_billing_mode_observation',960002,'frozen','observation-price-v1','cost_observation')`)
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000068_provider_reported_model_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")
	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000065_model_endpoint_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000064_rate_card_request_pricing_authority.down.sql")

	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000063_onprem_billing_mode.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "non-prepaid billing evidence exists")

	var columns, ledger, observationRows int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='freeze_idempotency'
  AND column_name IN (
    'billing_mode','admission_calculated_cost_micros','terminal_observation_receipt_id'
  )`).Scan(&columns))
	require.Equal(t, 3, columns, "a refused rollback must preserve every migration-63 column")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000063_onprem_billing_mode'`).Scan(&ledger))
	require.Equal(t, 1, ledger, "a refused rollback must preserve the schema ledger")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM freeze_idempotency
WHERE org_id=960002 AND request_id='op_billing_mode_observation'
  AND billing_mode='cost_observation'`).Scan(&observationRows))
	require.Equal(t, 1, observationRows, "a refused rollback must preserve observation evidence")
}

func execMigrationDown(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err, name)
}
