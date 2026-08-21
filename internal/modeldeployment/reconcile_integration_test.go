//go:build integration

package modeldeployment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestReconcileLegacyProjectionIsIdempotentAndPreservesProbeHistory(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, userID := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-ready")

	var channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,status,routing_epoch,circuit_epoch)
VALUES (
    'ready-connection','custom','https://provider.invalid','public-a,public-b',
    '{"public-a":"upstream-a"}'::jsonb,'staged',7,3)
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'encrypted-fixture','','active',4)
RETURNING id`, channelID).Scan(&keyID))
	probeTime := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
INSERT INTO channel_model_probes (
    channel_id,public_model,upstream_model,reported_model,routing_epoch,
    credential_id,credential_state_epoch,success,status_code,latency_ms,
    error_summary,tested_by,tested_at)
VALUES ($1,'public-a','upstream-a','upstream-a',7,$2,4,TRUE,200,21,'',$3,$4)`,
		channelID, keyID, userID, probeTime)
	require.NoError(t, err)
	releaseID := createCurrentRelease(t, ctx, pool, userID, "public-a", channelID, 7, "allow", "ready")

	first, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, first.Ready)
	require.Equal(t, Counts{
		LegacyConnections: 1, CanonicalConnections: 1,
		LegacyConfiguredModels: 2, CanonicalDeployments: 2,
		LegacyCredentials: 1, CanonicalCredentials: 1,
		ExpectedCredentialDeploymentLinks: 2, CredentialDeploymentLinks: 2,
		CanonicalOperations: 2,
		LegacyProbes:        1, CanonicalProbes: 1,
		CurrentRouteOverrides: 1, CurrentRouteItems: 1,
	}, first.Counts)
	require.Contains(t, first.Limitations, "legacy_capabilities_are_unknown_and_no_capability_support_is_inferred")
	var firstLinkCreatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT link.created_at
  FROM credential_binding_deployments link
  JOIN model_deployments deployment ON deployment.id=link.deployment_id
 WHERE deployment.customer_org_id=$1 AND deployment.legacy_public_model='public-a'`, orgID).Scan(&firstLinkCreatedAt))
	require.False(t, probeTime.Before(firstLinkCreatedAt), "initial canonical scope epoch must not invalidate later legacy probe evidence")

	second, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, second.Ready)
	require.Equal(t, first.Counts, second.Counts)
	var secondLinkCreatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT link.created_at
  FROM credential_binding_deployments link
  JOIN model_deployments deployment ON deployment.id=link.deployment_id
 WHERE deployment.customer_org_id=$1 AND deployment.legacy_public_model='public-a'`, orgID).Scan(&secondLinkCreatedAt))
	require.Equal(t, firstLinkCreatedAt, secondLinkCreatedAt, "idempotent reconciliation must preserve credential scope epoch")

	var deployments, operations, capabilities, probeAttempts, latestProbes, routeItems int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM model_deployments WHERE customer_org_id=$1),
       (SELECT COUNT(*) FROM model_deployment_operations operation
          JOIN model_deployments deployment ON deployment.id=operation.deployment_id
         WHERE deployment.customer_org_id=$1),
       (SELECT COUNT(*) FROM model_deployment_capabilities capability
          JOIN model_deployments deployment ON deployment.id=capability.deployment_id
         WHERE deployment.customer_org_id=$1),
       (SELECT COUNT(*) FROM model_deployment_probes),
       (SELECT COUNT(*) FROM model_deployment_latest_probes),
       (SELECT COUNT(*) FROM runtime_route_revision_items WHERE release_id=$2)`, orgID, releaseID).Scan(
		&deployments, &operations, &capabilities, &probeAttempts, &latestProbes, &routeItems,
	))
	require.Equal(t, 2, deployments)
	require.Equal(t, 2, operations)
	require.Zero(t, capabilities, "legacy names and generic probes must not invent capability support")
	require.Equal(t, 1, probeAttempts)
	require.Equal(t, 1, latestProbes)
	require.Equal(t, 1, routeItems)

	updatedProbeTime := probeTime.Add(time.Second)
	_, err = pool.Exec(ctx, `
UPDATE channel_model_probes
   SET latency_ms=34,tested_at=$1
 WHERE channel_id=$2 AND public_model='public-a'`, updatedProbeTime, channelID)
	require.NoError(t, err)
	third, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, third.Ready)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM model_deployment_probes),
       (SELECT COUNT(*) FROM model_deployment_latest_probes),
       (SELECT latency_ms FROM model_deployment_latest_probes
         WHERE operation_id=$1)`, LegacyOperationID).Scan(&probeAttempts, &latestProbes, &routeItems))
	require.Equal(t, 2, probeAttempts, "a changed legacy latest row must append evidence")
	require.Equal(t, 1, latestProbes)
	require.Equal(t, 34, routeItems)

	_, err = pool.Exec(ctx, `UPDATE model_deployment_probes SET latency_ms=35`)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, `DELETE FROM provider_credential_bindings`)
	require.ErrorContains(t, err, "must be retired, not deleted")
	_, err = pool.Exec(ctx, `DELETE FROM model_deployments`)
	require.ErrorContains(t, err, "must be retired, not deleted")
	_, err = pool.Exec(ctx, `DELETE FROM provider_connections`)
	require.ErrorContains(t, err, "must be retired, not deleted")

	_, err = pool.Exec(ctx, `
UPDATE runtime_route_revision_items
   SET expected_deployment_revision=expected_deployment_revision+1
 WHERE release_id=$1`, releaseID)
	require.NoError(t, err)
	drifted, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorIs(t, err, ErrIncomplete)
	require.True(t, hasIssueCode(drifted.BlockingIssues, "route_revision_item_conflict"), "%+v", drifted.BlockingIssues)
}

func TestReconcileLegacyProjectionAcceptsMonotonicLegacyRevisionGaps(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, _ := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-revision-gap")

	var channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,status,routing_epoch,circuit_state,circuit_epoch)
VALUES ('revision-gap','custom','https://provider.invalid','public-gap','staged',2,'healthy',3)
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch)
VALUES ($1,'revision-gap-secret','active',2)
RETURNING id`, channelID).Scan(&keyID))

	first, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, first.Ready)

	_, err = pool.Exec(ctx, `
UPDATE channels
   SET base_url='https://changed.provider.invalid',status='active',routing_epoch=7,
       circuit_state='open',circuit_epoch=9,updated_at=NOW()
 WHERE id=$1`, channelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE channel_keys
   SET key_enc='revision-gap-secret-rotated',status='disabled',state_epoch=8,updated_at=NOW()
 WHERE id=$1`, keyID)
	require.NoError(t, err)

	second, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err, "legacy projections must catch up to monotonic source epochs without replaying every intermediate value")
	require.True(t, second.Ready)
	var connectionRevision, deploymentRevision, circuitRevision, credentialRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision,deployment.circuit_revision,binding.state_revision
  FROM provider_connections connection
  JOIN model_deployments deployment ON deployment.connection_id=connection.id
  JOIN provider_credential_bindings binding ON binding.connection_id=connection.id
 WHERE connection.legacy_channel_id=$1
   AND deployment.customer_org_id=$2 AND deployment.legacy_public_model='public-gap'
   AND binding.legacy_channel_key_id=$3`, channelID, orgID, keyID).Scan(
		&connectionRevision, &deploymentRevision, &circuitRevision, &credentialRevision,
	))
	require.Equal(t, int64(7), connectionRevision)
	require.Equal(t, int64(7), deploymentRevision)
	require.Equal(t, int64(9), circuitRevision)
	require.Equal(t, int64(8), credentialRevision)

	_, err = pool.Exec(ctx, `
UPDATE provider_connections SET base_url='https://unversioned.invalid'
 WHERE legacy_channel_id=$1`, channelID)
	require.ErrorContains(t, err, "semantic changes require")
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings SET status='active'
 WHERE legacy_channel_key_id=$1`, keyID)
	require.ErrorContains(t, err, "semantic changes require")
}

func TestReconcileLegacyProjectionFailsClosedOnEquivalentIdentity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, _ := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-conflict")
	_, err := pool.Exec(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'DeepSeek-V4-Flash','DeepSeek V4 Flash','deepseek','ga',TRUE)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO channels (name,provider,base_url,models,status)
VALUES ('conflicting-connection','deepseek','https://provider.invalid','DeepSeek V4 Flash','staged')`)
	require.NoError(t, err)

	report, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorIs(t, err, ErrIncomplete)
	require.False(t, report.Ready)
	require.True(t, hasIssueCode(report.BlockingIssues, "equivalent_model_identity_conflict"), "%+v", report.BlockingIssues)
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployments WHERE customer_org_id=$1`, orgID).Scan(&rows))
	require.Zero(t, rows)
}

func TestReconcileLegacyProjectionFailsClosedWhenCurrentRouteCannotMapEvenIfDenied(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, userID := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-missing-route")
	createCurrentRelease(t, ctx, pool, userID, "missing-public-model", 999999, 1, "deny", "missing")

	report, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorIs(t, err, ErrIncomplete)
	require.False(t, report.Ready)
	require.True(t, hasIssueCode(report.BlockingIssues, "current_route_deployment_mapping_missing"), "%+v", report.BlockingIssues)
	require.Equal(t, 1, report.Counts.CurrentRouteOverrides)
	require.Zero(t, report.Counts.CurrentRouteItems)
}

func TestReconcileLegacyProjectionRejectsPartnerOrganization(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('model-deployment-partner','active','partner') RETURNING id`).Scan(&orgID))

	report, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorContains(t, err, "root customer")
	require.False(t, report.Ready)
	var connections int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM provider_connections`).Scan(&connections))
	require.Zero(t, connections, "wrong organization type must fail before writing canonical projection rows")
}

func TestReconcileLegacyProjectionRequiresExactlyOneMatchingActiveRootCustomerBeforeWrites(t *testing.T) {
	testCases := []struct {
		name      string
		setup     func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64
		wantError string
	}{
		{
			name: "zero active roots",
			setup: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
				return 999999
			},
			wantError: "exactly one active root customer organization (found 0)",
		},
		{
			name: "multiple active roots",
			setup: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
				var first int64
				require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('root-a','active','customer') RETURNING id`).Scan(&first))
				_, err := pool.Exec(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('root-b','active','customer')`)
				require.NoError(t, err)
				return first
			},
			wantError: "exactly one active root customer organization (found 2)",
		},
		{
			name: "requested root is inactive",
			setup: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
				var id int64
				require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('inactive-root','disabled','customer') RETURNING id`).Scan(&id))
				return id
			},
			wantError: "exactly one active root customer organization (found 0)",
		},
		{
			name: "requested id mismatches sole root",
			setup: func(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
				_, err := pool.Exec(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('sole-root','active','customer')`)
				require.NoError(t, err)
				var partnerID int64
				require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('requested-partner','active','partner') RETURNING id`).Scan(&partnerID))
				return partnerID
			},
			wantError: "does not match the sole active root customer",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			requestedOrgID := testCase.setup(t, ctx, pool)
			_, err := pool.Exec(ctx, `
INSERT INTO channels (name,provider,base_url,models,status)
VALUES ($1,'custom','https://provider.invalid','must-not-project','staged')`, "org-authority-"+testCase.name)
			require.NoError(t, err)

			report, err := ReconcileLegacyProjection(ctx, pool, requestedOrgID)
			require.ErrorContains(t, err, testCase.wantError)
			require.False(t, report.Ready)
			require.Equal(t, requestedOrgID, report.CustomerOrgID)

			var writes int
			require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM provider_connections)
     + (SELECT COUNT(*) FROM model_deployments)
     + (SELECT COUNT(*) FROM provider_credential_bindings)
     + (SELECT COUNT(*) FROM model_registry)
     + (SELECT COUNT(*) FROM runtime_route_revision_items)
     + (SELECT COUNT(*) FROM model_deployment_probes)`).Scan(&writes))
			require.Zero(t, writes, "organization authority failure must happen before every projection write")
		})
	}
}

func TestReconcileLegacyProjectionFailsClosedOnUnrepresentableLegacyMapping(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, _ := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-invalid-mapping")
	_, err := pool.Exec(ctx, `
INSERT INTO channels (name,provider,base_url,models,model_mapping,status)
VALUES ('invalid-mapping','custom','https://provider.invalid','public-a','[]'::jsonb,'staged')`)
	require.NoError(t, err)

	report, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorIs(t, err, ErrIncomplete)
	require.True(t, hasIssueCode(report.BlockingIssues, "model_mapping_invalid"), "%+v", report.BlockingIssues)
	var deployments int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployments`).Scan(&deployments))
	require.Zero(t, deployments, "an invalid legacy mapping must not guess public-to-upstream identity")
}

func TestReconcileLegacyProjectionRetiresDeletedSourcesWithoutLosingProvenanceOrResurrectingRows(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, _ := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-retirement")

	var channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ('retirement-source','custom','https://provider.invalid','public-retire','staged',3)
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch)
VALUES ($1,'encrypted-retirement-fixture','active',2)
RETURNING id`, channelID).Scan(&keyID))

	first, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, first.Ready)
	var connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT id FROM provider_connections WHERE legacy_channel_id=$1),
       (SELECT id FROM model_deployments WHERE customer_org_id=$2 AND legacy_channel_id=$1 AND admin_state <> 'retired'),
       (SELECT id FROM provider_credential_bindings WHERE legacy_channel_key_id=$3)`, channelID, orgID, keyID).Scan(
		&connectionID, &deploymentID, &bindingID,
	))

	_, err = pool.Exec(ctx, `UPDATE model_deployments SET upstream_model='rewritten' WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "identity tuple is immutable")

	_, err = pool.Exec(ctx, `DELETE FROM channel_keys WHERE id=$1`, keyID)
	require.NoError(t, err)
	afterKeyDelete, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, afterKeyDelete.Ready)
	require.Equal(t, 1, afterKeyDelete.Counts.CredentialsRetired)
	var bindingStatus, bindingSecret string
	var bindingRetiredAt *time.Time
	var retainedKeyID int64
	var links int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status,key_enc,retired_at,legacy_channel_key_id,
       (SELECT COUNT(*) FROM credential_binding_deployments WHERE binding_id=$1)
  FROM provider_credential_bindings WHERE id=$1`, bindingID).Scan(
		&bindingStatus, &bindingSecret, &bindingRetiredAt, &retainedKeyID, &links,
	))
	require.Equal(t, "retired", bindingStatus)
	require.Empty(t, bindingSecret)
	require.NotNil(t, bindingRetiredAt)
	require.Equal(t, keyID, retainedKeyID)
	require.Zero(t, links)
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='active',key_enc='forbidden',retired_at=NULL
 WHERE id=$1`, bindingID)
	require.ErrorContains(t, err, "retired provider credential binding is immutable")

	_, err = pool.Exec(ctx, `DELETE FROM channels WHERE id=$1`, channelID)
	require.NoError(t, err)
	afterChannelDelete, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, afterChannelDelete.Ready)
	require.Equal(t, 1, afterChannelDelete.Counts.ConnectionsRetired)
	require.Equal(t, 1, afterChannelDelete.Counts.DeploymentsRetired)
	var connectionState, deploymentState string
	var retainedChannelID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT admin_state FROM provider_connections WHERE id=$1),
       (SELECT legacy_channel_id FROM provider_connections WHERE id=$1),
       (SELECT admin_state FROM model_deployments WHERE id=$2)`, connectionID, deploymentID).Scan(
		&connectionState, &retainedChannelID, &deploymentState,
	))
	require.Equal(t, "retired", connectionState)
	require.Equal(t, channelID, retainedChannelID)
	require.Equal(t, "retired", deploymentState)
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET admin_state='enabled',retired_at=NULL WHERE id=$1`, connectionID)
	require.ErrorContains(t, err, "retired provider connection is immutable")
	_, err = pool.Exec(ctx, `DELETE FROM model_deployments WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "must be retired, not deleted")

	secondRetirementPass, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, secondRetirementPass.Ready)
	require.Zero(t, secondRetirementPass.Counts.ConnectionsRetired)
	require.Zero(t, secondRetirementPass.Counts.DeploymentsRetired)
	require.Zero(t, secondRetirementPass.Counts.CredentialsRetired)

	_, err = pool.Exec(ctx, `
INSERT INTO channels (id,name,provider,base_url,models,status,routing_epoch)
VALUES ($1,'forbidden-resurrection','custom','https://provider.invalid','public-retire','staged',4)`, channelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO channel_keys (id,channel_id,key_enc,status,state_epoch)
VALUES ($1,$2,'forbidden-resurrection-secret','active',3)`, keyID, channelID)
	require.NoError(t, err)
	blocked, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.ErrorIs(t, err, ErrIncomplete)
	require.True(t, hasIssueCode(blocked.BlockingIssues, "provider_connection_retired"), "%+v", blocked.BlockingIssues)
	require.NoError(t, pool.QueryRow(ctx, `SELECT admin_state FROM provider_connections WHERE id=$1`, connectionID).Scan(&connectionState))
	require.Equal(t, "retired", connectionState)
	_, err = pool.Exec(ctx, `DELETE FROM channels WHERE id=$1`, channelID)
	require.NoError(t, err)

	var replacementChannelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ('replacement-source','custom','https://provider.invalid','public-retire','staged',5)
RETURNING id`).Scan(&replacementChannelID))
	require.NotEqual(t, channelID, replacementChannelID)
	replacement, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, replacement.Ready)
	var replacementConnectionID, replacementDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT id FROM provider_connections WHERE legacy_channel_id=$1 AND admin_state <> 'retired'),
       (SELECT id FROM model_deployments WHERE customer_org_id=$2 AND legacy_channel_id=$1 AND admin_state <> 'retired')`, replacementChannelID, orgID).Scan(
		&replacementConnectionID, &replacementDeploymentID,
	))
	require.NotEqual(t, connectionID, replacementConnectionID)
	require.NotEqual(t, deploymentID, replacementDeploymentID)
}

func TestReconcileLegacyProjectionCreatesNewDeploymentIdentityWhenUpstreamMappingChanges(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, _ := createReconcileOrgAndUser(t, ctx, pool, "model-deployment-remap")
	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,model_mapping,status,routing_epoch)
VALUES ('remap-source','custom','https://provider.invalid','public-remap',
        '{"public-remap":"upstream-a"}'::jsonb,'staged',1)
RETURNING id`).Scan(&channelID))

	first, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, first.Ready)
	var firstDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT id FROM model_deployments
 WHERE customer_org_id=$1 AND legacy_channel_id=$2 AND admin_state <> 'retired'`, orgID, channelID).Scan(&firstDeploymentID))

	_, err = pool.Exec(ctx, `
UPDATE channels
   SET model_mapping='{"public-remap":"upstream-b"}'::jsonb,
       routing_epoch=routing_epoch+1,updated_at=NOW()
 WHERE id=$1`, channelID)
	require.NoError(t, err)
	second, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, second.Ready)
	require.Equal(t, 1, second.Counts.DeploymentsRetired)
	var secondDeploymentID int64
	var oldState, oldUpstream, newState, newUpstream string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT admin_state FROM model_deployments WHERE id=$1),
       (SELECT upstream_model FROM model_deployments WHERE id=$1),
       (SELECT id FROM model_deployments
         WHERE customer_org_id=$2 AND legacy_channel_id=$3 AND admin_state <> 'retired'),
       (SELECT admin_state FROM model_deployments
         WHERE customer_org_id=$2 AND legacy_channel_id=$3 AND admin_state <> 'retired'),
       (SELECT upstream_model FROM model_deployments
         WHERE customer_org_id=$2 AND legacy_channel_id=$3 AND admin_state <> 'retired')`, firstDeploymentID, orgID, channelID).Scan(
		&oldState, &oldUpstream, &secondDeploymentID, &newState, &newUpstream,
	))
	require.Equal(t, "retired", oldState)
	require.Equal(t, "upstream-a", oldUpstream)
	require.NotEqual(t, firstDeploymentID, secondDeploymentID)
	require.Equal(t, "enabled", newState)
	require.Equal(t, "upstream-b", newUpstream)

	third, err := ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, third.Ready)
	require.Zero(t, third.Counts.DeploymentsRetired)
	var deploymentRows int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployments
 WHERE customer_org_id=$1 AND legacy_channel_id=$2`, orgID, channelID).Scan(&deploymentRows))
	require.Equal(t, 2, deploymentRows)
}

func createReconcileOrgAndUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (int64, int64) {
	t.Helper()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, suffix).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'test-hash','org_admin','active','org_admin') RETURNING id`, orgID, suffix+"-owner").Scan(&userID))
	return orgID, userID
}

func createCurrentRelease(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, model string, channelID, epoch int64, admission, suffix string) int64 {
	t.Helper()
	var releaseID int64
	releaseRef := fmt.Sprintf("rcr_%s_fixture", suffix)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
    candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES (
    $1::text,$2::text,'unirouter.runtime-change-candidate.v1',
    'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    jsonb_build_object(
      'contract_version','unirouter.runtime-change-candidate.v1',
      'model_name',$2::text,
      'overrides',jsonb_build_array(jsonb_build_object(
        'channel_id',$3::bigint,'expected_routing_epoch',$4::bigint,'admission',$5::text))),
    '[]','{}','unchanged','active',1,
    'canonical reconciliation fixture',$6,$6,$6,NOW(),NOW())
RETURNING id`, releaseRef, model, channelID, epoch, admission, userID).Scan(&releaseID))
	_, err := pool.Exec(ctx, `
INSERT INTO runtime_change_slots (model_name,stable_release_id,canary_basis_points,slot_version)
VALUES ($1,$2,0,1)`, model, releaseID)
	require.NoError(t, err)
	return releaseID
}

func hasIssueCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
