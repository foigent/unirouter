//go:build integration

package integrationfixture

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type ModelCapabilityQualification struct {
	OrgID                  int64
	UserID                 int64
	ModelID                int64
	ConnectionID           int64
	DeploymentID           int64
	BindingID              int64
	LinkCreated            time.Time
	ProtocolProfileID      string
	ProtocolProfileVersion string
}

func InsertModelCapabilityQualification(t *testing.T, pool *pgxpool.Pool, suffix string) ModelCapabilityQualification {
	return InsertModelCapabilityQualificationWithProtocol(
		t, pool, suffix, "protocol.deepseek-openai-chat.v4", "1.0.0",
	)
}

func InsertModelCapabilityQualificationWithProtocol(
	t *testing.T,
	pool *pgxpool.Pool,
	suffix, protocolProfileID, protocolProfileVersion string,
) ModelCapabilityQualification {
	t.Helper()
	ctx := context.Background()
	fixture := ModelCapabilityQualification{
		ProtocolProfileID:      protocolProfileID,
		ProtocolProfileVersion: protocolProfileVersion,
	}
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, "capability-customer-"+suffix).Scan(&fixture.OrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id`,
		fixture.OrgID, "capability-admin-"+suffix).Scan(&fixture.UserID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,'deepseek','ga',TRUE,393216,393216,
        'deepseek.v4-flash.api','1.4.0') RETURNING id`,
		fixture.OrgID, "capability-deepseek-flash-"+suffix).Scan(&fixture.ModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'deepseek','https://api.deepseek.com','openai_compatible',
        '{"endpoint_path_mode":"base_relative"}','enabled',1)
RETURNING id`, "capability-connection-"+suffix).Scan(&fixture.ConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-flash','enabled',1,393216,32768) RETURNING id`,
		fixture.OrgID, fixture.ModelID, fixture.ConnectionID).Scan(&fixture.DeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'fixture-encrypted-secret','active',1) RETURNING id`, fixture.ConnectionID).Scan(&fixture.BindingID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute') RETURNING created_at`,
		fixture.BindingID, fixture.DeploymentID, fixture.ConnectionID).Scan(&fixture.LinkCreated))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','production_approved','integration',1,
		$2,$3)`, fixture.DeploymentID, fixture.ProtocolProfileID, fixture.ProtocolProfileVersion)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared',
		'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',1)`, fixture.DeploymentID)
	require.NoError(t, err)
	return fixture
}

func AppendModelCapabilityProbeApproval(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture ModelCapabilityQualification,
	credentialRevision int64,
	suffix string,
) (probeID, approvalID int64, approvedAt time.Time) {
	t.Helper()
	ctx := context.Background()
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
		'deepseek.v4-flash.api','1.4.0',$4,$5,
		'unirouter.capability-probe.deepseek-chat-sse.v1','2.0.0',
		'deepseek','https://api.deepseek.com','openai_compatible',
		'{"endpoint_path_mode":"base_relative"}',
		'deepseek-v4-flash',1,1,1,1,$6,$7,$8,TRUE,TRUE,200,12,
		'deepseek-v4-flash','','{}','{}','admin.model_capability_probe.v1',$9,
		$10,$11)
RETURNING capability_probe_id`, fixture.OrgID, fixture.DeploymentID, fixture.ConnectionID,
		fixture.ProtocolProfileID, fixture.ProtocolProfileVersion,
		fixture.BindingID, credentialRevision, fixture.LinkCreated, "capability-"+suffix,
		fmt.Sprintf("sha256:%064x", probeFingerprintSequence.Add(1)), fixture.UserID).Scan(&probeID))
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
RETURNING approval_id,approved_at`, probeID, fixture.UserID, fixture.OrgID, "approved "+suffix).Scan(&approvalID, &approvedAt))
	return probeID, approvalID, approvedAt
}

var probeFingerprintSequence atomic.Uint64
