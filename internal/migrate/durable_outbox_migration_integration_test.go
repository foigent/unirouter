//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
)

func TestP6F20DurableOutboxMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000027_prd6_durable_internal_outbox.up.sql")
	ctx := context.Background()

	assertUp := func() {
		t.Helper()
		var tables, destinationColumns, freezeColumns, ledgerCount int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
 WHERE table_schema='public'
   AND table_name IN (
       'internal_outbox_events','usage_finalization_journal','project_budget_usage',
       'runtime_terminal_hooks'
   )`).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public'
   AND column_name='outbox_event_key'
   AND table_name IN (
       'call_logs','request_trace_events','audit_logs','notification_deliveries'
   )`).Scan(&destinationColumns))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='freeze_idempotency'
   AND column_name='project_id'`).Scan(&freezeColumns))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version='000027_prd6_durable_internal_outbox'`,
		).Scan(&ledgerCount))
		require.Equal(t, 4, tables)
		require.Equal(t, 4, destinationColumns)
		require.Equal(t, 1, freezeColumns)
		require.Equal(t, 1, ledgerCount)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000027_prd6_durable_internal_outbox.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "an unused outbox schema permits controlled rollback")

	var remainingTables, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
 WHERE table_schema='public'
   AND table_name IN (
       'internal_outbox_events','usage_finalization_journal','project_budget_usage',
       'runtime_terminal_hooks'
   )`).Scan(&remainingTables))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000027_prd6_durable_internal_outbox'`,
	).Scan(&ledgerCount))
	require.Zero(t, remainingTables)
	require.Zero(t, ledgerCount)

	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('p6f20-migration-baseline', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (
			org_id, name, slug, description, status, budget_cents, budget_period
		 ) VALUES ($1, 'P6F20 baseline', 'p6f20-migration-baseline', '', 'active', 100, 'daily')
		 RETURNING id`, orgID,
	).Scan(&projectID))
	_, err = pool.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, project_id, cost_cents, created_at)
		 VALUES ('op_p6f20_pre_outbox', $1, $2, 7, NOW())`, orgID, projectID)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
	var baseline int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spent_cents FROM project_budget_usage
		 WHERE org_id = $1 AND project_id = $2 AND budget_period = 'daily'`, orgID, projectID,
	).Scan(&baseline))
	require.EqualValues(t, 7, baseline, "roll-forward must establish the pre-outbox current-period baseline exactly once")
	_, err = pool.Exec(ctx,
		`INSERT INTO internal_outbox_events (
			event_key, event_type, priority, payload
		 ) VALUES ('p6f20-down-guard', 'evidence.trace.v1', 100, '{}'::jsonb)`,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F20 down refused")
	assertUp()

	var payload string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT payload::text FROM internal_outbox_events WHERE event_key='p6f20-down-guard'`,
	).Scan(&payload))
	require.JSONEq(t, `{}`, payload)
}
