//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelDeploymentExpandMigratesFrom68WithoutGuessingOrganizationAndRollsBack(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")

	var channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ('legacy-safe-backfill','custom','https://provider.invalid','public-a,public-b','staged',7)
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch)
VALUES ($1,'encrypted-fixture','active',3)
RETURNING id`, channelID).Scan(&keyID))

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	var connections, credentials, deployments, capabilities int
	var latestProbeView, deploymentColumn, attemptColumn, ledger bool
	var lifecycleDeleteRestrict, legacyProvenanceHasNoFK bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM provider_connections WHERE legacy_channel_id=$1),
       (SELECT COUNT(*) FROM provider_credential_bindings WHERE legacy_channel_key_id=$2),
       (SELECT COUNT(*) FROM model_deployments),
       (SELECT COUNT(*) FROM model_deployment_capabilities),
       to_regclass('public.model_deployment_latest_probes') IS NOT NULL,
       EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema='public' AND table_name='call_logs'
                  AND column_name='route_deployment_id'),
       EXISTS (SELECT 1 FROM information_schema.columns
                WHERE table_schema='public' AND table_name='billing_provider_attempts'
                  AND column_name='deployment_id'),
	       EXISTS (SELECT 1 FROM schema_migrations
	                WHERE version='000069_model_deployment_expand'),
	       (SELECT COUNT(*)=2 FROM pg_constraint
	         WHERE conname IN ('credential_binding_deployments_binding_fk',
	                           'credential_binding_deployments_deployment_fk')
	           AND confdeltype='r'),
	       NOT EXISTS (
	         SELECT 1 FROM pg_constraint
	          WHERE contype='f'
	            AND conrelid IN ('provider_connections'::regclass,
	                             'provider_credential_bindings'::regclass)
	            AND confrelid IN ('channels'::regclass,'channel_keys'::regclass)
	       )`, channelID, keyID).Scan(
		&connections, &credentials, &deployments, &capabilities,
		&latestProbeView, &deploymentColumn, &attemptColumn, &ledger,
		&lifecycleDeleteRestrict, &legacyProvenanceHasNoFK,
	))
	require.Equal(t, 1, connections)
	require.Equal(t, 1, credentials)
	require.Zero(t, deployments, "SQL migration must not guess a customer organization for model ownership")
	require.Zero(t, capabilities, "SQL migration must not infer model capabilities")
	require.True(t, latestProbeView)
	require.True(t, deploymentColumn)
	require.True(t, attemptColumn)
	require.True(t, ledger)
	require.True(t, lifecycleDeleteRestrict)
	require.True(t, legacyProvenanceHasNoFK)

	_, err := pool.Exec(ctx, `DELETE FROM channels WHERE id=$1`, channelID)
	require.NoError(t, err, "removing a legacy source must not cascade-delete canonical connection state")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT (SELECT COUNT(*) FROM provider_connections WHERE legacy_channel_id=$1),
	       (SELECT COUNT(*) FROM provider_credential_bindings WHERE legacy_channel_key_id=$2)`, channelID, keyID).Scan(
		&connections, &credentials,
	))
	require.Equal(t, 1, connections, "deleted legacy channel ID must remain immutable provenance")
	require.Equal(t, 1, credentials, "deleted legacy key ID must remain immutable provenance")

	_, err = pool.Exec(ctx, `DELETE FROM provider_connections`)
	require.ErrorContains(t, err, "must be retired, not deleted")
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")

	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000069_model_deployment_expand.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "non-reconstructable provider connection exists")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM schema_migrations
                WHERE version='000069_model_deployment_expand')`).Scan(&ledger))
	require.True(t, ledger, "refused rollback must preserve the migration ledger")

	_, err = pool.Exec(ctx, `
INSERT INTO channels (
    id,name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
    status,routing_epoch,created_at,updated_at)
SELECT legacy_channel_id,name,provider,base_url,'public-a,public-b','{}'::jsonb,
       adapter_type,adapter_config,'staged',config_revision,created_at,updated_at
  FROM provider_connections
 WHERE legacy_channel_id=$1`, channelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO channel_keys (
    id,channel_id,key_enc,status,state_epoch,fail_count,last_used_at,
    cooldown_until,created_at,updated_at)
SELECT binding.legacy_channel_key_id,connection.legacy_channel_id,
       binding.key_enc,binding.status,binding.state_revision,binding.fail_count,
       binding.last_used_at,binding.cooldown_until,binding.created_at,binding.updated_at
  FROM provider_credential_bindings binding
  JOIN provider_connections connection ON connection.id=binding.connection_id
 WHERE binding.legacy_channel_key_id=$1`, keyID)
	require.NoError(t, err)

	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.provider_connections') IS NULL,
       NOT EXISTS (SELECT 1 FROM information_schema.columns
                    WHERE table_schema='public' AND table_name='call_logs'
                      AND column_name='route_deployment_id'),
       NOT EXISTS (SELECT 1 FROM schema_migrations
                    WHERE version='000069_model_deployment_expand')`).Scan(
		&latestProbeView, &deploymentColumn, &ledger,
	))
	require.True(t, latestProbeView)
	require.True(t, deploymentColumn)
	require.True(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
}

func TestModelDeploymentExpandEnforcesTenantAttributionAndSurrogateIdentity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")

	var firstOrgID, secondOrgID, publicModelID, connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('tenant-fk-a','active','customer') RETURNING id`).Scan(&firstOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('tenant-fk-b','active','customer') RETURNING id`).Scan(&secondOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'tenant-model','Tenant Model','custom','ga',TRUE) RETURNING id`, firstOrgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('tenant-connection','custom','https://provider.invalid','openai_compatible','{}')
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'tenant-upstream') RETURNING id`, firstOrgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc)
VALUES ($1,'tenant-secret') RETURNING id`, connectionID).Scan(&bindingID))

	_, err := pool.Exec(ctx, `UPDATE provider_connections SET id=id+1000000 WHERE id=$1`, connectionID)
	require.ErrorContains(t, err, "provider connection surrogate identity is immutable")
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET id=id+1000000 WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "model deployment identity tuple is immutable")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings SET id=id+1000000 WHERE id=$1`, bindingID)
	require.ErrorContains(t, err, "provider credential binding identity tuple is immutable")

	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (request_id,org_id,model,route_deployment_id)
VALUES ('tenant-call-valid',$1,'tenant-model',$2)`, firstOrgID, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (request_id,org_id,model,route_deployment_id)
VALUES ('tenant-call-cross',$1,'tenant-model',$2)`, secondOrgID, deploymentID)
	require.ErrorContains(t, err, "call_logs_route_deployment_fk")
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (request_id,org_id,model)
VALUES ('tenant-call-update',$1,'tenant-model')`, secondOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE call_logs SET route_deployment_id=$1 WHERE request_id='tenant-call-update'`, deploymentID)
	require.ErrorContains(t, err, "call_logs_route_deployment_fk")

	for _, fixture := range []struct {
		orgID     int64
		requestID string
	}{
		{firstOrgID, "tenant-attempt-valid"},
		{secondOrgID, "tenant-attempt-cross"},
		{secondOrgID, "tenant-attempt-update"},
	} {
		_, err = pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'settled','tenant-attribution-test')`, fixture.requestID, fixture.orgID)
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty,deployment_id)
VALUES ($1,'tenant-attempt-valid','attempt-valid',1,1,1,1,1,
        '/v1/chat/completions','custom','tenant','settled','known_usage',$2)`, firstOrgID, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty,deployment_id)
VALUES ($1,'tenant-attempt-cross','attempt-cross',1,1,1,1,1,
        '/v1/chat/completions','custom','tenant','settled','known_usage',$2)`, secondOrgID, deploymentID)
	require.ErrorContains(t, err, "billing_provider_attempts_deployment_fk")
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty)
VALUES ($1,'tenant-attempt-update','attempt-update',1,1,1,1,1,
        '/v1/chat/completions','custom','tenant','settled','known_usage')`, secondOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE billing_provider_attempts SET deployment_id=$1
 WHERE org_id=$2 AND request_id='tenant-attempt-update'`, deploymentID, secondOrgID)
	require.ErrorContains(t, err, "billing_provider_attempts_deployment_fk")

	var deploymentUnique, callLogComposite, attemptComposite bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM pg_constraint
          WHERE conname='model_deployments_customer_org_id_id_unique'
            AND contype='u'
       ),
       pg_get_constraintdef((SELECT oid FROM pg_constraint WHERE conname='call_logs_route_deployment_fk'))
         LIKE 'FOREIGN KEY (org_id, route_deployment_id) REFERENCES model_deployments(customer_org_id, id)%',
       pg_get_constraintdef((SELECT oid FROM pg_constraint WHERE conname='billing_provider_attempts_deployment_fk'))
         LIKE 'FOREIGN KEY (org_id, deployment_id) REFERENCES model_deployments(customer_org_id, id)%'`).Scan(
		&deploymentUnique, &callLogComposite, &attemptComposite,
	))
	require.True(t, deploymentUnique)
	require.True(t, callLogComposite)
	require.True(t, attemptComposite)
}

func TestModelDeploymentExpandRollbackRejectsCrossTenantAttribution(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var customerOrgID, otherOrgID, channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('rollback-attribution-customer','active','customer') RETURNING id`).Scan(&customerOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('rollback-attribution-other','active','partner') RETURNING id`).Scan(&otherOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ('rollback-attribution-source','custom','https://provider.invalid',
        'public-attribution','staged',1)
RETURNING id`).Scan(&channelID))
	report, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, customerOrgID)
	require.NoError(t, err)
	require.True(t, report.Ready)

	var deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT id FROM model_deployments
 WHERE customer_org_id=$1 AND legacy_channel_id=$2
   AND legacy_public_model='public-attribution'`, customerOrgID, channelID).Scan(&deploymentID))
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")

	// Simulate corruption from an old binary or direct SQL after temporarily
	// removing the new tenant FK. The down migration must still fail closed
	// instead of silently erasing the only evidence of the mismatch.
	_, err = pool.Exec(ctx, `ALTER TABLE call_logs DROP CONSTRAINT call_logs_route_deployment_fk`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,model,route_channel_id,route_deployment_id)
VALUES ('rollback-cross-tenant-attribution',$1,'public-attribution',$2,$3)`,
		otherOrgID, channelID, deploymentID)
	require.NoError(t, err)

	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000069_model_deployment_expand.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "non-reconstructable deployment attribution exists")

	var ledger bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM schema_migrations
                WHERE version='000069_model_deployment_expand')`).Scan(&ledger))
	require.True(t, ledger, "refused rollback must preserve migration 69")
}

func TestModelDeploymentExpandRollbackRejectsNonLegacyCanonicalEvidence(t *testing.T) {
	for _, evidenceKind := range []string{"connection", "deployment", "binding", "operation", "capability", "probe"} {
		t.Run(evidenceKind, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			var orgID, channelID int64
			require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, "rollback-canonical-"+evidenceKind).Scan(&orgID))
			require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ($1,'custom','https://provider.invalid','public-canonical','staged',1)
RETURNING id`, "rollback-canonical-"+evidenceKind).Scan(&channelID))
			report, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
			require.NoError(t, err)
			require.True(t, report.Ready)
			var deploymentID, connectionID, publicModelID int64
			require.NoError(t, pool.QueryRow(ctx, `
	SELECT id,connection_id,public_model_id FROM model_deployments
	 WHERE customer_org_id=$1 AND legacy_channel_id=$2 AND admin_state <> 'retired'`, orgID, channelID).Scan(
				&deploymentID, &connectionID, &publicModelID,
			))
			execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
			execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")

			switch evidenceKind {
			case "connection":
				_, err = pool.Exec(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('canonical-only','custom','https://canonical.invalid','openai_compatible','{}')`)
			case "deployment":
				_, err = pool.Exec(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'canonical-only-upstream')`, orgID, publicModelID, connectionID)
			case "binding":
				_, err = pool.Exec(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc)
VALUES ($1,'canonical-only-secret')`, connectionID)
			case "operation":
				_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'chat.completions.v1','enabled','declared','canonical:test',1)`, deploymentID)
			case "capability":
				_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
    deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'legacy.any.v1','tools','enabled','provider_documented','canonical:test',1)`, deploymentID)
			case "probe":
				_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,capabilities_verified,request_summary,response_summary,
    source_kind,source_ref,source_fingerprint,tested_at)
SELECT id,connection_id,'legacy.any.v1','canonical.manual','v1',upstream_model,'',
       config_revision,config_revision,NULL,0,TRUE,200,1,'','[]','{}','{}',
       'canonical.manual','canonical:test',
       'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',NOW()
  FROM model_deployments WHERE id=$1`, deploymentID)
			}
			require.NoError(t, err)

			downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000069_model_deployment_expand.down.sql"))
			require.NoError(t, err)
			_, err = pool.Exec(ctx, string(downSQL))
			switch evidenceKind {
			case "connection":
				require.ErrorContains(t, err, "non-reconstructable provider connection exists")
			case "deployment":
				require.ErrorContains(t, err, "non-reconstructable model deployment exists")
			case "binding":
				require.ErrorContains(t, err, "non-reconstructable provider credential binding exists")
			case "probe":
				require.ErrorContains(t, err, "non-reconstructable canonical probe evidence exists")
			default:
				require.ErrorContains(t, err, "canonical operation or capability evidence exists")
			}
			var ledger bool
			require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM schema_migrations
                WHERE version='000069_model_deployment_expand')`).Scan(&ledger))
			require.True(t, ledger)
		})
	}
}

func TestModelDeploymentExpandRollbackAllowsExactLegacyProjection(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID, channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('rollback-exact-legacy','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'rollback-exact-owner','test-hash','org_admin','active','org_admin')
RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,status,routing_epoch,circuit_epoch)
VALUES ('rollback-exact-source','custom','https://provider.invalid','public-exact',
        '{"public-exact":"upstream-exact"}'::jsonb,'staged',3,2)
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch)
VALUES ($1,'encrypted-exact-fixture','active',2) RETURNING id`, channelID).Scan(&keyID))
	_, err := pool.Exec(ctx, `
INSERT INTO channel_model_probes (
    channel_id,public_model,upstream_model,reported_model,routing_epoch,
    credential_id,credential_state_epoch,success,status_code,latency_ms,
    error_summary,tested_by,tested_at)
VALUES ($1,'public-exact','upstream-exact','upstream-exact',3,$2,2,
        TRUE,200,8,'',$3,NOW())`, channelID, keyID, userID)
	require.NoError(t, err)
	report, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoError(t, err)
	require.True(t, report.Ready)

	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")
	var removed bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.model_deployments') IS NULL
   AND NOT EXISTS (SELECT 1 FROM schema_migrations
                    WHERE version='000069_model_deployment_expand')`).Scan(&removed))
	require.True(t, removed)
}
