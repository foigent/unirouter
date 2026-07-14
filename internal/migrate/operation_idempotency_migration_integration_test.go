package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F17OperationIdempotencyMigrationUpDownAndSafeRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	var orgA, orgB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f17-org-a', 'active', 'standard') RETURNING id`,
	).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f17-org-b', 'active', 'standard') RETURNING id`,
	).Scan(&orgB))
	_, err := pool.Exec(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, reason, created_by, client_request_id)
		 VALUES ($1, 10000, 10000, 'a', 0, 'same-client-value'),
		        ($2, 10000, 10000, 'b', 0, 'same-client-value')`, orgA, orgB)
	require.NoError(t, err, "tenant-scoped grant idempotency must permit cross-org reuse")

	_, err = pool.Exec(ctx,
		`INSERT INTO request_idempotency
		 (org_id, api_key_id, key_hash, request_fingerprint, operation_id, state, expires_at)
		 VALUES ($1, 11, $2, $3, 'op_00000000000000000000000000000001', 'completed', NOW() + INTERVAL '1 day')`,
		orgA, strings.Repeat("a", 64), strings.Repeat("b", 64),
	)
	require.NoError(t, err)

	var identityColumnCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND ((table_name = 'call_logs' AND column_name IN
		        ('client_request_id', 'idempotency_key_hash', 'workflow_id', 'agent_id', 'attempt_id'))
		    OR (table_name = 'request_trace_events' AND column_name IN
		        ('client_request_id', 'idempotency_key_hash', 'workflow_id', 'agent_id', 'attempt_id'))
		    OR (table_name = 'transactions' AND column_name = 'client_request_id')
		    OR (table_name = 'audit_logs' AND column_name = 'client_request_id'))`,
	).Scan(&identityColumnCount))
	require.Equal(t, 12, identityColumnCount)

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000021_prd6_operation_idempotency_identity.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var tableCount, migrationLedgerCount, retainedGrantIndexCount, grantClientIDLength int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'request_idempotency'`,
	).Scan(&tableCount))
	require.Zero(t, tableCount)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND ((table_name = 'call_logs' AND column_name IN
		        ('client_request_id', 'idempotency_key_hash', 'workflow_id', 'agent_id', 'attempt_id'))
		    OR (table_name = 'request_trace_events' AND column_name IN
		        ('client_request_id', 'idempotency_key_hash', 'workflow_id', 'agent_id', 'attempt_id'))
		    OR (table_name = 'transactions' AND column_name = 'client_request_id')
		    OR (table_name = 'audit_logs' AND column_name = 'client_request_id'))`,
	).Scan(&identityColumnCount))
	require.Zero(t, identityColumnCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = '000021_prd6_operation_idempotency_identity'`,
	).Scan(&migrationLedgerCount))
	require.Zero(t, migrationLedgerCount)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_grants_org_client_request'`,
	).Scan(&retainedGrantIndexCount))
	require.Equal(t, 1, retainedGrantIndexCount, "safe down must not restore the cross-tenant collision")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT character_maximum_length FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'grants' AND column_name = 'client_request_id'`,
	).Scan(&grantClientIDLength))
	require.Equal(t, 128, grantClientIDLength, "safe down must not truncate accepted client correlation values")

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'request_idempotency'`,
	).Scan(&tableCount))
	require.Equal(t, 1, tableCount)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM grants WHERE client_request_id = 'same-client-value'`).Scan(&tableCount))
	require.Equal(t, 2, tableCount, "roll-forward must preserve legitimate cross-tenant history")
}
