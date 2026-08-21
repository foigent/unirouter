//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelOperationProductionApprovalSeparatesActorAndCustomerOrganizationsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var customerOrgID, operatorOrgID, platformUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-customer','active','customer')
RETURNING id`).Scan(&customerOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-operator','active','platform')
RETURNING id`).Scan(&operatorOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'qualification-platform-admin','fixture','platform_admin','platform_admin','active')
RETURNING id`, operatorOrgID).Scan(&platformUserID))

	encryptionKey := []byte("0123456789abcdef01234567")
	fixture := insertCanonicalOperationProbeFixture(t, pool, customerOrgID, encryptionKey, "qualification")
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,'1.0.0')`,
		fixture.DeploymentID, modeloperation.IDResponses, endpointprotocol.ProfileOpenAIResponsesText)
	require.NoError(t, err)

	platform := canonicalOperationProbeAuthorizedRequest(
		operatorOrgID, customerOrgID, platformUserID, auth.RolePlatformAdmin,
	)
	handler := NewHandler(pool, "qualification-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/responses", request.URL.Path)
		return canonicalProbeHTTPResponse(http.StatusOK,
			`{"id":"resp_qualification","object":"response","status":"completed","model":"upstream-qualification","error":null,"incomplete_details":null,"output":[{"id":"msg_qualification","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`), nil
	}))
	probe := serveCanonicalOperationProbeForTest(
		t, handler, platform, fixture.DeploymentID, string(modeloperation.IDResponses),
		fixture.BindingID, "qualification probe", http.StatusOK,
	)
	require.True(t, probe.Success)
	require.True(t, probe.OperationAdvanced)

	var connectionRevision, deploymentRevision, operationRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision,operation.revision
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
 WHERE deployment.id=$1 AND operation.operation_id=$2`,
		fixture.DeploymentID, modeloperation.IDResponses,
	).Scan(&connectionRevision, &deploymentRevision, &operationRevision))

	approvalRequest := modelOperationProductionApprovalRequest{
		ProbeID:                    probe.ProbeID,
		ExpectedConnectionRevision: connectionRevision, ExpectedDeploymentRevision: deploymentRevision,
		ExpectedOperationRevision:       operationRevision,
		ExpectedExecutorContractVersion: modelqualification.ExecutorContractVersion,
		Reason:                          "platform reviewed exact HTTPS network proof",
	}
	serve := func(authorized func(string, string, string) *http.Request) *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(approvalRequest)
		require.NoError(t, marshalErr)
		target := "/api/v1/model-deployments/" + strconv.FormatInt(fixture.DeploymentID, 10) +
			"/operations/" + string(modeloperation.IDResponses) + "/production-approvals"
		request := authorized(http.MethodPost, target, string(body))
		request.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
		request.SetPathValue("operationID", string(modeloperation.IDResponses))
		response := httptest.NewRecorder()
		handler.ApproveCanonicalModelOperationProduction(response, request)
		return response
	}

	created := serve(platform)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var approval modelOperationProductionApprovalResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &approval))
	require.Equal(t, customerOrgID, approval.CustomerOrgID)
	require.Equal(t, platformUserID, approval.ApprovedBy)
	require.True(t, approval.Qualified)
	require.False(t, approval.Replayed)

	var storedCustomerOrgID, storedActorOrgID int64
	var storedBindingID, storedCredentialRevision int64
	var storedLinkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT customer_org_id,approved_by_org_id,credential_binding_id,
       credential_state_revision,credential_link_created_at
  FROM model_operation_production_approvals WHERE approval_id=$1`, approval.ApprovalID,
	).Scan(&storedCustomerOrgID, &storedActorOrgID, &storedBindingID, &storedCredentialRevision, &storedLinkedAt))
	require.Equal(t, customerOrgID, storedCustomerOrgID)
	require.Equal(t, operatorOrgID, storedActorOrgID)
	require.Equal(t, fixture.BindingID, storedBindingID)
	require.Equal(t, int64(1), storedCredentialRevision)
	require.False(t, storedLinkedAt.IsZero())

	replayed := serve(platform)
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	var replay modelOperationProductionApprovalResponse
	require.NoError(t, json.Unmarshal(replayed.Body.Bytes(), &replay))
	require.True(t, replay.Replayed)
	require.Equal(t, approval.ApprovalID, replay.ApprovalID)

	forgedActorOrg := canonicalOperationProbeAuthorizedRequest(
		customerOrgID, customerOrgID, platformUserID, auth.RolePlatformAdmin,
	)
	forged := serve(forgedActorOrg)
	require.Equal(t, http.StatusConflict, forged.Code, forged.Body.String())
	var approvalCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_operation_production_approvals WHERE probe_id=$1`, probe.ProbeID,
	).Scan(&approvalCount))
	require.Equal(t, 1, approvalCount)

	rotatedSecret, err := auth.EncryptKey([]byte("rotated-provider-secret"), encryptionKey)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET key_enc=$2,state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.BindingID, rotatedSecret)
	require.NoError(t, err)
	var qualified bool
	var qualificationState string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT qualified,qualification_state FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDResponses,
	).Scan(&qualified, &qualificationState))
	require.True(t, qualified, "credential rotation is operational binding state, not structural production qualification")
	require.Equal(t, "production_qualified", qualificationState)
	postRotationReplay := serve(platform)
	require.Equal(t, http.StatusOK, postRotationReplay.Code, postRotationReplay.Body.String())

	_, err = pool.Exec(ctx, `
UPDATE provider_connections SET config_revision=config_revision+1 WHERE id=$1`, fixture.ConnectionID)
	require.NoError(t, err)
	revisionDriftReplay := serve(platform)
	require.Equal(t, http.StatusOK, revisionDriftReplay.Code, revisionDriftReplay.Body.String())
	var staleReplay modelOperationProductionApprovalResponse
	require.NoError(t, json.Unmarshal(revisionDriftReplay.Body.Bytes(), &staleReplay))
	require.True(t, staleReplay.Replayed)
	require.Equal(t, approval.ApprovalID, staleReplay.ApprovalID)
	require.False(t, staleReplay.Qualified)
	require.Equal(t, "connection_revision_stale", staleReplay.QualificationState)
}

func TestModelOperationProductionApprovalRejectsStaleBindingAndCustomerRootIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var customerOrgID, operatorOrgID, platformUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-guard-customer','active','customer')
RETURNING id`).Scan(&customerOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-guard-operator','active','platform')
RETURNING id`).Scan(&operatorOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'qualification-guard-admin','fixture','platform_admin','platform_admin','active')
RETURNING id`, operatorOrgID).Scan(&platformUserID))

	encryptionKey := []byte("0123456789abcdef01234567")
	platform := canonicalOperationProbeAuthorizedRequest(
		operatorOrgID, customerOrgID, platformUserID, auth.RolePlatformAdmin,
	)
	handler := NewHandler(pool, "qualification-guard-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		var upstreamRequest struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&upstreamRequest))
		responseBody, err := json.Marshal(map[string]interface{}{
			"id": "resp_guard", "object": "response", "status": "completed", "model": upstreamRequest.Model,
			"error": nil, "incomplete_details": nil,
			"output": []map[string]interface{}{{
				"id": "msg_guard", "type": "message", "status": "completed", "role": "assistant",
				"content": []map[string]string{{"type": "output_text", "text": "OK"}},
			}},
			"usage": map[string]int{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3},
		})
		require.NoError(t, err)
		return canonicalProbeHTTPResponse(http.StatusOK, string(responseBody)), nil
	}))

	t.Run("root authority is locked and exact in Go and SQL", func(t *testing.T) {
		fixture, probe, approvalRequest := createResponsesProductionApprovalProof(
			t, pool, handler, platform, customerOrgID, encryptionKey, "root-authority",
		)
		serve := func() *httptest.ResponseRecorder {
			return serveProductionApprovalRequest(t, handler, platform, fixture.DeploymentID, approvalRequest)
		}
		assertRejected := func() {
			response := serve()
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			directErr := insertProductionApprovalDirect(
				ctx, pool, probe.ProbeID, platformUserID, operatorOrgID, "direct SQL root authority check",
			)
			require.ErrorContains(t, directErr, "customer root authority")
			var count int
			require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_operation_production_approvals WHERE probe_id=$1`, probe.ProbeID).Scan(&count))
			require.Zero(t, count)
		}

		_, err := pool.Exec(ctx, `UPDATE orgs SET status='disabled' WHERE id=$1`, customerOrgID)
		require.NoError(t, err)
		assertRejected()
		_, err = pool.Exec(ctx, `UPDATE orgs SET status='active',org_type='platform' WHERE id=$1`, customerOrgID)
		require.NoError(t, err)
		assertRejected()
		_, err = pool.Exec(ctx, `UPDATE orgs SET org_type='customer',parent_id=$2 WHERE id=$1`, customerOrgID, operatorOrgID)
		require.NoError(t, err)
		assertRejected()
		_, err = pool.Exec(ctx, `UPDATE orgs SET parent_id=NULL WHERE id=$1`, customerOrgID)
		require.NoError(t, err)
		var ambiguousRootID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('qualification-ambiguous-root','active','customer')
RETURNING id`).Scan(&ambiguousRootID))
		assertRejected()
		_, err = pool.Exec(ctx, `UPDATE orgs SET status='disabled' WHERE id=$1`, ambiguousRootID)
		require.NoError(t, err)
		created := serve()
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	})

	for _, test := range []struct {
		name       string
		mutate     func(canonicalOperationProbeFixture)
		wantStatus int
		relink     bool
	}{
		{
			name: "unlinked", wantStatus: http.StatusNotFound, relink: true,
			mutate: func(fixture canonicalOperationProbeFixture) {
				_, err := pool.Exec(ctx, `
DELETE FROM credential_binding_deployments WHERE binding_id=$1 AND deployment_id=$2`, fixture.BindingID, fixture.DeploymentID)
				require.NoError(t, err)
			},
		},
		{
			name: "revoked", wantStatus: http.StatusConflict,
			mutate: func(fixture canonicalOperationProbeFixture) {
				_, err := pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW() WHERE id=$1`, fixture.BindingID)
				require.NoError(t, err)
			},
		},
		{
			name: "rotated", wantStatus: http.StatusConflict,
			mutate: func(fixture canonicalOperationProbeFixture) {
				rotated, err := auth.EncryptKey([]byte("rotated-before-approval"), encryptionKey)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET key_enc=$2,state_revision=state_revision+1,updated_at=NOW() WHERE id=$1`, fixture.BindingID, rotated)
				require.NoError(t, err)
			},
		},
		{
			name: "cooldown", wantStatus: http.StatusConflict,
			mutate: func(fixture canonicalOperationProbeFixture) {
				_, err := pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET cooldown_until=NOW()+INTERVAL '10 minutes',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.BindingID)
				require.NoError(t, err)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, probe, approvalRequest := createResponsesProductionApprovalProof(
				t, pool, handler, platform, customerOrgID, encryptionKey, "credential-"+test.name,
			)
			test.mutate(fixture)
			response := serveProductionApprovalRequest(t, handler, platform, fixture.DeploymentID, approvalRequest)
			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			directErr := insertProductionApprovalDirect(
				ctx, pool, probe.ProbeID, platformUserID, operatorOrgID, "direct SQL credential currentness check",
			)
			require.Error(t, directErr)
			var count int
			require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_operation_production_approvals WHERE probe_id=$1`, probe.ProbeID).Scan(&count))
			require.Zero(t, count)
			if test.relink {
				_, err := pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.BindingID, fixture.DeploymentID, fixture.ConnectionID)
				require.NoError(t, err)
				relinked := serveProductionApprovalRequest(t, handler, platform, fixture.DeploymentID, approvalRequest)
				require.Equal(t, http.StatusConflict, relinked.Code, relinked.Body.String(), "a new link epoch needs a fresh probe")
			}
		})
	}
}

func createResponsesProductionApprovalProof(
	t *testing.T,
	pool *pgxpool.Pool,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	customerOrgID int64,
	encryptionKey []byte,
	suffix string,
) (canonicalOperationProbeFixture, canonicalOperationProbeResponse, modelOperationProductionApprovalRequest) {
	t.Helper()
	fixture := insertCanonicalOperationProbeFixture(t, pool, customerOrgID, encryptionKey, suffix)
	_, err := pool.Exec(context.Background(), `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,'1.0.0')`,
		fixture.DeploymentID, modeloperation.IDResponses, endpointprotocol.ProfileOpenAIResponsesText)
	require.NoError(t, err)
	probe := serveCanonicalOperationProbeForTest(
		t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDResponses),
		fixture.BindingID, "qualification proof "+suffix, http.StatusOK,
	)
	require.True(t, probe.Success, "production approval probe was not usable: %+v", probe)
	var connectionRevision, deploymentRevision, operationRevision int64
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT connection.config_revision,deployment.config_revision,operation.revision
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
 WHERE deployment.id=$1 AND operation.operation_id=$2`, fixture.DeploymentID, modeloperation.IDResponses,
	).Scan(&connectionRevision, &deploymentRevision, &operationRevision))
	return fixture, probe, modelOperationProductionApprovalRequest{
		ProbeID: probe.ProbeID, ExpectedConnectionRevision: connectionRevision,
		ExpectedDeploymentRevision: deploymentRevision, ExpectedOperationRevision: operationRevision,
		ExpectedExecutorContractVersion: modelqualification.ExecutorContractVersion,
		Reason:                          "reviewed exact live proof " + suffix,
	}
}

func serveProductionApprovalRequest(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	deploymentID int64,
	approvalRequest modelOperationProductionApprovalRequest,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(approvalRequest)
	require.NoError(t, err)
	target := "/api/v1/model-deployments/" + strconv.FormatInt(deploymentID, 10) +
		"/operations/" + string(modeloperation.IDResponses) + "/production-approvals"
	request := authorized(http.MethodPost, target, string(body))
	request.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
	request.SetPathValue("operationID", string(modeloperation.IDResponses))
	response := httptest.NewRecorder()
	handler.ApproveCanonicalModelOperationProduction(response, request)
	return response
}

func insertProductionApprovalDirect(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeID, actorID, actorOrgID int64,
	reason string,
) error {
	_, err := pool.Exec(ctx, `
INSERT INTO model_operation_production_approvals (
  customer_org_id,deployment_id,connection_id,operation_id,probe_id,
  credential_binding_id,credential_state_revision,credential_link_created_at,
  public_path,protocol_profile_id,protocol_profile_version,executor_contract_version,https_origin,
  connection_revision,deployment_revision,probe_operation_revision,
  source_operation_revision,approved_operation_revision,approved_by,approved_by_org_id,
  reason,source_kind,source_ref,approved_at)
SELECT deployment.customer_org_id,probe.deployment_id,probe.connection_id,probe.operation_id,probe.probe_id,
       probe.credential_binding_id,probe.credential_state_revision,credential_link.created_at,
       probe.public_path,probe.probe_profile_id,probe.probe_profile_version,
       probe.executor_contract_version,probe.https_origin,
       probe.connection_revision,probe.deployment_revision,probe.operation_revision,
       operation.revision,operation.revision+1,$2,$3,$4,$5,'probe:' || probe.probe_id::TEXT,NOW()
  FROM model_deployment_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  LEFT JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
 WHERE probe.probe_id=$1`, probeID, actorID, actorOrgID, reason, modelqualification.ApprovalSourceKind)
	return err
}
