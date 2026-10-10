//go:build integration

package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

const kimiK26ReasoningProbeContractID = "unirouter.capability-probe.kimi-chat-reasoning.v1"

func TestModelCapabilityQualificationKimiK26NonStreamProbeContractV2MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "nonstream-v2-lifecycle", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m98Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m98Dir))
	assertKimiK26NonStreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
	execMigrationDown(t, ctx, pool, "000098_kimi_k26_nonstream_usage_probe_v2.down.sql")
	assertKimiK26NonStreamProbeContractState(t, pool, fixture.deploymentID, "1.0.0", 1)
	require.NoError(t, migrate.Run(ctx, pool, m98Dir))
	assertKimiK26NonStreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
}

func TestModelCapabilityQualificationKimiK26NonStreamProbeContractV2DownAllowsHistoricalFailedV1ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "nonstream-v1-failure", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m97Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m97Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "reasoning", kimiK26ReasoningProbeContractID, "1.0.0", false, "m98-historical-v1-failure")

	m98Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m98Dir))
	execMigrationDown(t, ctx, pool, "000098_kimi_k26_nonstream_usage_probe_v2.down.sql")
	assertKimiK26NonStreamProbeContractState(t, pool, fixture.deploymentID, "1.0.0", 1)

	var retained int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probes
 WHERE execution_profile_id='kimi.k2.6.api'
   AND capability_id='reasoning'
   AND probe_contract_version='1.0.0'`).Scan(&retained))
	require.Equal(t, 1, retained)
}

func TestModelCapabilityQualificationKimiK26NonStreamProbeContractV2DownRefusesRetainedV2ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "nonstream-v2-retained", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m98Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m98Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "reasoning", kimiK26ReasoningProbeContractID, "2.0.0", true, "m98-retained-v2")

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained v2 qualification evidence")
	assertKimiK26NonStreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
}

func TestModelCapabilityQualificationKimiK26NonStreamProbeContractV2DownRefusesProjectionIdentityDriftIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "nonstream-v2-drift", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m98Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m98Dir))

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_id='unverified.chat',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "projection identity drift")
}

func assertKimiK26NonStreamProbeContractState(
	t *testing.T, pool *pgxpool.Pool, deploymentID int64, wantVersion string, wantRevision int64,
) {
	t.Helper()
	ctx := t.Context()
	var current int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id IN ('reasoning','tools','function_tools','tool_choice_auto','json_object')
   AND execution_profile_id='kimi.k2.6.api'
   AND execution_profile_version='1.7.0'
   AND protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND protocol_profile_version='1.0.0'
   AND probe_contract_version=$1`, wantVersion).Scan(&current))
	require.Equal(t, 5, current)

	var projections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1
   AND operation_id='openai.chat_completions.v1'
   AND capability_id IN ('reasoning','tools','function_tools','tool_choice_auto','json_object')
   AND evidence_ref LIKE 'capability_contract:%@' || $2
   AND revision=$3`, deploymentID, wantVersion, wantRevision).Scan(&projections))
	require.Equal(t, 5, projections)
}

func insertKimiK26CapabilityProbeFixture(
	t *testing.T, pool *pgxpool.Pool, fixture m88CapabilityFixture,
	capabilityID, contractID, contractVersion string, success bool, sourceRef string,
) int64 {
	t.Helper()
	ctx := t.Context()
	var operationRevision, capabilityRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT operation.revision,capability.support_revision
  FROM model_deployment_operations operation
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=operation.deployment_id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id=$2
 WHERE operation.deployment_id=$1
   AND operation.operation_id='openai.chat_completions.v1'`, fixture.deploymentID, capabilityID).Scan(
		&operationRevision, &capabilityRevision,
	))
	var probeID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_capability_probes (
  customer_org_id,deployment_id,connection_id,operation_id,capability_id,
  execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,
  probe_contract_id,probe_contract_version,provider,base_url,adapter_type,adapter_config,
  upstream_model,connection_revision,deployment_revision,operation_revision,
  capability_support_revision,credential_binding_id,credential_state_revision,
  credential_link_created_at,provider_success,success,status_code,latency_ms,
  reported_model,error_summary,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by)
VALUES ($1,$2,$3,'openai.chat_completions.v1',$4,
        'kimi.k2.6.api','1.7.0','protocol.kimi-openai-chat.v5','1.0.0',
        $5,$6,'kimi','https://api.moonshot.cn/v1','openai_compatible','{}',
        'kimi-k2.6',1,1,$7,$8,$9,1,$10,TRUE,$11,200,12,
        'kimi-k2.6',CASE WHEN $11 THEN '' ELSE 'provider_reasoning_contract_invalid' END,'{}','{}',
        'admin.model_capability_probe.v1',$12,
        'sha256:' || repeat(CASE WHEN $11 THEN '8' ELSE '7' END,64),$13)
RETURNING capability_probe_id`, fixture.orgID, fixture.deploymentID, fixture.connectionID,
		capabilityID, contractID, contractVersion, operationRevision, capabilityRevision,
		fixture.bindingID, fixture.linkCreated, success, sourceRef, fixture.userID,
	).Scan(&probeID))
	require.Positive(t, probeID)
	return probeID
}
