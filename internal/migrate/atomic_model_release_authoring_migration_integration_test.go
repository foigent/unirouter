//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/migrate/testdata/m77artifact"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type m78MigrationSnapshotBuilder struct{}

func (m78MigrationSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

func TestAtomicModelReleaseAuthoringRejectsM77DraftBeforeSchemaMutationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000078_atomic_model_release_authoring.down.sql")

	var orgID, userID, publicModelID, legacyChannelID, connectionID, deploymentID, credentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m78-m77-draft-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m78-m77-draft-owner','fixture','org_admin','org_admin','active') RETURNING id`,
		orgID).Scan(&userID))
	const modelName = "m78-m77-draft-model"
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit)
VALUES ($1,$2,'M78 M77 draft model','custom','ga',TRUE,128000,8192) RETURNING id`,
		orgID, modelName).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,models,adapter_type,adapter_config,status,routing_epoch)
VALUES ('m78-m77-draft-connection','custom','https://m78-m77-draft.invalid',
        $1,'openai_compatible','{}','active',1) RETURNING id`, modelName).Scan(&legacyChannelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,
  admin_state,config_revision)
VALUES ($1,'m78-m77-draft-connection','custom','https://m78-m77-draft.invalid',
        'openai_compatible','{}','enabled',1) RETURNING id`, legacyChannelID).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision,
  max_context,default_max_output)
VALUES ($1,$2,$3,'m78-m77-draft-upstream','enabled',1,128000,8192) RETURNING id`,
		orgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture','active',1) RETURNING id`, connectionID).Scan(&credentialID))
	_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, credentialID, deploymentID, connectionID)
	require.NoError(t, err)
	const operationID = "openai.chat_completions.v1"
	const profileID = "integration.m78.m77.chat.v1"
	const profileVersion = "1.0.0"
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,
  revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','live_probed','m78-m77-draft',NOW(),1,$3,$4)`,
		deploymentID, operationID, profileID, profileVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  source_kind,source_ref,source_fingerprint,tested_by)
VALUES ($1,$2,$3,$4,$5,'m78-m77-draft-upstream','m78-m77-draft-upstream',1,1,
        $6,1,TRUE,200,1,'integration.route','m78-m77-draft',
        'sha256:'||repeat('1',64),$7)`, deploymentID, connectionID, operationID,
		profileID, profileVersion, credentialID, userID)
	require.NoError(t, err)

	route := runtimechange.DeploymentRoute{
		DeploymentID: deploymentID, ModelOperationID: operationID,
		ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1,
		ExpectedOperationRevision: 1, ProtocolProfileID: profileID,
		ProtocolProfileVersion: profileVersion, Admission: "allow",
	}
	candidate := runtimechange.DeploymentCandidate{
		ContractVersion: runtimechange.DeploymentCandidateVersion,
		PublicModelID:   publicModelID, ModelName: modelName,
		DeploymentRoutes: []runtimechange.DeploymentRoute{route},
	}
	baseline := runtimechange.DeploymentCandidate{
		ContractVersion: runtimechange.DeploymentCandidateVersion,
		PublicModelID:   publicModelID, ModelName: modelName,
		DeploymentRoutes: []runtimechange.DeploymentRoute{},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	var releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
  canary_basis_points,reason,created_by,public_model_id,route_items_hash,route_items_count)
VALUES ('rcr_m78_m77_draft',$1,$2,$3,$4,$5::jsonb,'[]'::jsonb,
        '{"contract_version":"unirouter.runtime-deployment-manifest-seal.v1","state":"prepared"}'::jsonb,
        'review_required','draft',1,0,'M77 legal unsealed draft',$6,$7,$8,1)
RETURNING id`, modelName, runtimechange.DeploymentCandidateVersion,
		testSHA256JSON(candidate), testSHA256JSON(baseline), string(candidateJSON), userID,
		publicModelID, testSHA256JSON([]runtimechange.DeploymentRoute{route})).Scan(&releaseID))
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
  release_id,ordinal,deployment_id,operation_id,
  expected_connection_revision,expected_deployment_revision,expected_operation_revision,
  protocol_profile_id,protocol_profile_version,admission,priority,weight,source_candidate_version)
VALUES ($1,1,$2,$3,1,1,1,$4,$5,'allow',NULL,NULL,$6)`, releaseID,
		deploymentID, operationID, profileID, profileVersion, runtimechange.DeploymentCandidateVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,1,'create','none','draft',$2,0,1,
        '{"runtime_mutation":"none","manifest_state":"prepared"}'::jsonb)`,
		releaseID, userID)
	require.NoError(t, err)

	err = migrate.Run(ctx, pool, testutil.MigrationsDir(t))
	require.ErrorContains(t, err, "seal every canonical v2 draft with the M77 artifact SealDeploymentDraft")
	var ledgerApplied, receiptTableExists, receiptColumnExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000078_atomic_model_release_authoring'),
  to_regclass('public.model_deployment_release_authoring_receipts') IS NOT NULL,
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='runtime_change_releases'
            AND column_name='authoring_receipt_id')`).Scan(
		&ledgerApplied, &receiptTableExists, &receiptColumnExists,
	))
	require.False(t, ledgerApplied)
	require.False(t, receiptTableExists)
	require.False(t, receiptColumnExists)

	// The failed migration transaction leaves the database at M77. Drain the
	// draft through the still-running M77 artifact's supported seal operation;
	// the current M78 runtime intentionally exposes only a fail-closed shell.
	legacyArtifact := m77artifact.New(pool)
	require.NoError(t, legacyArtifact.SealDeploymentDraft(
		ctx, "rcr_m78_m77_draft", 1, userID, orgID,
	))
	var sealedStage string
	var sealedVersion int64
	var sealedAtPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT stage,version,route_items_sealed_at IS NOT NULL
  FROM runtime_change_releases WHERE id=$1`, releaseID).Scan(
		&sealedStage, &sealedVersion, &sealedAtPresent,
	))
	require.Equal(t, "validated", sealedStage)
	require.EqualValues(t, 2, sealedVersion)
	require.True(t, sealedAtPresent)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	var historicalReceiptNull bool
	var historicalEventCount, historicalCreateCount, historicalValidateCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT release.authoring_receipt_id IS NULL,
       COUNT(event.id),
       COUNT(*) FILTER (
           WHERE event.sequence=1 AND event.action='create'
             AND event.from_stage='none' AND event.to_stage='draft'
             AND event.actor_user_id=$2
             AND event.expected_version=0 AND event.resulting_version=1
             AND event.detail='{"runtime_mutation":"none","manifest_state":"prepared"}'::jsonb
       ),
       COUNT(*) FILTER (
           WHERE event.sequence=2 AND event.action='validate'
             AND event.from_stage='draft' AND event.to_stage='validated'
             AND event.actor_user_id=$2
             AND event.expected_version=1 AND event.resulting_version=2
             AND event.detail='{"runtime_mutation":"none","manifest_state":"sealed"}'::jsonb
       )
  FROM runtime_change_releases release
  LEFT JOIN runtime_change_events event ON event.release_id=release.id
 WHERE release.id=$1
 GROUP BY release.authoring_receipt_id`, releaseID, userID).Scan(
		&historicalReceiptNull, &historicalEventCount,
		&historicalCreateCount, &historicalValidateCount,
	))
	require.True(t, historicalReceiptNull)
	require.Equal(t, 2, historicalEventCount)
	require.Equal(t, 1, historicalCreateCount)
	require.Equal(t, 1, historicalValidateCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000078_atomic_model_release_authoring'),
  to_regclass('public.model_deployment_release_authoring_receipts') IS NOT NULL,
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='runtime_change_releases'
            AND column_name='authoring_receipt_id')`).Scan(
		&ledgerApplied, &receiptTableExists, &receiptColumnExists,
	))
	require.True(t, ledgerApplied)
	require.True(t, receiptTableExists)
	require.True(t, receiptColumnExists)

	manager, err := runtimechange.NewManager(pool, m78MigrationSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	canonicalCommand := runtimechange.DeploymentLifecycleCommand{
		Mutation: runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleActivateInitial,
			ReleaseRef:             "rcr_m78_m77_draft",
			ExpectedReleaseVersion: 2,
			Reason:                 "freeze canonical M76 replay across M78 migration",
		},
		CustomerOrgID:      orgID,
		ActorUserID:        userID,
		ActorOrgID:         orgID,
		IdempotencyKeyHash: strings.Repeat("a", 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat("b", 64),
		ServerOperationID:  "m78-canonical-m76-replay",
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: "m78-canonical-m76-replay",
			Source:    "integration_test",
		},
	}
	canonicalBeforeDown, err := manager.ApplyDeploymentLifecycle(ctx, canonicalCommand)
	require.NoError(t, err)
	canonicalBeforeJSON, err := json.Marshal(canonicalBeforeDown)
	require.NoError(t, err)
	require.NotContains(t, string(canonicalBeforeJSON), "before_public_model_id")
	require.NotContains(t, string(canonicalBeforeJSON), "after_public_model_id")

	execMigrationDown(t, ctx, pool, "000078_atomic_model_release_authoring.down.sql")
	var ledgerRemoved, receiptTableRemoved, receiptColumnRemoved, creatorConstraintRestored bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  NOT EXISTS(SELECT 1 FROM schema_migrations
              WHERE version='000078_atomic_model_release_authoring'),
  to_regclass('public.model_deployment_release_authoring_receipts') IS NULL,
  NOT EXISTS(SELECT 1 FROM information_schema.columns
              WHERE table_schema='public' AND table_name='runtime_change_releases'
                AND column_name='authoring_receipt_id'),
  EXISTS(
      SELECT 1 FROM pg_constraint
       WHERE conname='model_operation_public_execution_bindings_identity_check'
         AND pg_get_constraintdef(oid) LIKE '%created_by_org_id = customer_org_id%'
  )`).Scan(&ledgerRemoved, &receiptTableRemoved, &receiptColumnRemoved, &creatorConstraintRestored))
	require.True(t, ledgerRemoved)
	require.True(t, receiptTableRemoved)
	require.True(t, receiptColumnRemoved)
	require.True(t, creatorConstraintRestored)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
              WHERE version='000078_atomic_model_release_authoring')`).Scan(&ledgerApplied))
	require.True(t, ledgerApplied)
	canonicalAfterUp, err := manager.ApplyDeploymentLifecycle(ctx, canonicalCommand)
	require.NoError(t, err)
	canonicalAfterJSON, err := json.Marshal(canonicalAfterUp)
	require.NoError(t, err)
	require.Equal(t, canonicalBeforeJSON, canonicalAfterJSON,
		"canonical M76 exact replay body must not drift across M78 down/up")
}

func TestAtomicModelReleaseAuthoringDownRejectsFutureMigrationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
INSERT INTO schema_migrations(version) VALUES ('000079_future_contract')`)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000078_atomic_model_release_authoring.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	var m78Present, futurePresent, receiptTablePresent, authoringColumnPresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000078_atomic_model_release_authoring'),
  EXISTS(SELECT 1 FROM schema_migrations
          WHERE version='000079_future_contract'),
  to_regclass('public.model_deployment_release_authoring_receipts') IS NOT NULL,
  EXISTS(SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='runtime_change_releases'
            AND column_name='authoring_receipt_id')`).Scan(
		&m78Present, &futurePresent, &receiptTablePresent, &authoringColumnPresent,
	))
	require.True(t, m78Present)
	require.True(t, futurePresent)
	require.True(t, receiptTablePresent)
	require.True(t, authoringColumnPresent)
}

func testSHA256JSON(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
