package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestMiniMaxM3ProcessingTierCandidateModelsExactWireAndEvidenceGap(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m3.token-plan", "1.5.0")
	for _, test := range []struct {
		name      string
		field     string
		requested string
		effective string
		explicit  bool
	}{
		{name: "omitted", requested: "omitted", effective: modelprofile.ProcessingTierStandard},
		{name: "standard", field: `,"service_tier":"standard"`, requested: "standard", effective: modelprofile.ProcessingTierStandard, explicit: true},
		{name: "priority", field: `,"service_tier":"priority"`, requested: "priority", effective: modelprofile.ProcessingTierPriority, explicit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(
				`{"model":"MiniMax-M3","messages":[{"role":"user","content":"hello"}]`+test.field+`}`,
			), plan)
			require.NoError(t, err)
			require.Equal(t, modelprofile.ProcessingTierPolicyMiniMaxOpenAIChatV1, evidence.ProcessingTierPolicy)
			require.Equal(t, test.requested, evidence.ProcessingTierRequest)
			require.Equal(t, test.effective, evidence.ProcessingTierEffective)
			require.Equal(t, 300, evidence.ProcessingTierTimeoutSeconds)
			require.Equal(t, test.explicit, evidence.ProcessingTierExplicit)
		})
	}

	for _, value := range []string{`"default"`, `"flex"`, `"auto"`, `" priority "`, `""`, `null`, `1`} {
		_, err := validateChatExecutionProfileRequest([]byte(
			`{"model":"MiniMax-M3","messages":[],"service_tier":`+value+`}`,
		), plan)
		require.Error(t, err, value)
	}

	historical := executionProfilePlan(t, "minimax.m3.token-plan", "1.4.0")
	_, err := validateChatExecutionProfileRequest([]byte(
		`{"model":"MiniMax-M3","messages":[],"service_tier":"priority"}`,
	), historical)
	require.ErrorContains(t, err, "service_tier is not supported")

	ctx := contextWithProcessingTierEvidence(t.Context(), modelExecutionProfileEvidence{
		ProcessingTierPolicy:    modelprofile.ProcessingTierPolicyMiniMaxOpenAIChatV1,
		ProcessingTierRequest:   "priority",
		ProcessingTierEffective: modelprofile.ProcessingTierPriority,
		ProcessingTierExplicit:  true,
	})
	for _, metadata := range []map[string]interface{}{
		processingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"priority"}}),
		withProcessingTierResponseMetadata(ctx, http.Header{"X-Gemini-Service-Tier": []string{"priority"}}, nil),
	} {
		require.Equal(t, "unavailable_by_surface", metadata["processing_tier_observation"])
		require.NotContains(t, metadata, "processing_tier_observation_source")
		require.NotContains(t, metadata, "processing_tier_observed")
		require.NotContains(t, metadata, "processing_tier_disposition")
	}
}

func TestMiniMaxM3PriorityTierUsesOnePlanSnapshotAndZeroChargeBoundary(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	newRelay := func(t *testing.T, readiness billing.AdmissionReadiness, encryptedSecret string) (*Relay, *processingTierReadinessStub) {
		t.Helper()
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
		setExecutionTestProfile(t, &plan, "minimax.m3.token-plan", "1.5.0")
		plan.Routes[0].Provider = "minimax"
		plan.Routes[0].UpstreamModel = "MiniMax-M3"
		if encryptedSecret != "" {
			plan.Routes[0].EncryptedSecret = encryptedSecret
		}
		inspector := &processingTierReadinessStub{result: readiness}
		relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
		relay.SetBillingAdmissionReadinessInspector(inspector)
		require.NoError(t, relay.SetModelExecutionRuntime(
			modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
		))
		return relay, inspector
	}

	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"priority","max_completion_tokens":128}`)
	explicitOutput := 128
	relay, inspector := newRelay(t, billing.AdmissionReadiness{BillingMode: billing.BillingModeCostObservation}, "")
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-priority",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 1, inspector.calls)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityServiceTier, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.JSONEq(t, `"priority"`, string(payload["service_tier"]))

	standardRelay, standardInspector := newRelay(t, billing.AdmissionReadiness{BillingMode: billing.BillingModePrepaidEnforced}, "")
	standardBody := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"service_tier":"standard","max_completion_tokens":128}`)
	standardBatch, err := standardRelay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-standard",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		standardBody, time.Now(),
	)
	require.NoError(t, err)
	require.Zero(t, standardInspector.calls)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityServiceTier, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, standardBatch.plan.CapabilityRequirements)

	omittedRelay, omittedInspector := newRelay(t, billing.AdmissionReadiness{BillingMode: billing.BillingModePrepaidEnforced}, "")
	omittedBody := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":128}`)
	omittedBatch, err := omittedRelay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-omitted-tier",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		omittedBody, time.Now(),
	)
	require.NoError(t, err)
	require.Zero(t, omittedInspector.calls)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
	}, omittedBatch.plan.CapabilityRequirements)
	require.Equal(t, omittedBatch.plan.CapabilityRequirements, omittedBatch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, omittedBatch.plan.SnapshotHash, omittedBatch.routes[0].compat.SnapshotVersion)

	blocked, blockedInspector := newRelay(t, billing.AdmissionReadiness{BillingMode: billing.BillingModePrepaidEnforced}, "not-ciphertext")
	blockedBatch, err := blocked.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-minimax-m3-priority-prepaid",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.Nil(t, blockedBatch)
	require.ErrorIs(t, err, errProcessingTierContractUnavailable)
	require.Equal(t, 1, blockedInspector.calls)
}
