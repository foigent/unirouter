//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestExecutionEvidenceProjectionMigrationKeepsHistoricalRowsUnknown(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")
	execMigrationDown(t, ctx, pool, "000072_execution_evidence_projection.down.sql")

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('projection-history','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO call_logs (request_id,org_id,model,provider,channel,status_code)
VALUES ('projection-history-call',$1,'duplicate-model','duplicate-provider','duplicate-channel',200)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO request_trace_events (request_id,org_id,event_type,stage,model,provider,channel)
VALUES ('projection-history-trace',$1,'provider.response_accepted','provider',
        'duplicate-model','duplicate-provider','duplicate-channel')`, orgID)
	require.NoError(t, err)

	require.NoError(t, testutil.InstallPreRetirementTestArtifact(t, ctx, pool))
	var callSource, traceSource *string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT route_evidence_source FROM call_logs
 WHERE org_id=$1 AND request_id='projection-history-call'`, orgID).Scan(&callSource))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT route_evidence_source FROM request_trace_events
 WHERE org_id=$1 AND request_id='projection-history-trace'`, orgID).Scan(&traceSource))
	require.Nil(t, callSource, "display names must not manufacture historical call evidence")
	require.Nil(t, traceSource, "display names must not manufacture historical trace evidence")

	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)
	downSQL, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000072_execution_evidence_projection.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000075_future_contract'`)
	require.NoError(t, err)

	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")
	execMigrationDown(t, ctx, pool, "000072_execution_evidence_projection.down.sql")
	require.NoError(t, testutil.InstallPreRetirementTestArtifact(t, ctx, pool),
		"nullable historical rows must permit an ordered rollback and reapply")
}

func TestExecutionEvidenceProjectionRejectsCrossOrgMutationAndUnsafeRollback(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	var orgID, otherOrgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('projection-owner','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('projection-other','active','customer') RETURNING id`).Scan(&otherOrgID))
	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ('projection-exact-attempt',$1,'settled','projection-test')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty)
VALUES ($1,'projection-exact-attempt','attempt-exact',101,201,301,1,1,
        '/v1/chat/completions','duplicate-provider','duplicate-channel','settled','known_usage')`, orgID)
	require.NoError(t, err)

	hash := strings.Repeat("a", 64)
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,route_channel_id,route_evidence_source,source_event_hash)
VALUES ('projection-exact-attempt',$1,'attempt-exact',201,'precontract_provider_attempt',$2)`, otherOrgID, hash)
	require.ErrorContains(t, err, "pre-contract projection does not match exact provider attempt")
	_, err = pool.Exec(ctx, `
INSERT INTO request_trace_events (
    request_id,org_id,event_type,stage,attempt_id,route_channel_id,
    route_evidence_source,source_event_hash)
VALUES ('projection-exact-attempt',$1,'provider.response_accepted','provider',
        'attempt-exact',201,'precontract_provider_attempt',$2)`, otherOrgID, hash)
	require.ErrorContains(t, err, "pre-contract projection does not match exact provider attempt")

	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,route_channel_id,route_evidence_source,source_event_hash)
VALUES ('projection-exact-attempt',$1,'attempt-exact',201,'precontract_provider_attempt',$2)`, orgID, hash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO request_trace_events (
    request_id,org_id,event_type,stage,attempt_id,route_channel_id,
    route_evidence_source,source_event_hash)
VALUES ('projection-exact-attempt',$1,'provider.response_accepted','provider',
        'attempt-exact',201,'precontract_provider_attempt',$2)`, orgID, hash)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE call_logs SET source_event_hash=$3
 WHERE org_id=$1 AND request_id=$2`, orgID, "projection-exact-attempt", strings.Repeat("b", 64))
	require.ErrorContains(t, err, "execution evidence projection is immutable")
	_, err = pool.Exec(ctx, `
DELETE FROM request_trace_events
 WHERE org_id=$1 AND request_id=$2`, orgID, "projection-exact-attempt")
	require.NoError(t, err,
		"query projection retention stays governed by the existing log lifecycle")

	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")
	downSQL, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000072_execution_evidence_projection.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "versioned projection evidence exists")
}
