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

func TestP6F11StreamPolicyTerminationMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.PreRetirementMigrationsDir(t)
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000034_prd6_stream_policy_termination.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var tables, columns, checkpointTriggers, extensionTriggers, ledger int
		var receiptConstraint string
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema='public' AND table_name IN (
				'billing_reservation_extensions','stream_delivery_leases','stream_delivery_checkpoints'
			)`).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='freeze_idempotency'
			  AND column_name IN ('reserved_completion_tokens','reservation_extension_sequence')`).Scan(&columns))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(DISTINCT trigger_name) FROM information_schema.triggers
			WHERE event_object_schema='public' AND event_object_table='stream_delivery_checkpoints'
			  AND trigger_name IN ('stream_delivery_checkpoint_transition','stream_delivery_checkpoint_append_only')`).Scan(&checkpointTriggers))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(DISTINCT trigger_name) FROM information_schema.triggers
			WHERE event_object_schema='public' AND event_object_table='billing_reservation_extensions'
			  AND trigger_name IN ('billing_reservation_extension_transition','billing_reservation_extension_append_only')`).Scan(&extensionTriggers))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM schema_migrations
			WHERE version='000034_prd6_stream_policy_termination'`).Scan(&ledger))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint
			WHERE conname='request_delivery_receipts_outcome_counts_check'`).Scan(&receiptConstraint))
		require.Equal(t, 3, tables)
		require.Equal(t, 2, columns)
		require.Equal(t, 2, checkpointTriggers)
		require.Equal(t, 2, extensionTriggers)
		require.Equal(t, 1, ledger)
		require.Contains(t, receiptConstraint, "interrupted")
		require.Contains(t, receiptConstraint, "delivered_bytes = 0")
	}

	assertUp()
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "unused stream schema permits controlled rollback")
	var tables, columns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
			'billing_reservation_extensions','stream_delivery_leases','stream_delivery_checkpoints'
		)`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='freeze_idempotency'
		  AND column_name IN ('reserved_completion_tokens','reservation_extension_sequence')`).Scan(&columns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000034_prd6_stream_policy_termination'`).Scan(&ledger))
	require.Zero(t, tables)
	require.Zero(t, columns)
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
	_, err = pool.Exec(ctx, `
		INSERT INTO guardrail_policies (
			name,stage,detector_ref,action,mode,failure_mode,mandatory,config
		) VALUES (
			'p6f11-stream-guard','stream_output','builtin:pii_secrets',
			'block','enforce','fail_closed',FALSE,'{"entities":["email"]}'
		)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F11 rollback refused")
	assertUp()
}
