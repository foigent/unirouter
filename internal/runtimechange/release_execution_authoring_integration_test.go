//go:build integration

package runtimechange_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

func prepareDataReleaseProfileFixture(t *testing.T, pool *pgxpool.Pool) (canonicalDeploymentFixture, modelprofile.Profile, modelprofile.Definition) {
	t.Helper()
	ctx := t.Context()
	f := createCanonicalDeploymentFixtureWithProduct(t, pool, "data-release", "active", 1, 1, "glm", "https://open.bigmodel.cn/api/paas/v4", "glm-5.3")
	makeSoleInitialCustomer(t, pool, f.OrgID)
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	require.Empty(t, p.AlwaysRequiredCapabilities)
	require.Equal(t, modelprofile.CapabilityReasoning, modelprofile.DefaultChatReasoningCapability(p))
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
	// apply; the exact capability evidence below is synthetic test data.
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
	_, err = tx.Exec(ctx, `INSERT INTO model_capability_probe_contract_bindings(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,introduced_publication_id,profile_definition_digest)
SELECT operation_id,capability_id,execution_profile_id,$1,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,'data-release-fixture',$2
FROM model_capability_probe_contract_bindings WHERE execution_profile_id='glm.5.3.api' AND execution_profile_version='1.9.0' AND capability_id='reasoning'`, p.Version, d.Digest())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
SELECT operation_id,capability_id,execution_profile_id,$1,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version
FROM model_capability_current_probe_contracts WHERE execution_profile_id='glm.5.3.api' AND execution_profile_version='1.9.0' AND capability_id='reasoning'`, p.Version)
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
	return f, p, d
}

func TestDataReleaseAuthoringPinsDefinitionWithoutRewritingModelOrHistory(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	f, p, d := prepareDataReleaseProfileFixture(t, pool)
	approvalID := insertDataReleaseReasoningApproval(t, pool, f, p)
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	command := deploymentReleaseAuthoringCommand(f, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}, []runtimechange.DeploymentReleaseSelection{{DeploymentID: f.DeploymentID, OperationID: f.OperationID}}, "a", "b", "op_data_release")
	command.Mutation.ProfileDefinitionDigest = d.Digest()
	command.Mutation.PublicContextLimit = 64000
	command.Mutation.PublicOutputLimit = 4096
	command.Mutation.CapabilityApprovalIDs = []int64{approvalID}
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

func TestDeploymentReleaseSelectionObservationScopeIntegration(t *testing.T) {
	for _, kind := range []string{"no_observation", "matching", "different_identity", "different_scope", "foreign_tenant"} {
		t.Run(kind, func(t *testing.T) {
			// Retained evidence may predate M128's new-approval guard. Keep it
			// immutable through the actual migration; never disable that guard.
			pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
			ctx := t.Context()
			f, profile, definition := prepareDataReleaseProfileFixture(t, pool)
			foreignOrg := testutil.NewTestOrg(t, pool)
			current, scope, err := upstreamrelease.Read(ctx, pool, f.OrgID, f.DeploymentID, false)
			require.NoError(t, err)
			summary := map[string]any{}
			if kind != "no_observation" {
				current, err = upstreamrelease.Next(scope, current, upstreamrelease.Input{
					ExpectedRevision: current.Revision, ExpectedIdentity: upstreamrelease.Identity(current.Observation),
					ReleaseLabel: "selection observation", SourceURL: "https://provider.example.test/release",
				}, f.UserID, time.Now())
				require.NoError(t, err)
				captured := *current.Observation
				switch kind {
				case "different_identity":
					captured.Identity = "uor_" + strings.Repeat("f", 32)
					require.NotEqual(t, current.Observation.Identity, captured.Identity)
				case "different_scope":
					captured.Scope.APIModel += "-different"
				case "foreign_tenant":
					captured.Scope.CustomerOrgID = foreignOrg
				}
				summary["upstream_release_observation"] = captured
			}
			wire, err := json.Marshal(summary)
			require.NoError(t, err)
			approvalID := insertDataReleaseReasoningApprovalWithSummary(t, pool, f, profile, wire)
			require.NoError(t, migrate.RunExisting(ctx, pool, testutil.MigrationsDir(t)))
			if current.Observation != nil {
				observation, err := json.Marshal(current.Observation)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `UPDATE model_deployments SET upstream_release_observation=$2,upstream_observation_revision=$3 WHERE id=$1`, f.DeploymentID, observation, current.Revision)
				require.NoError(t, err)
			} else {
				var absent bool
				require.NoError(t, pool.QueryRow(ctx, `SELECT upstream_release_observation IS NULL FROM model_deployments WHERE id=$1`, f.DeploymentID).Scan(&absent))
				require.True(t, absent, "exercise the actual M128 SQL NULL column")
			}
			var qualified bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT qualified FROM model_capability_approval_qualifications WHERE approval_id=$1`, approvalID).Scan(&qualified))
			require.True(t, qualified, "observation filtering must be exercised independently of the retained qualification view")
			manager := newScopeLifecycleManager(t, pool)
			options, err := manager.ListDeploymentReleaseAuthoringOptions(ctx, f.OrgID)
			require.NoError(t, err)
			var selected *runtimechange.DeploymentReleaseSelectionOption
			for _, model := range options.Models {
				if model.PublicModel.ID != f.PublicModelID {
					continue
				}
				for index := range model.SelectionOptions {
					option := &model.SelectionOptions[index]
					if option.DeploymentID == f.DeploymentID && option.OperationID == f.OperationID {
						selected = option
					}
				}
			}
			require.NotNil(t, selected)
			require.Equal(t, definition.Digest(), selected.ProfileDefinitionDigest)
			require.Equal(t, upstreamrelease.Identity(current.Observation), selected.ExpectedUpstreamObservationIdentity)
			if kind == "no_observation" || kind == "matching" {
				require.Equal(t, []int64{approvalID}, selected.CapabilityApprovalIDs)
			} else {
				require.Empty(t, selected.CapabilityApprovalIDs)
			}
			foreign, err := manager.ListDeploymentReleaseAuthoringOptions(ctx, foreignOrg)
			require.NoError(t, err)
			require.Empty(t, foreign.Models, "a foreign tenant cannot see this model or its approval")
			require.Empty(t, foreign.Releases)
		})
	}
}

// This DB fixture follows the retained binding/selector and Probe/Approval
// tables. It proves authoring behavior without claiming a Provider call.
func insertDataReleaseReasoningApproval(t *testing.T, pool *pgxpool.Pool, f canonicalDeploymentFixture, p modelprofile.Profile) int64 {
	t.Helper()
	return insertDataReleaseReasoningApprovalWithSummary(t, pool, f, p, []byte(`{}`))
}

func insertDataReleaseReasoningApprovalWithSummary(t *testing.T, pool *pgxpool.Pool, f canonicalDeploymentFixture, p modelprofile.Profile, requestSummary []byte) int64 {
	t.Helper()
	ctx := t.Context()
	var contractID, contractVersion string
	require.NoError(t, pool.QueryRow(ctx, `SELECT probe_contract_id,probe_contract_version
FROM model_capability_probe_contract_bindings
WHERE operation_id=$1 AND capability_id='reasoning' AND execution_profile_id=$2 AND execution_profile_version=$3
  AND protocol_profile_id=$4 AND protocol_profile_version=$5`,
		f.OperationID, p.ID, p.Version, f.ProtocolProfileID, f.ProtocolVersion).Scan(&contractID, &contractVersion))
	_, err := pool.Exec(ctx, `INSERT INTO model_deployment_capabilities
    (deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,$2,'reasoning','enabled','declared',$3,1)`, f.DeploymentID, f.OperationID, "capability_contract:"+contractID+"@"+contractVersion)
	require.NoError(t, err)
	var linkCreated time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT created_at FROM credential_binding_deployments
WHERE binding_id=$1 AND deployment_id=$2`, f.CredentialID, f.DeploymentID).Scan(&linkCreated))
	var probeID int64
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fmt.Sprintf("data-release-reasoning/%d", f.DeploymentID))))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_capability_probes (
  customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,provider,base_url,adapter_type,adapter_config,
  upstream_model,connection_revision,deployment_revision,operation_revision,
  capability_support_revision,credential_binding_id,credential_state_revision,
  credential_link_created_at,provider_success,success,status_code,latency_ms,
  reported_model,error_summary,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by)
VALUES ($1,$2,$3,$4,'reasoning',$5,$6,$7,$8,$9,$10,'glm','https://open.bigmodel.cn/api/paas/v4',
        'openai_compatible','{"endpoint_path_mode":"base_relative"}',
        $11,$12,$13,$14,1,$15,$16,$17,TRUE,TRUE,200,12,
        $11,'',$20::jsonb,'{}','admin.model_capability_probe.v1','data-release-reasoning-fixture',$18,$19)
RETURNING capability_probe_id`, f.OrgID, f.DeploymentID, f.ConnectionID, f.OperationID,
		p.ID, p.Version, f.ProtocolProfileID, f.ProtocolVersion, contractID, contractVersion,
		f.UpstreamModel, f.ConnectionRevision, f.DeploymentRevision, f.OperationRevision,
		f.CredentialID, f.CredentialRevision, linkCreated, fingerprint, f.UserID, requestSummary).Scan(&probeID))
	var approvalID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_capability_routing_approvals (
  capability_probe_id,customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,connection_revision,deployment_revision,
  operation_revision,capability_support_revision,credential_binding_id,
  credential_state_revision,credential_link_created_at,approved_by,approved_by_org_id,
  reason,source_kind,source_ref)
SELECT capability_probe_id,customer_org_id,deployment_id,connection_id,operation_id,capability_id,
       execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
       probe_contract_id,probe_contract_version,connection_revision,deployment_revision,
       operation_revision,capability_support_revision,credential_binding_id,
       credential_state_revision,credential_link_created_at,$2,$3,'exact data Release fixture',
       'admin.model_capability_routing_approval.v1','capability_probe:'||capability_probe_id::text
FROM model_capability_probes WHERE capability_probe_id=$1 RETURNING approval_id`, probeID, f.UserID, f.OrgID).Scan(&approvalID))
	return approvalID
}
