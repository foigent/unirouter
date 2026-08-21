//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDashboardUsesCanonicalDeploymentOperationHealthAndKeepsLegacyHistorySeparateIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('dashboard-canonical-health','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'dashboard-canonical-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))

	handler := NewHandler(pool, "dashboard-canonical-jwt", []byte("0123456789abcdef01234567"), nil)
	created := createCanonicalApplicationForTest(t, handler, canonicalApplicationAuthorizedRequest(orgID, userID), canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{
			Name: "Dashboard canonical connection", Provider: "openai",
			BaseURL: "https://dashboard-provider.invalid/v1", AdapterType: "openai_compatible",
			AdapterConfig: json.RawMessage(`{}`),
		},
		PublicModel: canonicalPublicModelInput{
			ModelName: "dashboard-canonical-model", DisplayName: "Dashboard Canonical Model",
			ProviderFamily: "openai", Lifecycle: "ga", ApprovedCatalog: canonicalTestPtr(true),
		},
		Deployment: canonicalDeploymentInput{UpstreamModel: "dashboard-upstream-model"},
		Credential: canonicalCredentialInput{APIKey: "dashboard-provider-secret"},
		Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}, http.StatusCreated)
	application := created.Application
	require.Len(t, application.Credential.Bindings, 1)
	require.Len(t, application.Operations, 1)

	credentialBindingID := application.Credential.Bindings[0].ID
	var credentialLinkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT created_at
  FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2 AND connection_id=$3`,
		credentialBindingID, application.Deployment.ID, application.Connection.ID,
	).Scan(&credentialLinkedAt))

	var probeID int64
	var probeTestedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,source_kind,source_ref,
    source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,'openai.chat_completions','1.0.0',
        'dashboard-upstream-model','dashboard-upstream-model',$4,$5,$6,$7,TRUE,200,
        'integration','dashboard-canonical-health',$8,$9,NOW())
RETURNING probe_id,tested_at`,
		application.Deployment.ID, application.Connection.ID, string(modeloperation.IDChatCompletions),
		application.Connection.ConfigRevision, application.Deployment.ConfigRevision,
		credentialBindingID, application.Credential.Bindings[0].StateRevision,
		"sha256:"+strings.Repeat("d", 64), userID,
	).Scan(&probeID, &probeTestedAt))
	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`,
		application.Deployment.ID, string(modeloperation.IDChatCompletions), probeID, probeTestedAt)
	require.NoError(t, err)

	var operationRevision int64
	var operationEvidenceState, operationEvidenceRef string
	var operationVerifiedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT revision,evidence_state,evidence_ref,verified_at
  FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`,
		application.Deployment.ID, string(modeloperation.IDChatCompletions),
	).Scan(&operationRevision, &operationEvidenceState, &operationEvidenceRef, &operationVerifiedAt))

	var releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ('rcr_dashboard_canonical_health',$1,'unirouter.runtime-change-candidate.v1',$2,$3,
        '{}','[]','{}','unchanged','active',1,0,'dashboard canonical history fixture',$4,$4,$4,NOW(),NOW())
RETURNING id`, application.PublicModel.ModelName,
		"sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), userID,
	).Scan(&releaseID))

	const canonicalRequestID = "dashboard-canonical-attempt"
	_, err = pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'settled','dashboard-canonical-test')`, canonicalRequestID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,deployment_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,route_evidence_hash,
    route_public_model_id,route_connection_id,route_credential_binding_id,
    route_connection_revision,route_deployment_revision,route_circuit_revision,
    route_credential_state_revision,route_credential_linked_at,
    route_model_operation_id,route_model_operation_revision,
    route_protocol_profile_id,route_protocol_profile_version,
    route_operation_evidence_state,route_operation_evidence_ref,route_operation_verified_at,
    route_capability_evidence,route_capability_evidence_hash,
    route_credential_probe_id,route_credential_probe_tested_at,
    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,route_slot_version,
    route_snapshot_hash,route_requires_probe)
VALUES ($1,$2,'attempt-canonical',101,NULL,NULL,$3,1,1,'/v1/chat/completions',$4,$5,
        'unirouter.execution-route.v1','canonical','canonical_profile',$6,
        $7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'openai.chat_completions','1.0.0',
        $17,$18,$19,'[]',$20,$21,$22,$23,'rcr_dashboard_canonical_health','active',1,$24,FALSE)`,
		orgID, canonicalRequestID, application.Deployment.ID,
		application.Connection.Provider, application.Connection.Name,
		"sha256:"+strings.Repeat("1", 64), application.PublicModel.ID, application.Connection.ID,
		credentialBindingID, application.Connection.ConfigRevision, application.Deployment.ConfigRevision,
		application.Deployment.CircuitRevision, application.Credential.Bindings[0].StateRevision, credentialLinkedAt,
		string(modeloperation.IDChatCompletions), operationRevision, operationEvidenceState, operationEvidenceRef,
		operationVerifiedAt, "sha256:"+strings.Repeat("f", 64), probeID, probeTestedAt, releaseID,
		"mrs1_"+strings.Repeat("e", 64),
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,source_event_hash,model,requested_model,provider,channel,
    status_code,latency_ms,created_at,
    route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
    route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
    route_model_operation_id,route_runtime_release_id,route_snapshot_hash)
VALUES ($1,$2,'attempt-canonical',$3,$4,$4,$5,$6,200,42,NOW(),
        'provider_attempt','unirouter.execution-route.v1','canonical',$7,$8,$9,$10,$11,$12,$13,$14)`,
		canonicalRequestID, orgID, strings.Repeat("9", 64), application.PublicModel.ModelName,
		application.Connection.Provider, application.Connection.Name,
		"sha256:"+strings.Repeat("1", 64), application.PublicModel.ID, application.Deployment.ID,
		application.Connection.ID, credentialBindingID, string(modeloperation.IDChatCompletions), releaseID,
		"mrs1_"+strings.Repeat("e", 64),
	)
	require.NoError(t, err)

	var legacyChannelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,status,circuit_state)
VALUES ('Legacy dashboard history','legacy','https://legacy.invalid','dashboard-canonical-model','active','healthy')
RETURNING id`).Scan(&legacyChannelID))
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,model,requested_model,provider,channel,route_channel_id,status_code,latency_ms,created_at)
VALUES ('dashboard-legacy-history',$1,$2,$2,'legacy','Legacy dashboard history',$3,503,84,NOW())`,
		orgID, application.PublicModel.ModelName, legacyChannelID)
	require.NoError(t, err)

	models, err := handler.dashboardModelDistribution(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, 2, models[0]["requests"])
	require.Equal(t, 1, models[0]["success_count"])
	require.Equal(t, 1, models[0]["fail_count"])

	health, err := handler.dashboardChannelHealth(ctx, orgID, "2000-01-01T00:00:00Z", "2100-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, health, 1)
	require.EqualValues(t, application.Deployment.ID, health[0]["deployment_id"])
	require.Equal(t, string(modeloperation.IDChatCompletions), health[0]["model_operation_id"])
	require.Equal(t, "route_deployment_id+route_model_operation_id", health[0]["historical_identity_authority"])
	require.Equal(t, 1, health[0]["success_count"])
	require.Equal(t, 0, health[0]["fail_count"], "legacy route_channel_id history must not be current canonical health")
	require.Equal(t, true, health[0]["credential_ready"])
	require.Equal(t, true, health[0]["operation_ready"])
	require.Equal(t, false, health[0]["runtime_published"])
	require.Equal(t, false, health[0]["available"])
	require.Equal(t, "awaiting_release", health[0]["status"])
}
