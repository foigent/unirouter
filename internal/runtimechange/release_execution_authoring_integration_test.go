//go:build integration

package runtimechange_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDataReleaseAuthoringPinsDefinitionWithoutRewritingModelOrHistory(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	f := createCanonicalDeploymentFixtureWithProduct(t, pool, "data-release", "active", 1, 1, "glm", "https://open.bigmodel.cn/api/paas/v4", "glm-5.3")
	makeSoleInitialCustomer(t, pool, f.OrgID)
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	require.Empty(t, p.AlwaysRequiredCapabilities, "this fixture seals a plain-Chat route; it does not invent tool approvals")
	_, err := pool.Exec(ctx, `UPDATE model_registry SET execution_profile_id=$2,execution_profile_version=$3 WHERE id=$1`, f.PublicModelID, p.ID, p.Version)
	require.NoError(t, err)
	f.ConnectionRevision++
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET adapter_config='{"endpoint_path_mode":"base_relative"}',config_revision=config_revision+1 WHERE id=$1`, f.ConnectionID)
	require.NoError(t, err)
	f.ProtocolProfileID = p.ProtocolProfile
	f.OperationRevision++
	_, err = pool.Exec(ctx, `UPDATE model_deployment_operations SET protocol_profile_id=$3,revision=revision+1 WHERE deployment_id=$1 AND operation_id=$2`, f.DeploymentID, f.OperationID, p.ProtocolProfile)
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, f, "data-release-exact-operation")
	p.Version = "99.0.0"
	_, compiled := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, compiled, "exercise an address absent from the running binary")
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	// Explicit isolated storage fixture. It does not claim a live Publication
	// apply or manufacture any capability Probe/Approval.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	plan := []byte(`{"fixture":"data Release authoring"}`)
	_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity)
VALUES('data-release-fixture','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(plan)), plan)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','data-release-fixture',true)`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,schema_version,canonical_bytes,publication_id)
VALUES($1,$2,$3,$4,$5,$6,'data-release-fixture')`, d.Digest(), p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	// Isolated candidate authoring fixture; the production owner will expose
	// this exact pin without mutating operations referenced by a sealed Release.
	_, err = pool.Exec(ctx, `UPDATE model_registry SET product_lineage_id=$2 WHERE id=$1`, f.PublicModelID, d.ProductLineageID())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE model_deployment_operations SET profile_definition_digest=$3,revision=revision+1 WHERE deployment_id=$1 AND operation_id=$2`, f.DeploymentID, f.OperationID, d.Digest())
	require.NoError(t, err)
	f.OperationRevision++
	insertSuccessfulProbe(t, pool, f, "data-profile-candidate-operation")
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	command := deploymentReleaseAuthoringCommand(f, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}, []runtimechange.DeploymentReleaseSelection{{DeploymentID: f.DeploymentID, OperationID: f.OperationID}}, "a", "b", "op_data_release")
	command.Mutation.ProfileDefinitionDigest = d.Digest()
	command.Mutation.PublicContextLimit = 64000
	command.Mutation.PublicOutputLimit = 4096
	result, err := manager.AuthorSealedDeploymentRelease(ctx, command)
	require.NoError(t, err)
	require.Equal(t, d.Digest(), result.Release.ProfileDefinitionDigest)
	require.NotEmpty(t, result.Release.ExecutionSnapshotDigest)
	sealed, err := releaseexecution.ReadSnapshot(ctx, pool, result.Release.ID)
	require.NoError(t, err)
	require.NotNil(t, sealed)
	require.NoError(t, sealed.ValidateDefinition(d))
	var contextLimit int
	var profileVersion string
	require.NoError(t, pool.QueryRow(ctx, `SELECT context_length,execution_profile_version FROM model_registry WHERE id=$1`, f.PublicModelID).Scan(&contextLimit, &profileVersion))
	require.Equal(t, 128000, contextLimit, "release limits do not mutate stable model bootstrap metadata")
	require.Equal(t, "1.9.0", profileVersion)
	_, err = pool.Exec(ctx, `UPDATE runtime_change_releases SET execution_snapshot_digest=$2 WHERE id=$1`, result.Release.ID, strings.Repeat("a", 64))
	require.Error(t, err)
	// Same idempotent operation recovers the same sealed identity.
	replayed, err := manager.AuthorSealedDeploymentRelease(ctx, command)
	require.NoError(t, err)
	require.Equal(t, result.Release.ID, replayed.Release.ID)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_change_slots(model_name,public_model_id,stable_release_id,slot_version) VALUES($1,$2,$3,1)`, f.ModelName, f.PublicModelID, result.Release.ID)
	require.Error(t, err, "no potential compatible ingress fleet has been proved")
	// The fixture joins a compatible generation before the formal Slot owner
	// activates; raw SQL is not used to bypass Release lifecycle receipts.
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	manifestBytes, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	manifestDigest, err := manifest.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("d", 64)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, manifestDigest, manifestBytes)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, "uri_"+strings.Repeat("e", 32), artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	// A second live receiver lacking one execution implementation must block
	// activation, even though its schema/compatibility epoch is identical.
	limited := manifest
	limited.Implementations = nil
	for _, ref := range manifest.Implementations {
		if ref.Kind != "runtime_release" {
			limited.Implementations = append(limited.Implementations, ref)
		}
	}
	limitedBytes, err := limited.CanonicalBytes()
	require.NoError(t, err)
	limitedDigest, err := limited.Digest()
	require.NoError(t, err)
	limitedArtifact := "sha256:" + strings.Repeat("f", 64)
	limitedInstance := "uri_" + strings.Repeat("f", 32)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, limitedArtifact, limitedDigest, limitedBytes)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','pending_join',clock_timestamp()+interval '1 hour')`, limitedInstance, limitedArtifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	_, err = manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(f, runtimechange.DeploymentLifecycleActivateInitial, result.Release, 0, 0, 'd', 'e', "op_data_activate"))
	require.Error(t, err, "pending ingress receiver must also block incompatible activation")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET ingress_state='absent' WHERE instance_id=$1`, limitedInstance)
	require.NoError(t, err)
	activated, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(f, runtimechange.DeploymentLifecycleActivateInitial, result.Release, 0, 0, 'd', 'e', "op_data_activate"))
	require.NoError(t, err)
	require.Equal(t, int64(1), activated.Slot.AfterVersion)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET ingress_state='pending_join' WHERE instance_id=$1`, limitedInstance)
	require.Error(t, err, "old incompatible generation cannot join after activation")
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	request := modelrouting.Request{Mode: modelrouting.ModeCanonical, CustomerOrgID: f.OrgID, PublicModel: f.ModelName, ModelOperationID: f.OperationID, SelectionKey: "data-release-request"}
	executionPlan, err := planner.Plan(ctx, request)
	require.NoError(t, err, "exclusions: %+v", executionPlan.Exclusions)
	require.Len(t, executionPlan.Routes, 1)
	require.Equal(t, d.Digest(), executionPlan.Definition.Digest())
	require.Equal(t, result.Release.ID, executionPlan.RuntimeReleaseID)
	// Bootstrap metadata drift cannot reinterpret the activated data snapshot.
	_, err = pool.Exec(ctx, `UPDATE model_registry SET execution_profile_version='1.8.0',context_length=128000,output_limit=8192 WHERE id=$1`, f.PublicModelID)
	require.NoError(t, err)
	after, err := planner.Plan(ctx, request)
	require.NoError(t, err)
	require.Len(t, after.Routes, 1)
	require.Equal(t, executionPlan.ExecutionProfileVersion, after.ExecutionProfileVersion)
	require.Equal(t, executionPlan.PublicContextLimit, after.PublicContextLimit)
	require.Equal(t, executionPlan.PublicOutputLimit, after.PublicOutputLimit)
	require.Equal(t, executionPlan.Definition.Digest(), after.Definition.Digest())
	// Use the existing billing Freeze/ProviderAttempt owner. This stops at
	// dispatch authorization: no network request or new pricing authority.
	testutil.NewTestWalletCents(t, pool, f.OrgID, 10000)
	var apiKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status) VALUES($1,'data-release-fixture','data-key',repeat('a',64),'active') RETURNING id`, f.OrgID).Scan(&apiKeyID))
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents) VALUES($1,'glm',2,10)`, f.ModelName)
	require.NoError(t, err)
	bill := billing.NewBilling(pool, nil)
	freeze := func(id string) modelexecution.AttemptIdentity {
		frozen, err := bill.Freeze(ctx, billing.FreezeRequest{RequestID: id, OrgID: f.OrgID, APIKeyID: apiKeyID, Model: f.ModelName, EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 1, ExecutionSnapshotHash: after.SnapshotHash})
		require.NoError(t, err)
		require.True(t, frozen.Success, frozen.ErrorDetail)
		return modelexecution.AttemptIdentity{RequestID: id, AttemptID: id + "-attempt", OrgID: f.OrgID, APIKeyID: apiKeyID, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions"}
	}
	coordinator, err := modelexecution.New(pool, bill, modelexecution.DefaultConfig())
	require.NoError(t, err)
	ctx = runtimegeneration.WithIdentity(ctx, runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("e", 32), Artifact: artifact})
	identity := freeze("data-release-authorize")
	decision, err := coordinator.Authorize(ctx, after, after.Routes[0], identity)
	require.NoError(t, err)
	require.Equal(t, modelexecution.Applied, decision.Status)
	var attemptCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1`, identity.RequestID).Scan(&attemptCount))
	require.Equal(t, 1, attemptCount)
	var retainedInstance, retainedArtifact string
	require.NoError(t, pool.QueryRow(ctx, `SELECT a.runtime_instance_id,g.artifact_digest FROM billing_provider_attempts a JOIN runtime_generations g ON g.instance_id=a.runtime_instance_id WHERE a.request_id=$1`, identity.RequestID).Scan(&retainedInstance, &retainedArtifact))
	require.Equal(t, "uri_"+strings.Repeat("e", 32), retainedInstance)
	require.Equal(t, artifact, retainedArtifact)
	_, err = pool.Exec(ctx, `UPDATE billing_provider_attempts SET runtime_instance_id=$2 WHERE request_id=$1`, identity.RequestID, limitedInstance)
	require.Error(t, err, "historical attempt cannot change execution generation")
	deniedIdentity := freeze("data-release-revoked")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings SET status='disabled',state_revision=state_revision+1 WHERE id=$1`, f.CredentialID)
	require.NoError(t, err)
	denied, err := coordinator.Authorize(ctx, after, after.Routes[0], deniedIdentity)
	require.NoError(t, err)
	require.NotEqual(t, modelexecution.Applied, denied.Status)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1`, deniedIdentity.RequestID).Scan(&attemptCount))
	require.Zero(t, attemptCount)

}
