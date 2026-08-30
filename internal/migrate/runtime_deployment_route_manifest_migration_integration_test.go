//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeDeploymentRouteManifestRollbackAllowsSealedV1CompatibilityEvidence(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000071_model_execution_evidence.up.sql")
	ctx := context.Background()
	var orgID, userID, publicModelID, channelID, connectionID, deploymentID, secondDeploymentID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('manifest-v1-down','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'manifest-v1-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-v1-model','Manifest V1 Model','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch)
VALUES ('manifest-v1-channel','custom','https://legacy.invalid','manifest-v1-model','active',1) RETURNING id`,
	).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'manifest-v1-connection','custom','https://legacy.invalid','openai_compatible','{}','enabled',1)
RETURNING id`, channelID).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,'manifest-v1-upstream','enabled',1,$4,'manifest-v1-model') RETURNING id`,
		orgID, publicModelID, connectionID, channelID).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','legacy:test',1,
	        'legacy.channel_model_probe','v1')`, deploymentID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision)
VALUES ($1,$2,$3,'manifest-v1-second-upstream','enabled',1) RETURNING id`,
		orgID, publicModelID, connectionID).Scan(&secondDeploymentID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','legacy:test:second',1,
        'legacy.channel_model_probe','v1')`, secondDeploymentID)
	require.NoError(t, err)

	candidate := runtimechange.Candidate{
		ContractVersion: runtimechange.CandidateVersion,
		ModelName:       "manifest-v1-model",
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID: channelID, ExpectedRoutingEpoch: 1, Admission: routingpolicy.AdmissionAllow,
		}},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	candidateHash := fmt.Sprintf("sha256:%x", sha256.Sum256(candidateJSON))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ('rcr_manifest_v1_down','manifest-v1-model',$1,$2,$3,$4::jsonb,
        '[]','{}','unchanged','active',1,0,'migration rollback fixture',$5,$5,$5,NOW(),NOW())
RETURNING id`, runtimechange.CandidateVersion, candidateHash,
		"sha256:"+strings.Repeat("b", 64), string(candidateJSON), userID).Scan(&releaseID))
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,
    protocol_profile_id,protocol_profile_version,admission,source_candidate_version)
VALUES ($1,1,$2,'legacy.any.v1',1,1,1,'legacy.channel_model_probe','v1','allow',$3)`,
		releaseID, deploymentID, runtimechange.CandidateVersion)
	require.NoError(t, err)

	concurrentItemTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = concurrentItemTx.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,
    protocol_profile_id,protocol_profile_version,admission,source_candidate_version)
VALUES ($1,2,$2,'legacy.any.v1',1,1,1,'legacy.channel_model_probe','v1','allow',$3)`,
		releaseID, secondDeploymentID, runtimechange.CandidateVersion)
	require.NoError(t, err)
	sealResult := make(chan error, 1)
	go func() {
		_, sealErr := pool.Exec(ctx, `
UPDATE runtime_change_releases
   SET public_model_id=$2,route_items_hash=$3,route_items_count=1,route_items_sealed_at=NOW()
 WHERE id=$1`, releaseID, publicModelID, "sha256:"+strings.Repeat("c", 64))
		sealResult <- sealErr
	}()
	select {
	case sealErr := <-sealResult:
		t.Fatalf("direct SQL seal did not serialize with the uncommitted route item: %v", sealErr)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, concurrentItemTx.Commit(ctx))
	select {
	case sealErr := <-sealResult:
		require.ErrorContains(t, sealErr, "runtime deployment manifest item count mismatch")
	case <-time.After(5 * time.Second):
		t.Fatal("direct SQL seal remained blocked after the route item committed")
	}
	_, err = pool.Exec(ctx, `DELETE FROM runtime_route_revision_items WHERE release_id=$1 AND ordinal=2`, releaseID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE runtime_change_releases
   SET public_model_id=$2,route_items_hash=$3,route_items_count=1,route_items_sealed_at=NOW()
 WHERE id=$1`, releaseID, publicModelID, "sha256:"+strings.Repeat("c", 64))
	require.NoError(t, err)
	var unsealedReleaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by)
VALUES ('rcr_manifest_v1_unsealed','manifest-v1-model',$1,$2,$3,$4::jsonb,
        '[]','{}','unchanged','draft',1,0,'sealed row move target',$5)
RETURNING id`, runtimechange.CandidateVersion, candidateHash,
		"sha256:"+strings.Repeat("b", 64), string(candidateJSON), userID).Scan(&unsealedReleaseID))
	_, err = pool.Exec(ctx, `
UPDATE runtime_route_revision_items SET release_id=$2
 WHERE release_id=$1 AND ordinal=1`, releaseID, unsealedReleaseID)
	require.ErrorContains(t, err, "sealed runtime route",
		"a sealed row must not be movable into an unsealed release")
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_slots (model_name,stable_release_id,canary_basis_points,slot_version)
VALUES ('manifest-v1-model',$1,0,1)`, releaseID)
	require.NoError(t, err)

	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	var releaseRetained, metadataDropped, ledgerDropped bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM runtime_change_releases WHERE id=$1 AND candidate_version=$2),
       NOT EXISTS(SELECT 1 FROM information_schema.columns
                   WHERE table_schema='public' AND table_name='runtime_change_releases'
                     AND column_name='route_items_hash'),
       NOT EXISTS(SELECT 1 FROM schema_migrations
                   WHERE version='000070_runtime_deployment_route_manifest')`, releaseID,
		runtimechange.CandidateVersion).Scan(&releaseRetained, &metadataDropped, &ledgerDropped))
	require.True(t, releaseRetained)
	require.True(t, metadataDropped)
	require.True(t, ledgerDropped)
	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000070_runtime_deployment_route_manifest"))
}

func TestRuntimeDeploymentRouteManifestSealRejectsStaleConnectionAndDeploymentPins(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000070_runtime_deployment_route_manifest.up.sql")
	ctx := context.Background()
	var orgID, userID, publicModelID, connectionID, deploymentID, bindingID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('manifest-stale-pins','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'manifest-stale-pins-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-stale-pins-model','Manifest Stale Pins','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('manifest-stale-pins-connection','custom','https://stale-pins.invalid','openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision)
VALUES ($1,$2,$3,'manifest-stale-upstream','enabled',1) RETURNING id`,
		orgID, publicModelID, connectionID).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','live_probed','probe:manifest-stale',NOW(),1,
        'openai.chat_completions.v1','1.0.0')`, deploymentID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture-secret','active',1) RETURNING id`, connectionID).Scan(&bindingID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, bindingID, deploymentID, connectionID)
	require.NoError(t, err)

	candidate := runtimechange.DeploymentCandidate{
		ContractVersion: runtimechange.DeploymentCandidateVersion,
		PublicModelID:   publicModelID,
		ModelName:       "manifest-stale-pins-model",
		DeploymentRoutes: []runtimechange.DeploymentRoute{{
			DeploymentID: deploymentID, ModelOperationID: "openai.chat_completions.v1",
			ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1,
			ExpectedOperationRevision: 1, ProtocolProfileID: "openai.chat_completions.v1",
			ProtocolProfileVersion: "1.0.0", Admission: routingpolicy.AdmissionAllow,
		}},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,public_model_id,route_items_hash,route_items_count)
VALUES ('rcr_manifest_stale_pins','manifest-stale-pins-model',$1,$2,$3,$4::jsonb,
        '[]','{}','unchanged','draft',1,0,'stale revision seal fixture',$5,$6,$7,1)
RETURNING id`, runtimechange.DeploymentCandidateVersion,
		fmt.Sprintf("sha256:%x", sha256.Sum256(candidateJSON)),
		"sha256:"+strings.Repeat("b", 64), string(candidateJSON), userID, publicModelID,
		"sha256:"+strings.Repeat("c", 64)).Scan(&releaseID))
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,
    protocol_profile_id,protocol_profile_version,admission,source_candidate_version)
VALUES ($1,1,$2,'openai.chat_completions.v1',1,1,1,
        'openai.chat_completions.v1','1.0.0','allow',$3)`,
		releaseID, deploymentID, runtimechange.DeploymentCandidateVersion)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE provider_connections
   SET base_url='https://stale-pins-v2.invalid',config_revision=2
 WHERE id=$1`, connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE model_deployments SET priority=101,config_revision=2 WHERE id=$1`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,source_kind,
    source_ref,source_fingerprint,tested_by)
VALUES ($1,$2,'openai.chat_completions.v1','openai.chat_completions.v1','1.0.0',
        'manifest-stale-upstream','manifest-stale-upstream',2,2,$3,1,TRUE,200,
        'integration','manifest-stale-current-probe',$4,$5)`,
		deploymentID, connectionID, bindingID, "sha256:"+strings.Repeat("d", 64), userID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE runtime_change_releases SET route_items_sealed_at=NOW() WHERE id=$1`, releaseID)
	require.ErrorContains(t, err,
		"runtime route item connection/deployment/operation/profile revision mismatch")
}

func TestRuntimeDeploymentRouteManifestRollbackFailsClosedOnCanonicalEvidence(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000071_model_execution_evidence.up.sql")
	ctx := context.Background()
	var orgID, userID, publicModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('manifest-v2-down','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'manifest-v2-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-v2-model','Manifest V2 Model','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	candidate := runtimechange.DeploymentCandidate{
		ContractVersion: runtimechange.DeploymentCandidateVersion,
		PublicModelID:   publicModelID,
		ModelName:       "manifest-v2-model",
		DeploymentRoutes: []runtimechange.DeploymentRoute{{
			DeploymentID: 1, ModelOperationID: "openai.chat_completions.v1",
			ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1,
			ExpectedOperationRevision: 1, ProtocolProfileID: "explicit.profile.v1",
			ProtocolProfileVersion: "1.0.0", Admission: routingpolicy.AdmissionAllow,
		}},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,public_model_id,route_items_hash,route_items_count)
VALUES ('rcr_manifest_v2_down','manifest-v2-model',$1,$2,$3,$4::jsonb,
        '[]','{}','review_required','draft',1,0,'canonical rollback guard',$5,$6,$7,1)`,
		runtimechange.DeploymentCandidateVersion,
		fmt.Sprintf("sha256:%x", sha256.Sum256(candidateJSON)),
		"sha256:"+strings.Repeat("b", 64), string(candidateJSON), userID, publicModelID,
		"sha256:"+strings.Repeat("c", 64))
	require.NoError(t, err)

	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000070_runtime_deployment_route_manifest.down.sql"))
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "canonical release, current slot, or canonical-only route evidence exists")
	var ledger bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
              WHERE version='000070_runtime_deployment_route_manifest')`).Scan(&ledger))
	require.True(t, ledger)
}

func TestRuntimeDeploymentRouteManifestMigrationNeverInfersNonLegacyProfile(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000071_model_execution_evidence.up.sql")
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	var orgID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('manifest-no-inference','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-explicit-profile','Manifest Explicit Profile','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('manifest-explicit-connection','custom','https://explicit.invalid','openai_compatible','{}') RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'explicit-upstream') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','pre-profile-writer',1)`, deploymentID)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000070_runtime_deployment_route_manifest")
	require.ErrorContains(t, err, "non-legacy operations require an explicit protocol profile")
	var ledger, profileColumn bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
              WHERE version='000070_runtime_deployment_route_manifest'),
       EXISTS(SELECT 1 FROM information_schema.columns
               WHERE table_schema='public' AND table_name='model_deployment_operations'
                 AND column_name='protocol_profile_id')`).Scan(&ledger, &profileColumn))
	require.False(t, ledger)
	require.False(t, profileColumn, "failed migration transaction must not leave partial profile columns")
	_, err = pool.Exec(ctx, `DELETE FROM model_deployment_operations WHERE deployment_id=$1`, deploymentID)
	require.NoError(t, err)
	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000070_runtime_deployment_route_manifest"))
}

func TestRuntimeDeploymentRouteManifestRequiresExplicitNonLegacyProfile(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000070_runtime_deployment_route_manifest.up.sql")
	ctx := context.Background()
	var orgID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('manifest-explicit-runtime-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-runtime-profile','Manifest Runtime Profile','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('manifest-runtime-profile-connection','custom','https://runtime-profile.invalid','openai_compatible','{}')
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'runtime-profile-upstream') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))

	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','missing-profile',1)`, deploymentID)
	require.Error(t, err, "a non-legacy operation must never inherit a protocol profile")

	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','legacy-probe-is-not-execution',1,
        'legacy.channel_model_probe','v1')`, deploymentID)
	require.ErrorContains(t, err, "model_deployment_operations_protocol_profile_check")

	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','explicit-legacy-compatibility',1,
        'legacy.channel_model_probe','v1')`, deploymentID)
	require.NoError(t, err, "the compatibility operation must declare its probe profile explicitly")
}

func TestRuntimeDeploymentAdminSafetyContractionPreservesRevisionAndResumeInvalidatesEvidence(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000070_runtime_deployment_route_manifest.up.sql")
	ctx := context.Background()
	var orgID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('manifest-admin-safety','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-admin-safety-model','Manifest Admin Safety','custom','ga',TRUE)
RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('manifest-admin-safety-connection','custom','https://admin-safety.invalid','openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision)
VALUES ($1,$2,$3,'manifest-admin-safety-upstream','enabled',1)
RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))

	_, err := pool.Exec(ctx, `UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, connectionID)
	require.NoError(t, err, "connection emergency pause is an independent administrative gate")
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET admin_state='enabled' WHERE id=$1`, connectionID)
	require.ErrorContains(t, err, "next config revision", "connection resume cannot reuse sealed configuration evidence")
	_, err = pool.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='enabled',config_revision=config_revision+1
 WHERE id=$1`, connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET admin_state='disabled' WHERE id=$1`, deploymentID)
	require.NoError(t, err, "deployment pause is an independent administrative gate")
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET admin_state='enabled' WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "next config revision", "deployment resume cannot reuse sealed configuration evidence")
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET admin_state='enabled',config_revision=config_revision+1
 WHERE id=$1`, deploymentID)
	require.NoError(t, err)

	var connectionRevision, deploymentRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision
  FROM provider_connections connection
  JOIN model_deployments deployment ON deployment.connection_id=connection.id
 WHERE connection.id=$1 AND deployment.id=$2`, connectionID, deploymentID).Scan(
		&connectionRevision, &deploymentRevision,
	))
	require.EqualValues(t, 2, connectionRevision)
	require.EqualValues(t, 2, deploymentRevision)

	_, err = pool.Exec(ctx, `
UPDATE model_deployments SET admin_state='retired',retired_at=NOW() WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "next config revision", "terminal retirement remains a versioned semantic change")
	_, err = pool.Exec(ctx, `
UPDATE provider_connections SET admin_state='retired',retired_at=NOW() WHERE id=$1`, connectionID)
	require.ErrorContains(t, err, "next config revision", "terminal retirement remains a versioned semantic change")
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1
 WHERE id=$1`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1
 WHERE id=$1`, connectionID)
	require.NoError(t, err)
}

func TestRuntimeDeploymentRouteManifestRollbackRefusesExplicitProfileLoss(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000071_model_execution_evidence.up.sql")
	ctx := context.Background()
	var orgID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('manifest-profile-down','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-profile-down-model','Manifest Profile Down','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('manifest-profile-down-connection','custom','https://profile.invalid','openai_compatible','{}') RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'profile-upstream') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','explicit-profile',1,
        'explicit.profile.v1','1.0.0')`, deploymentID)
	require.NoError(t, err)
	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000070_runtime_deployment_route_manifest.down.sql"))
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "explicit non-legacy protocol contracts would be lost")
}

func TestRuntimeDeploymentProbeLeaseUsesStrictRevisionNeutralCAS(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000070_runtime_deployment_route_manifest.up.sql")
	ctx := context.Background()
	var orgID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('manifest-probe-lease','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-probe-lease-model','Manifest Probe Lease','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('manifest-probe-lease-connection','custom','https://probe-lease.invalid','openai_compatible','{}')
RETURNING id`).Scan(&connectionID))
	now := time.Now().UTC()
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,
    circuit_state,half_open_until,half_open_probe_token,half_open_probe_until)
VALUES ($1,$2,$3,'probe-lease-valid','half_open',$4,'lease-a',$5)
RETURNING id`, orgID, publicModelID, connectionID, now.Add(-time.Minute), now.Add(time.Minute)).Scan(&deploymentID))

	_, err := pool.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_until=$2
 WHERE id=$1 AND half_open_probe_token='lease-a'`, deploymentID, now.Add(2*time.Minute))
	require.NoError(t, err, "same-token renewal should not bump circuit revision")
	var circuitRevision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT circuit_revision FROM model_deployments WHERE id=$1`, deploymentID).Scan(&circuitRevision))
	require.EqualValues(t, 1, circuitRevision)

	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_token='lease-b',half_open_probe_until=$2
 WHERE id=$1`, deploymentID, now.Add(2*time.Minute))
	require.ErrorContains(t, err, "live probe lease token cannot be replaced")
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET circuit_state='healthy',half_open_until=NULL,circuit_revision=circuit_revision+1
 WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "breaker transitions must release probe ownership")
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET admin_state='disabled',config_revision=config_revision+1
 WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "disabled deployment cannot retain a probe lease")

	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_token=NULL,half_open_probe_until=NULL
 WHERE id=$1 AND half_open_probe_token='lease-a'`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET half_open_probe_token='lease-b',half_open_probe_until=$2
 WHERE id=$1 AND half_open_probe_token IS NULL`, deploymentID, now.Add(2*time.Minute))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET circuit_state='open',half_open_until=NULL,circuit_revision=circuit_revision+1,
       half_open_probe_token=NULL,half_open_probe_until=NULL
 WHERE id=$1`, deploymentID)
	require.NoError(t, err, "breaker transition may atomically clear ownership")

	insertWithLease := func(upstream, state string, halfOpenUntil any, token any, leaseUntil any) error {
		_, insertErr := pool.Exec(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,
    circuit_state,half_open_until,half_open_probe_token,half_open_probe_until)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			orgID, publicModelID, connectionID, upstream, state, halfOpenUntil, token, leaseUntil)
		return insertErr
	}
	require.Error(t, insertWithLease("probe-lease-healthy", "healthy", nil, "lease", now.Add(time.Minute)))
	require.Error(t, insertWithLease("probe-lease-future-breaker", "half_open", now.Add(time.Minute), "lease", now.Add(2*time.Minute)))
	require.Error(t, insertWithLease("probe-lease-blank", "half_open", now.Add(-time.Minute), "   ", now.Add(time.Minute)))
	require.Error(t, insertWithLease("probe-lease-control", "half_open", now.Add(-time.Minute), "lease\ninvalid", now.Add(time.Minute)))
	require.Error(t, insertWithLease("probe-lease-expired", "half_open", now.Add(-time.Minute), "lease", now.Add(-time.Second)))
	require.Error(t, insertWithLease("probe-lease-too-long", "half_open", now.Add(-time.Minute), "lease", now.Add(10*time.Minute)))
	require.Error(t, insertWithLease("probe-lease-one-sided", "half_open", now.Add(-time.Minute), "lease", nil))
	require.NoError(t, insertWithLease("probe-lease-direct-valid", "half_open", now.Add(-time.Minute), "lease-direct", now.Add(time.Minute)))
}

func TestRuntimeDeploymentRevisionGuardsAllowNativeCASAndExactLegacyCatchup(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000070_runtime_deployment_route_manifest.up.sql")
	ctx := context.Background()
	var orgID, publicModelID, channelID, channelKeyID, connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('manifest-revision-guards','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'manifest-revision-model','Manifest Revision Model','custom','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,routing_epoch,circuit_epoch,circuit_state)
VALUES ('manifest-revision-channel','custom','https://source.invalid','manifest-revision-model','active',5,6,'open')
RETURNING id`).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch,cooldown_until)
VALUES ($1,'encrypted-fixture','active',7,NOW()+INTERVAL '7 minutes') RETURNING id`, channelID).Scan(&channelKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,config_revision)
VALUES ($1,'manifest-revision-connection','custom','https://initial.invalid','openai_compatible','{}',1)
RETURNING id`, channelID).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,
    legacy_channel_id,legacy_public_model,config_revision,circuit_revision)
VALUES ($1,$2,$3,'revision-upstream',$4,'manifest-revision-model',1,1)
RETURNING id`, orgID, publicModelID, connectionID, channelID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,state_revision,legacy_channel_key_id)
VALUES ($1,'encrypted-fixture','active',1,$2) RETURNING id`, connectionID, channelKeyID).Scan(&bindingID))

	_, err := pool.Exec(ctx, `UPDATE provider_connections
SET base_url='https://native.invalid',config_revision=2 WHERE id=$1`, connectionID)
	require.NoError(t, err, "legacy provenance must not block an active canonical +1 CAS")
	_, err = pool.Exec(ctx, `UPDATE provider_connections
SET base_url='https://wrong-catchup.invalid',config_revision=4 WHERE id=$1`, connectionID)
	require.ErrorContains(t, err, "jump must match the legacy source epoch")
	_, err = pool.Exec(ctx, `UPDATE provider_connections
SET base_url='https://source.invalid',config_revision=5 WHERE id=$1`, connectionID)
	require.NoError(t, err, "reconcile may catch up exactly to the source routing epoch")

	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET priority=101,config_revision=2 WHERE id=$1`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET priority=102,config_revision=4 WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "jump must match the legacy source epoch")
	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET priority=100,config_revision=5 WHERE id=$1`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET circuit_state='half_open',half_open_until=NOW()-INTERVAL '1 second',circuit_revision=2
WHERE id=$1`, deploymentID)
	require.NoError(t, err, "native breaker CAS must advance one revision")
	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET circuit_state='healthy',half_open_until=NULL,circuit_revision=5 WHERE id=$1`, deploymentID)
	require.ErrorContains(t, err, "jump must match the legacy source epoch")
	_, err = pool.Exec(ctx, `UPDATE model_deployments
SET circuit_state='open',half_open_until=NULL,circuit_revision=6 WHERE id=$1`, deploymentID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings
SET cooldown_until=NOW()+INTERVAL '2 minutes',state_revision=2 WHERE id=$1`, bindingID)
	require.NoError(t, err, "native credential cooldown CAS must advance one revision")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings
SET cooldown_until=NOW()+INTERVAL '6 minutes',state_revision=6 WHERE id=$1`, bindingID)
	require.ErrorContains(t, err, "jump must match the legacy source epoch")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings binding
SET cooldown_until=legacy_key.cooldown_until,state_revision=legacy_key.state_epoch
FROM channel_keys legacy_key
WHERE binding.id=$1 AND legacy_key.id=binding.legacy_channel_key_id`, bindingID)
	require.NoError(t, err, "reconcile may catch up exactly to the source credential epoch")

	var nativeConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('manifest-native-revision','custom','https://native-one.invalid','openai_compatible','{}')
RETURNING id`).Scan(&nativeConnectionID))
	_, err = pool.Exec(ctx, `UPDATE provider_connections
SET base_url='https://native-skip.invalid',config_revision=3 WHERE id=$1`, nativeConnectionID)
	require.ErrorContains(t, err, "must not decrease or skip")
}
