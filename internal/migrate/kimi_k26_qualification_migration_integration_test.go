//go:build integration

package migrate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelCapabilityQualificationKimiK26MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	ctx := t.Context()
	m95Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m95Dir))
	assertM95KimiAuthorityState(t, pool, 6, 6)
	execMigrationDown(t, ctx, pool, "000095_kimi_k26_chat_core_qualification.down.sql")
	assertM95KimiAuthorityState(t, pool, 0, 0)
	require.NoError(t, migrate.Run(ctx, pool, m95Dir))
	assertM95KimiAuthorityState(t, pool, 6, 6)
}

func TestModelCapabilityQualificationKimiK26MaterializesExactTupleAndBlocksUnsafeDownIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	ctx := t.Context()
	fixture := insertM95KimiCapabilityFixture(t, pool, "exact", "protocol.kimi-openai-chat.v5")
	m95Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m95Dir))
	assertM95KimiAuthorityState(t, pool, 6, 6)

	var projections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID).Scan(&projections))
	require.Equal(t, 6, projections)

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.Error(t, err)
	assertM95KimiAuthorityState(t, pool, 6, 6)
	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000095_kimi_k26_chat_core_qualification')`).Scan(&applied))
	require.True(t, applied)
}

func TestModelCapabilityQualificationKimiK26DownRefusesRetainedProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	ctx := t.Context()
	fixture := insertM95KimiCapabilityFixture(t, pool, "retained-probe", "protocol.kimi-openai-chat.v5")
	m95Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m95Dir))

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
        'unirouter.capability-probe.kimi-chat-sse.v1','1.0.0',
        'kimi','https://api.moonshot.cn/v1','openai_compatible','{}',
        'kimi-k2.6',1,1,$4,$5,$6,1,$7,TRUE,TRUE,200,12,
        'kimi-k2.6','','{}','{}','admin.model_capability_probe.v1','m95-retained-probe',
        'sha256:9595959595959595959595959595959595959595959595959595959595959595',$8)
RETURNING capability_probe_id`, fixture.orgID, fixture.deploymentID, fixture.connectionID,
		operationRevision, capabilityRevision, fixture.bindingID, fixture.linkCreated, fixture.userID,
	).Scan(&probeID))
	require.Positive(t, probeID)

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained evidence")

	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000095_kimi_k26_chat_core_qualification')`).Scan(&applied))
	require.True(t, applied)
}

func TestModelCapabilityQualificationKimiK26DoesNotUpgradeGenericAttachmentIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	ctx := t.Context()
	fixture := insertM95KimiCapabilityFixture(t, pool, "generic", "openai.chat_completions")
	m95Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000095_kimi_k26_chat_core_qualification.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m95Dir))

	var projections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID).Scan(&projections))
	require.Zero(t, projections)
	execMigrationDown(t, ctx, pool, "000095_kimi_k26_chat_core_qualification.down.sql")
	assertM95KimiAuthorityState(t, pool, 0, 0)
}

func assertM95KimiAuthorityState(t *testing.T, pool *pgxpool.Pool, wantBindings, wantCurrent int) {
	t.Helper()
	ctx := t.Context()
	var bindings, current int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000095_kimi_k26_chat_core_qualification'`).Scan(&bindings))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_current_probe_contracts
 WHERE execution_profile_id='kimi.k2.6.api'
   AND execution_profile_version='1.7.0'
   AND protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND protocol_profile_version='1.0.0'`).Scan(&current))
	require.Equal(t, wantBindings, bindings)
	require.Equal(t, wantCurrent, current)
}

func insertM95KimiCapabilityFixture(t *testing.T, pool *pgxpool.Pool, suffix, protocolProfileID string) m88CapabilityFixture {
	t.Helper()
	ctx := t.Context()
	fixture := m88CapabilityFixture{}
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, "m95-customer-"+suffix).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id`,
		fixture.orgID, "m95-admin-"+suffix).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,'kimi','ga',TRUE,262144,32768,
        'kimi.k2.6.api','1.7.0') RETURNING id`,
		fixture.orgID, "m95-kimi-k26-"+suffix).Scan(&fixture.modelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'kimi','https://api.moonshot.cn/v1','openai_compatible','{}','enabled',1)
RETURNING id`, "m95-connection-"+suffix).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'kimi-k2.6','enabled',1,262144,32768) RETURNING id`,
		fixture.orgID, fixture.modelID, fixture.connectionID).Scan(&fixture.deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture-encrypted-secret','active',1) RETURNING id`, fixture.connectionID).Scan(&fixture.bindingID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute') RETURNING created_at`,
		fixture.bindingID, fixture.deploymentID, fixture.connectionID).Scan(&fixture.linkCreated))
	fixture.linkCreated = fixture.linkCreated.Round(time.Microsecond)
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,$2,'1.0.0')`,
		fixture.deploymentID, protocolProfileID)
	require.NoError(t, err)
	return fixture
}
