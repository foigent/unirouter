//go:build integration

package modelcapabilitypublication

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testdb"
)

func TestOpenAIChatRuntimeCapabilityM110ActualArtifactsRoundTripWithExistingProfilesIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.LatestSupportedMigration, artifactLedger[len(artifactLedger)-1])
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("7", 40), Tree: strings.Repeat("8", 40),
		Version: "openai-chat-runtime-capability-m110-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("9", 64),
	}
	applyPublication := func(publicationID string) {
		t.Helper()
		declaration, predecessor, compileErr := CompiledPublication(publicationID)
		require.NoError(t, compileErr)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, planErr := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, planErr)
		require.Equal(t, ValidationValid, plan.Validation)
		hooks, hooksErr := HooksForPlan("integration", authority, plan)
		require.NoError(t, hooksErr)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification",
		"000104_minimax_payg_function_tools_qualification",
		"000105_glm_toggle_function_tools_qualification",
		"000106_minimax_official_cn_payg_endpoint_qualification",
	} {
		applyPublication(publicationID)
	}
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, "000107_stable_model_successor_cutover"))
	applyPublication("000108_minimax_cache_threshold_qualification")
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, "000109_staging_model_successor_cutover"))

	models := []struct {
		profileID, version, protocolID, upstreamModel, provider, contractID string
	}{
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v9", "MiniMax-M3", "minimax", "unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1"},
		{"minimax.m2.7.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v7", "MiniMax-M2.7", "minimax", "unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1"},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v7", "MiniMax-M2.7-highspeed", "minimax", "unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1"},
		{"glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5", "glm-5.3", "glm", "unirouter.capability-probe.glm-chat-function-tools.v1"},
		{"glm.5.2.api", "1.8.0", "protocol.glm-openai-chat.v6", "glm-5.2", "glm", "unirouter.capability-probe.glm-toggle-chat-function-tools.v1"},
		{"glm.5.1.api", "1.0.0", "protocol.glm-openai-chat.v6", "glm-5.1", "glm", "unirouter.capability-probe.glm-toggle-chat-function-tools.v1"},
		{"glm.5-turbo.api", "1.0.0", "protocol.glm-openai-chat.v6", "glm-5-turbo", "glm", "unirouter.capability-probe.glm-toggle-chat-function-tools.v1"},
		{"glm.5.api", "1.0.0", "protocol.glm-openai-chat.v6", "glm-5", "glm", "unirouter.capability-probe.glm-toggle-chat-function-tools.v1"},
		{"glm.5v-turbo.api", "1.11.0", "protocol.glm-openai-chat.glm5v.v5", "glm-5v-turbo", "glm", "unirouter.capability-probe.glm-toggle-chat-function-tools.v1"},
	}
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES ('m110-runtime-capability','active','customer') RETURNING id`).Scan(&orgID))
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users(org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m110-runtime-capability-admin','fixture','platform_admin','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	var retainedModelIDs []int64
	var evidenceConnectionID, evidenceDeploymentID int64
	for index, model := range models {
		var modelID, connectionID, deploymentID int64
		name := fmt.Sprintf("m110-model-%02d", index)
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,$3,'staging',FALSE,204800,65536,$4,$5) RETURNING id`,
			orgID, name, model.provider, model.profileID, model.version).Scan(&modelID))
		retainedModelIDs = append(retainedModelIDs, modelID)
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections(name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,$2,'https://provider.invalid/v1','openai_compatible','{}','enabled',1) RETURNING id`,
			"m110-connection-"+name, model.provider).Scan(&connectionID))
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments(customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,$4,'enabled',1,204800,65536) RETURNING id`,
			orgID, modelID, connectionID, model.upstreamModel).Scan(&deploymentID))
		if index == 0 {
			evidenceConnectionID = connectionID
			evidenceDeploymentID = deploymentID
		}
		_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations(deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,$2,'1.0.0')`, deploymentID, model.protocolID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities(deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision,support_revision)
VALUES ($1,'openai.chat_completions.v1','function_tools','enabled','declared',$2,1,1)`,
			deploymentID, "capability_contract:"+model.contractID+"@1.0.0")
		require.NoError(t, err)
	}
	var evidenceBindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings(connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture-encrypted-secret','active',1) RETURNING id`, evidenceConnectionID).Scan(&evidenceBindingID))
	var evidenceLinkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute') RETURNING created_at`,
		evidenceBindingID, evidenceDeploymentID, evidenceConnectionID).Scan(&evidenceLinkedAt))

	declaration, predecessor, err := CompiledPublication("000110_openai_chat_runtime_capability_qualification")
	require.NoError(t, err)
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationValid, plan.Validation)
	require.Len(t, plan.ProjectionItems, 42)
	var inserts, updates int
	for _, item := range plan.ProjectionItems {
		switch item.Action {
		case ProjectionActionInsert:
			inserts++
		case ProjectionActionUpdate:
			updates++
		}
	}
	require.Equal(t, 33, inserts)
	require.Equal(t, 9, updates)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))

	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks),
		"existing Profile attachments alone must not block M110 rollback before dynamic evidence")
	var retainedModels, m109Count int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_registry WHERE id=ANY($1)`, retainedModelIDs).Scan(&retainedModels))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000109_staging_model_successor_cutover'`).Scan(&m109Count))
	require.EqualValues(t, 9, retainedModels)
	require.EqualValues(t, 1, m109Count)

	hooks, err = HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))

	_, err = pool.Exec(ctx, `
INSERT INTO model_capability_probes (
  customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,provider,base_url,adapter_type,adapter_config,
  upstream_model,connection_revision,deployment_revision,operation_revision,
  capability_support_revision,credential_binding_id,credential_state_revision,
  credential_link_created_at,provider_success,success,status_code,latency_ms,
  reported_model,error_summary,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by)
VALUES ($1,$2,$3,'openai.chat_completions.v1','function_tools',
  'minimax.m3.payg','1.2.0','protocol.minimax-m3-openai-chat.v9','1.0.0',
  'unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1','2.0.0',
  'minimax','https://provider.invalid/v1','openai_compatible','{}','MiniMax-M3',
  1,1,1,1,$4,1,$5,TRUE,TRUE,200,1,'MiniMax-M3','','{}','{}',
  'admin.model_capability_probe.v1','m110-retained-evidence',
  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$6)`,
		orgID, evidenceDeploymentID, evidenceConnectionID, evidenceBindingID, evidenceLinkedAt, userID)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks),
		"retained Probe/Approval prevents publication down")
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan), "failed guarded down must leave M110 intact")
}
