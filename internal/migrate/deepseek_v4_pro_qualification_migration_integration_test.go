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

func TestDeepSeekV4ProQualificationMigrationLifecycleAndProjectionIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000093_deepseek_function_tool_probe_contract_v2.up.sql")
	ctx := t.Context()
	fixture := insertM94ProCapabilityFixture(t, pool, "lifecycle")
	m94Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000094_deepseek_v4_pro_chat_core_qualification.up.sql")

	assertState := func(wantBindings, wantCurrent, wantProjection int) {
		t.Helper()
		var bindings, current, projection int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000094_deepseek_v4_pro_chat_core_qualification'
   AND execution_profile_id='deepseek.v4-pro.api'
   AND execution_profile_version='1.5.0'
   AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND protocol_profile_version='1.0.0'`).Scan(&bindings))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_current_probe_contracts
 WHERE execution_profile_id='deepseek.v4-pro.api'
   AND execution_profile_version='1.5.0'
   AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND protocol_profile_version='1.0.0'`).Scan(&current))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID).Scan(&projection))
		require.Equal(t, wantBindings, bindings)
		require.Equal(t, wantCurrent, current)
		require.Equal(t, wantProjection, projection)
	}

	require.NoError(t, migrate.Run(ctx, pool, m94Dir))
	assertState(9, 9, 9)
	var toolsRef string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='tools'`, fixture.deploymentID).Scan(&toolsRef))
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0", toolsRef)

	execMigrationDown(t, ctx, pool, "000094_deepseek_v4_pro_chat_core_qualification.down.sql")
	assertState(0, 0, 0)
	require.NoError(t, migrate.Run(ctx, pool, m94Dir))
	assertState(9, 9, 9)
}

func TestDeepSeekV4ProQualificationDownRefusesRetainedProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000093_deepseek_function_tool_probe_contract_v2.up.sql")
	ctx := t.Context()
	fixture := insertM94ProCapabilityFixture(t, pool, "retained-probe")
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

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
VALUES ($1,$2,$3,'openai.chat_completions.v1','tools',
        'deepseek.v4-pro.api','1.5.0','protocol.deepseek-openai-chat.v4','1.0.0',
        'unirouter.capability-probe.deepseek-chat-function-tools.v1','2.0.0',
        'deepseek','https://api.deepseek.com','openai_compatible','{}',
        'deepseek-v4-pro',1,1,1,1,$4,1,$5,TRUE,TRUE,200,12,
        'deepseek-v4-pro','','{}','{}','admin.model_capability_probe.v1','m94-retained-probe',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$6)
RETURNING capability_probe_id`, fixture.orgID, fixture.deploymentID, fixture.connectionID,
		fixture.bindingID, fixture.linkCreated, fixture.userID).Scan(&probeID))
	require.Positive(t, probeID)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000094_deepseek_v4_pro_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.Error(t, err)

	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000094_deepseek_v4_pro_chat_core_qualification')`).Scan(&applied))
	require.True(t, applied)
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 9, 9, 9)
}

func TestDeepSeekV4ProQualificationDownPreservesForeignCapabilityRowsIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000093_deepseek_function_tool_probe_contract_v2.up.sql")
	ctx := t.Context()
	fixture := insertM94ProCapabilityFixture(t, pool, "foreign-projection")
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision
) VALUES
  ($1,'openai.chat_completions.v1','tools','enabled','declared','fixture:pre-m94-tools',1),
  ($1,'openai.chat_completions.v1','fixture_unrelated','unsupported','declared','fixture:unrelated',1)`,
		fixture.deploymentID)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 9, 9, 10)
	execMigrationDown(t, ctx, pool, "000094_deepseek_v4_pro_chat_core_qualification.down.sql")
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 0, 0, 2)

	rows, err := pool.Query(ctx, `
SELECT capability_id,support_state,evidence_ref
  FROM model_deployment_capabilities
 WHERE deployment_id=$1
 ORDER BY capability_id`, fixture.deploymentID)
	require.NoError(t, err)
	defer rows.Close()
	got := make([]string, 0, 2)
	for rows.Next() {
		var capabilityID, supportState, evidenceRef string
		require.NoError(t, rows.Scan(&capabilityID, &supportState, &evidenceRef))
		got = append(got, capabilityID+":"+supportState+":"+evidenceRef)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"fixture_unrelated:unsupported:fixture:unrelated",
		"tools:enabled:fixture:pre-m94-tools",
	}, got)
}

func TestDeepSeekV4ProQualificationDownIgnoresFlashSharedContractProjectionIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000093_deepseek_function_tool_probe_contract_v2.up.sql")
	ctx := t.Context()
	fixture := insertM94ProCapabilityFixture(t, pool, "flash-coexistence")
	var flashModelID, flashDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,'m94-flash-coexistence','m94-flash-coexistence','deepseek','ga',TRUE,
        1048576,384000,'deepseek.v4-flash.api','1.5.0') RETURNING id`,
		fixture.orgID).Scan(&flashModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-flash','enabled',1,1048576,32768) RETURNING id`,
		fixture.orgID, flashModelID, fixture.connectionID).Scan(&flashDeploymentID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,
	    'protocol.deepseek-openai-chat.v4','1.0.0')`, flashDeploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision
) VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared',
          'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',1)`,
		flashDeploymentID)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 9, 9, 9)
	execMigrationDown(t, ctx, pool, "000094_deepseek_v4_pro_chat_core_qualification.down.sql")
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 0, 0, 0)
	var flashRef string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, flashDeploymentID).Scan(&flashRef))
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0", flashRef)
}

func TestDeepSeekV4ProQualificationDownRejectsProjectionIdentityDriftAtomicallyIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000093_deepseek_function_tool_probe_contract_v2.up.sql")
	ctx := t.Context()
	fixture := insertM94ProCapabilityFixture(t, pool, "identity-drift")
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 9, 9, 9)

	_, err := pool.Exec(ctx, `
UPDATE model_registry SET execution_profile_version='1.4.0' WHERE id=$1`, fixture.modelID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000094_deepseek_v4_pro_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.Error(t, err)
	assertM94ProAuthorityState(t, pool, fixture.deploymentID, 9, 9, 9)
	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000094_deepseek_v4_pro_chat_core_qualification')`).Scan(&applied))
	require.True(t, applied)
}

func assertM94ProAuthorityState(
	t *testing.T, pool *pgxpool.Pool, deploymentID int64,
	wantBindings, wantCurrent, wantProjections int,
) {
	t.Helper()
	ctx := t.Context()
	var bindings, current, projections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probe_contract_bindings
 WHERE introduced_migration='000094_deepseek_v4_pro_chat_core_qualification'`).Scan(&bindings))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_current_probe_contracts
 WHERE execution_profile_id='deepseek.v4-pro.api'
   AND execution_profile_version='1.5.0'
   AND protocol_profile_id='protocol.deepseek-openai-chat.v4'
   AND protocol_profile_version='1.0.0'`).Scan(&current))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, deploymentID).Scan(&projections))
	require.Equal(t, wantBindings, bindings)
	require.Equal(t, wantCurrent, current)
	require.Equal(t, wantProjections, projections)
}

func insertM94ProCapabilityFixture(t *testing.T, pool *pgxpool.Pool, suffix string) m88CapabilityFixture {
	t.Helper()
	ctx := t.Context()
	fixture := m88CapabilityFixture{}
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, "m94-customer-"+suffix).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id`,
		fixture.orgID, "m94-admin-"+suffix).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,'deepseek','ga',TRUE,1048576,384000,
        'deepseek.v4-pro.api','1.5.0') RETURNING id`,
		fixture.orgID, "m94-deepseek-pro-"+suffix).Scan(&fixture.modelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'deepseek','https://api.deepseek.com','openai_compatible','{}','enabled',1)
RETURNING id`, "m94-connection-"+suffix).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-pro','enabled',1,1048576,32768) RETURNING id`,
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
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,
        'protocol.deepseek-openai-chat.v4','1.0.0')`, fixture.deploymentID)
	require.NoError(t, err)
	return fixture
}
