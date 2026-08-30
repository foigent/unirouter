//go:build integration

package billing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestProviderAttemptPersistsCanonicalAndTruthfulVerifyEvidence(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	canonical := seedProviderAttemptCanonicalEvidence(t, pool, orgID, model)

	requestID := "attempt-evidence-canonical"
	freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, canonical.SnapshotHash)
	start := providerAttemptFixture(requestID, "attempt-canonical", orgID, 1, 1)
	start.ChannelID = 0
	start.ChannelKeyID = 0
	start.ExecutionSnapshotHash = canonical.SnapshotHash
	start.RouteEvidence = &canonical
	wrongSnapshot := canonical
	wrongSnapshot.SnapshotHash = "mrs1_" + strings.Repeat("d", 64)
	wrongSnapshotStart := start
	wrongSnapshotStart.ExecutionSnapshotHash = wrongSnapshot.SnapshotHash
	wrongSnapshotStart.RouteEvidence = &wrongSnapshot
	require.ErrorIs(t, b.BeginProviderAttempt(t.Context(), wrongSnapshotStart), ErrProviderAttemptConflict)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, b.BeginProviderAttemptTx(t.Context(), tx, start))
	require.NoError(t, tx.Rollback(t.Context()))
	var rows int
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&rows))
	require.Zero(t, rows, "transactional begin must never commit the caller's transaction")

	require.NoError(t, b.BeginProviderAttempt(t.Context(), start))
	require.NoError(t, b.BeginProviderAttempt(t.Context(), start), "identical evidence replay must be idempotent")
	conflictEvidence := canonical
	conflictEvidence.SlotVersion++
	conflict := start
	conflict.RouteEvidence = &conflictEvidence
	require.ErrorIs(t, b.BeginProviderAttempt(t.Context(), conflict), ErrProviderAttemptConflict)

	var storedMode, storedSource, storedHash, storedProfile string
	var storedChannelID, storedChannelKeyID *int64
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT route_authority_mode,route_execution_contract_source,route_evidence_hash,
       route_protocol_profile_id,channel_id,channel_key_id
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, start.AttemptID).Scan(
		&storedMode, &storedSource, &storedHash, &storedProfile, &storedChannelID, &storedChannelKeyID))
	require.Equal(t, "canonical", storedMode)
	require.Equal(t, "canonical_profile", storedSource)
	require.True(t, executionevidence.IsDigest(storedHash))
	require.Equal(t, canonical.ProtocolProfileID, storedProfile)
	require.Nil(t, storedChannelID)
	require.Nil(t, storedChannelKeyID)

	verifyRequestID := "attempt-evidence-verify-legacy"
	freezeForInvariantTestWithSnapshot(t, b, orgID, model, verifyRequestID, 100, canonical.SnapshotHash)
	verify := canonical
	verify.AuthorityMode = executionevidence.ModeVerify
	verify.ExecutionContractSource = executionevidence.ContractSourceLegacyAdapter
	verify.LegacyChannelID = 2201
	verify.LegacyChannelKeyID = 2301
	verify.LegacyRoutingRevision = 4
	verify.LegacyCircuitRevision = 5
	verify.LegacyCredentialStateRevision = 6
	verify.OperationRevision = 0
	verify.ProtocolProfileID = ""
	verify.ProtocolProfileVersion = ""
	verify.OperationEvidenceState = ""
	verify.OperationEvidenceRef = ""
	verify.OperationVerifiedAt = nil
	verify.CapabilityEvidence = nil
	verify.CapabilityEvidenceHash = ""
	verifyStart := providerAttemptFixture(verifyRequestID, "attempt-verify-legacy", orgID, 1, 1)
	verifyStart.ChannelID = verify.LegacyChannelID
	verifyStart.ChannelKeyID = verify.LegacyChannelKeyID
	verifyStart.ExecutionSnapshotHash = verify.SnapshotHash
	verifyStart.RouteEvidence = &verify
	require.NoError(t, b.BeginProviderAttempt(t.Context(), verifyStart))

	var verifySource, actualOperation string
	var verifyProfile *string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT route_execution_contract_source,route_model_operation_id,route_protocol_profile_id
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, verifyRequestID, verifyStart.AttemptID).Scan(&verifySource, &actualOperation, &verifyProfile))
	require.Equal(t, "legacy_adapter", verifySource)
	require.Equal(t, "openai.chat_completions.v1", actualOperation)
	require.Nil(t, verifyProfile, "v1 verify execution must not masquerade as legacy.any or a probe profile")

	badCanonical := canonical
	badStart := providerAttemptFixture("unused", "unused", orgID, 1, 1)
	badStart.RouteEvidence = &badCanonical
	_, err = normalizeProviderAttemptStart(badStart)
	require.ErrorContains(t, err, "cannot carry legacy route identities")
}

func TestProviderAttemptPersistsEvidenceV2AndExactPublicProjectionIntegration(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	evidence := seedProviderAttemptPublicEvidenceV2(t, pool, orgID, model)
	requestID := "attempt-evidence-public-v2"
	freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, evidence.SnapshotHash)
	start := providerAttemptFixture(requestID, "attempt-public-v2", orgID, 1, 1)
	start.ChannelID = 0
	start.ChannelKeyID = 0
	start.ExecutionSnapshotHash = evidence.SnapshotHash
	start.Endpoint = modeloperation.PathResponses
	start.PublicRouteEvidence = &evidence
	v1Public, err := evidence.RouteEvidenceV1()
	require.NoError(t, err)
	v1Start := start
	v1Start.PublicRouteEvidence = nil
	v1Start.RouteEvidence = &v1Public
	_, err = normalizeProviderAttemptStart(v1Start)
	require.ErrorContains(t, err, "v1_public_operation")

	require.NoError(t, b.BeginProviderAttempt(t.Context(), start))
	require.NoError(t, b.BeginProviderAttempt(t.Context(), start), "exact V2 replay must be idempotent")
	conflicting := evidence
	conflicting.SlotVersion++
	conflictStart := start
	conflictStart.PublicRouteEvidence = &conflicting
	require.ErrorIs(t, b.BeginProviderAttempt(t.Context(), conflictStart), ErrProviderAttemptConflict)

	var version, evidenceHash, delivery, charge, idempotency string
	var publicBindingID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT route_evidence_version,route_evidence_hash,
       route_public_execution_binding_id,route_delivery_contract_version,
       route_charge_contract_version,route_idempotency_contract_version
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, start.AttemptID).Scan(
		&version, &evidenceHash, &publicBindingID, &delivery, &charge, &idempotency,
	))
	require.Equal(t, executionevidence.ContractVersionV2, version)
	require.True(t, executionevidence.IsDigest(evidenceHash))
	require.Equal(t, evidence.PublicExecutionBindingID, publicBindingID)
	require.Equal(t, evidence.DeliveryContractVersion, delivery)
	require.Equal(t, evidence.ChargeContractVersion, charge)
	require.Equal(t, evidence.IdempotencyContractVersion, idempotency)

	_, err = pool.Exec(t.Context(), `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,
    project_id,access_credential_id,credential_ref,access_grant_id,
    channel_id,channel_key_id,deployment_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_public_model_id,route_connection_id,
    route_credential_binding_id,route_connection_revision,route_deployment_revision,
    route_circuit_revision,route_credential_state_revision,route_credential_linked_at,
    route_model_operation_id,route_model_operation_revision,
    route_protocol_profile_id,route_protocol_profile_version,
    route_operation_evidence_state,route_operation_evidence_ref,route_operation_verified_at,
    route_capability_evidence,route_capability_evidence_hash,
    route_credential_probe_id,route_credential_probe_tested_at,
    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,
    route_slot_version,route_snapshot_hash,
    route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe,route_probe_lease_token_hash,
    route_public_execution_binding_id,route_delivery_contract_version,
    route_charge_contract_version,route_idempotency_contract_version,
    dispatch_owner_hash)
SELECT
    org_id,request_id,'attempt-public-v1-direct',api_key_id,
    project_id,access_credential_id,credential_ref,access_grant_id,
    channel_id,channel_key_id,deployment_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    'unirouter.execution-route.v1',route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_public_model_id,route_connection_id,
    route_credential_binding_id,route_connection_revision,route_deployment_revision,
    route_circuit_revision,route_credential_state_revision,route_credential_linked_at,
    route_model_operation_id,route_model_operation_revision,
    route_protocol_profile_id,route_protocol_profile_version,
    route_operation_evidence_state,route_operation_evidence_ref,route_operation_verified_at,
    route_capability_evidence,route_capability_evidence_hash,
    route_credential_probe_id,route_credential_probe_tested_at,
    route_runtime_release_id,route_runtime_release_ref,route_runtime_stage,
    route_slot_version,route_snapshot_hash,
    route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe,route_probe_lease_token_hash,
    NULL,NULL,NULL,NULL,dispatch_owner_hash
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID)
	require.ErrorContains(t, err, "billing_provider_attempts_execution_evidence_check",
		"direct SQL must not downgrade a public operation into EvidenceV1")

	projection, err := executionprojection.Resolve(t.Context(), pool, executionprojection.ResolveInput{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		NoAttemptSource: executionprojection.SourceNoProviderAttempt, AttemptRequired: true,
	})
	require.NoError(t, err)
	require.NotNil(t, projection.Compact)
	require.Equal(t, publicBindingID, projection.Compact.PublicExecutionBindingID)
	require.Equal(t, delivery, projection.Compact.DeliveryContractVersion)
	require.Equal(t, charge, projection.Compact.ChargeContractVersion)
	require.Equal(t, idempotency, projection.Compact.IdempotencyContractVersion)
	boundHash, err := executionprojection.BindSourceEventHash(strings.Repeat("9", 64), projection)
	require.NoError(t, err)
	compact := projection.Compact
	_, err = pool.Exec(t.Context(), `
INSERT INTO call_logs (
    request_id,org_id,attempt_id,source_event_hash,
    route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
    route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
    route_model_operation_id,route_runtime_release_id,route_snapshot_hash,
    route_public_execution_binding_id,route_delivery_contract_version,
    route_charge_contract_version,route_idempotency_contract_version)
VALUES ($1,$2,$3,$4,'provider_attempt',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,
        $15,$16,$17,$18)`,
		requestID, orgID, start.AttemptID, boundHash,
		compact.ContractVersion, string(compact.AuthorityMode), compact.EvidenceHash,
		compact.PublicModelID, compact.DeploymentID, compact.ConnectionID,
		compact.CredentialBindingID, compact.ModelOperationID,
		compact.RuntimeReleaseID, compact.SnapshotHash,
		compact.PublicExecutionBindingID, compact.DeliveryContractVersion,
		compact.ChargeContractVersion, compact.IdempotencyContractVersion)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
UPDATE billing_provider_attempts
   SET route_charge_contract_version='other'
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID)
	require.ErrorContains(t, err, "provider attempt execution identity is immutable")
	_, err = pool.Exec(t.Context(), `
UPDATE call_logs
   SET route_charge_contract_version='other'
 WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.ErrorContains(t, err, "execution evidence projection is immutable")
}

func TestOwnedProviderAttemptAndTerminalHealthFenceAreReplaySafe(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	evidence := seedProviderAttemptCanonicalEvidence(t, pool, orgID, model)
	requestID := "attempt-owned-health-fence"
	freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, evidence.SnapshotHash)
	start := providerAttemptFixture(requestID, "attempt-owned", orgID, 1, 1)
	start.ChannelID = 0
	start.ChannelKeyID = 0
	start.ExecutionSnapshotHash = evidence.SnapshotHash
	start.RouteEvidence = &evidence
	ownerHash := "sha256:" + strings.Repeat("1", 64)
	otherOwnerHash := "sha256:" + strings.Repeat("2", 64)
	evidenceHash, err := executionevidence.Digest(evidence)
	require.NoError(t, err)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	created, err := b.BeginProviderAttemptOwnedTx(t.Context(), tx, start, ownerHash)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, tx.Commit(t.Context()))

	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	created, err = b.BeginProviderAttemptOwnedTx(t.Context(), tx, start, ownerHash)
	require.NoError(t, err)
	require.False(t, created, "same-owner commit-ambiguity replay must not mint a new receipt")
	require.NoError(t, tx.Commit(t.Context()))

	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	_, err = b.BeginProviderAttemptOwnedTx(t.Context(), tx, start, otherOwnerHash)
	require.ErrorIs(t, err, ErrProviderAttemptConflict)
	require.NoError(t, tx.Rollback(t.Context()))
	owner := ProviderAttemptOwnerRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		EvidenceHash: evidenceHash, DispatchOwnerHash: ownerHash,
	}
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, b.ValidateProviderAttemptOwnerTx(t.Context(), tx, owner))
	require.NoError(t, tx.Commit(t.Context()))
	wrongOwnerRequest := owner
	wrongOwnerRequest.DispatchOwnerHash = otherOwnerHash
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, b.ValidateProviderAttemptOwnerTx(t.Context(), tx, wrongOwnerRequest), ErrProviderAttemptConflict)
	require.NoError(t, tx.Rollback(t.Context()))
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	terminalProbeTx := tx
	defer func() { _ = terminalProbeTx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), `
UPDATE billing_provider_attempts SET state='response_accepted'
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID)
	require.NoError(t, err)
	require.NoError(t, b.ValidateProviderAttemptOwnerTx(t.Context(), tx, owner),
		"accepted headers must not end a streaming lease")
	releaseAfterAccepted := ProviderAttemptHealthFenceRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		EvidenceHash: evidenceHash, DispatchOwnerHash: ownerHash,
		Outcome: ProviderAttemptHealthReleasedUndispatched,
	}
	_, err = b.FenceProviderAttemptHealthTx(t.Context(), tx, releaseAfterAccepted)
	require.ErrorIs(t, err, ErrProviderAttemptHealthConflict,
		"a response-accepted attempt was dispatched and cannot be released as undispatched")
	_, err = tx.Exec(t.Context(), `
UPDATE billing_provider_attempts
	SET state='response_unusable',
		outcome_certainty='unknown',
		failure_kind='channel_failure',
		response_received_at=COALESCE(response_received_at,NOW())
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID)
	require.NoError(t, err)
	require.ErrorIs(t, b.ValidateProviderAttemptOwnerTx(t.Context(), tx, owner), ErrProviderAttemptConflict)
	require.NoError(t, tx.Rollback(t.Context()))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))

	fence := ProviderAttemptHealthFenceRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		EvidenceHash: evidenceHash, DispatchOwnerHash: ownerHash,
		Outcome: ProviderAttemptHealthProviderFailure,
	}
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	result, err := b.FenceProviderAttemptHealthTx(t.Context(), tx, fence)
	require.NoError(t, err)
	require.Equal(t, ProviderAttemptHealthClaimed, result)
	require.NoError(t, tx.Commit(t.Context()))
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, b.ValidateProviderAttemptOwnerTx(t.Context(), tx, owner), ErrProviderAttemptHealthConflict)
	require.NoError(t, tx.Rollback(t.Context()))

	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	result, err = b.FenceProviderAttemptHealthTx(t.Context(), tx, fence)
	require.NoError(t, err)
	require.Equal(t, ProviderAttemptHealthReplay, result)
	require.NoError(t, tx.Commit(t.Context()))

	conflict := fence
	conflict.Outcome = ProviderAttemptHealthSuccess
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	_, err = b.FenceProviderAttemptHealthTx(t.Context(), tx, conflict)
	require.ErrorIs(t, err, ErrProviderAttemptHealthConflict)
	require.NoError(t, tx.Rollback(t.Context()))

	wrongOwner := fence
	wrongOwner.DispatchOwnerHash = otherOwnerHash
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	_, err = b.FenceProviderAttemptHealthTx(t.Context(), tx, wrongOwner)
	require.ErrorIs(t, err, ErrProviderAttemptConflict)
	require.NoError(t, tx.Rollback(t.Context()))

	_, err = pool.Exec(t.Context(), `
UPDATE billing_provider_attempts SET dispatch_owner_hash=$4
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID, otherOwnerHash)
	require.ErrorContains(t, err, "dispatch owner is immutable")
	_, err = pool.Exec(t.Context(), `
UPDATE billing_provider_attempts SET health_outcome='success'
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, start.AttemptID)
	require.ErrorContains(t, err, "health outcome is terminal")

	// Exercise the current Billing implementation against the latest schema
	// before proving the older dispatch-fence migration remains guarded. M83
	// owns columns read by reservationForUpdate, so removing it before the
	// attempt would create an unsupported mixed binary/schema state rather than
	// test the provider-attempt invariant.
	_, err = pool.Exec(t.Context(), `
UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, evidence.ConnectionID)
	require.NoError(t, err)
	// This test deliberately continues into the pre-M85 schema to exercise the
	// older dispatch-fence rollback guard. Discard only the M85-owned binding;
	// the provider-attempt evidence that M73 must reject remains intact.
	_, err = pool.Exec(t.Context(), `ALTER TABLE freeze_idempotency DISABLE TRIGGER freeze_execution_snapshot_immutable`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `ALTER TABLE billing_provider_attempts DISABLE TRIGGER provider_attempt_execution_snapshot_immutable`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
UPDATE freeze_idempotency SET execution_snapshot_hash=NULL WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
UPDATE billing_provider_attempts SET execution_snapshot_hash=NULL WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `ALTER TABLE freeze_idempotency ENABLE TRIGGER freeze_execution_snapshot_immutable`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `ALTER TABLE billing_provider_attempts ENABLE TRIGGER provider_attempt_execution_snapshot_immutable`)
	require.NoError(t, err)
	// The newer public-binding migrations must be reversed before the M85
	// guard can permit this isolated fixture to enter the legacy schema.
	require.NoError(t, testutil.RewindKimiK27PublicationForLegacyMigrationTest(
		t.Context(), pool, testutil.MigrationsDir(t),
	))
	for _, name := range []string{
		"000100_kimi_k26_function_tools_tool_index_normalizer.down.sql",
		"000099_kimi_k26_function_tools_probe_v3.down.sql",
		"000098_kimi_k26_nonstream_usage_probe_v2.down.sql",
		"000097_kimi_k26_stream_choice_usage_probe_v3.down.sql",
		"000096_kimi_k26_stream_probe_contract_v2.down.sql",
		"000095_kimi_k26_chat_core_qualification.down.sql",
		"000094_deepseek_v4_pro_chat_core_qualification.down.sql",
		"000093_deepseek_function_tool_probe_contract_v2.down.sql",
		"000092_deepseek_reasoning_tool_choice_contract.down.sql",
		"000091_deepseek_chat_tools_json_capabilities.down.sql",
		"000090_deepseek_chat_reasoning_capabilities.down.sql",
		"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
		"000088_model_capability_execution_qualification.down.sql",
		"000087_anthropic_messages_tools_public_binding.down.sql",
		"000086_exact_public_execution_bindings.down.sql",
	} {
		down, readErr := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
		require.NoError(t, readErr)
		_, err = pool.Exec(t.Context(), string(down))
		require.NoError(t, err, name)
	}
	planFenceDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000085_model_execution_plan_freeze_fence.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(planFenceDownSQL))
	require.NoError(t, err)
	currencyDraftDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000084_official_rate_source_currency_drafts.down.sql",
	))
	require.NoError(t, err)
	unitMeterDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000083_versioned_unit_meter_billing.down.sql",
	))
	require.NoError(t, err)
	sealedContractDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000082_sealed_model_execution_contracts.down.sql",
	))
	require.NoError(t, err)
	profileDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000081_versioned_model_execution_profiles.down.sql",
	))
	require.NoError(t, err)
	observationReconciliationDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000080_observation_provider_cost_reconciliation.down.sql",
	))
	require.NoError(t, err)
	connectionLicenseDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000079_canonical_connection_license_authority.down.sql",
	))
	require.NoError(t, err)
	authoringDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000078_atomic_model_release_authoring.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(currencyDraftDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(unitMeterDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(sealedContractDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(profileDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(observationReconciliationDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(connectionLicenseDownSQL))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(authoringDownSQL))
	require.NoError(t, err)

	publicAuthorityDownSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(publicAuthorityDownSQL))
	require.NoError(t, err)
	lifecycleReceiptDownSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000076_model_deployment_lifecycle_receipts.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(lifecycleReceiptDownSQL))
	require.NoError(t, err)
	qualificationDownSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000075_model_operation_production_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(qualificationDownSQL))
	require.NoError(t, err)
	createReceiptDownSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000074_model_deployment_create_idempotency.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(createReceiptDownSQL))
	require.NoError(t, err)
	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000073_model_execution_dispatch_fence.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(downSQL))
	require.ErrorContains(t, err, "dispatch owner or terminal health evidence exists")
}

func TestReleasedUndispatchedAttemptResolvesLiabilityAndDoesNotBlockTheNextAttemptOrCancel(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	evidence := seedProviderAttemptCanonicalEvidence(t, pool, orgID, model)
	requestID := "attempt-released-undispatched"
	freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, evidence.SnapshotHash)
	start := providerAttemptFixture(requestID, "attempt-released-one", orgID, 1, 2)
	start.ChannelID = 0
	start.ChannelKeyID = 0
	start.ExecutionSnapshotHash = evidence.SnapshotHash
	start.RouteEvidence = &evidence
	evidenceHash, err := executionevidence.Digest(evidence)
	require.NoError(t, err)

	release := func(attemptID, ownerHash string) {
		t.Helper()
		candidate := start
		candidate.AttemptID = attemptID
		if attemptID != start.AttemptID {
			candidate.RouteSequence = 2
		}
		tx, txErr := pool.Begin(t.Context())
		require.NoError(t, txErr)
		created, txErr := b.BeginProviderAttemptOwnedTx(t.Context(), tx, candidate, ownerHash)
		require.NoError(t, txErr)
		require.True(t, created)
		require.NoError(t, tx.Commit(t.Context()))

		tx, txErr = pool.Begin(t.Context())
		require.NoError(t, txErr)
		result, txErr := b.FenceProviderAttemptHealthTx(t.Context(), tx, ProviderAttemptHealthFenceRequest{
			OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
			EvidenceHash: evidenceHash, DispatchOwnerHash: ownerHash,
			Outcome: ProviderAttemptHealthReleasedUndispatched,
		})
		require.NoError(t, txErr)
		require.Equal(t, ProviderAttemptHealthClaimed, result)
		require.NoError(t, tx.Commit(t.Context()))
	}

	firstOwner := "sha256:" + strings.Repeat("3", 64)
	release(start.AttemptID, firstOwner)
	var state, certainty string
	var resolved bool
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT state,outcome_certainty,financial_resolved_at IS NOT NULL
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
		orgID, requestID, start.AttemptID).Scan(&state, &certainty, &resolved))
	require.Equal(t, ProviderAttemptStateReleasedUndispatched, state)
	require.Equal(t, providerAttemptCertaintyNotDispatched, certainty)
	require.True(t, resolved)
	require.ErrorIs(t, b.CompleteProviderAttempt(t.Context(), ProviderAttemptCompletion{
		OrgID: orgID, RequestID: requestID, AttemptID: start.AttemptID,
		Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}), ErrProviderAttemptConflict)

	release("attempt-released-two", "sha256:"+strings.Repeat("4", 64))
	require.NoError(t, b.Cancel(t.Context(), requestID, orgID),
		"released attempts are financially resolved and must not force a provider-cost hold")
	var reservationState string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`,
		orgID, requestID).Scan(&reservationState))
	require.Equal(t, "cancelled", reservationState)
}

func TestTerminalHealthFenceRejectsOutOfSequenceAttemptStates(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	evidence := seedProviderAttemptCanonicalEvidence(t, pool, orgID, model)
	evidenceHash, err := executionevidence.Digest(evidence)
	require.NoError(t, err)
	tests := []struct {
		name       string
		completion *ProviderAttemptCompletion
		outcome    string
	}{
		{name: "success before provider dispatch", outcome: ProviderAttemptHealthSuccess},
		{name: "auth health on client request", outcome: ProviderAttemptHealthAuthFailure, completion: &ProviderAttemptCompletion{
			Outcome: ProviderAttemptOutcomeRejected, HTTPStatus: 400, FailureKind: "client_request",
		}},
		{name: "provider health on auth rejection", outcome: ProviderAttemptHealthProviderFailure, completion: &ProviderAttemptCompletion{
			Outcome: ProviderAttemptOutcomeRejected, HTTPStatus: 401, FailureKind: "auth",
		}},
		{name: "rate health after accepted response", outcome: ProviderAttemptHealthRateLimited, completion: &ProviderAttemptCompletion{
			Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
		}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestID := fmt.Sprintf("attempt-health-sequence-%d", index)
			attemptID := fmt.Sprintf("attempt-sequence-%d", index)
			freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, evidence.SnapshotHash)
			start := providerAttemptFixture(requestID, attemptID, orgID, 1, 1)
			start.ChannelID = 0
			start.ChannelKeyID = 0
			start.ExecutionSnapshotHash = evidence.SnapshotHash
			start.RouteEvidence = &evidence
			ownerHash := "sha256:" + strings.Repeat(fmt.Sprintf("%x", index+5), 64)
			tx, txErr := pool.Begin(t.Context())
			require.NoError(t, txErr)
			_, txErr = b.BeginProviderAttemptOwnedTx(t.Context(), tx, start, ownerHash)
			require.NoError(t, txErr)
			require.NoError(t, tx.Commit(t.Context()))
			if test.completion != nil {
				completion := *test.completion
				completion.OrgID = orgID
				completion.RequestID = requestID
				completion.AttemptID = attemptID
				require.NoError(t, b.CompleteProviderAttempt(t.Context(), completion))
			}

			tx, txErr = pool.Begin(t.Context())
			require.NoError(t, txErr)
			_, txErr = b.FenceProviderAttemptHealthTx(t.Context(), tx, ProviderAttemptHealthFenceRequest{
				OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
				EvidenceHash: evidenceHash, DispatchOwnerHash: ownerHash, Outcome: test.outcome,
			})
			require.ErrorIs(t, txErr, ErrProviderAttemptHealthConflict)
			require.NoError(t, tx.Rollback(t.Context()))
			var outcome *string
			require.NoError(t, pool.QueryRow(t.Context(), `
SELECT health_outcome FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`,
				orgID, requestID, attemptID).Scan(&outcome))
			require.Nil(t, outcome)
			if index == 0 {
				_, directErr := pool.Exec(t.Context(), `
UPDATE billing_provider_attempts
   SET health_outcome='success',health_applied_at=clock_timestamp()
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, orgID, requestID, attemptID)
				require.ErrorContains(t, directErr, "health outcome is out of sequence",
					"the schema trigger must enforce the same pre-dispatch state matrix")
			}
		})
	}
}

func seedProviderAttemptCanonicalEvidence(t *testing.T, pool *pgxpool.Pool, orgID int64, model string) executionevidence.EvidenceV1 {
	t.Helper()
	ctx := t.Context()
	var userID, publicModelID, connectionID, deploymentID, bindingID, probeID, releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','org_admin','active','org_admin') RETURNING id`,
		orgID, "attempt-evidence-owner-"+strings.ReplaceAll(model, "_", "-")).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,$2,'Attempt Evidence Model','custom','ga',TRUE) RETURNING id`, orgID, model).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ($1,'custom','https://attempt-evidence.invalid','openai_compatible','{}') RETURNING id`,
		"attempt-evidence-connection-"+model).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'upstream-model') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'encrypted-fixture','active',1) RETURNING id`, connectionID).Scan(&bindingID))
	var linkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3) RETURNING created_at`, bindingID, deploymentID, connectionID).Scan(&linkedAt))
	var verifiedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','production_approved','approval:operation',NOW(),1,
        'openai.chat_completions','1.0.0') RETURNING verified_at`, deploymentID).Scan(&verifiedAt))
	var capabilityVerifiedAt time.Time
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
    deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,verified_at,revision)
VALUES ($1,'openai.chat_completions.v1','stream','enabled','production_approved','approval:stream',NOW(),1)`, deploymentID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT verified_at FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1' AND capability_id='stream'`,
		deploymentID).Scan(&capabilityVerifiedAt))
	var probeTestedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    credential_binding_id,credential_state_revision,success,status_code,source_kind,source_ref,
    source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,'openai.chat_completions.v1','openai.chat_completions','1.0.0',
        'upstream-model','upstream-model',1,1,$3,1,TRUE,200,'integration','attempt-evidence',
        $4,$5,NOW()) RETURNING probe_id,tested_at`, deploymentID, connectionID, bindingID,
		"sha256:"+strings.Repeat("d", 64), userID).Scan(&probeID, &probeTestedAt))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,candidate,
    provider_pack_pins,simulation_report,overall_risk,stage,version,canary_basis_points,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ($1,$2,'unirouter.runtime-change-candidate.v1',$3,$4,'{}','[]','{}','unchanged',
        'active',1,0,'attempt evidence fixture',$5,$5,$5,NOW(),NOW()) RETURNING id`,
		"rcr_attempt_evidence_"+strings.ReplaceAll(model, "_", "-"), model,
		"sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), userID).Scan(&releaseID))

	capabilities := []executionevidence.CapabilityEntry{{
		CapabilityID: "stream", SupportState: "enabled", EvidenceState: "production_approved",
		EvidenceRef: "approval:stream", VerifiedAt: &capabilityVerifiedAt, Revision: 1,
	}}
	capabilityHash, err := executionevidence.CapabilityDigest(capabilities)
	require.NoError(t, err)
	return executionevidence.EvidenceV1{
		ContractVersion: executionevidence.ContractVersion, AuthorityMode: executionevidence.ModeCanonical,
		ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
		PublicModelID:           publicModelID, DeploymentID: deploymentID, ConnectionID: connectionID,
		CredentialBindingID: bindingID, ConnectionRevision: 1, DeploymentRevision: 1,
		CircuitRevision: 1, CredentialStateRevision: 1, CredentialLinkedAt: linkedAt,
		ModelOperationID: "openai.chat_completions.v1", OperationRevision: 1,
		ProtocolProfileID: "openai.chat_completions", ProtocolProfileVersion: "1.0.0",
		OperationEvidenceState: "production_approved", OperationEvidenceRef: "approval:operation",
		OperationVerifiedAt: &verifiedAt, CapabilityEvidence: capabilities,
		CapabilityEvidenceHash: capabilityHash, CredentialProbeID: probeID,
		CredentialProbeTestedAt: probeTestedAt, RuntimeReleaseID: releaseID,
		RuntimeReleaseRef: "rcr_attempt_evidence_" + strings.ReplaceAll(model, "_", "-"),
		RuntimeStage:      "active", SlotVersion: 1,
		SnapshotHash: "mrs1_" + strings.Repeat("c", 64),
	}
}

type billingPublicSnapshotBuilder struct{}

func (billingPublicSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

func seedProviderAttemptPublicEvidenceV2(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID int64,
	model string,
) executionevidence.EvidenceV2 {
	t.Helper()
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
UPDATE orgs SET status='active',org_type='customer',parent_id=NULL,updated_at=NOW()
 WHERE id=$1`, orgID)
	require.NoError(t, err)

	var userID, publicModelID, connectionID, deploymentID, credentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','platform_admin','active','platform_admin') RETURNING id`,
		orgID, "attempt-public-owner-"+strings.ReplaceAll(model, "_", "-")).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,'Attempt Public Model','openai','ga',TRUE,128000,8192) RETURNING id`, orgID, model).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
    name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'openai','https://provider.invalid','openai_compatible','{}','enabled',1)
RETURNING id`, "attempt-public-connection-"+model).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,degradation_threshold)
VALUES ($1,$2,$3,'upstream-public-model','enabled',1,100,100,128000,4096,10)
RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'encrypted-fixture','active',1) RETURNING id`, connectionID).Scan(&credentialID))
	var linkedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3) RETURNING created_at`, credentialID, deploymentID, connectionID).Scan(&linkedAt))

	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.responses.v1','enabled','declared','',1,$2,'1.0.0')`,
		deploymentID, endpointprotocol.ProfileOpenAIResponsesText)
	require.NoError(t, err)
	var probeID int64
	var probeTestedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    operation_revision,public_path,executor_contract_version,https_origin,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,'openai.responses.v1',$3,'1.0.0','upstream-public-model','upstream-public-model',
        1,1,1,$4,$5,'https://provider.invalid',$6,1,TRUE,200,1,
        'admin.endpointprotocol_live_probe.v1',$7,$8,$9,NOW())
RETURNING probe_id,tested_at`, deploymentID, connectionID,
		endpointprotocol.ProfileOpenAIResponsesText, modeloperation.PathResponses,
		modelqualification.ExecutorContractVersion, credentialID,
		fmt.Sprintf("billing-public:%d", deploymentID),
		fmt.Sprintf("sha256:%064x", deploymentID), userID).Scan(&probeID, &probeTestedAt))
	result, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=1`,
		deploymentID, string(modeloperation.IDResponses), probeID, probeTestedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())
	_, err = pool.Exec(ctx, `
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
       operation.revision,operation.revision+1,$2,$3,'billing EvidenceV2 fixture',
       'admin.model_operation_production_approval.v1','probe:' || probe.probe_id::text,NOW()
  FROM model_deployment_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
 WHERE probe.probe_id=$1`, probeID, userID, orgID)
	require.NoError(t, err)

	var operationRevision int64
	var operationState, operationRef string
	var operationVerifiedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT operation.revision,operation.evidence_state,operation.evidence_ref,operation.verified_at
  FROM model_deployment_operations operation
  JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
 WHERE operation.deployment_id=$1 AND operation.operation_id='openai.responses.v1'
   AND qualification.qualified`, deploymentID).Scan(
		&operationRevision, &operationState, &operationRef, &operationVerifiedAt,
	))

	manager, err := runtimechange.NewManager(pool, billingPublicSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	sealed := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   userID,
		ActorOrgID:    orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: deploymentID,
			OperationID:  string(modeloperation.IDResponses),
		}},
		Reason: "billing EvidenceV2 public authority: " + strings.ReplaceAll(model, "_", "-"),
	})
	active := integrationfixture.ActivateInitialDeploymentRelease(t, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   userID,
		ActorOrgID:    orgID,
	}, sealed)
	route := active.Candidate.DeploymentRoutes[0]

	capabilityHash, err := executionevidence.CapabilityDigest(nil)
	require.NoError(t, err)
	return executionevidence.EvidenceV2{
		EvidenceV1: executionevidence.EvidenceV1{
			ContractVersion:         executionevidence.ContractVersionV2,
			AuthorityMode:           executionevidence.ModeCanonical,
			ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
			PublicModelID:           publicModelID, DeploymentID: deploymentID,
			ConnectionID: connectionID, CredentialBindingID: credentialID,
			ConnectionRevision: 1, DeploymentRevision: 1, CircuitRevision: 1,
			CredentialStateRevision: 1, CredentialLinkedAt: linkedAt,
			ModelOperationID: string(modeloperation.IDResponses), OperationRevision: operationRevision,
			ProtocolProfileID: endpointprotocol.ProfileOpenAIResponsesText, ProtocolProfileVersion: "1.0.0",
			OperationEvidenceState: operationState, OperationEvidenceRef: operationRef,
			OperationVerifiedAt: &operationVerifiedAt, CapabilityEvidence: []executionevidence.CapabilityEntry{},
			CapabilityEvidenceHash: capabilityHash, CredentialProbeID: probeID,
			CredentialProbeTestedAt: probeTestedAt, RuntimeReleaseID: active.ID,
			RuntimeReleaseRef: active.ReleaseRef, RuntimeStage: active.Stage,
			SlotVersion: 1, SnapshotHash: "mrs1_" + strings.Repeat("c", 64),
		},
		PublicExecutionBindingID:   route.PublicExecutionBindingID,
		DeliveryContractVersion:    route.DeliveryContractVersion,
		ChargeContractVersion:      route.ChargeContractVersion,
		IdempotencyContractVersion: route.IdempotencyContractVersion,
	}
}
