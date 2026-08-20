//go:build integration

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

func TestModelExecutionEvidenceMigrationPreservesHistoryAndHasOrderedRollback(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('execution-evidence-history','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ('execution-evidence-history',$1,'frozen','migration-test')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name)
VALUES ($1,'execution-evidence-history','attempt-history',101,201,301,
        1,1,'/v1/chat/completions','legacy-provider','legacy-channel')`, orgID)
	require.NoError(t, err, "pre-contract provider attempt history must remain writable")

	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)
	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000071_model_execution_evidence.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000075_future_contract'`)
	require.NoError(t, err)

	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	var historyRetained, evidenceColumnsDropped bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(
         SELECT 1 FROM billing_provider_attempts
          WHERE org_id=$1 AND request_id='execution-evidence-history' AND attempt_id='attempt-history'),
       NOT EXISTS(
         SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='billing_provider_attempts'
            AND column_name='route_evidence_version')`, orgID).Scan(&historyRetained, &evidenceColumnsDropped))
	require.True(t, historyRetained)
	require.True(t, evidenceColumnsDropped)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
}

func TestModelExecutionEvidenceMigrationRejectsPartialBundlesAndFreezesReceipt(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('execution-evidence-constraints','active','customer') RETURNING id`).Scan(&orgID))
	for _, requestID := range []string{"evidence-partial", "evidence-legacy"} {
		_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'frozen','migration-test')`, requestID, orgID)
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_model_operation_id,route_requires_probe)
VALUES ($1,'evidence-partial','attempt-partial',101,201,301,1,1,
        '/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','verify','legacy_adapter',$2,
        'openai.chat_completions.v1',FALSE)`, orgID, "sha256:"+strings.Repeat("a", 64))
	require.ErrorContains(t, err, "billing_provider_attempts_execution_evidence_check",
		"verify mode must not accept a partial legacy/canonical bundle")
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,route_evidence_hash)
VALUES ($1,'evidence-partial','attempt-hidden-history',101,201,301,1,1,
        '/v1/chat/completions','provider','channel',$2)`,
		orgID, "sha256:"+strings.Repeat("a", 64))
	require.ErrorContains(t, err, "billing_provider_attempts_execution_evidence_check",
		"pre-contract history cannot hide lossy 71-only evidence under a NULL version")
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name)
VALUES ($1,'evidence-partial','attempt-null-legacy',101,NULL,NULL,1,1,
        '/v1/chat/completions','provider','channel')`, orgID)
	require.ErrorContains(t, err, "billing_provider_attempts_identity_check",
		"historical rows require real legacy identities")

	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_model_operation_id,
    route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe)
VALUES ($1,'evidence-legacy','attempt-legacy',101,201,301,1,1,
        '/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','legacy','legacy_adapter',$2,
        'openai.chat_completions.v1',2,3,4,FALSE)`, orgID, "sha256:"+strings.Repeat("b", 64))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE billing_provider_attempts SET route_evidence_hash=$4
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, "evidence-legacy", "attempt-legacy", "sha256:"+strings.Repeat("c", 64))
	require.ErrorContains(t, err, "provider attempt execution identity is immutable")

	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")
	execMigrationDown(t, ctx, pool, "000072_execution_evidence_projection.down.sql")
	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000071_model_execution_evidence.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "versioned provider attempt evidence exists")
	var ledgerRetained bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
               WHERE version='000071_model_execution_evidence')`).Scan(&ledgerRetained))
	require.True(t, ledgerRetained)
}
