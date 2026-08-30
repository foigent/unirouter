//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalModelApplicationQualificationOriginGatePrecedesAllWritesIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-origin-customer','active','customer')
RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'qualification-origin-admin','fixture','org_admin','org_admin','active')
RETURNING id`, orgID).Scan(&userID))
	handler := NewHandler(pool, "qualification-origin-jwt", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(orgID, userID)

	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Pathful Responses", Provider: "openai", BaseURL: "https://api.example.com/v1",
			AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{ModelName: "pathful-responses-model"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "pathful-responses-upstream"},
		Credential:  canonicalCredentialInput{APIKey: "pathful-responses-secret"},
		Operation:   canonicalOperationInput{OperationID: string(modeloperation.IDResponses)},
	}
	createCanonicalApplicationForTest(t, handler, authorized, request, http.StatusBadRequest)

	var pathfulConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state)
VALUES ('Existing pathful','openai','https://existing.example.com/v1','openai_compatible','{}','enabled')
RETURNING id`).Scan(&pathfulConnectionID))
	existing := request
	existing.Connection = canonicalConnectionInput{ID: pathfulConnectionID}
	existing.PublicModel.ModelName = "existing-pathful-responses-model"
	existing.Deployment.UpstreamModel = "existing-pathful-responses-upstream"
	createCanonicalApplicationForTest(t, handler, authorized, existing, http.StatusConflict)

	for query, args := range map[string][]any{
		`SELECT COUNT(*) FROM provider_connections WHERE name='Pathful Responses'`:             nil,
		`SELECT COUNT(*) FROM model_registry WHERE org_id=$1`:                                  {orgID},
		`SELECT COUNT(*) FROM model_deployments WHERE customer_org_id=$1`:                      {orgID},
		`SELECT COUNT(*) FROM provider_credential_bindings WHERE connection_id=$1`:             {pathfulConnectionID},
		`SELECT COUNT(*) FROM model_deployment_create_receipts WHERE customer_org_id=$1`:       {orgID},
		`SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create' AND org_id=$1`: {orgID},
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&count), query)
		require.Zero(t, count, query)
	}

	pathFree := request
	pathFree.Connection.Name = "Path-free Responses"
	pathFree.Connection.BaseURL = "https://api.example.com"
	pathFree.PublicModel.ModelName = "path-free-responses-model"
	pathFree.Deployment.UpstreamModel = "path-free-responses-upstream"
	created := createCanonicalApplicationForTest(t, handler, authorized, pathFree, http.StatusCreated)
	require.Equal(t, string(modeloperation.IDResponses), created.Application.Operations[0].OperationID)
	require.Equal(t, "https://api.example.com", created.Application.Connection.BaseURL)
}

func TestCanonicalModelApplicationCurrentCapabilityRollsBackThroughM92Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	m99Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m99Body))
	require.NoError(t, err)
	m98Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m98Body))
	require.NoError(t, err)
	m97Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m97Body))
	require.NoError(t, err)
	m96Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m96Body))
	require.NoError(t, err)
	m95Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000095_kimi_k26_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m95Body))
	require.NoError(t, err)
	m94Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000094_deepseek_v4_pro_chat_core_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m94Body))
	require.NoError(t, err)
	m93Body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000093_deepseek_function_tool_probe_contract_v2.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(m93Body))
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000092_deepseek_reasoning_tool_choice_contract.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(body))
	require.NoError(t, err)
	var migrationCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
	 WHERE version='000092_deepseek_reasoning_tool_choice_contract'`).Scan(&migrationCount))
	require.Zero(t, migrationCount)
	m92OnlyDir := t.TempDir()
	m92Up, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000092_deepseek_reasoning_tool_choice_contract.up.sql"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(m92OnlyDir, "000092_deepseek_reasoning_tool_choice_contract.up.sql"), m92Up, 0o600))
	require.NoError(t, migrate.Run(ctx, pool, m92OnlyDir))

	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('reviewed-binding-rollback-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users
 (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'reviewed-binding-rollback-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&userID))
	var proModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,
  execution_profile_id,execution_profile_version
) VALUES ($1,'reviewed-deepseek-pro-rollback','Reviewed DeepSeek Pro rollback','deepseek','staging',
          'deepseek.v4-pro.api','1.5.0')
RETURNING id`, orgID).Scan(&proModelID))
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(body))
	require.ErrorContains(t, err, "cannot remove M92 with retained DeepSeek 1.5 model attachment")
	_, rollbackErr := conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	conn.Release()
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_registry
 WHERE id=$1 AND execution_profile_id='deepseek.v4-pro.api'
   AND execution_profile_version='1.5.0'`, proModelID).Scan(&migrationCount))
	require.Equal(t, 1, migrationCount)
	_, err = pool.Exec(ctx, `
UPDATE model_registry
   SET execution_profile_version='1.4.0'
 WHERE id=$1`, proModelID)
	require.NoError(t, err)

	var modelID, connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,'reviewed-deepseek-rollback','Reviewed DeepSeek rollback','deepseek','staging',TRUE,
        393216,384000,'deepseek.v4-flash.api','1.5.0') RETURNING id`, orgID).Scan(&modelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('Reviewed DeepSeek rollback','deepseek','https://api.deepseek.com',
        'openai_compatible','{"endpoint_path_mode":"base_relative"}','enabled',1)
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-flash','enabled',1,393216,32768) RETURNING id`,
		orgID, modelID, connectionID).Scan(&deploymentID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,
        'protocol.deepseek-openai-chat.v4','1.0.0')`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared',
        'capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0',1)`, deploymentID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'reviewed-binding-rollback-fixture','active',1) RETURNING id`, connectionID).Scan(&bindingID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, bindingID, deploymentID, connectionID)
	require.NoError(t, err)
	assertCapability := func(wantRef string, wantRevision int64) {
		t.Helper()
		var evidenceRef string
		var revision int64
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref,revision FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, deploymentID).Scan(&evidenceRef, &revision))
		require.Equal(t, wantRef, evidenceRef)
		require.Equal(t, wantRevision, revision)
	}
	assertCapability("capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0", 1)

	conn, err = pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(body))
	require.ErrorContains(t, err, "cannot remove M92 with retained DeepSeek 1.5 model attachment")
	_, rollbackErr = conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	conn.Release()
	assertCapability("capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0", 1)

	// Retain one exact Profile 1.5 capability probe. The evidence-specific
	// guard precedes the attachment guard so the operator sees the strongest
	// irreversible downgrade reason.
	var connectionRevision, deploymentRevision, operationRevision, capabilityRevision int64
	var bindingRevision int64
	var linkCreated time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision,operation.revision,
       capability.support_revision,binding.state_revision,link.created_at
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id
   AND operation.operation_id='openai.chat_completions.v1'
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=deployment.id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id='streaming'
  JOIN provider_credential_bindings binding
    ON binding.id=$2 AND binding.connection_id=connection.id
  JOIN credential_binding_deployments link
    ON link.binding_id=binding.id AND link.deployment_id=deployment.id
 WHERE deployment.id=$1`, deploymentID, bindingID).Scan(
		&connectionRevision, &deploymentRevision, &operationRevision,
		&capabilityRevision, &bindingRevision, &linkCreated,
	))
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
VALUES ($1,$2,$3,'openai.chat_completions.v1','streaming',
        'deepseek.v4-flash.api','1.5.0','protocol.deepseek-openai-chat.v4','1.0.0',
        'unirouter.capability-probe.deepseek-chat-sse.v1','2.0.0',
        'deepseek','https://api.deepseek.com','openai_compatible','{"endpoint_path_mode":"base_relative"}',
        'deepseek-v4-flash',$4,$5,$6,$7,$8,$9,$10,TRUE,TRUE,200,12,
        'deepseek-v4-flash','','{}','{}','admin.model_capability_probe.v1','m92-retained',
        'sha256:9292929292929292929292929292929292929292929292929292929292929292',$11)`,
		orgID, deploymentID, connectionID, connectionRevision, deploymentRevision,
		operationRevision, capabilityRevision, bindingID, bindingRevision, linkCreated, userID,
	)
	require.NoError(t, err)

	conn, err = pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, string(body))
	require.ErrorContains(t, err, "cannot remove M92 with retained DeepSeek 1.5 qualification evidence")
	_, rollbackErr = conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	var probeCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
 WHERE version='000092_deepseek_reasoning_tool_choice_contract'`).Scan(&migrationCount))
	require.Equal(t, 1, migrationCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probes
 WHERE deployment_id=$1 AND execution_profile_version='1.5.0'`, deploymentID).Scan(&probeCount))
	require.Equal(t, 1, probeCount)
}

func TestCanonicalModelApplicationReviewedProfileRejectsCrossProviderReuseIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('reviewed-binding-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users
 (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'reviewed-binding-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&userID))
	handler := NewHandler(pool, "reviewed-binding-jwt", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(orgID, userID)
	request := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Reviewed DeepSeek", Provider: "deepseek", BaseURL: "https://api.deepseek.com",
			AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
		},
		PublicModel: canonicalPublicModelInput{
			ModelName: "reviewed-deepseek", ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.5.0",
		},
		Deployment: canonicalDeploymentInput{UpstreamModel: "deepseek-v4-flash"},
		Credential: canonicalCredentialInput{APIKey: "reviewed-binding-fixture"},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	created := createCanonicalApplicationForTest(t, handler, authorized, request, http.StatusCreated)
	require.Equal(t, "deepseek.v4-flash.api", created.Application.PublicModel.ExecutionProfileID)
	require.Equal(t, "1.5.0", created.Application.PublicModel.ExecutionProfileVersion)
	require.Equal(t, "protocol.deepseek-openai-chat.v4", created.Application.Operations[0].ProtocolProfileID)
	require.Equal(t, "1.0.0", created.Application.Operations[0].ProtocolProfileVersion)
	var declaredCapabilityCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND support_state='enabled' AND evidence_state='declared'`,
		created.Application.Deployment.ID,
	).Scan(&declaredCapabilityCount))
	require.Equal(t, 9, declaredCapabilityCount)
	var capabilitySupport, capabilityEvidence, capabilityEvidenceRef string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT support_state,evidence_state,evidence_ref
  FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='streaming'`, created.Application.Deployment.ID).Scan(
		&capabilitySupport, &capabilityEvidence, &capabilityEvidenceRef,
	))
	require.Equal(t, "enabled", capabilitySupport)
	require.Equal(t, "declared", capabilityEvidence)
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@2.0.0", capabilityEvidenceRef)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='tools'`, created.Application.Deployment.ID).Scan(&capabilityEvidenceRef))
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0", capabilityEvidenceRef)

	proRequest := request
	proRequest.Connection = canonicalConnectionInput{ID: created.Application.Connection.ID}
	proRequest.PublicModel = canonicalPublicModelInput{
		ModelName: "reviewed-deepseek-pro", ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
	}
	proRequest.Deployment = canonicalDeploymentInput{UpstreamModel: "deepseek-v4-pro"}
	proRequest.Credential = canonicalCredentialInput{BindingID: created.Application.Credential.Bindings[0].ID}
	pro := createCanonicalApplicationForTest(t, handler, authorized, proRequest, http.StatusCreated)
	require.Equal(t, "protocol.deepseek-openai-chat.v4", pro.Application.Operations[0].ProtocolProfileID)
	require.Equal(t, modelprofile.ChatOperationProtocolBindingVersionV1, pro.Application.Operations[0].ProtocolProfileVersion)
	require.True(t, pro.Application.Operations[0].ProbeAvailable)
	require.Equal(t, "exact_probe_contract_available", pro.Application.Operations[0].ProbeAvailabilityReason)
	require.Len(t, pro.Application.Operations[0].CapabilityProbeContracts, 9)
	require.Equal(t, "function_tools", pro.Application.Operations[0].CapabilityProbeContracts[0].CapabilityID)
	require.Equal(t, "tools", pro.Application.Operations[0].CapabilityProbeContracts[8].CapabilityID)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND support_state='enabled' AND evidence_state='declared'`,
		pro.Application.Deployment.ID,
	).Scan(&declaredCapabilityCount))
	require.Equal(t, 9, declaredCapabilityCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_ref FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND capability_id='tools'`, pro.Application.Deployment.ID).Scan(&capabilityEvidenceRef))
	require.Equal(t, "capability_contract:unirouter.capability-probe.deepseek-chat-function-tools.v1@2.0.0", capabilityEvidenceRef)

	kimiRequest := request
	kimiRequest.Connection = canonicalConnectionInput{
		Name: "Reviewed Kimi", Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
	}
	kimiRequest.PublicModel = canonicalPublicModelInput{
		ModelName: "reviewed-kimi-k26", ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
	}
	kimiRequest.Deployment = canonicalDeploymentInput{UpstreamModel: "kimi-k2.6"}
	kimiRequest.Credential = canonicalCredentialInput{APIKey: "reviewed-kimi-binding-fixture"}
	kimi := createCanonicalApplicationForTest(t, handler, authorized, kimiRequest, http.StatusCreated)
	require.Equal(t, "protocol.kimi-openai-chat.v5", kimi.Application.Operations[0].ProtocolProfileID)
	require.Equal(t, modelprofile.ChatOperationProtocolBindingVersionV1, kimi.Application.Operations[0].ProtocolProfileVersion)
	require.True(t, kimi.Application.Operations[0].ProbeAvailable)
	require.Equal(t, "exact_probe_contract_available", kimi.Application.Operations[0].ProbeAvailabilityReason)
	require.Len(t, kimi.Application.Operations[0].CapabilityProbeContracts, 6)
	require.Equal(t, "function_tools", kimi.Application.Operations[0].CapabilityProbeContracts[0].CapabilityID)
	require.Equal(t, "tools", kimi.Application.Operations[0].CapabilityProbeContracts[5].CapabilityID)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
   AND support_state='enabled' AND evidence_state='declared'`,
		kimi.Application.Deployment.ID,
	).Scan(&declaredCapabilityCount))
	require.Equal(t, 6, declaredCapabilityCount)

	qualificationTarget := fmt.Sprintf(
		"/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/capabilities/streaming",
		created.Application.Deployment.ID,
	)
	qualificationRequest := authorized(http.MethodGet, qualificationTarget, "")
	qualificationRequest.SetPathValue("deploymentID", strconv.FormatInt(created.Application.Deployment.ID, 10))
	qualificationRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	qualificationRequest.SetPathValue("capabilityID", "streaming")
	qualificationResponse := httptest.NewRecorder()
	handler.GetCanonicalModelCapabilityQualification(qualificationResponse, qualificationRequest)
	require.Equal(t, http.StatusOK, qualificationResponse.Code, qualificationResponse.Body.String())
	var qualification modelCapabilityQualificationView
	require.NoError(t, json.Unmarshal(qualificationResponse.Body.Bytes(), &qualification))
	require.Equal(t, "not_probed", qualification.State)
	require.True(t, qualification.ProbeAvailable)
	aggregator := request
	aggregator.Connection = canonicalConnectionInput{
		Name: "Rejected aggregator", Provider: "openrouter", BaseURL: "https://openrouter.example/api/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
	}
	aggregator.PublicModel = canonicalPublicModelInput{ID: created.Application.PublicModel.ID}
	aggregator.Deployment.UpstreamModel = "deepseek/deepseek-v4-flash"
	createCanonicalApplicationForTest(t, handler, authorized, aggregator, http.StatusBadRequest)
	wrongOperation := request
	wrongOperation.Connection = canonicalConnectionInput{ID: created.Application.Connection.ID}
	wrongOperation.PublicModel = canonicalPublicModelInput{ID: created.Application.PublicModel.ID}
	wrongOperation.Credential = canonicalCredentialInput{BindingID: created.Application.Credential.Bindings[0].ID}
	wrongOperation.Operation.OperationID = string(modeloperation.IDEmbeddings)
	createCanonicalApplicationForTest(t, handler, authorized, wrongOperation, http.StatusBadRequest)
	var rejectedConnections, deployments, operations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT
 (SELECT COUNT(*) FROM provider_connections WHERE name='Rejected aggregator'),
 (SELECT COUNT(*) FROM model_deployments WHERE customer_org_id=$1),
 (SELECT COUNT(*) FROM model_deployment_operations WHERE deployment_id=$2)`,
		orgID, created.Application.Deployment.ID).Scan(&rejectedConnections, &deployments, &operations))
	require.Zero(t, rejectedConnections)
	require.Equal(t, 3, deployments)
	require.Equal(t, 1, operations)
}

func TestCanonicalModelApplicationUsesExactDeploymentOperationAndFiveIndependentAxesIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('canonical-app-customer','active','customer')
RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-app-admin','fixture','org_admin','org_admin','active')
RETURNING id`, orgID).Scan(&userID))

	encryptionKey := []byte("0123456789abcdef01234567")
	handler := NewHandler(pool, "canonical-app-jwt", encryptionKey, nil)
	authorized := canonicalApplicationAuthorizedRequest(orgID, userID)

	officialRequest := canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "DeepSeek official", Provider: "deepseek", BaseURL: "https://api.deepseek.com",
			AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
		},
		PublicModel: canonicalPublicModelInput{
			ModelName: "company-deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash",
			ProviderFamily: "deepseek", Lifecycle: "ga", ContextLength: canonicalTestPtr(128000),
			OutputLimit: canonicalTestPtr(8192), ApprovedCatalog: canonicalTestPtr(true),
			// This projection fixture intentionally spans providers and operations.
			// It must not claim an official provider/surface-specific contract.
			ExecutionProfileID: modelprofile.UnverifiedProfileID, ExecutionProfileVersion: modelprofile.UnverifiedProfileVersion,
		},
		Deployment: canonicalDeploymentInput{
			UpstreamModel: "deepseek-v4-flash", MaxContext: canonicalTestPtr(128000), DefaultMaxOutput: canonicalTestPtr(8192),
		},
		Credential: canonicalCredentialInput{APIKey: "official-provider-secret"},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	official := createCanonicalApplicationForTest(t, handler, authorized, officialRequest, http.StatusCreated)
	require.False(t, official.Reused.Connection)
	require.False(t, official.Reused.PublicModel)
	require.False(t, official.Reused.Deployment)
	require.False(t, official.Reused.Credential)
	require.False(t, official.Reused.Operation)
	require.Equal(t, "openai.chat_completions", official.Application.Operations[0].ProtocolProfileID)
	require.Equal(t, "1.0.0", official.Application.Operations[0].ProtocolProfileVersion)
	require.Equal(t, "declared", official.Application.Operations[0].EvidenceState)
	require.Equal(t, modelprofile.UnverifiedProfileID, official.Application.PublicModel.ExecutionProfileID)
	require.Equal(t, modelprofile.UnverifiedProfileVersion, official.Application.PublicModel.ExecutionProfileVersion)
	require.Nil(t, official.Application.Operations[0].VerifiedAt)
	require.Equal(t, "enabled", official.Application.Connection.AdminState)
	require.Equal(t, "ready", official.Application.Credential.State)
	require.Equal(t, "missing", official.Application.Operations[0].ProbeState)
	require.Equal(t, "unassigned", official.Application.Runtime.State)
	require.False(t, official.Application.Callability.Evaluated)
	require.Equal(t, "not_evaluated", official.Application.Callability.State)
	require.Equal(t, "deployment_id+operation_id", official.Application.Actions.IdentityType)

	publicModelID := official.Application.PublicModel.ID
	officialDeploymentID := official.Application.Deployment.ID
	officialConnectionID := official.Application.Connection.ID
	require.Len(t, official.Application.Credential.Bindings, 1)
	officialBindingID := official.Application.Credential.Bindings[0].ID

	var encrypted string
	require.NoError(t, pool.QueryRow(ctx, `SELECT key_enc FROM provider_credential_bindings WHERE id=$1`, officialBindingID).Scan(&encrypted))
	require.NotEqual(t, "official-provider-secret", encrypted)
	plain, err := auth.DecryptKey(encrypted, encryptionKey)
	require.NoError(t, err)
	require.Equal(t, "official-provider-secret", string(plain))

	aggregatorRequest := officialRequest
	aggregatorRequest.Connection = canonicalConnectionInput{
		Name: "OpenRouter", Provider: "openrouter", BaseURL: "https://openrouter.example/api/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
	}
	aggregatorRequest.PublicModel = canonicalPublicModelInput{
		ID: publicModelID, ModelName: officialRequest.PublicModel.ModelName,
	}
	aggregatorRequest.Deployment.UpstreamModel = "deepseek/deepseek-v4-flash"
	aggregatorRequest.Credential = canonicalCredentialInput{APIKey: "aggregator-provider-secret"}
	aggregator := createCanonicalApplicationForTest(t, handler, authorized, aggregatorRequest, http.StatusCreated)
	require.True(t, aggregator.Reused.PublicModel)
	require.NotEqual(t, officialConnectionID, aggregator.Application.Connection.ID)
	require.NotEqual(t, officialDeploymentID, aggregator.Application.Deployment.ID)
	require.Equal(t, publicModelID, aggregator.Application.PublicModel.ID)

	detailResponse := httptest.NewRecorder()
	detailRequest := authorized(http.MethodGet, fmt.Sprintf(
		"/api/v1/model-deployments/%d?operation=%s",
		aggregator.Application.Deployment.ID, modeloperation.IDChatCompletions,
	), "")
	detailRequest.SetPathValue("deploymentID", strconv.FormatInt(aggregator.Application.Deployment.ID, 10))
	handler.GetCanonicalModelApplication(detailResponse, detailRequest)
	require.Equal(t, http.StatusOK, detailResponse.Code, detailResponse.Body.String())
	var selected canonicalModelApplication
	require.NoError(t, json.Unmarshal(detailResponse.Body.Bytes(), &selected))
	require.Equal(t, aggregator.Application.Deployment.ID, selected.Deployment.ID)
	require.Equal(t, "OpenRouter", selected.Connection.Name)
	require.Equal(t, "deepseek/deepseek-v4-flash", selected.Deployment.UpstreamModel)
	require.Len(t, selected.Operations, 1)
	require.Equal(t, string(modeloperation.IDChatCompletions), selected.Operations[0].OperationID)

	missingOperationResponse := httptest.NewRecorder()
	missingOperationRequest := authorized(
		http.MethodGet,
		fmt.Sprintf("/api/v1/model-deployments/%d?operation=%s", aggregator.Application.Deployment.ID, modeloperation.IDEmbeddings),
		"",
	)
	missingOperationRequest.SetPathValue("deploymentID", strconv.FormatInt(aggregator.Application.Deployment.ID, 10))
	handler.GetCanonicalModelApplication(missingOperationResponse, missingOperationRequest)
	require.Equal(t, http.StatusNotFound, missingOperationResponse.Code, missingOperationResponse.Body.String())

	// A second operation on the same deployment remains independently
	// addressable. Detail must resolve the requested operation, never the first
	// operation attached to the deployment.
	aggregatorEmbeddingsRequest := aggregatorRequest
	aggregatorEmbeddingsRequest.Connection = canonicalConnectionInput{ID: aggregator.Application.Connection.ID}
	aggregatorEmbeddingsRequest.PublicModel = canonicalPublicModelInput{ID: publicModelID}
	aggregatorEmbeddingsRequest.Credential = canonicalCredentialInput{BindingID: aggregator.Application.Credential.Bindings[0].ID}
	aggregatorEmbeddingsRequest.Operation = canonicalOperationInput{OperationID: string(modeloperation.IDEmbeddings)}
	aggregatorEmbeddings := createCanonicalApplicationForTest(t, handler, authorized, aggregatorEmbeddingsRequest, http.StatusCreated)
	require.Len(t, aggregatorEmbeddings.Application.Operations, 1)
	require.Equal(t, string(modeloperation.IDEmbeddings), aggregatorEmbeddings.Application.Operations[0].OperationID)
	require.Equal(t, "openai.embeddings", aggregatorEmbeddings.Application.Operations[0].ProtocolProfileID)
	selectedChat := getCanonicalApplicationForTest(
		t, handler, authorized, aggregator.Application.Deployment.ID,
		string(modeloperation.IDChatCompletions), http.StatusOK,
	)
	require.Len(t, selectedChat.Operations, 1)
	require.Equal(t, string(modeloperation.IDChatCompletions), selectedChat.Operations[0].OperationID)

	// Explicit reuse is pinned by ID plus the exact non-display connection
	// identity. It cannot silently fall back to the first deployment.
	reuseRequest := officialRequest
	reuseRequest.Connection = canonicalConnectionInput{ID: officialConnectionID}
	reuseRequest.PublicModel = canonicalPublicModelInput{ID: publicModelID}
	reuseRequest.Credential = canonicalCredentialInput{BindingID: officialBindingID}
	reused := createCanonicalApplicationForTest(t, handler, authorized, reuseRequest, http.StatusCreated)
	require.True(t, reused.Reused.Connection)
	require.True(t, reused.Reused.PublicModel)
	require.True(t, reused.Reused.Deployment)
	require.True(t, reused.Reused.Credential)
	require.True(t, reused.Reused.Operation)
	require.Equal(t, officialDeploymentID, reused.Application.Deployment.ID)
	configurationConflict := reuseRequest
	configurationConflict.Deployment.Priority = canonicalTestPtr(101)
	createCanonicalApplicationForTest(t, handler, authorized, configurationConflict, http.StatusConflict)
	var retainedPriority int
	require.NoError(t, pool.QueryRow(ctx, `SELECT priority FROM model_deployments WHERE id=$1`, officialDeploymentID).Scan(&retainedPriority))
	require.Equal(t, 100, retainedPriority)
	identityAssertionConflict := reuseRequest
	identityAssertionConflict.Connection.Provider = "not-deepseek"
	createCanonicalApplicationForTest(t, handler, authorized, identityAssertionConflict, http.StatusConflict)

	// A client cannot manufacture a profile/evidence identity. Unknown fields
	// are rejected before any canonical row is written.
	profileInjection := `{
      "connection":{"name":"forged","provider":"deepseek","base_url":"https://forged.example/v1","adapter_type":"openai_compatible","adapter_config":{}},
      "public_model":{"model_name":"forged-model"},
      "deployment":{"upstream_model":"forged-upstream"},
      "credential":{"api_key":"forged-secret"},
      "operation":{"operation_id":"openai.chat_completions.v1","protocol_profile_id":"client.forged","protocol_profile_version":"99"}
	}`
	forgedResponse := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, forgedResponse,
		authorized(http.MethodPost, "/api/v1/model-deployments", profileInjection), "",
	)
	require.Equal(t, http.StatusBadRequest, forgedResponse.Code, forgedResponse.Body.String())
	var forgedRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM provider_connections WHERE name='forged'`).Scan(&forgedRows))
	require.Zero(t, forgedRows)

	// The canonical read never turns a probe into a runtime or caller decision.
	var connectionRevision, deploymentRevision, bindingRevision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT config_revision FROM provider_connections WHERE id=$1`, officialConnectionID).Scan(&connectionRevision))
	require.NoError(t, pool.QueryRow(ctx, `SELECT config_revision FROM model_deployments WHERE id=$1`, officialDeploymentID).Scan(&deploymentRevision))
	require.NoError(t, pool.QueryRow(ctx, `SELECT state_revision FROM provider_credential_bindings WHERE id=$1`, officialBindingID).Scan(&bindingRevision))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  capabilities_verified,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0','deepseek-v4-flash',
  'deepseek-v4-flash',$4,$5,$6,$7,TRUE,200,17,'[]','{}','{}',
  'server_provider_probe','canonical-five-axis',
  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$8)`,
		officialDeploymentID, officialConnectionID, modeloperation.IDChatCompletions,
		connectionRevision, deploymentRevision, officialBindingID, bindingRevision, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  capabilities_verified,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0','deepseek-v4-flash',
  'deepseek-v4-flash',$4,$5,$6,$7,FALSE,503,29,'[]','{}','{}',
  'server_provider_probe','canonical-binding-a-historical',
  'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',$8,NOW()-INTERVAL '1 hour')`,
		officialDeploymentID, officialConnectionID, modeloperation.IDChatCompletions,
		connectionRevision, deploymentRevision, officialBindingID, bindingRevision, userID)
	require.NoError(t, err)

	// The newest audit probe may fail on binding B while binding A remains a
	// current success. Operational readiness must aggregate each binding's own
	// latest probe instead of inheriting the globally newest result.
	secondEncrypted, err := auth.EncryptKey([]byte("second-official-secret"), encryptionKey)
	require.NoError(t, err)
	var secondBindingID, secondBindingRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status)
VALUES ($1,$2,'active') RETURNING id,state_revision`, officialConnectionID, secondEncrypted).Scan(&secondBindingID, &secondBindingRevision))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, secondBindingID, officialDeploymentID, officialConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  capabilities_verified,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0','deepseek-v4-flash',
  'deepseek-v4-flash',$4,$5,$6,$7,FALSE,401,11,'[]','{}','{}',
  'server_provider_probe','canonical-binding-b-failure',
  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',$8,NOW()+INTERVAL '1 second')`,
		officialDeploymentID, officialConnectionID, modeloperation.IDChatCompletions,
		connectionRevision, deploymentRevision, secondBindingID, secondBindingRevision, userID)
	require.NoError(t, err)

	// Build one sealed v2 release containing only the aggregator deployment.
	// The public-model slot is shared, but exact membership must remain D2+op.
	aggregatorDeploymentID := aggregator.Application.Deployment.ID
	aggregatorConnectionID := aggregator.Application.Connection.ID
	aggregatorBindingID := aggregator.Application.Credential.Bindings[0].ID
	var aggregatorConnectionRevision, aggregatorDeploymentRevision, aggregatorBindingRevision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT config_revision FROM provider_connections WHERE id=$1`, aggregatorConnectionID).Scan(&aggregatorConnectionRevision))
	require.NoError(t, pool.QueryRow(ctx, `SELECT config_revision FROM model_deployments WHERE id=$1`, aggregatorDeploymentID).Scan(&aggregatorDeploymentRevision))
	require.NoError(t, pool.QueryRow(ctx, `SELECT state_revision FROM provider_credential_bindings WHERE id=$1`, aggregatorBindingID).Scan(&aggregatorBindingRevision))
	_, err = pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='production_approved',evidence_ref='test:canonical-approved',
       verified_at=NOW(),revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, aggregatorDeploymentID, modeloperation.IDChatCompletions)
	require.NoError(t, err)
	var aggregatorOperationRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT revision FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, aggregatorDeploymentID, modeloperation.IDChatCompletions).Scan(&aggregatorOperationRevision))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  capabilities_verified,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0','deepseek/deepseek-v4-flash',
  'deepseek/deepseek-v4-flash',$4,$5,$6,$7,TRUE,200,23,'[]','{}','{}',
  'server_provider_probe','canonical-aggregator-ready',
  'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',$8)`,
		aggregatorDeploymentID, aggregatorConnectionID, modeloperation.IDChatCompletions,
		aggregatorConnectionRevision, aggregatorDeploymentRevision, aggregatorBindingID, aggregatorBindingRevision, userID)
	require.NoError(t, err)
	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	sealed := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   userID,
		ActorOrgID:    orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: aggregatorDeploymentID,
			OperationID:  string(modeloperation.IDChatCompletions),
		}},
		Reason: "integration fixture for exact deployment operation membership",
	})
	sealed = integrationfixture.ActivateInitialDeploymentRelease(t, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   userID,
		ActorOrgID:    orgID,
	}, sealed)

	ready := getCanonicalApplicationForTest(t, handler, authorized, officialDeploymentID, string(modeloperation.IDChatCompletions), http.StatusOK)
	require.Equal(t, "ready", ready.Operations[0].ProbeState)
	require.True(t, ready.Operations[0].LatestProbe.Current)
	require.False(t, ready.Operations[0].LatestProbe.Success)
	require.Equal(t, 2, ready.Operations[0].ProbedBindingCount)
	require.Equal(t, 2, ready.Credential.EligibleCount)
	require.Equal(t, 1, ready.Operations[0].CurrentSuccessCount)
	require.Equal(t, 1, ready.Operations[0].CurrentFailureCount)
	var officialProbeHistoryCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_probes
 WHERE deployment_id=$1 AND operation_id=$2`, officialDeploymentID, modeloperation.IDChatCompletions).Scan(&officialProbeHistoryCount))
	require.Equal(t, 3, officialProbeHistoryCount)
	require.Equal(t, 2, ready.Operations[0].ProbedBindingCount, "projection counts latest-per-binding facts, not historical rows")
	require.Equal(t, "slot_present", ready.Runtime.State)
	require.Equal(t, "unassigned", ready.Operations[0].Runtime.State)
	require.Nil(t, ready.Operations[0].Runtime.StableMembership)
	require.Equal(t, "not_evaluated", ready.Callability.State)
	aggregatorActive := getCanonicalApplicationForTest(t, handler, authorized, aggregatorDeploymentID, string(modeloperation.IDChatCompletions), http.StatusOK)
	require.Equal(t, "stable", aggregatorActive.Operations[0].Runtime.State)
	require.NotNil(t, aggregatorActive.Operations[0].Runtime.StableMembership)
	require.Equal(t, sealed.ReleaseRef, aggregatorActive.Operations[0].Runtime.StableMembership.ReleaseRef)
	require.Equal(t, sealed.RouteItemsHash, aggregatorActive.Operations[0].Runtime.StableMembership.RouteItemsHash)
	require.Equal(t, "allow", aggregatorActive.Operations[0].Runtime.StableMembership.Admission)
	require.Equal(t, aggregatorDeploymentID, aggregatorActive.Deployment.ID)

	// Isolate the projection contract from the separate admin-state writer.
	// Migration 70 currently couples admin-state writes to config revisions, so
	// this fixture temporarily disables only those two revision triggers in its
	// private test database. No probe revision is forged: pausing the connection
	// and deployment must not rewrite credential eligibility or probe currency.
	adminStateTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `ALTER TABLE provider_connections DISABLE TRIGGER provider_connections_revision_guard`)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `ALTER TABLE model_deployments DISABLE TRIGGER model_deployments_revision_guard`)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `UPDATE provider_connections SET admin_state='disabled',updated_at=NOW() WHERE id=$1`, officialConnectionID)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `UPDATE model_deployments SET admin_state='disabled',updated_at=NOW() WHERE id=$1`, officialDeploymentID)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `ALTER TABLE model_deployments ENABLE TRIGGER model_deployments_revision_guard`)
	require.NoError(t, err)
	_, err = adminStateTx.Exec(ctx, `ALTER TABLE provider_connections ENABLE TRIGGER provider_connections_revision_guard`)
	require.NoError(t, err)
	require.NoError(t, adminStateTx.Commit(ctx))

	paused := getCanonicalApplicationForTest(t, handler, authorized, officialDeploymentID, string(modeloperation.IDChatCompletions), http.StatusOK)
	require.Equal(t, "disabled", paused.Connection.AdminState)
	require.Equal(t, "disabled", paused.Deployment.AdminState)
	require.Equal(t, "ready", paused.Credential.State)
	require.Equal(t, 2, paused.Credential.EligibleCount)
	require.Equal(t, "ready", paused.Operations[0].ProbeState)
	require.True(t, paused.Operations[0].LatestProbe.Current)
	require.Equal(t, 2, paused.Operations[0].ProbedBindingCount)
	require.Zero(t, paused.Operations[0].StaleProbedBindingCount)
	require.Equal(t, 1, paused.Operations[0].CurrentSuccessCount)
	require.Equal(t, 1, paused.Operations[0].CurrentFailureCount)

	_, err = pool.Exec(ctx, `UPDATE provider_connections SET config_revision=config_revision+1,updated_at=NOW() WHERE id=$1`, officialConnectionID)
	require.NoError(t, err)
	stale := getCanonicalApplicationForTest(t, handler, authorized, officialDeploymentID, string(modeloperation.IDChatCompletions), http.StatusOK)
	require.Equal(t, "stale", stale.Operations[0].ProbeState)
	require.False(t, stale.Operations[0].LatestProbe.Current)
	require.Equal(t, 2, stale.Operations[0].StaleProbedBindingCount)
	require.Equal(t, "slot_present", stale.Runtime.State)
	require.Equal(t, "unassigned", stale.Operations[0].Runtime.State)

	listResponse := httptest.NewRecorder()
	handler.ListCanonicalModelApplications(listResponse, authorized(http.MethodGet, "/api/v1/model-deployments", ""))
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	var list canonicalModelApplicationProjection
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &list))
	require.Equal(t, canonicalModelApplicationContractVersion, list.ContractVersion)
	require.Len(t, list.Applications, 2)
	require.Equal(t, 2, list.Summary.Deployments)
	require.Equal(t, 2, list.Summary.CredentialsReady)
	require.Equal(t, 1, list.Summary.RuntimeAssignedOperations)

	var viewerID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-app-viewer','fixture','org_viewer','org_viewer','active')
RETURNING id`, orgID).Scan(&viewerID))
	viewer := canonicalApplicationRoleRequest(orgID, viewerID, auth.RoleOrgViewer)
	viewerDetail := getCanonicalApplicationForTest(
		t, handler, viewer, officialDeploymentID, string(modeloperation.IDChatCompletions), http.StatusOK,
	)
	require.Empty(t, viewerDetail.Credential.Bindings)
	require.Positive(t, viewerDetail.Credential.BoundCount)
	require.Positive(t, viewerDetail.Credential.EligibleCount)
	require.NotNil(t, viewerDetail.Operations[0].LatestProbe)
	require.Positive(t, viewerDetail.Operations[0].LatestProbe.ProbeID)
	require.Nil(t, viewerDetail.Operations[0].LatestProbe.CredentialBindingID)
	require.Nil(t, viewerDetail.Operations[0].LatestProbe.TestedBy)

	viewerListResponse := httptest.NewRecorder()
	handler.ListCanonicalModelApplications(
		viewerListResponse,
		viewer(http.MethodGet, "/api/v1/model-deployments", ""),
	)
	require.Equal(t, http.StatusOK, viewerListResponse.Code, viewerListResponse.Body.String())
	var viewerList canonicalModelApplicationProjection
	require.NoError(t, json.Unmarshal(viewerListResponse.Body.Bytes(), &viewerList))
	require.Len(t, viewerList.Applications, 2)
	for _, application := range viewerList.Applications {
		require.Empty(t, application.Credential.Bindings)
		for _, operation := range application.Operations {
			if operation.LatestProbe != nil {
				require.Nil(t, operation.LatestProbe.CredentialBindingID)
				require.Nil(t, operation.LatestProbe.TestedBy)
			}
		}
	}
}

func TestCanonicalModelApplicationRejectsIDORAndRollsBackPartialCreateIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('canonical-owner','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'owner','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&userID))
	handler := NewHandler(pool, "canonical-id-secret", []byte("0123456789abcdef01234567"), nil)
	owner := canonicalApplicationAuthorizedRequest(orgID, userID)
	request := canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{Name: "owner-connection", Provider: "openai", BaseURL: "https://owner.example/v1", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`)},
		PublicModel: canonicalPublicModelInput{ModelName: "owner-model", Lifecycle: "ga", ContextLength: canonicalTestPtr(8192), OutputLimit: canonicalTestPtr(1024)},
		Deployment: canonicalDeploymentInput{
			UpstreamModel: "owner-upstream", Priority: canonicalTestPtr(77),
			MaxContext: canonicalTestPtr(8192), DefaultMaxOutput: canonicalTestPtr(1024),
		},
		Credential: canonicalCredentialInput{APIKey: "owner-secret"},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	created := createCanonicalApplicationForTest(t, handler, owner, request, http.StatusCreated)
	bindingID := created.Application.Credential.Bindings[0].ID

	// ID=0 is idempotent only when the complete server-resolved public-model
	// configuration matches. It must neither mutate nor silently ignore drift.
	naturalIdentityRetry := request
	naturalIdentityRetry.Connection = canonicalConnectionInput{ID: created.Application.Connection.ID}
	naturalIdentityRetry.Credential = canonicalCredentialInput{BindingID: bindingID}
	retried := createCanonicalApplicationForTest(t, handler, owner, naturalIdentityRetry, http.StatusCreated)
	require.True(t, retried.Reused.Connection)
	require.True(t, retried.Reused.PublicModel)
	require.True(t, retried.Reused.Deployment)
	require.True(t, retried.Reused.Credential)
	require.True(t, retried.Reused.Operation)
	require.Equal(t, created.Application.PublicModel.ID, retried.Application.PublicModel.ID)
	omittedDeploymentConfiguration := naturalIdentityRetry
	omittedDeploymentConfiguration.Deployment.Priority = nil
	createCanonicalApplicationForTest(t, handler, owner, omittedDeploymentConfiguration, http.StatusConflict)
	var retainedDeploymentPriority int
	require.NoError(t, pool.QueryRow(ctx, `SELECT priority FROM model_deployments WHERE id=$1`, created.Application.Deployment.ID).Scan(&retainedDeploymentPriority))
	require.Equal(t, 77, retainedDeploymentPriority)

	publicModelDrift := naturalIdentityRetry
	publicModelDrift.PublicModel.ApprovedCatalog = canonicalTestPtr(true)
	createCanonicalApplicationForTest(t, handler, owner, publicModelDrift, http.StatusConflict)
	var approvedCatalog bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT approved_catalog FROM model_registry WHERE id=$1`, created.Application.PublicModel.ID).Scan(&approvedCatalog))
	require.False(t, approvedCatalog)

	var foreignOrgID, foreignUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('canonical-foreign','active','standard') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'foreign','fixture','org_admin','org_admin','active') RETURNING id`, foreignOrgID).Scan(&foreignUserID))
	foreign := canonicalApplicationAuthorizedRequest(foreignOrgID, foreignUserID)
	getCanonicalApplicationForTest(t, handler, foreign, created.Application.Deployment.ID, string(modeloperation.IDChatCompletions), http.StatusNotFound)

	var foreignConnectionID, foreignModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('foreign-owned-connection','openai','https://foreign-owned.example/v1','openai_compatible','{}')
RETURNING id`).Scan(&foreignConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,lifecycle)
VALUES ($1,'foreign-owned-model','Foreign owned model','ga') RETURNING id`, foreignOrgID).Scan(&foreignModelID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,priority,routing_weight,degradation_threshold)
VALUES ($1,$2,$3,'foreign-owned-upstream',100,100,10)`, foreignOrgID, foreignModelID, foreignConnectionID)
	require.NoError(t, err)
	foreignConnectionReuse := request
	foreignConnectionReuse.Connection = canonicalConnectionInput{
		ID: foreignConnectionID, Provider: "openai", BaseURL: "https://foreign-owned.example/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
	}
	foreignConnectionReuse.PublicModel = canonicalPublicModelInput{ModelName: "foreign-connection-must-not-bind", Lifecycle: "ga"}
	foreignConnectionReuse.Deployment.UpstreamModel = "foreign-connection-owner-upstream"
	foreignConnectionReuse.Credential = canonicalCredentialInput{APIKey: "must-not-persist"}
	createCanonicalApplicationForTest(t, handler, owner, foreignConnectionReuse, http.StatusNotFound)
	var forbiddenBindingRows int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_registry
 WHERE org_id=$1 AND model_name='foreign-connection-must-not-bind'`, orgID).Scan(&forbiddenBindingRows))
	require.Zero(t, forbiddenBindingRows)

	rollbackRequest := request
	rollbackRequest.Connection = canonicalConnectionInput{Name: "must-rollback", Provider: "openai", BaseURL: "https://rollback.example/v1", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`)}
	rollbackRequest.PublicModel = canonicalPublicModelInput{ModelName: "must-rollback-model", Lifecycle: "ga"}
	rollbackRequest.Deployment.UpstreamModel = "must-rollback-upstream"
	rollbackRequest.Credential = canonicalCredentialInput{BindingID: bindingID}
	createCanonicalApplicationForTest(t, handler, owner, rollbackRequest, http.StatusNotFound)

	for query, args := range map[string][]any{
		`SELECT COUNT(*) FROM provider_connections WHERE name=$1`:                                    {"must-rollback"},
		`SELECT COUNT(*) FROM model_registry WHERE org_id=$1 AND model_name=$2`:                      {orgID, "must-rollback-model"},
		`SELECT COUNT(*) FROM model_deployments WHERE upstream_model=$1`:                             {"must-rollback-upstream"},
		`SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create' AND resource_id<>$1`: {strconv.FormatInt(created.Application.Deployment.ID, 10)},
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&count), query)
		require.Zero(t, count, query)
	}

	// A projection/readback failure happens after every canonical row and audit
	// insert, but before commit. The entire create transaction must roll back.
	var auditsBeforeReadbackFailure int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create'`).Scan(&auditsBeforeReadbackFailure))
	var bindingsBeforeReadbackFailure int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM provider_credential_bindings`).Scan(&bindingsBeforeReadbackFailure))
	readbackFailureRequest := request
	readbackFailureRequest.Connection = canonicalConnectionInput{
		Name: "readback-must-rollback", Provider: "openai", BaseURL: "https://readback-rollback.example/v1",
		AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
	}
	readbackFailureRequest.PublicModel = canonicalPublicModelInput{ModelName: "readback-must-rollback-model", Lifecycle: "ga"}
	readbackFailureRequest.Deployment = canonicalDeploymentInput{UpstreamModel: "readback-must-rollback-upstream"}
	readbackFailureRequest.Credential = canonicalCredentialInput{APIKey: "readback-must-not-persist"}
	injectedReadbackFailure := errors.New("injected canonical readback failure")
	readbackRequest := owner(http.MethodPost, "/api/v1/model-deployments", "")
	_, err = handler.createCanonicalModelApplicationWithReadback(
		readbackRequest.Context(), orgID, readbackFailureRequest,
		requestAuditMeta(readbackRequest, orgID, 0),
		func(context.Context, canonicalApplicationReadStore, int64, int64, string, time.Time) (canonicalModelApplicationProjection, error) {
			return canonicalModelApplicationProjection{}, injectedReadbackFailure
		},
	)
	require.ErrorIs(t, err, injectedReadbackFailure)
	for query, args := range map[string][]any{
		`SELECT COUNT(*) FROM provider_connections WHERE name=$1`:               {"readback-must-rollback"},
		`SELECT COUNT(*) FROM model_registry WHERE org_id=$1 AND model_name=$2`: {orgID, "readback-must-rollback-model"},
		`SELECT COUNT(*) FROM model_deployments WHERE upstream_model=$1`:        {"readback-must-rollback-upstream"},
	} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&count), query)
		require.Zero(t, count, query)
	}
	var auditsAfterReadbackFailure int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='model_deployment.create'`).Scan(&auditsAfterReadbackFailure))
	require.Equal(t, auditsBeforeReadbackFailure, auditsAfterReadbackFailure)
	var bindingsAfterReadbackFailure int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM provider_credential_bindings`).Scan(&bindingsAfterReadbackFailure))
	require.Equal(t, bindingsBeforeReadbackFailure, bindingsAfterReadbackFailure)

	// The old governance writer shares the same fail-closed identity guard:
	// model_name is immutable from creation, while display_name can change.
	rename := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(created.Application.PublicModel.ID, 10), `{"model_name":"renamed-owner-model"}`)
	rename.SetPathValue("id", strconv.FormatInt(created.Application.PublicModel.ID, 10))
	renameResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(renameResponse, rename)
	require.Equal(t, http.StatusConflict, renameResponse.Code, renameResponse.Body.String())

	display := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(created.Application.PublicModel.ID, 10), `{"display_name":"Owner model display label"}`)
	display.SetPathValue("id", strconv.FormatInt(created.Application.PublicModel.ID, 10))
	displayResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(displayResponse, display)
	require.Equal(t, http.StatusOK, displayResponse.Code, displayResponse.Body.String())
	var modelName, displayName string
	require.NoError(t, pool.QueryRow(ctx, `SELECT model_name,display_name FROM model_registry WHERE id=$1`, created.Application.PublicModel.ID).Scan(&modelName, &displayName))
	require.Equal(t, "owner-model", modelName)
	require.Equal(t, "Owner model display label", displayName)

	// Model-governance writes share the same attachment invariant as canonical
	// onboarding. Neither a historical target nor a current target may rewrite
	// the execution identity of an already deployed public model.
	historicalProfile := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(created.Application.PublicModel.ID, 10), `{"execution_profile_id":"kimi.k2.7-code.api","execution_profile_version":"1.0.0"}`)
	historicalProfile.SetPathValue("id", strconv.FormatInt(created.Application.PublicModel.ID, 10))
	historicalProfileResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(historicalProfileResponse, historicalProfile)
	require.Equal(t, http.StatusBadRequest, historicalProfileResponse.Code, historicalProfileResponse.Body.String())
	require.Contains(t, historicalProfileResponse.Body.String(), profileAttachmentErrorCode)

	currentProfile := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(created.Application.PublicModel.ID, 10), `{"execution_profile_id":"kimi.k3.api","execution_profile_version":"1.8.0"}`)
	currentProfile.SetPathValue("id", strconv.FormatInt(created.Application.PublicModel.ID, 10))
	currentProfileResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(currentProfileResponse, currentProfile)
	require.Equal(t, http.StatusConflict, currentProfileResponse.Code, currentProfileResponse.Body.String())
	require.Contains(t, currentProfileResponse.Body.String(), deployedProfileIdentityImmutableErrorCode)
	var retainedProfileID, retainedProfileVersion string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1`, created.Application.PublicModel.ID).Scan(&retainedProfileID, &retainedProfileVersion))
	require.Equal(t, modelprofile.UnverifiedProfileID, retainedProfileID)
	require.Equal(t, modelprofile.UnverifiedProfileVersion, retainedProfileVersion)

	historicalCreate := owner(http.MethodPost, "/api/v1/model-registry", `{
		"model_name":"historical-profile-must-not-persist",
		"execution_profile_id":"kimi.k2.7-code.api",
		"execution_profile_version":"1.0.0"
	}`)
	historicalCreateResponse := httptest.NewRecorder()
	handler.CreateModelRegistry(historicalCreateResponse, historicalCreate)
	require.Equal(t, http.StatusBadRequest, historicalCreateResponse.Code, historicalCreateResponse.Body.String())
	var historicalCreateCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_registry
 WHERE org_id=$1 AND model_name='historical-profile-must-not-persist'`, orgID).Scan(&historicalCreateCount))
	require.Zero(t, historicalCreateCount)

	var unboundModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,lifecycle)
VALUES ($1,'unbound-stable-model','Unbound stable model','staging') RETURNING id`, orgID).Scan(&unboundModelID))
	unboundRename := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(unboundModelID, 10), `{"model_name":"unbound-renamed-model"}`)
	unboundRename.SetPathValue("id", strconv.FormatInt(unboundModelID, 10))
	unboundResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(unboundResponse, unboundRename)
	require.Equal(t, http.StatusConflict, unboundResponse.Code, unboundResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT model_name FROM model_registry WHERE id=$1`, unboundModelID).Scan(&modelName))
	require.Equal(t, "unbound-stable-model", modelName)

	unboundProfile := owner(http.MethodPut, "/api/v1/model-registry/"+strconv.FormatInt(unboundModelID, 10), `{"execution_profile_id":"kimi.k3.api","execution_profile_version":"1.8.0"}`)
	unboundProfile.SetPathValue("id", strconv.FormatInt(unboundModelID, 10))
	unboundProfileResponse := httptest.NewRecorder()
	handler.UpdateModelRegistry(unboundProfileResponse, unboundProfile)
	require.Equal(t, http.StatusOK, unboundProfileResponse.Code, unboundProfileResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM model_registry WHERE id=$1`, unboundModelID).Scan(&retainedProfileID, &retainedProfileVersion))
	require.Equal(t, "kimi.k3.api", retainedProfileID)
	require.Equal(t, "1.8.0", retainedProfileVersion)
}

func canonicalApplicationAuthorizedRequest(orgID, userID int64) func(string, string, string) *http.Request {
	return canonicalApplicationRoleRequest(orgID, userID, auth.RoleOrgAdmin)
}

func canonicalApplicationRoleRequest(orgID, userID int64, organizationRole string) func(string, string, string) *http.Request {
	return func(method, target, body string) *http.Request {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		ctx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
		ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
		ctx = context.WithValue(ctx, middleware.CtxUserRole, organizationRole)
		ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, organizationRole)
		ctx = context.WithValue(ctx, middleware.CtxCustomerContext, customercontext.Snapshot{
			State: customercontext.StateReady, OrganizationID: orgID, OrganizationName: "Canonical customer",
		})
		ctx = context.WithValue(ctx, middleware.CtxCustomerOrgID, orgID)
		return request.WithContext(ctx)
	}
}

func canonicalTestPtr[T any](value T) *T { return &value }

func createCanonicalApplicationForTest(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	request canonicalModelApplicationCreateRequest,
	expectedStatus int,
) canonicalModelApplicationCreateResult {
	t.Helper()
	body, err := json.Marshal(request)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	serveCanonicalApplicationCreateForTest(
		t, handler, response,
		authorized(http.MethodPost, "/api/v1/model-deployments", string(body)), "",
	)
	require.Equal(t, expectedStatus, response.Code, response.Body.String())
	var result canonicalModelApplicationCreateResult
	if expectedStatus == http.StatusCreated {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return result
}

func serveCanonicalApplicationCreateForTest(
	t *testing.T,
	handler *Handler,
	response *httptest.ResponseRecorder,
	request *http.Request,
	idempotencyKey string,
) {
	t.Helper()
	if idempotencyKey == "" {
		var err error
		idempotencyKey, err = middleware.NewOperationID()
		require.NoError(t, err)
	}
	request.Header.Set(middleware.IdempotencyKeyHeader, idempotencyKey)
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
		http.HandlerFunc(handler.CreateCanonicalModelApplication),
	).ServeHTTP(response, request)
}

func getCanonicalApplicationForTest(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	deploymentID int64,
	operationID string,
	expectedStatus int,
) canonicalModelApplication {
	t.Helper()
	response := httptest.NewRecorder()
	request := authorized(
		http.MethodGet,
		fmt.Sprintf("/api/v1/model-deployments/%d?operation=%s", deploymentID, operationID),
		"",
	)
	request.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
	handler.GetCanonicalModelApplication(response, request)
	require.Equal(t, expectedStatus, response.Code, response.Body.String())
	var result canonicalModelApplication
	if expectedStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return result
}
