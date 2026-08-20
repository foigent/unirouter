//go:build integration

package executionprojection_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestResolveExactProviderAttemptIdentitiesAcrossAuthorityModes(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	fixture := seedProjectionCanonicalFixture(t, pool)

	insertProjectionFreeze(t, pool, fixture.orgID, "projection-legacy")
	_, err := pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_model_operation_id,
    route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe)
VALUES ($1,'projection-legacy','attempt-legacy',101,701,801,1,1,
        '/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','legacy','legacy_adapter',$2,
        'openai.chat_completions.v1',1,1,1,FALSE)`,
		fixture.orgID, "sha256:"+strings.Repeat("1", 64))
	require.NoError(t, err)

	insertProjectionFreeze(t, pool, fixture.orgID, "projection-verify")
	insertCanonicalProjectionAttempt(t, fixture, "projection-verify", "attempt-verify", "verify", 702, 802, "2")
	insertProjectionFreeze(t, pool, fixture.orgID, "projection-canonical")
	insertCanonicalProjectionAttempt(t, fixture, "projection-canonical", "attempt-canonical", "canonical", 0, 0, "3")

	insertProjectionFreeze(t, pool, fixture.orgID, "projection-precontract")
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name)
VALUES ($1,'projection-precontract','attempt-precontract',101,703,803,1,1,
        '/v1/chat/completions','provider','channel')`, fixture.orgID)
	require.NoError(t, err)

	tests := []struct {
		name, requestID, attemptID string
		source                     executionprojection.Source
		mode                       executionevidence.AuthorityMode
		channelID                  int64
		canonical                  bool
	}{
		{name: "legacy", requestID: "projection-legacy", attemptID: "attempt-legacy", source: executionprojection.SourceProviderAttempt, mode: executionevidence.ModeLegacy, channelID: 701},
		{name: "verify", requestID: "projection-verify", attemptID: "attempt-verify", source: executionprojection.SourceProviderAttempt, mode: executionevidence.ModeVerify, channelID: 702, canonical: true},
		{name: "canonical", requestID: "projection-canonical", attemptID: "attempt-canonical", source: executionprojection.SourceProviderAttempt, mode: executionevidence.ModeCanonical, canonical: true},
		{name: "precontract", requestID: "projection-precontract", attemptID: "attempt-precontract", source: executionprojection.SourcePrecontractProviderAttempt, channelID: 703},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
				OrgID: fixture.orgID, RequestID: test.requestID, AttemptID: test.attemptID,
				NoAttemptSource: executionprojection.SourceNoProviderAttempt, AttemptRequired: true,
			})
			require.NoError(t, err)
			require.Equal(t, test.source, actual.Source)
			if test.source == executionprojection.SourcePrecontractProviderAttempt {
				require.Nil(t, actual.Compact)
				require.Equal(t, test.channelID, actual.LegacyChannelID)
				return
			}
			require.NotNil(t, actual.Compact)
			require.Equal(t, test.mode, actual.Compact.AuthorityMode)
			require.Equal(t, test.channelID, actual.Compact.LegacyChannelID)
			require.Equal(t, "openai.chat_completions.v1", actual.Compact.ModelOperationID)
			if test.canonical {
				require.Equal(t, fixture.publicModelID, actual.Compact.PublicModelID)
				require.Equal(t, fixture.deploymentID, actual.Compact.DeploymentID)
				require.Equal(t, fixture.connectionID, actual.Compact.ConnectionID)
				require.Equal(t, fixture.bindingID, actual.Compact.CredentialBindingID)
				require.Equal(t, fixture.releaseID, actual.Compact.RuntimeReleaseID)
			}
		})
	}
	canonical, err := executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: "projection-canonical", AttemptID: "attempt-canonical",
		NoAttemptSource: executionprojection.SourceNoProviderAttempt, AttemptRequired: true,
	})
	require.NoError(t, err)
	require.NotNil(t, canonical.Compact)
	compact := canonical.Compact
	boundHash, err := executionprojection.BindSourceEventHash(strings.Repeat("9", 64), canonical)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,source_event_hash,
    route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
    route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
    route_model_operation_id,route_runtime_release_id,route_snapshot_hash)
VALUES ('projection-canonical',$1,'attempt-canonical',$2,
        'provider_attempt',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		fixture.orgID, boundHash, compact.ContractVersion, string(compact.AuthorityMode), compact.EvidenceHash,
		compact.PublicModelID, compact.DeploymentID, compact.ConnectionID, compact.CredentialBindingID,
		compact.ModelOperationID, compact.RuntimeReleaseID, compact.SnapshotHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO request_trace_events (
    request_id,org_id,event_type,stage,attempt_id,source_event_hash,
    route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
    route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
    route_model_operation_id,route_runtime_release_id,route_snapshot_hash)
VALUES ('projection-canonical',$1,'provider.response_accepted','provider','attempt-canonical',$2,
        'provider_attempt',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		fixture.orgID, boundHash, compact.ContractVersion, string(compact.AuthorityMode), compact.EvidenceHash,
		compact.PublicModelID, compact.DeploymentID, compact.ConnectionID, compact.CredentialBindingID,
		compact.ModelOperationID, compact.RuntimeReleaseID, compact.SnapshotHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
DELETE FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2`, fixture.bindingID, fixture.deploymentID)
	require.NoError(t, err,
		"historical evidence must not prevent a later explicit credential unlink or rotation")

	_, err = executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: "projection-canonical", AttemptID: "attempt-not-present",
		NoAttemptSource: executionprojection.SourceNoProviderAttempt, AttemptRequired: true,
	})
	require.ErrorIs(t, err, executionprojection.ErrExpectedAttemptMissing)
	_, err = executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: "projection-precontract", AttemptID: "attempt-precontract",
		NoAttemptSource: executionprojection.SourceNoProviderAttempt, AttemptRequired: true,
		ExpectedLegacyChannelID: 999,
	})
	require.ErrorContains(t, err, "exact provider attempt route mismatch")
	cache, err := executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: "projection-cache", NoAttemptSource: executionprojection.SourceResponseCache,
	})
	require.NoError(t, err)
	require.Equal(t, executionprojection.SourceResponseCache, cache.Source)
	local, err := executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: "projection-local-terminal", NoAttemptSource: executionprojection.SourceNoProviderAttempt,
	})
	require.NoError(t, err)
	require.Equal(t, executionprojection.SourceNoProviderAttempt, local.Source)
}

type projectionCanonicalFixture struct {
	pool                                                                            *pgxpool.Pool
	orgID, publicModelID, connectionID, deploymentID, bindingID, probeID, releaseID int64
	linkedAt, probeTestedAt                                                         time.Time
}

func seedProjectionCanonicalFixture(t *testing.T, pool *pgxpool.Pool) projectionCanonicalFixture {
	t.Helper()
	ctx := t.Context()
	fixture := projectionCanonicalFixture{pool: pool}
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('projection-modes','active','customer') RETURNING id`).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'projection-owner','fixture','org_admin','active','org_admin') RETURNING id`,
		fixture.orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'projection-model','Projection Model','custom','ga',TRUE) RETURNING id`,
		fixture.orgID).Scan(&fixture.publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('projection-connection','custom','https://projection.invalid','openai_compatible','{}')
RETURNING id`).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'projection-upstream') RETURNING id`,
		fixture.orgID, fixture.publicModelID, fixture.connectionID).Scan(&fixture.deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'encrypted-fixture','active',1) RETURNING id`, fixture.connectionID).Scan(&fixture.bindingID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3) RETURNING created_at`, fixture.bindingID, fixture.deploymentID, fixture.connectionID).Scan(&fixture.linkedAt))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','production_approved','approval:operation',NOW(),1,
        'openai.chat_completions','1.0.0')`, fixture.deploymentID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,source_kind,source_ref,
    source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,'openai.chat_completions.v1','openai.chat_completions','1.0.0',
        'projection-upstream','projection-upstream',1,1,$3,1,TRUE,200,'integration','projection-test',
        $4,$5,NOW()) RETURNING probe_id,tested_at`,
		fixture.deploymentID, fixture.connectionID, fixture.bindingID,
		"sha256:"+strings.Repeat("d", 64), userID).Scan(&fixture.probeID, &fixture.probeTestedAt))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ('rcr_projection_modes','projection-model','unirouter.runtime-change-candidate.v1',$1,$2,
        '{}','[]','{}','unchanged','active',1,0,'projection fixture',$3,$3,$3,NOW(),NOW())
RETURNING id`, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), userID).Scan(&fixture.releaseID))
	return fixture
}

func insertProjectionFreeze(t *testing.T, pool *pgxpool.Pool, orgID int64, requestID string) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ($1,$2,'settled','projection-test')`, requestID, orgID)
	require.NoError(t, err)
}

func insertCanonicalProjectionAttempt(t *testing.T, fixture projectionCanonicalFixture, requestID, attemptID, mode string, channelID, channelKeyID int64, hashDigit string) {
	t.Helper()
	ctx := t.Context()
	commonArgs := []any{
		fixture.orgID, requestID, attemptID, "sha256:" + strings.Repeat(hashDigit, 64),
		fixture.publicModelID, fixture.deploymentID, fixture.connectionID, fixture.bindingID,
		fixture.linkedAt, fixture.probeID, fixture.probeTestedAt, fixture.releaseID,
		"mrs1_" + strings.Repeat("e", 64),
	}
	if mode == "canonical" {
		_, err := fixture.pool.Exec(ctx, `
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
VALUES ($1,$2,$3,101,NULL,NULL,$6,1,1,'/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','canonical','canonical_profile',$4,
        $5,$7,$8,1,1,1,1,$9,
        'openai.chat_completions.v1',1,'openai.chat_completions','1.0.0',
        'production_approved','approval:operation',NOW(),'[]',$14,
        $10,$11,$12,'rcr_projection_modes','active',1,$13,FALSE)`,
			append(commonArgs, "sha256:"+strings.Repeat("f", 64))...)
		require.NoError(t, err)
		return
	}
	require.Equal(t, "verify", mode)
	_, err := fixture.pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,deployment_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,route_evidence_hash,
    route_public_model_id,route_connection_id,route_credential_binding_id,
    route_connection_revision,route_deployment_revision,route_circuit_revision,
    route_credential_state_revision,route_credential_linked_at,
    route_model_operation_id,route_credential_probe_id,route_credential_probe_tested_at,
    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,route_slot_version,
    route_snapshot_hash,route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe)
VALUES ($1,$2,$3,101,$14,$15,$6,1,1,'/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','verify','legacy_adapter',$4,
        $5,$7,$8,1,1,1,1,$9,'openai.chat_completions.v1',$10,$11,
        $12,'rcr_projection_modes','active',1,$13,1,1,1,FALSE)`, append(commonArgs, channelID, channelKeyID)...)
	require.NoError(t, err)
}
