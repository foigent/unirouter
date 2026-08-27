//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

type m88CapabilityFixture struct {
	orgID        int64
	userID       int64
	modelID      int64
	connectionID int64
	deploymentID int64
	bindingID    int64
	linkCreated  time.Time
}

func TestModelCapabilityQualificationEmptyDownAndReapplyIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000087_anthropic_messages_tools_public_binding.up.sql")
	ctx := t.Context()
	exactDeploymentID, mismatchDeploymentID, unsupportedDeploymentID := insertPreM88CapabilityProjectionFixtures(t, pool)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))

	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
               WHERE version='000088_model_capability_execution_qualification')`).Scan(&applied))
	require.True(t, applied)
	var supportState, evidenceState, evidenceRef string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state,evidence_state,evidence_ref
  FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, exactDeploymentID).Scan(&supportState, &evidenceState, &evidenceRef))
	require.Equal(t, "enabled", supportState)
	require.Equal(t, "declared", evidenceState)
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0", evidenceRef)
	var mismatchCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, mismatchDeploymentID).Scan(&mismatchCount))
	require.Zero(t, mismatchCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state,evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, unsupportedDeploymentID).Scan(&supportState, &evidenceRef))
	require.Equal(t, "unsupported", supportState)
	require.Equal(t, "operator-decision", evidenceRef)

	execMigrationDown(t, ctx, pool, "000088_model_capability_execution_qualification.down.sql")
	var tableName *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('model_capability_routing_approvals')::TEXT`).Scan(&tableName))
	require.Nil(t, tableName)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, exactDeploymentID).Scan(&mismatchCount))
	require.Zero(t, mismatchCount, "M88-created declared projection must not survive downgrade to raw-capability routing")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, unsupportedDeploymentID).Scan(&supportState))
	require.Equal(t, "unsupported", supportState, "pre-existing explicit support decision must survive M88 down")

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('model_capability_routing_approvals')::TEXT`).Scan(&tableName))
	require.NotNil(t, tableName)
	require.Equal(t, "model_capability_routing_approvals", *tableName)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state,evidence_state,evidence_ref
  FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, exactDeploymentID).Scan(&supportState, &evidenceState, &evidenceRef))
	require.Equal(t, "enabled", supportState)
	require.Equal(t, "declared", evidenceState)
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0", evidenceRef)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, mismatchDeploymentID).Scan(&mismatchCount))
	require.Zero(t, mismatchCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state,evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND capability_id='streaming'`, unsupportedDeploymentID).Scan(&supportState, &evidenceRef))
	require.Equal(t, "unsupported", supportState)
	require.Equal(t, "operator-decision", evidenceRef)
}

func insertPreM88CapabilityProjectionFixtures(t *testing.T, pool *pgxpool.Pool) (int64, int64, int64) {
	t.Helper()
	ctx := t.Context()
	var orgID, modelID, connectionID, unsupportedConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m88-projection-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,'m88-projection-flash','M88 Projection Flash','deepseek','ga',TRUE,
        393216,393216,'deepseek.v4-flash.api','1.4.0') RETURNING id`, orgID).Scan(&modelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('m88-projection-connection','deepseek','https://api.deepseek.com',
        'openai_compatible','{}','enabled',1) RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('m88-projection-unsupported-connection','deepseek','https://api.deepseek.com',
        'openai_compatible','{}','enabled',1) RETURNING id`).Scan(&unsupportedConnectionID))
	insertDeployment := func(upstream string, targetConnectionID int64) int64 {
		var deploymentID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,$4,'enabled',1,393216,32768) RETURNING id`,
			orgID, modelID, targetConnectionID, upstream).Scan(&deploymentID))
		_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,
        'protocol.deepseek-openai-chat.v4','1.0.0')`, deploymentID)
		require.NoError(t, err)
		return deploymentID
	}
	exactDeploymentID := insertDeployment("deepseek-v4-flash", connectionID)
	mismatchDeploymentID := insertDeployment("deepseek-v4-flash-mismatch", connectionID)
	unsupportedDeploymentID := insertDeployment("deepseek-v4-flash", unsupportedConnectionID)
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','streaming','unsupported','declared','operator-decision',1)`,
		unsupportedDeploymentID)
	require.NoError(t, err)
	return exactDeploymentID, mismatchDeploymentID, unsupportedDeploymentID
}

func TestModelCapabilityQualificationReceiptSelectionAndBoundValidationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := insertM88CapabilityFixture(t, pool, "receipt-selection")

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_capabilities
   SET evidence_state='production_approved',evidence_ref='capability_approval:999'
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, fixture.deploymentID)
	require.ErrorContains(t, err, "lacks a current typed receipt")

	probeA := insertM88CapabilityProbe(t, pool, fixture, 1, "probe-a")
	approvalA := insertM88CapabilityApproval(t, pool, fixture, probeA, "approval-a")
	assertM88ApprovalQualified(t, pool, approvalA, true, "qualified")
	assertM88PreferredApproval(t, pool, fixture.deploymentID, approvalA)

	// Transient credential health is recorded on the source receipt, but it is
	// deliberately not part of capability approval currentness.
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET state_revision=state_revision+1,cooldown_until=NOW()+INTERVAL '2 minutes'
 WHERE id=$1`, fixture.bindingID)
	require.NoError(t, err)
	assertM88ApprovalQualified(t, pool, approvalA, true, "qualified")

	_, err = pool.Exec(ctx, `
	UPDATE provider_credential_bindings
	   SET cooldown_until=NULL,state_revision=state_revision+1 WHERE id=$1`, fixture.bindingID)
	require.NoError(t, err)
	probeB := insertM88CapabilityProbe(t, pool, fixture, 3, "probe-b")
	approvalB := insertM88CapabilityApproval(t, pool, fixture, probeB, "approval-b")

	// New Plans deterministically select B, while a frozen Plan bound to A may
	// still validate A by identity because none of A's stable conditions drifted.
	assertM88PreferredApproval(t, pool, fixture.deploymentID, approvalB)
	assertM88ApprovalQualified(t, pool, approvalA, true, "qualified")
	assertM88ApprovalQualified(t, pool, approvalB, true, "qualified")

	_, err = pool.Exec(ctx, `
	UPDATE model_deployment_capabilities
	   SET support_state='unsupported',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, fixture.deploymentID)
	require.NoError(t, err)
	assertM88ApprovalQualified(t, pool, approvalA, false, "capability_unsupported")
	assertM88ApprovalQualified(t, pool, approvalB, false, "capability_unsupported")
}

func TestModelCapabilityQualificationStableIdentityDriftAndImmutableContractsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := insertM88CapabilityFixture(t, pool, "stable-drift")
	probeID := insertM88CapabilityProbe(t, pool, fixture, 1, "probe")
	approvalID := insertM88CapabilityApproval(t, pool, fixture, probeID, "approval")

	_, err := pool.Exec(ctx, `
UPDATE model_capability_current_probe_contracts
   SET probe_contract_version='9.9.9'
 WHERE operation_id='openai.chat_completions.v1' AND capability_id='streaming'
   AND execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0'`)
	require.ErrorContains(t, err, "migration-owned")

	_, err = pool.Exec(ctx, `
INSERT INTO model_capability_probe_contract_bindings
SELECT * FROM model_capability_probe_contract_bindings WHERE FALSE`)
	require.ErrorContains(t, err, "model capability probe contract bindings are immutable")

	_, err = pool.Exec(ctx, `
INSERT INTO model_capability_current_probe_contracts
SELECT * FROM model_capability_current_probe_contracts WHERE FALSE`)
	require.ErrorContains(t, err, "current model capability probe contracts are migration-owned")

	_, err = pool.Exec(ctx, `
UPDATE model_deployment_operations SET revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	assertM88ApprovalQualified(t, pool, approvalID, false, "operation_stale")
}

func insertM88CapabilityFixture(t *testing.T, pool *pgxpool.Pool, suffix string) m88CapabilityFixture {
	t.Helper()
	ctx := context.Background()
	fixture := m88CapabilityFixture{}
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, "m88-customer-"+suffix).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id`,
		fixture.orgID, "m88-admin-"+suffix).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,'deepseek','ga',TRUE,393216,393216,
        'deepseek.v4-flash.api','1.4.0') RETURNING id`,
		fixture.orgID, "m88-deepseek-flash-"+suffix).Scan(&fixture.modelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'deepseek','https://api.deepseek.com','openai_compatible','{}','enabled',1)
RETURNING id`, "m88-connection-"+suffix).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-flash','enabled',1,393216,32768) RETURNING id`,
		fixture.orgID, fixture.modelID, fixture.connectionID).Scan(&fixture.deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture-encrypted-secret','active',1) RETURNING id`, fixture.connectionID).Scan(&fixture.bindingID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute') RETURNING created_at`,
		fixture.bindingID, fixture.deploymentID, fixture.connectionID).Scan(&fixture.linkCreated))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,
        'protocol.deepseek-openai-chat.v4','1.0.0')`, fixture.deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared','',1)`, fixture.deploymentID)
	require.NoError(t, err)
	return fixture
}

func insertM88CapabilityProbe(t *testing.T, pool *pgxpool.Pool, fixture m88CapabilityFixture, credentialRevision int64, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
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
        'deepseek.v4-flash.api','1.4.0','protocol.deepseek-openai-chat.v4','1.0.0',
        'unirouter.capability-probe.deepseek-chat-sse.v1','1.0.0',
        'deepseek','https://api.deepseek.com','openai_compatible','{}',
        'deepseek-v4-flash',1,1,1,1,$4,$5,$6,TRUE,TRUE,200,12,
        'deepseek-v4-flash','','{}','{}','admin.model_capability_probe.v1',$7,
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$8)
RETURNING capability_probe_id`, fixture.orgID, fixture.deploymentID, fixture.connectionID,
		fixture.bindingID, credentialRevision, fixture.linkCreated, "m88-"+suffix, fixture.userID).Scan(&probeID))
	return probeID
}

func insertM88CapabilityApproval(t *testing.T, pool *pgxpool.Pool, fixture m88CapabilityFixture, probeID int64, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var approvalID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_capability_routing_approvals (
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
       credential_state_revision,credential_link_created_at,$2,$3,$4,
       'admin.model_capability_routing_approval.v1','capability_probe:' || capability_probe_id::TEXT
  FROM model_capability_probes WHERE capability_probe_id=$1
RETURNING approval_id`, probeID, fixture.userID, fixture.orgID, "approved "+suffix).Scan(&approvalID))
	return approvalID
}

func assertM88ApprovalQualified(t *testing.T, pool *pgxpool.Pool, approvalID int64, want bool, state string) {
	t.Helper()
	var qualified bool
	var gotState string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT qualified,qualification_state
  FROM model_capability_approval_qualifications WHERE approval_id=$1`, approvalID).Scan(&qualified, &gotState))
	require.Equal(t, want, qualified)
	require.Equal(t, state, gotState)
}

func assertM88PreferredApproval(t *testing.T, pool *pgxpool.Pool, deploymentID, wantApprovalID int64) {
	t.Helper()
	var got int64
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT approval_id FROM model_capability_execution_qualifications
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, deploymentID).Scan(&got))
	require.Equal(t, wantApprovalID, got, fmt.Sprintf("preferred approval for deployment %d", deploymentID))
}
