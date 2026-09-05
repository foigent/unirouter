//go:build integration

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

const (
	publicGatewayPGIdempotencyHashSecret = "m77-public-idempotency-hash-secret"
	publicGatewayPGFingerprintSecret     = "m77-public-fingerprint-secret"
)

func publicGatewayPGDowngradeCapabilityPublicationsForLegacyRollbackTest(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	require.NoError(t, testutil.RewindCapabilityPublicationsForLegacyMigrationTest(
		t.Context(), pool, testutil.MigrationsDir(t),
	), "legacy rollback fixture must start from the exact M100 ceiling")
}

func TestPublicResponsesExactProfileCapabilityEvidenceLoadsThroughSQLPlannerIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	for _, model := range []struct {
		name, profileID, upstreamModel string
	}{
		{name: "sol", profileID: "openai.gpt-5.6-sol.responses", upstreamModel: "gpt-5.6-sol"},
		{name: "terra", profileID: "openai.gpt-5.6-terra.responses", upstreamModel: "gpt-5.6-terra"},
		{name: "luna", profileID: "openai.gpt-5.6-luna.responses", upstreamModel: "gpt-5.6-luna"},
	} {
		t.Run(model.name, func(t *testing.T) {
			label := model.name + "-reasoning-sql-evidence"
			suite := newPublicGatewayPGSuite(t, pool, label)
			fixture := suite.newRouteWithExecutionProfile(
				t,
				label,
				modeloperation.IDResponses,
				publicGatewayPGExecutionProfile{
					id: model.profileID, version: "1.2.0",
					baseURL: "https://api.openai.com", upstreamModel: model.upstreamModel,
					context: 272_000, output: 128_000,
					capabilities: []string{modelprofile.CapabilityReasoningEffort},
				},
			)

			plan, err := fixture.planner.Plan(t.Context(), modelrouting.Request{
				Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
				PublicModel: fixture.model, ModelOperationID: string(fixture.operation),
				SelectionKey: model.name + "-reasoning-empty-caller-capabilities",
			})
			require.NoError(t, err)
			require.Empty(t, plan.RequiredCapabilities,
				"a payload-free Planner call must not treat every supported capability as required")

			// The operation parser derives provider-default semantics while the SQL
			// Planner holds the exact Profile and route snapshot. Do not manufacture
			// requirements from the Profile's supported-capability list.
			derive := func(profile modelprofile.Profile, definition modelprofile.Definition) ([]modelrouting.CapabilityRequirement, error) {
				require.Equal(t, model.profileID, profile.ID)
				require.Equal(t, "1.2.0", profile.Version)
				binding, bindErr := modelqualification.CompiledBindingFor(string(modeloperation.IDResponses), profile.ProtocolProfile, profile.ProtocolProfileVersion)
				require.NoError(t, bindErr)
				identity, bindErr := modelqualification.CompiledPublicBindingFor(binding)
				require.NoError(t, bindErr)
				contract, contractErr := publicRequestContractForProfile(profile, string(modeloperation.IDResponses), identity)
				require.NoError(t, contractErr)
				parser, parserErr := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
				require.NoError(t, parserErr)
				prepared, prepareErr := parser.PrepareRequest(publicGatewayPGRequestBody(modeloperation.IDResponses, fixture.model), fixture.model, contract)
				require.NoError(t, prepareErr)
				return publicInvocationCapabilityRequirements(profile, prepared)
			}
			plan, err = fixture.planner.Plan(t.Context(), modelrouting.Request{
				Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
				PublicModel: fixture.model, ModelOperationID: string(fixture.operation),
				SelectionKey:                 model.name + "-typed-provider-default",
				DeriveInvocationCapabilities: derive,
			})
			require.NoError(t, err)
			require.Equal(t, []string{modelprofile.CapabilityReasoningEffort}, plan.RequiredCapabilities)
			require.Equal(t, []modelrouting.CapabilityRequirement{{
				CapabilityID: modelprofile.CapabilityReasoningEffort,
				Origins:      []string{modelrouting.CapabilityOriginProviderDefault},
			}}, plan.CapabilityRequirements)
			require.Len(t, plan.Routes, 1)

			result, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployment_capabilities
   SET evidence_state='provider_documented',revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`,
				fixture.deploymentID, fixture.operation, modelprofile.CapabilityReasoningEffort)
			require.NoError(t, err)
			require.Equal(t, int64(1), result.RowsAffected())

			_, err = fixture.planner.Plan(t.Context(), modelrouting.Request{
				Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
				PublicModel: fixture.model, ModelOperationID: string(fixture.operation),
				SelectionKey:                 model.name + "-reasoning-downgraded-evidence",
				DeriveInvocationCapabilities: derive,
			})
			require.ErrorIs(t, err, modelrouting.ErrNoRoute)
		})
	}
}

func TestPublicModelExecutionAuthorityExactBindingsMigrationRoundTripIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	fixture := newPublicGatewayPGFixture(t, pool, "exact-bindings-v1-roundtrip", modeloperation.IDResponses)
	readBinding := func() string {
		var body string
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT row_to_json(binding)::text FROM model_operation_public_execution_bindings binding
 WHERE public_execution_binding_id=$1`, fixture.activeBindingID).Scan(&body))
		return body
	}
	original := readBinding()
	for cycle := 0; cycle < 2; cycle++ {
		publicGatewayPGDowngradeCapabilityPublicationsForLegacyRollbackTest(t, pool)
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
			body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
			require.NoError(t, err)
			_, err = pool.Exec(t.Context(), string(body))
			require.NoError(t, err)
		}
		require.Equal(t, original, readBinding(), "downgrade must preserve the sealed v1 binding")
		require.NoError(t, testutil.InstallExactTestArtifact(t.Context(), pool, testutil.MigrationsDir(t)))
		require.Equal(t, original, readBinding(), "upgrade must not reinterpret the sealed v1 binding")
		plan, err := fixture.planner.Plan(t.Context(), modelrouting.Request{
			Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
			PublicModel: fixture.model, ModelOperationID: string(fixture.operation), SelectionKey: "v1-roundtrip",
		})
		require.NoError(t, err)
		require.Len(t, plan.Routes, 1)
		require.Equal(t, fixture.activeBindingID, plan.Routes[0].PublicEvidence.PublicExecutionBindingID)
	}
}

func TestPublicModelExecutionAuthorityExactBindingsRollbackProtectsSealedEvidenceIntegration(t *testing.T) {
	for _, tc := range []struct {
		name, profileID, version, upstream string
		operation                          modeloperation.ID
	}{
		{"responses-v2", "openai.gpt-5.6-sol.responses", "1.3.0", "gpt-5.6-sol", modeloperation.IDResponses},
		{"messages-v2", "anthropic.claude-opus-5.api", "1.2.0", "claude-opus-5", modeloperation.IDMessages},
		{"messages-tools-v3", "anthropic.claude-sonnet-5.api", "1.4.0", "claude-sonnet-5", modeloperation.IDMessages},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			suite := newPublicGatewayPGSuite(t, pool, tc.name)
			origin := "https://api.anthropic.com"
			if tc.operation == modeloperation.IDResponses {
				origin = "https://api.openai.com"
			}
			fixture := suite.newRouteWithExecutionProfile(t, tc.name, tc.operation, publicGatewayPGExecutionProfile{
				id: tc.profileID, version: tc.version, baseURL: origin, upstreamModel: tc.upstream,
				capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort},
			})
			publicGatewayPGDowngradeCapabilityPublicationsForLegacyRollbackTest(t, pool)
			for _, capabilityDownName := range []string{
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
			} {
				capabilityDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), capabilityDownName))
				require.NoError(t, err)
				_, err = pool.Exec(t.Context(), string(capabilityDown))
				require.NoError(t, err, capabilityDownName)
			}
			name := "000087_anthropic_messages_tools_public_binding.down.sql"
			if tc.name != "messages-tools-v3" {
				body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
				require.NoError(t, err)
				_, err = pool.Exec(t.Context(), string(body))
				require.NoError(t, err)
				name = "000086_exact_public_execution_bindings.down.sql"
			}
			body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
			require.NoError(t, err)
			_, err = pool.Exec(t.Context(), string(body))
			require.ErrorContains(t, err, "immutable evidence refers to")
			var retained bool
			require.NoError(t, pool.QueryRow(t.Context(), `
SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`,
				strings.TrimSuffix(name, ".down.sql")).Scan(&retained))
			require.True(t, retained)
			plan, err := fixture.planner.Plan(t.Context(), modelrouting.Request{
				Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
				PublicModel: fixture.model, ModelOperationID: string(fixture.operation), SelectionKey: "retained-exact-binding",
			})
			require.NoError(t, err)
			require.Len(t, plan.Routes, 1)
			require.Equal(t, fixture.activeBindingID, plan.Routes[0].PublicEvidence.PublicExecutionBindingID)
			require.Equal(t, fixture.identity.ProfileID, plan.Routes[0].ProtocolProfileID)
		})
	}
}

func TestPublicModelExecutionAuthorityStreamFailuresPreserveFinancialOutcomeIntegration(t *testing.T) {
	for _, surface := range []struct {
		operation modeloperation.ID
		version   string
	}{
		{modeloperation.IDResponses, "1.3.0"},
		{modeloperation.IDMessages, "1.3.0"},
		{modeloperation.IDMessages, "1.4.0"},
	} {
		operation := surface.operation
		for _, tc := range []struct {
			name, state, reservation string
			status                   int
		}{
			{"transport", billing.ProviderAttemptOutcomeUnknown, "settlement_pending", 0},
			{"rejected", billing.ProviderAttemptOutcomeRejected, "cancelled", http.StatusServiceUnavailable},
			{"after_bytes", billing.ProviderAttemptOutcomeResponseUnusable, "settlement_pending", http.StatusOK},
			{"terminal_receipt_failure", billing.ProviderAttemptOutcomeResponseAccepted, "settlement_pending", http.StatusOK},
			{"completed", "settled", "settled", http.StatusOK},
		} {
			t.Run(string(operation)+"/"+surface.version+"/"+tc.name, func(t *testing.T) {
				if tc.name == "terminal_receipt_failure" && operation != modeloperation.IDResponses {
					t.Skip("shared receipt-failure recovery contract is covered by Responses")
				}
				pool := testutil.SetupMigratedTestDB(t)
				publicGatewayPGDowngradeCapabilityPublicationsForLegacyRollbackTest(t, pool)
				suite := newPublicGatewayPGSuite(t, pool, "stream-financial-"+tc.name)
				profile := publicGatewayPGExecutionProfile{
					id: "openai.gpt-5.6-sol.responses", version: "1.3.0",
					baseURL: "https://api.openai.com", upstreamModel: "gpt-5.6-sol",
					capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort, modelprofile.CapabilityStreaming},
				}
				sse := publicResponsesSSE("safe", " answer")
				terminal := "event: response.completed"
				bind := newPublicResponsesStreamBinding
				if operation == modeloperation.IDMessages {
					profile.id, profile.version = "anthropic.claude-sonnet-5.api", surface.version
					profile.baseURL, profile.upstreamModel = "https://api.anthropic.com", "claude-sonnet-5"
					sse, terminal, bind = publicAnthropicMessagesSSE("safe", " answer"), "event: message_stop", newPublicAnthropicMessagesStreamBinding
				}
				fixture := suite.newRouteWithExecutionProfile(t, tc.name, operation, profile)
				sse = strings.ReplaceAll(sse, "provider-private-model", profile.upstreamModel)
				if tc.name == "after_bytes" || tc.name == "terminal_receipt_failure" {
					sse = strings.Split(sse, terminal)[0]
				}
				calls := 0
				doer := publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if tc.status == 0 {
						return nil, errors.New("transport interrupted")
					}
					return &http.Response{
						StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
						Body: io.NopCloser(strings.NewReader(sse)),
					}, nil
				})
				coordinator := &publicGatewayPGMutatingCoordinator{publicModelExecutionCoordinator: fixture.coordinator}
				core := fixture.newOrchestratorWithCoordinator(coordinator, doer, allowPublicNonStreamGuard{})
				core.registry = endpointprotocol.NewPublicStreamRegistry()
				orchestrator := &publicStreamOrchestrator{
					core: core, billing: fixture.billing, pinned: disabledPublicStreamGuard(t),
					streamPolicy: streampolicy.Default(), bindStream: bind,
				}
				if tc.name == "terminal_receipt_failure" {
					orchestrator.billing = &publicGatewayPGFailedStreamTerminalBilling{Billing: fixture.billing}
				}
				request := publicGatewayPGRequest(fixture, tc.name, fixture.model)
				request.Body = []byte(strings.TrimSuffix(string(request.Body), "}") + `,"stream":true}`)
				events := []string{}
				writer := &publicRuntimeWriter{events: &events}
				err := orchestrator.execute(t.Context(), writer, request)
				require.NoError(t, coordinator.authorizeError, "real stream authorization")
				if tc.name == "completed" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				require.Equal(t, 1, calls)
				var state, reservation, health string
				var status, charges int
				require.NoError(t, pool.QueryRow(t.Context(), `
SELECT a.state,COALESCE(a.http_status,0),f.status,COALESCE(a.health_outcome,'')
FROM billing_provider_attempts a JOIN freeze_idempotency f USING (org_id,request_id)
WHERE a.org_id=$1 AND a.request_id=$2`, fixture.orgID, request.RequestID).Scan(&state, &status, &reservation, &health))
				require.Equal(t, tc.state, state)
				require.Equal(t, tc.status, status)
				require.Equal(t, tc.reservation, reservation)
				if tc.name == "terminal_receipt_failure" {
					require.Empty(t, health, "unpersisted provider outcome cannot authorize a health action")
				} else {
					require.NotEmpty(t, health, "terminal completion must precede the durable health action")
				}
				if tc.name != "completed" {
					require.NoError(t, pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, fixture.orgID, request.RequestID).Scan(&charges))
					require.Zero(t, charges)
				}
				if tc.name == "after_bytes" {
					require.NotEmpty(t, writer.body.String())
				}
				if tc.name == "terminal_receipt_failure" {
					_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases SET lease_expires_at=clock_timestamp()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID)
					require.NoError(t, err)
					recovery, recoveryErr := fixture.billing.RecoverExpiredStreamDeliveries(t.Context(), 10)
					require.NoError(t, recoveryErr)
					require.Equal(t, 1, recovery.ProviderCostHeld)
					require.Zero(t, recovery.Settled)
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, fixture.orgID, request.RequestID).Scan(&charges))
					require.Zero(t, charges, "provider-final public contract cannot charge estimated checkpoint usage")
					var missingPayload bool
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT status,settlement_payload_ready_at IS NULL FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID).Scan(&reservation, &missingPayload))
					require.Equal(t, "settlement_pending", reservation)
					require.True(t, missingPayload)
					var attemptID, certainty, pendingReason string
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT a.attempt_id,a.state,a.outcome_certainty,f.settlement_pending_reason
FROM billing_provider_attempts a JOIN freeze_idempotency f USING(org_id,request_id)
WHERE a.org_id=$1 AND a.request_id=$2`, fixture.orgID, request.RequestID).Scan(&attemptID, &state, &certainty, &pendingReason))
					require.Equal(t, billing.ProviderAttemptOutcomeResponseAccepted, state)
					require.Equal(t, "pending", certainty)
					require.Equal(t, "provider_attempt_unresolved", pendingReason)
					estimated := billing.SettleRequest{RequestID: request.RequestID, AttemptID: attemptID, OrgID: fixture.orgID,
						PromptTokens: 10, CompletionTokens: 4, UsageSource: metering.UsageSourceCounterV2,
						UsageAuthority: metering.UsageAuthorityGatewayCounter, UsagePurpose: metering.UsagePurposeCustomerCharge,
						UsageFinality: metering.UsageFinalityEstimated, UsageContractVersion: metering.UsageContractVersion,
						UsageEstimateVersion: metering.GatewayCounterEstimateVersionV2}
					require.ErrorIs(t, fixture.billing.PersistSettlementUsage(t.Context(), estimated), billing.ErrProviderFinalUsageRequired)
					_, err = fixture.billing.Settle(t.Context(), estimated)
					require.ErrorIs(t, err, billing.ErrProviderFinalUsageRequired)
					again, err := fixture.billing.RecoverExpiredStreamDeliveries(t.Context(), 10)
					require.NoError(t, err)
					require.Zero(t, again.Claimed)
					pending, err := fixture.billing.RecoverPendingSettlements(t.Context(), 10)
					require.NoError(t, err)
					require.Zero(t, pending.Claimed)
					require.ErrorIs(t, fixture.billing.Cancel(t.Context(), request.RequestID, fixture.orgID), billing.ErrSettlementPending)
				}
				if tc.name == "completed" {
					publicGatewayPGVerifyExactAttemptProjections(t, fixture, request.RequestID)
				}
			})
		}
	}
}

// Inject both terminal-write failures so the existing expiry worker, rather
// than a successful local Finish, owns the crash-recovery continuation.
type publicGatewayPGFailedStreamTerminalBilling struct{ *billing.Billing }

func (b *publicGatewayPGFailedStreamTerminalBilling) CompleteProviderAttempt(ctx context.Context, req billing.ProviderAttemptCompletion) error {
	if req.Outcome == billing.ProviderAttemptOutcomeResponseUnusable {
		return errors.New("injected terminal receipt storage failure")
	}
	return b.Billing.CompleteProviderAttempt(ctx, req)
}

func (*publicGatewayPGFailedStreamTerminalBilling) FinishStreamDelivery(context.Context, billing.StreamDeliveryStart, string, string) error {
	return errors.New("injected process loss before lease finish")
}

func publicGatewayPGVerifyExactAttemptProjections(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	pool, ctx := fixture.pool, t.Context()
	var attemptID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_id FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID).Scan(&attemptID))
	projection, err := executionprojection.Resolve(ctx, pool, executionprojection.ResolveInput{
		OrgID: fixture.orgID, RequestID: requestID, AttemptID: attemptID, AttemptRequired: true,
		NoAttemptSource: executionprojection.SourceNoProviderAttempt,
	})
	require.NoError(t, err)
	require.NotNil(t, projection.Compact)
	require.Equal(t, fixture.identity.DeliveryContractVersion, projection.Compact.DeliveryContractVersion)
	boundHash, err := executionprojection.BindSourceEventHash(strings.Repeat("9", 64), projection)
	require.NoError(t, err)
	for _, table := range []string{"call_logs", "request_trace_events"} {
		extraColumns, extraValues := "", ""
		if table == "request_trace_events" {
			extraColumns, extraValues = ",event_type,stage", ",'provider.response_accepted','provider'"
		}
		_, err = pool.Exec(ctx, `INSERT INTO `+table+` (
request_id,org_id,attempt_id,source_event_hash,route_evidence_source,
route_evidence_version,route_authority_mode,route_evidence_hash,
route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
route_model_operation_id,route_runtime_release_id,route_snapshot_hash,
route_public_execution_binding_id,route_delivery_contract_version,route_charge_contract_version,route_idempotency_contract_version`+extraColumns+`)
SELECT request_id,org_id,attempt_id,$3,'provider_attempt',route_evidence_version,route_authority_mode,route_evidence_hash,
route_public_model_id,deployment_id,route_connection_id,route_credential_binding_id,
route_model_operation_id,route_runtime_release_id,route_snapshot_hash,
route_public_execution_binding_id,route_delivery_contract_version,route_charge_contract_version,route_idempotency_contract_version`+extraValues+`
FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID, boundHash)
		require.NoError(t, err, table)
		for index, mutation := range []map[string]any{
			{"route_delivery_contract_version": nil}, {"route_charge_contract_version": nil},
			{"route_idempotency_contract_version": nil}, {"route_delivery_contract_version": "unirouter.public-nonstream-delivery.v1"},
		} {
			mutation["id"] = -index - 1
			wire, err := json.Marshal(mutation)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO `+table+`
SELECT (jsonb_populate_record(NULL::`+table+`,to_jsonb(p)||$3::jsonb)).*
FROM `+table+` p WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID, string(wire))
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr, "%s mutation %d", table, index)
			require.Equal(t, "23503", pgErr.Code, "exact attempt projection must reject mismatches before insertion")
		}
	}
	for index, mutation := range []map[string]any{
		{"route_delivery_contract_version": nil}, {"route_delivery_contract_version": ""},
		{"route_charge_contract_version": nil}, {"route_idempotency_contract_version": nil},
		{"route_delivery_contract_version": "unknown"},
		{"route_delivery_contract_version": "unirouter.public-nonstream-delivery.v1"},
		{"route_protocol_profile_id": "unirouter.openai.responses.text.v1"},
	} {
		mutation["attempt_id"] = fmt.Sprintf("bad-tuple-%d", index)
		wire, err := json.Marshal(mutation)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts
SELECT (jsonb_populate_record(NULL::billing_provider_attempts,to_jsonb(a)||$3::jsonb)).*
FROM billing_provider_attempts a WHERE org_id=$1 AND request_id=$2 AND attempt_id=$4`, fixture.orgID, requestID, string(wire), attemptID)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, "mutation %d", index)
		require.Contains(t, []string{"23503", "23514"}, pgErr.Code)
	}
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	var down []byte
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
	} {
		var readErr error
		down, readErr = os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
		require.NoError(t, readErr)
		_, err = conn.Exec(ctx, string(down))
		require.NoError(t, err, name)
	}
	guardedDown := "000086_exact_public_execution_bindings.down.sql"
	guardedError := "provider-attempt or projection evidence refers"
	if fixture.identity.DeliveryContractVersion == endpointprotocol.PublicAnthropicMessagesToolsDeliveryV1 {
		guardedDown = "000087_anthropic_messages_tools_public_binding.down.sql"
		guardedError = "immutable evidence refers"
	} else {
		name := "000087_anthropic_messages_tools_public_binding.down.sql"
		down, readErr := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
		require.NoError(t, readErr)
		_, err = conn.Exec(ctx, string(down))
		require.NoError(t, err, name)
	}
	down, err = os.ReadFile(filepath.Join(testutil.MigrationsDir(t), guardedDown))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(down))
	require.ErrorContains(t, err, guardedError)
	_, err = conn.Exec(ctx, "ROLLBACK")
	require.NoError(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID, requestID, attemptID).Scan(&count))
	require.Equal(t, 1, count, "refused down must preserve the original evidence")
}

func TestPublicClaudeSonnet5ExactProfileUsesTypedMessagesPolicyAndCapabilityEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	suite := newPublicGatewayPGSuite(t, pool, "sonnet5-messages-policy")
	fixture := suite.newRouteWithExecutionProfile(
		t,
		"sonnet5-messages-policy",
		modeloperation.IDMessages,
		publicGatewayPGExecutionProfile{
			id: "anthropic.claude-sonnet-5.api", version: "1.2.0",
			baseURL: "https://api.anthropic.com", upstreamModel: "claude-sonnet-5",
			context: 1_000_000, output: 128_000,
			capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort},
		},
	)
	requestedModel := "public-alias-" + publicGatewayPGHash(fixture.releaseSuffix)[:16]
	doer := &publicGatewayPGDoer{
		operation: modeloperation.IDMessages, upstreamModel: fixture.upstreamModel,
		secret: fixture.providerSecret, responseText: "public answer",
		responseBody: `{
  "id":"msg_thinking","type":"message","role":"assistant","model":"claude-sonnet-5",
  "content":[{"type":"thinking","thinking":"","signature":"opaque-signature"},{"type":"text","text":"public answer"}],
  "stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":3},"inference_geo":"global","service_tier":"standard"}
}`,
	}
	relay := publicGatewayPGRelay(t, fixture, doer)
	handler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[0])
	encodedModel, err := json.Marshal(requestedModel)
	require.NoError(t, err)
	validBody := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"messages":[{"role":"user","content":"hello"}]}`)
	valid := publicGatewayPGServe(
		handler,
		fixture.identity.PublicPath,
		validBody,
		"sonnet5-valid",
	)
	require.Equal(t, http.StatusOK, valid.Code, valid.Body.String())
	require.EqualValues(t, 1, doer.calls.Load())
	providerRequests := doer.snapshot()
	require.Len(t, providerRequests, 1)
	require.Contains(t, providerRequests[0].body, `"model":"claude-sonnet-5"`)
	require.Contains(t, providerRequests[0].body, `"thinking":{"type":"adaptive"}`)
	require.Contains(t, providerRequests[0].body, `"output_config":{"effort":"max"}`)
	require.Contains(t, valid.Body.String(), `"signature":"opaque-signature"`)
	require.Contains(t, valid.Body.String(), `"inference_geo":"global"`)
	require.Contains(t, valid.Body.String(), `"service_tier":"standard"`)
	doer.responseBody = `{
  "id":"msg_refusal","type":"message","role":"assistant","model":"claude-sonnet-5",
  "content":[],"stop_reason":"refusal","stop_sequence":null,
  "stop_details":{"type":"refusal","category":"cyber","explanation":"request declined"},
  "usage":{"input_tokens":10,"output_tokens":1}
}`
	refusal := publicGatewayPGServe(
		handler,
		fixture.identity.PublicPath,
		publicGatewayPGRequestBody(modeloperation.IDMessages, requestedModel),
		"sonnet5-refusal",
	)
	require.Equal(t, http.StatusOK, refusal.Code, refusal.Body.String())
	require.Contains(t, refusal.Body.String(), `"stop_reason":"refusal"`)
	require.EqualValues(t, 2, doer.calls.Load())
	doer.responseBody = ""

	prefill := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prefix"}]}`)
	rejected := publicGatewayPGServe(handler, fixture.identity.PublicPath, prefill, "sonnet5-prefill")
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	require.EqualValues(t, 2, doer.calls.Load(), "invalid request must fail before provider dispatch")
	tooManyOutput := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":128001,"messages":[{"role":"user","content":"hello"}]}`)
	overLimit := publicGatewayPGServe(handler, fixture.identity.PublicPath, tooManyOutput, "sonnet5-over-output-limit")
	require.Equal(t, http.StatusBadRequest, overLimit.Code, overLimit.Body.String())
	require.EqualValues(t, 2, doer.calls.Load(), "output-limit rejection must precede provider dispatch")

	result, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployment_capabilities
   SET evidence_state='provider_documented',revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`,
		fixture.deploymentID, fixture.operation, modelprofile.CapabilityReasoningEffort)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())
	noEvidence := publicGatewayPGServe(
		handler,
		fixture.identity.PublicPath,
		publicGatewayPGRequestBody(modeloperation.IDMessages, requestedModel),
		"sonnet5-no-capability-evidence",
	)
	require.Equal(t, http.StatusBadGateway, noEvidence.Code, noEvidence.Body.String())
	require.EqualValues(t, 2, doer.calls.Load(), "route without capability evidence must not dispatch")
}

func TestPublicClaudeOpus5ExactProfileEnforcesThinkingEffortCombinationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	suite := newPublicGatewayPGSuite(t, pool, "opus5-thinking-effort")
	fixture := suite.newRouteWithExecutionProfile(
		t,
		"opus5-thinking-effort",
		modeloperation.IDMessages,
		publicGatewayPGExecutionProfile{
			id: "anthropic.claude-opus-5.api", version: "1.1.0",
			baseURL: "https://api.anthropic.com", upstreamModel: "claude-opus-5",
			context: 1_000_000, output: 128_000,
			capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningEffort},
		},
	)
	requestedModel := "public-alias-" + publicGatewayPGHash(fixture.releaseSuffix)[:16]
	doer := &publicGatewayPGDoer{
		operation: modeloperation.IDMessages, upstreamModel: fixture.upstreamModel,
		secret: fixture.providerSecret, responseText: "public answer",
		responseBody: `{
  "id":"msg_opus","type":"message","role":"assistant","model":"claude-opus-5",
  "content":[{"type":"thinking","thinking":"","signature":"opaque-signature"},{"type":"text","text":"public answer"}],
  "stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":3},"inference_geo":"global","service_tier":"standard"}
}`,
	}
	relay := publicGatewayPGRelay(t, fixture, doer)
	handler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[0])
	encodedModel, err := json.Marshal(requestedModel)
	require.NoError(t, err)

	validBody := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"messages":[{"role":"user","content":"hello"}]}`)
	valid := publicGatewayPGServe(handler, fixture.identity.PublicPath, validBody, "opus5-valid")
	require.Equal(t, http.StatusOK, valid.Code, valid.Body.String())
	require.EqualValues(t, 1, doer.calls.Load())
	require.Contains(t, doer.snapshot()[0].body, `"output_config":{"effort":"max"}`)
	require.Contains(t, valid.Body.String(), `"signature":"opaque-signature"`)
	require.Contains(t, valid.Body.String(), `"service_tier":"standard"`)

	for _, effort := range []string{"xhigh", "max"} {
		body := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"thinking":{"type":"disabled"},"output_config":{"effort":"` + effort + `"},"messages":[{"role":"user","content":"hello"}]}`)
		rejected := publicGatewayPGServe(handler, fixture.identity.PublicPath, body, "opus5-disabled-"+effort)
		require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
		require.EqualValues(t, 1, doer.calls.Load(), "invalid Opus combination must fail before provider dispatch")
	}
}

func TestPublicClaudeHaiku45ExactProfileEnforcesManualThinkingBudgetIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	suite := newPublicGatewayPGSuite(t, pool, "haiku45-thinking-budget")
	fixture := suite.newRouteWithExecutionProfile(
		t,
		"haiku45-thinking-budget",
		modeloperation.IDMessages,
		publicGatewayPGExecutionProfile{
			id: "anthropic.claude-haiku-4.5.api", version: "1.1.0",
			baseURL: "https://api.anthropic.com", upstreamModel: "claude-haiku-4-5-20251001",
			context: 200_000, output: 64_000,
			capabilities: []string{modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget},
		},
	)
	requestedModel := "public-alias-" + publicGatewayPGHash(fixture.releaseSuffix)[:16]
	doer := &publicGatewayPGDoer{
		operation: modeloperation.IDMessages, upstreamModel: fixture.upstreamModel,
		secret: fixture.providerSecret, responseText: "public answer",
		responseBody: `{
  "id":"msg_haiku","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001",
  "content":[{"type":"thinking","thinking":"thinking summary","signature":"opaque-signature"},{"type":"text","text":"public answer"}],
  "stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":10,"output_tokens":12,"output_tokens_details":{"thinking_tokens":7},"inference_geo":"global","service_tier":"standard"}
}`,
	}
	relay := publicGatewayPGRelay(t, fixture, doer)
	handler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[0])
	encodedModel, err := json.Marshal(requestedModel)
	require.NoError(t, err)

	validBody := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`)
	valid := publicGatewayPGServe(handler, fixture.identity.PublicPath, validBody, "haiku45-valid")
	require.Equal(t, http.StatusOK, valid.Code, valid.Body.String())
	require.EqualValues(t, 1, doer.calls.Load())
	providerRequests := doer.snapshot()
	require.Len(t, providerRequests, 1)
	require.Contains(t, providerRequests[0].body, `"thinking":{"type":"enabled","budget_tokens":1024}`)
	require.Contains(t, valid.Body.String(), `"thinking":"thinking summary"`)
	require.Contains(t, valid.Body.String(), `"signature":"opaque-signature"`)
	require.Contains(t, valid.Body.String(), `"service_tier":"standard"`)

	invalidBody := []byte(`{"model":` + string(encodedModel) + `,"max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":4096},"messages":[{"role":"user","content":"hello"}]}`)
	rejected := publicGatewayPGServe(handler, fixture.identity.PublicPath, invalidBody, "haiku45-invalid-budget")
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	require.EqualValues(t, 1, doer.calls.Load(), "invalid Haiku budget must fail before provider dispatch")

	result, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployment_capabilities
   SET evidence_state='provider_documented',revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=$3`,
		fixture.deploymentID, fixture.operation, modelprofile.CapabilityReasoningBudget)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())
	noBudgetEvidence := publicGatewayPGServe(
		handler, fixture.identity.PublicPath, validBody, "haiku45-no-budget-evidence",
	)
	require.Equal(t, http.StatusBadGateway, noBudgetEvidence.Code, noBudgetEvidence.Body.String())
	require.EqualValues(t, 1, doer.calls.Load(), "route without budget evidence must not dispatch")
}

type publicGatewayPGDoer struct {
	operation     modeloperation.ID
	upstreamModel string
	secret        string
	responseText  string
	responseBody  string
	transportErr  error
	calls         atomic.Int64
	mu            sync.Mutex
	requests      []publicGatewayPGProviderRequest
}

type publicGatewayPGProviderRequest struct {
	path             string
	body             string
	authorization    string
	apiKey           string
	anthropicVersion string
}

func (doer *publicGatewayPGDoer) Do(request *http.Request) (*http.Response, error) {
	doer.calls.Add(1)
	providerRequest := publicGatewayPGProviderRequest{}
	if request != nil {
		providerRequest.path = request.URL.Path
		providerRequest.authorization = request.Header.Get("Authorization")
		providerRequest.apiKey = request.Header.Get("x-api-key")
		providerRequest.anthropicVersion = request.Header.Get("anthropic-version")
		if request.Body != nil {
			body, _ := io.ReadAll(request.Body)
			providerRequest.body = string(body)
		}
	}
	doer.mu.Lock()
	doer.requests = append(doer.requests, providerRequest)
	doer.mu.Unlock()
	if doer.transportErr != nil {
		return nil, doer.transportErr
	}
	responseBody := doer.responseBody
	if responseBody == "" {
		responseBody = publicGatewayPGProviderResponse(doer.operation, doer.upstreamModel, doer.responseText)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}, nil
}

func (doer *publicGatewayPGDoer) snapshot() []publicGatewayPGProviderRequest {
	doer.mu.Lock()
	defer doer.mu.Unlock()
	return append([]publicGatewayPGProviderRequest(nil), doer.requests...)
}

type publicGatewayPGMutatingCoordinator struct {
	publicModelExecutionCoordinator
	authorizeError error
	once           sync.Once
	before         func(context.Context) error
	mutationErr    error
}

func (coordinator *publicGatewayPGMutatingCoordinator) AuthorizePublic(
	ctx context.Context,
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity modelexecution.AttemptIdentity,
) (modelexecution.Decision, error) {
	coordinator.once.Do(func() {
		if coordinator.before != nil {
			coordinator.mutationErr = coordinator.before(ctx)
		}
	})
	if coordinator.mutationErr != nil {
		return modelexecution.Decision{}, coordinator.mutationErr
	}
	decision, err := coordinator.publicModelExecutionCoordinator.AuthorizePublic(ctx, plan, route, identity)
	coordinator.authorizeError = err
	return decision, err
}

func TestPublicModelExecutionAuthorityGatewayIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)

	t.Run("responses and messages settle exact V2 delivery with metadata-only idempotency", func(t *testing.T) {
		suite := newPublicGatewayPGSuite(t, pool, "success")
		for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
			t.Run(string(operation), func(t *testing.T) {
				fixture := suite.newRoute(t, string(operation), operation)
				requestedModel := "public-alias-" + publicGatewayPGHash(fixture.releaseSuffix)[:16]
				doer := &publicGatewayPGDoer{
					operation: operation, upstreamModel: fixture.upstreamModel,
					secret: fixture.providerSecret, responseText: "public answer",
				}
				relay := publicGatewayPGRelay(t, fixture, doer)
				body := publicGatewayPGRequestBody(operation, requestedModel)
				handler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[0])
				idempotencyKey := "same-metadata-key-" + string(operation)

				first := publicGatewayPGServe(handler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusOK, first.Code, first.Body.String())
				require.Contains(t, first.Body.String(), `"model":"`+requestedModel+`"`)
				require.NotContains(t, first.Body.String(), fixture.model)
				require.NotContains(t, first.Body.String(), fixture.upstreamModel)
				require.EqualValues(t, 1, doer.calls.Load())
				requestID := first.Header().Get(middleware.OperationIDHeader)
				require.NotEmpty(t, requestID)
				publicGatewayPGRequireSettledV2(t, fixture, requestID)

				var freezes int
				require.NoError(t, pool.QueryRow(t.Context(), `
	SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1 AND model_name=$2`, fixture.orgID, fixture.model).Scan(&freezes))
				require.Equal(t, 1, freezes)

				replay := publicGatewayPGServe(handler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
				require.Equal(t, requestID, replay.Header().Get(middleware.OriginalOperationHeader))
				require.EqualValues(t, 1, doer.calls.Load(), "metadata replay must not dispatch")
				require.NoError(t, pool.QueryRow(t.Context(), `
	SELECT COUNT(*) FROM freeze_idempotency WHERE org_id=$1 AND model_name=$2`, fixture.orgID, fixture.model).Scan(&freezes))
				require.Equal(t, 1, freezes, "metadata replay must not freeze again")

				otherKeyHandler := publicGatewayPGHandler(fixture, relay, requestedModel, fixture.apiKeyIDs[1])
				otherKey := publicGatewayPGServe(otherKeyHandler, fixture.identity.PublicPath, body, idempotencyKey)
				require.Equal(t, http.StatusOK, otherKey.Code, otherKey.Body.String())
				require.EqualValues(t, 2, doer.calls.Load(), "an independent API-key scope must not hit the first key")
				publicGatewayPGRequireSettledV2(t, fixture, otherKey.Header().Get(middleware.OperationIDHeader))

				providerRequests := doer.snapshot()
				require.Len(t, providerRequests, 2)
				for _, providerRequest := range providerRequests {
					require.Equal(t, fixture.identity.PublicPath, providerRequest.path)
					require.Contains(t, providerRequest.body, `"model":"`+fixture.upstreamModel+`"`)
					require.NotContains(t, providerRequest.body, requestedModel)
					if operation == modeloperation.IDResponses {
						require.Equal(t, "Bearer "+fixture.providerSecret, providerRequest.authorization)
						require.Empty(t, providerRequest.apiKey)
					} else {
						require.Empty(t, providerRequest.authorization)
						require.Equal(t, fixture.providerSecret, providerRequest.apiKey)
						require.Equal(t, "2023-06-01", providerRequest.anthropicVersion)
					}
				}
			})
		}
	})

	t.Run("post-freeze structural and credential drift roll back attempt and cancel safely", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			mutate func(context.Context, *publicGatewayPGFixture) error
		}{
			{name: "structural revision", mutate: func(ctx context.Context, fixture *publicGatewayPGFixture) error {
				return fixture.mutateStructuralRevision(ctx)
			}},
			{name: "selected credential", mutate: func(ctx context.Context, fixture *publicGatewayPGFixture) error {
				return fixture.mutateCredentialDisabled(ctx)
			}},
		} {
			t.Run(test.name, func(t *testing.T) {
				fixture := newPublicGatewayPGFixture(t, pool, "drift-"+test.name, modeloperation.IDResponses)
				doer := &publicGatewayPGDoer{
					operation: fixture.operation, upstreamModel: fixture.upstreamModel,
					secret: fixture.providerSecret, responseText: "must not dispatch",
				}
				coordinator := &publicGatewayPGMutatingCoordinator{
					publicModelExecutionCoordinator: fixture.coordinator,
					before:                          func(ctx context.Context) error { return test.mutate(ctx, fixture) },
				}
				orchestrator := fixture.newOrchestratorWithCoordinator(coordinator, doer, allowPublicNonStreamGuard{})
				request := publicGatewayPGRequest(fixture, "drift", fixture.model)
				err := orchestrator.execute(t.Context(), httptest.NewRecorder(), request)
				require.Error(t, err)
				require.EqualValues(t, 0, doer.calls.Load())
				publicGatewayPGRequireUndispatchedCancel(t, fixture, request.RequestID)
			})
		}
	})

	t.Run("output block retains accepted provider evidence without delivery or charge", func(t *testing.T) {
		fixture := newPublicGatewayPGFixture(t, pool, "output-block", modeloperation.IDResponses)
		events := []string{}
		guard := &publicRuntimeGuard{
			events: &events,
			output: PublicOutputGuardDecision{Action: PublicOutputGuardBlock},
		}
		doer := &publicGatewayPGDoer{
			operation: fixture.operation, upstreamModel: fixture.upstreamModel,
			secret: fixture.providerSecret, responseText: "withheld answer",
		}
		request := publicGatewayPGRequest(fixture, "output-block", fixture.model)
		recorder := httptest.NewRecorder()
		err := fixture.newOrchestrator(doer, guard).execute(t.Context(), recorder, request)
		require.ErrorIs(t, err, ErrPublicNonStreamOutputWithheld)
		require.Empty(t, recorder.Body.String(), "guarded provider body is evidence-only and must never be written")
		require.EqualValues(t, 1, doer.calls.Load())
		require.Contains(t, events, "guard:output")

		var reservationStatus, pendingReason, usagePurpose string
		var attemptState, healthOutcome, failureKind, evidenceVersion string
		var publicBindingID int64
		var promptTokens, completionTokens int
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT status,settlement_pending_reason,usage_purpose,prompt_tokens,completion_tokens
  FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&reservationStatus, &pendingReason, &usagePurpose, &promptTokens, &completionTokens))
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT state,health_outcome,COALESCE(failure_kind,''),route_evidence_version,
       route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&attemptState, &healthOutcome, &failureKind, &evidenceVersion, &publicBindingID))
		require.Equal(t, "settlement_pending", reservationStatus)
		require.Equal(t, billing.OutputHoldReasonGuardrailWithheld, pendingReason)
		require.Equal(t, string(metering.UsagePurposeProviderEvidence), usagePurpose)
		require.Equal(t, 10, promptTokens)
		require.Equal(t, 4, completionTokens)
		require.Equal(t, billing.ProviderAttemptStateOutputWithheld, attemptState)
		require.Equal(t, "success", healthOutcome)
		require.Empty(t, failureKind)
		require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
		require.Equal(t, fixture.activeBindingID, publicBindingID)
		publicGatewayPGRequireNoDeliveryOrTerminalCharge(t, fixture, request.RequestID)
	})

	t.Run("transport ambiguity is terminal pending without retry cancel delivery or settlement", func(t *testing.T) {
		fixture := newPublicGatewayPGFixture(t, pool, "transport-ambiguity", modeloperation.IDMessages)
		doer := &publicGatewayPGDoer{
			operation: fixture.operation, upstreamModel: fixture.upstreamModel,
			secret: fixture.providerSecret, transportErr: errors.New("ambiguous transport fixture"),
		}
		request := publicGatewayPGRequest(fixture, "transport-ambiguity", fixture.model)
		recorder := httptest.NewRecorder()
		err := fixture.newOrchestrator(doer, allowPublicNonStreamGuard{}).execute(t.Context(), recorder, request)
		require.ErrorIs(t, err, errPublicNonStreamUpstreamFailed)
		require.Empty(t, recorder.Body.String())
		require.EqualValues(t, 1, doer.calls.Load(), "ambiguous transport must never retry")

		var reservationStatus, pendingReason string
		var attemptState, healthOutcome, failureKind, evidenceVersion string
		var publicBindingID int64
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT status,settlement_pending_reason
  FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&reservationStatus, &pendingReason))
		require.NoError(t, pool.QueryRow(t.Context(), `
SELECT state,health_outcome,COALESCE(failure_kind,''),route_evidence_version,
       route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, request.RequestID,
		).Scan(&attemptState, &healthOutcome, &failureKind, &evidenceVersion, &publicBindingID))
		require.Equal(t, "settlement_pending", reservationStatus)
		require.Equal(t, "ambiguous_attempt", pendingReason)
		require.Equal(t, billing.ProviderAttemptOutcomeUnknown, attemptState)
		require.Equal(t, "provider_failure", healthOutcome)
		require.Equal(t, "channel_failure", failureKind)
		require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
		require.Equal(t, fixture.activeBindingID, publicBindingID)
		publicGatewayPGRequireNoDeliveryOrTerminalCharge(t, fixture, request.RequestID)
	})
}

func publicGatewayPGRelay(t *testing.T, fixture *publicGatewayPGFixture, doer PublicNonStreamHTTPDoer) *Relay {
	t.Helper()
	relay := NewRelay(fixture.pool, nil, nil, fixture.billing, nil, fixture.encryptionKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, fixture.planner, fixture.coordinator, routingpolicy.Default(),
	))
	require.NoError(t, relay.SetBillingFreezePolicy("1", 1))
	service, err := guardrail.NewService(
		fixture.pool, filter.NewDFAFilter(nil), []byte("m77-public-guardrail-secret"), guardrail.DefaultOptions(),
	)
	require.NoError(t, err)
	relay.SetGuardrail(service)
	relay.providerHTTPDoer = doer
	return relay
}

func publicGatewayPGHandler(
	fixture *publicGatewayPGFixture,
	relay *Relay,
	requestedModel string,
	apiKeyID int64,
) http.Handler {
	apiKey := &models.ApiKey{ID: apiKeyID, OrgID: fixture.orgID, Status: "active"}
	endpoint := fixture.identity.PublicPath
	final := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		relay.HandlePublicNonStream(w, request, fixture.operation)
	})
	guarded := middleware.IdempotencyGuard(
		middleware.NewPostgresIdempotencyStore(fixture.pool), publicGatewayPGFingerprintSecret, final,
	)
	strict := middleware.StrictJSONIngress(guarded)
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	retained := payloadpolicy.RequestMiddlewareWithMax(
		budget, 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext,
	)(strict)
	bound := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
		ctx = context.WithValue(ctx, middleware.CtxOrgID, fixture.orgID)
		ctx = context.WithValue(ctx, middleware.CtxKeyID, apiKeyID)
		ctx = context.WithValue(ctx, middleware.CtxModelAlias, requestedModel)
		ctx = modelauthz.WithDecision(ctx, modelauthz.Decision{
			ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: endpoint,
			RequestedModel: requestedModel, ResolvedModel: fixture.model, RuntimeMutation: "none",
		})
		retained.ServeHTTP(w, request.WithContext(ctx))
	})
	return middleware.OperationIdentityWithSecret(publicGatewayPGIdempotencyHashSecret)(bound)
}

func publicGatewayPGServe(handler http.Handler, path string, body []byte, idempotencyKey string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.IdempotencyKeyHeader, idempotencyKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func publicGatewayPGRequest(
	fixture *publicGatewayPGFixture,
	label string,
	requestedModel string,
) publicNonStreamRequest {
	return publicNonStreamRequest{
		Operation: fixture.operation, Path: fixture.identity.PublicPath,
		Body:        publicGatewayPGRequestBody(fixture.operation, requestedModel),
		PublicModel: requestedModel, ResolvedModel: fixture.model,
		RequestID: "op_pg_" + publicGatewayPGHash(fixture.releaseSuffix + ":" + label)[:32],
		OrgID:     fixture.orgID, APIKeyID: fixture.apiKeyIDs[0],
	}
}

func publicGatewayPGRequestBody(operation modeloperation.ID, model string) []byte {
	encodedModel, _ := json.Marshal(model)
	if operation == modeloperation.IDMessages {
		return []byte(`{"model":` + string(encodedModel) + `,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	}
	return []byte(`{"model":` + string(encodedModel) + `,"input":"hello","max_output_tokens":64}`)
}

func publicGatewayPGProviderResponse(operation modeloperation.ID, model, text string) string {
	encodedModel, _ := json.Marshal(model)
	encodedText, _ := json.Marshal(text)
	if operation == modeloperation.IDMessages {
		return `{"id":"msg_pg","type":"message","role":"assistant","model":` + string(encodedModel) +
			`,"content":[{"type":"text","text":` + string(encodedText) +
			`}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4}}`
	}
	return `{"id":"resp_pg","object":"response","status":"completed","model":` + string(encodedModel) +
		`,"error":null,"incomplete_details":null,"output":[{"id":"msg_pg","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + string(encodedText) +
		`}]}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}`
}

func publicGatewayPGRequireSettledV2(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var reservationStatus, reservationModel string
	var attemptState, healthOutcome, evidenceVersion string
	var publicBindingID int64
	var deliveryReceipts, settleTransactions int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
	SELECT status,model_name FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&reservationStatus, &reservationModel))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT state,health_outcome,route_evidence_version,route_public_execution_binding_id
  FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&attemptState, &healthOutcome, &evidenceVersion, &publicBindingID))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveryReceipts))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle'`, fixture.orgID, requestID,
	).Scan(&settleTransactions))
	require.Equal(t, "settled", reservationStatus)
	require.Equal(t, fixture.model, reservationModel)
	require.Equal(t, "settled", attemptState)
	require.Equal(t, "success", healthOutcome)
	require.Equal(t, executionevidence.ContractVersionV2, evidenceVersion)
	require.Equal(t, fixture.activeBindingID, publicBindingID)
	require.Equal(t, 2, deliveryReceipts)
	require.Equal(t, 1, settleTransactions)
}

func publicGatewayPGRequireUndispatchedCancel(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var reservationStatus string
	var attempts, providerTerminals, deliveries int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT status FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&reservationStatus))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&attempts))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type IN ('settle','cancel')`, fixture.orgID, requestID,
	).Scan(&providerTerminals))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveries))
	require.Equal(t, "cancelled", reservationStatus)
	require.Zero(t, attempts, "stale authority must roll back attempt-first receipt")
	require.Equal(t, 1, providerTerminals, "safe cancellation is the only terminal ledger event")
	require.Zero(t, deliveries)
}

func publicGatewayPGRequireNoDeliveryOrTerminalCharge(t *testing.T, fixture *publicGatewayPGFixture, requestID string) {
	t.Helper()
	var deliveries, terminalTransactions int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2`, fixture.orgID, requestID,
	).Scan(&deliveries))
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type IN ('settle','cancel')`, fixture.orgID, requestID,
	).Scan(&terminalTransactions))
	require.Zero(t, deliveries)
	require.Zero(t, terminalTransactions)
}
