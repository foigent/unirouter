//go:build integration

package modelrouting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type plannerRuntimeSnapshotBuilder struct{}

func (plannerRuntimeSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

func TestPlannerLoadsRealPairedSnapshotAcrossV1AndV2SlotStates(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	modelName := "planner-paired-model"
	var userID, publicModelID, channelID, legacyKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'planner-paired-user','fixture','platform_admin','platform_admin','active')
RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,$2,'custom','ga',TRUE,128000,8192)
RETURNING id`, orgID, modelName).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
    max_context,default_max_output,priority,routing_weight,status,circuit_state,
    routing_epoch,circuit_epoch)
VALUES ('planner-paired-channel','custom','https://provider.invalid',$1,$2::jsonb,
        'openai_compatible','{"numeric_revision":9007199254740993}',128000,8192,
        10,100,'active','healthy',1,1)
RETURNING id`, modelName, fmt.Sprintf(`{"%s":"upstream-model"}`, modelName)).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'cipher:fixture',$2,'active',1) RETURNING id`, channelID, modelName).Scan(&legacyKeyID))

	var connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'planner-paired-connection','custom','https://provider.invalid','openai_compatible',
        '{"numeric_revision":9007199254740993}','enabled',1)
RETURNING id`, channelID).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,
    circuit_state,circuit_revision,legacy_channel_id,legacy_public_model)
VALUES ($1,$2,$3,'upstream-model','enabled',1,10,100,128000,8192,'healthy',1,$4,$5)
RETURNING id`, orgID, publicModelID, connectionID, channelID, modelName).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (
    connection_id,key_enc,status,state_revision,legacy_channel_key_id)
VALUES ($1,'cipher:fixture','active',1,$2) RETURNING id`, connectionID, legacyKeyID).Scan(&bindingID))
	_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute')`, bindingID, deploymentID, connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'legacy.any.v1','enabled','declared','legacy compatibility',1,
        'legacy.channel_model_probe','v1')`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','production_approved','integration',NOW(),1,
        'integration.openai.chat','1.0.0')`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.embeddings.v1','enabled','production_approved','integration',NOW(),1,
        'integration.openai.embeddings','1.0.0')`, deploymentID)
	require.NoError(t, err)
	insertPlannerProbe(t, pool, deploymentID, connectionID, bindingID, userID,
		"legacy.any.v1", "legacy.channel_model_probe", "v1", "legacy-probe")
	insertPlannerProbe(t, pool, deploymentID, connectionID, bindingID, userID,
		"openai.chat_completions.v1", "integration.openai.chat", "1.0.0", "chat-probe")
	insertPlannerProbe(t, pool, deploymentID, connectionID, bindingID, userID,
		"openai.embeddings.v1", "integration.openai.embeddings", "1.0.0", "embeddings-probe")

	legacyOverrides := []legacyOverride{{
		ChannelID: channelID, ExpectedRoutingEpoch: 1, Admission: routingpolicy.AdmissionAllow,
	}}
	legacyCandidate := struct {
		ContractVersion string           `json:"contract_version"`
		ModelName       string           `json:"model_name"`
		Overrides       []legacyOverride `json:"overrides"`
	}{legacyCandidateVersion, modelName, legacyOverrides}
	legacyCandidateJSON, err := json.Marshal(legacyCandidate)
	require.NoError(t, err)
	legacyItem := manifestItem{
		Ordinal: 1, DeploymentID: deploymentID, ModelOperationID: legacyCompatibilityOp,
		ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1, ExpectedOperationRevision: 1,
		ProtocolProfileID: "legacy.channel_model_probe", ProtocolProfileVersion: "v1",
		Admission: routingpolicy.AdmissionAllow, SourceCandidateVersion: legacyCandidateVersion,
	}
	var legacyReleaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ('rcr_planner_paired_v1',$1,$2,$3,
        'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        $4::jsonb,'[]','{}','unchanged','active',1,0,'planner v1 fixture',$5,$5,$5,NOW(),NOW())
RETURNING id`, modelName, legacyCandidateVersion, digestBytes(legacyCandidateJSON),
		string(legacyCandidateJSON), userID).Scan(&legacyReleaseID))
	insertPlannerManifestItem(t, pool, legacyReleaseID, legacyItem)
	_, err = pool.Exec(ctx, `
UPDATE runtime_change_releases
   SET public_model_id=$2,route_items_hash=$3,route_items_count=1,
       route_items_sealed_at=NOW()
 WHERE id=$1`, legacyReleaseID, publicModelID, manifestDigest([]manifestItem{legacyItem}))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_slots (
    model_name,stable_release_id,canary_release_id,canary_basis_points,slot_version)
VALUES ($1,$2,NULL,0,1)`, modelName, legacyReleaseID)
	require.NoError(t, err)

	planner, err := NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		pool, plannerRuntimeSnapshotBuilder{}, routingpolicy.Default(), planner,
	)
	require.NoError(t, err)
	canonicalRelease := integrationfixture.AuthorSealedDeploymentRelease(
		t, pool, manager, integrationfixture.AtomicRelease{
			PublicModelID: publicModelID,
			CustomerOrgID: orgID,
			ActorUserID:   userID,
			ActorOrgID:    orgID,
			Selections: []runtimechange.DeploymentReleaseSelection{
				{DeploymentID: deploymentID, OperationID: "openai.chat_completions.v1"},
				{DeploymentID: deploymentID, OperationID: "openai.embeddings.v1"},
			},
			Reason: "planner paired v1 to v2 fixture",
		},
	)
	canonicalReleaseID := canonicalRelease.ID
	request := Request{
		Mode: ModeVerify, CustomerOrgID: orgID, PublicModel: modelName,
		ModelOperationID: "openai.chat_completions.v1", SelectionKey: "integration-v1",
	}
	plan, err := planner.Plan(ctx, request)
	require.NoError(t, err)
	require.Len(t, plan.Routes, 1)
	require.Equal(t, deploymentID, plan.Routes[0].Evidence.DeploymentID)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	legacyHash, canonicalHash, err := planner.Verify(ctx, tx, orgID, publicModelID, legacyReleaseID, canonicalReleaseID)
	require.NoError(t, err)
	require.Equal(t, legacyHash, canonicalHash)
	require.NoError(t, tx.Rollback(ctx))

	cutoverReceipt := integrationfixture.ApplyDeploymentLifecycle(
		t, manager, integrationfixture.AtomicRelease{
			PublicModelID: publicModelID,
			CustomerOrgID: orgID,
			ActorUserID:   userID,
			ActorOrgID:    orgID,
		}, runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             canonicalRelease.ReleaseRef,
			ExpectedReleaseVersion: canonicalRelease.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "planner paired fixture canonical cutover",
		},
	)
	require.Equal(t, cutoverReceipt.LegacyPlanHash, cutoverReceipt.DeploymentPlanHash)
	request.SelectionKey = "integration-v2-verify"
	plan, err = planner.Plan(ctx, request)
	require.NoError(t, err)
	require.Len(t, plan.Routes, 1)
	require.Equal(t, canonicalReleaseID, plan.RuntimeReleaseID)
	request.Mode = ModeCanonical
	request.SelectionKey = "integration-v2-canonical"
	plan, err = planner.Plan(ctx, request)
	require.NoError(t, err)
	require.Len(t, plan.Routes, 1)
	require.Equal(t, bindingID, plan.Routes[0].Evidence.CredentialBindingID)
}

func TestPlannerAndCutoverSelectTheSamePreferredCapabilityApprovalIntegration(t *testing.T) {
	for _, protocolProfileID := range []string{
		"protocol.deepseek-openai-chat.v4",
		"openai.chat_completions",
	} {
		protocolProfileID := protocolProfileID
		t.Run(protocolProfileID, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			fixture := integrationfixture.InsertModelCapabilityQualificationWithProtocol(
				t, pool, "planner-preferred-"+protocolProfileID, protocolProfileID, "1.0.0",
			)
			ctx := context.Background()

			candidates := []canonicalCandidate{{
				DeploymentID: fixture.DeploymentID,
				Capabilities: make(map[string]capabilitySnapshot),
			}}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			require.NoError(t, err)
			require.NoError(t, loadCapabilities(ctx, tx, fixture.OrgID, fixture.ModelID,
				"openai.chat_completions.v1", "deepseek.v4-flash.api", "1.4.0",
				[]string{"streaming"}, candidates))
			require.Empty(t, candidates[0].Capabilities, "raw declared projection cannot authorize typed streaming")
			require.NoError(t, tx.Rollback(ctx))

			_, approvalA, _ := integrationfixture.AppendModelCapabilityProbeApproval(t, pool, fixture, 1, "planner-a")
			_, approvalB, approvedAtB := integrationfixture.AppendModelCapabilityProbeApproval(t, pool, fixture, 1, "planner-b")
			require.Greater(t, approvalB, approvalA)

			plannerCandidates := []canonicalCandidate{{DeploymentID: fixture.DeploymentID, Capabilities: make(map[string]capabilitySnapshot)}}
			cutoverCandidates := []canonicalCandidate{{DeploymentID: fixture.DeploymentID, Capabilities: make(map[string]capabilitySnapshot)}}
			tx, err = pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			require.NoError(t, err)
			require.NoError(t, loadCapabilities(ctx, tx, fixture.OrgID, fixture.ModelID,
				"openai.chat_completions.v1", "deepseek.v4-flash.api", "1.4.0",
				[]string{"streaming"}, plannerCandidates))
			require.NoError(t, loadAllCapabilities(ctx, tx, fixture.OrgID, fixture.ModelID,
				"openai.chat_completions.v1", "deepseek.v4-flash.api", "1.4.0", cutoverCandidates))
			require.NoError(t, tx.Rollback(ctx))

			for label, loaded := range map[string][]canonicalCandidate{"planner": plannerCandidates, "cutover": cutoverCandidates} {
				capability, ok := loaded[0].Capabilities["streaming"]
				require.True(t, ok, label)
				require.Equal(t, "capability_approval:"+fmt.Sprint(approvalB), capability.EvidenceRef, label)
				require.Equal(t, approvalB, capability.Revision, label)
				require.NotNil(t, capability.VerifiedAt, label)
				require.True(t, capability.VerifiedAt.Equal(approvedAtB), label)
			}
		})
	}
}

func TestAuthorityCapabilityApprovalBootstrapUsesExactLiveProjectionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	fixture := integrationfixture.InsertModelCapabilityQualification(t, pool, "authority-bootstrap")
	ctx := context.Background()
	load := func() snapshot {
		candidates := []canonicalCandidate{{
			DeploymentID: fixture.DeploymentID,
			Provider:     "deepseek", UpstreamModel: "deepseek-v4-flash",
			PublicPath:                "/v1/chat/completions",
			Capabilities:              make(map[string]capabilitySnapshot),
			CapabilityApprovalPending: make(map[string]bool),
		}}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		require.NoError(t, loadCapabilities(ctx, tx, fixture.OrgID, fixture.ModelID,
			"openai.chat_completions.v1", "deepseek.v4-flash.api", "1.4.0",
			[]string{"streaming"}, candidates))
		require.NoError(t, tx.Rollback(ctx))
		return snapshot{Canonical: candidates}
	}
	plan := Plan{Exclusions: []Exclusion{{
		DeploymentID: fixture.DeploymentID, Reason: "unsupported_capability:streaming",
	}}}

	value := load()
	require.Empty(t, value.Canonical[0].Capabilities)
	require.True(t, value.Canonical[0].CapabilityApprovalPending["streaming"])
	ready, structural := authorityPlanReady(value, plan, ErrNoRoute)
	require.True(t, ready)
	require.Empty(t, structural)

	_, err := pool.Exec(ctx, `
DELETE FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, fixture.DeploymentID)
	require.NoError(t, err)
	value = load()
	ready, structural = authorityPlanReady(value, plan, ErrNoRoute)
	require.False(t, ready)
	require.Equal(t, []string{"unsupported_capability:streaming"}, structural)

	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared',
        'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',1)`, fixture.DeploymentID)
	require.NoError(t, err)
	_, approvalID, approvedAt := integrationfixture.AppendModelCapabilityProbeApproval(t, pool, fixture, 1, "authority-bootstrap")
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_capabilities
   SET evidence_state='production_approved',evidence_ref=$2,verified_at=$3,
       revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, fixture.DeploymentID,
		"capability_approval:"+fmt.Sprint(approvalID), approvedAt)
	require.NoError(t, err)
	value = load()
	require.False(t, value.Canonical[0].CapabilityApprovalPending["streaming"])
	capability, ok := value.Canonical[0].Capabilities["streaming"]
	require.True(t, ok)
	require.Equal(t, "production_approved", capability.EvidenceState)
}

func insertPlannerProbe(
	t *testing.T,
	pool *pgxpool.Pool,
	deploymentID, connectionID, bindingID, userID int64,
	operationID, profileID, profileVersion, source string,
) {
	t.Helper()
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source)))
	_, err := pool.Exec(context.Background(), `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    error_summary,source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,'upstream-model','upstream-model',1,1,$6,1,TRUE,200,2,'',
        'integration.modelrouting',$7,$8,$9,NOW())`, deploymentID, connectionID,
		operationID, profileID, profileVersion, bindingID, source, fingerprint, userID)
	require.NoError(t, err)
}

func insertPlannerManifestItem(t *testing.T, pool *pgxpool.Pool, releaseID int64, item manifestItem) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
    expected_deployment_revision,expected_operation_revision,protocol_profile_id,
    protocol_profile_version,admission,priority,weight,source_candidate_version)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, releaseID, item.Ordinal,
		item.DeploymentID, item.ModelOperationID, item.ExpectedConnectionRevision,
		item.ExpectedDeploymentRevision, item.ExpectedOperationRevision,
		item.ProtocolProfileID, item.ProtocolProfileVersion, item.Admission,
		item.Priority, item.Weight, item.SourceCandidateVersion)
	require.NoError(t, err)
}
