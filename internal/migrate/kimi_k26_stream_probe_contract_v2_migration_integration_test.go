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

const kimiK26SSEProbeContractID = "unirouter.capability-probe.kimi-chat-sse.v1"

func TestModelCapabilityQualificationKimiK26StreamProbeContractV2MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v2-lifecycle", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m96Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m96Dir))
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
	execMigrationDown(t, ctx, pool, "000096_kimi_k26_stream_probe_contract_v2.down.sql")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "1.0.0", 1)
	require.NoError(t, migrate.Run(ctx, pool, m96Dir))
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV2DownAllowsHistoricalFailedV1ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v1-failure", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m95Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m95Dir))
	insertKimiK26StreamProbeFixture(t, pool, fixture, "1.0.0", false, "m96-historical-v1-failure")

	m96Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m96Dir))
	execMigrationDown(t, ctx, pool, "000096_kimi_k26_stream_probe_contract_v2.down.sql")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "1.0.0", 1)

	var retained int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probes
 WHERE execution_profile_id='kimi.k2.6.api'
   AND capability_id='streaming'
   AND probe_contract_version='1.0.0'`).Scan(&retained))
	require.Equal(t, 1, retained)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV2DownRefusesRetainedV2ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v2-retained", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m96Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m96Dir))
	insertKimiK26StreamProbeFixture(t, pool, fixture, "2.0.0", true, "m96-retained-v2")

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained v2 qualification evidence")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV2DownRefusesProjectionIdentityDriftIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v2-drift", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m96Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m96Dir))

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_id='unverified.chat',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "projection identity drift")

	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000096_kimi_k26_stream_probe_contract_v2')`).Scan(&applied))
	require.True(t, applied)
}

func assertKimiK26StreamProbeContractState(
	t *testing.T, pool *pgxpool.Pool, deploymentID int64, wantVersion string, wantRevision int64,
) {
	t.Helper()
	ctx := t.Context()
	var currentVersion, evidenceRef string
	var revision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT probe_contract_version
  FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'
   AND execution_profile_id='kimi.k2.6.api'
   AND execution_profile_version='1.7.0'
   AND protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id=$1`, kimiK26SSEProbeContractID).Scan(&currentVersion))
	require.Equal(t, wantVersion, currentVersion)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref,revision
  FROM model_deployment_capabilities
 WHERE deployment_id=$1
   AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, deploymentID).Scan(&evidenceRef, &revision))
	require.Equal(t, "capability_contract:"+kimiK26SSEProbeContractID+"@"+wantVersion, evidenceRef)
	require.Equal(t, wantRevision, revision)
}

func insertKimiK26StreamProbeFixture(
	t *testing.T, pool *pgxpool.Pool, fixture m88CapabilityFixture,
	contractVersion string, success bool, sourceRef string,
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
   AND capability.capability_id='streaming'
 WHERE operation.deployment_id=$1
   AND operation.operation_id='openai.chat_completions.v1'`, fixture.deploymentID).Scan(
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
VALUES ($1,$2,$3,'openai.chat_completions.v1','streaming',
        'kimi.k2.6.api','1.7.0','protocol.kimi-openai-chat.v5','1.0.0',
        $4,$5,'kimi','https://api.moonshot.cn/v1','openai_compatible','{}',
        'kimi-k2.6',1,1,$6,$7,$8,1,$9,TRUE,$10,200,12,
        'kimi-k2.6',CASE WHEN $10 THEN '' ELSE 'provider_stream_contract_invalid' END,'{}','{}',
        'admin.model_capability_probe.v1',$11,
        'sha256:' || repeat(CASE WHEN $10 THEN '6' ELSE '5' END,64),$12)
RETURNING capability_probe_id`, fixture.orgID, fixture.deploymentID, fixture.connectionID,
		kimiK26SSEProbeContractID, contractVersion, operationRevision, capabilityRevision,
		fixture.bindingID, fixture.linkCreated, success, sourceRef, fixture.userID,
	).Scan(&probeID))
	require.Positive(t, probeID)
	return probeID
}
