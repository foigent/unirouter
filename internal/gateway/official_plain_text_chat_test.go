package gateway

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/executionevidence"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestOfficialTextChatGatewayUsingCapabilityEvidence(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/official-plain-text-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	endpoints := map[string]string{
		"gemini": "https://generativelanguage.googleapis.com/v1beta/openai",
		"hy3":    "https://tokenhub.tencentmaas.com/v1",
		"xai":    "https://api.x.ai/v1",
		"doubao": "https://ark.cn-beijing.volces.com/api/v3",
		"openai": "https://api.openai.com/v1",
	}
	for _, raw := range bundle.Definitions {
		definition, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		profile := definition.Profile()
		t.Run(profile.ID, func(t *testing.T) {
			endpoint := endpoints[profile.Provider]
			require.NotEmpty(t, endpoint)
			key := []byte("0123456789abcdef0123456789abcdef")
			var approval []executionevidence.CapabilityEntry
			if len(profile.Capabilities) != 0 {
				now := time.Now().UTC().Truncate(time.Microsecond)
				approval = []executionevidence.CapabilityEntry{{
					CapabilityID: modelprofile.CapabilityReasoning, SupportState: "enabled", EvidenceState: "production_approved",
					EvidenceRef: "fixture://approval/reasoning", VerifiedAt: &now, Revision: 1,
				}}
			}
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, endpoint, key, approval)
			plan.Definition = definition
			plan.ExecutionProfileID, plan.ExecutionProfileVersion = profile.ID, profile.Version
			plan.ModelProtocolProfile = profile.ProtocolProfile
			plan.ReasoningProfile, plan.CacheUsageProfile = profile.ReasoningProfile, profile.CacheUsageProfile
			plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = profile.Provider, profile.UpstreamModelIDs[0]
			if profile.Provider == "gemini" || profile.Provider == "doubao" {
				plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
			}

			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))

			const prefix = `{"model":"public-model","messages":[{"role":"user","content":"Say Ready."}]`
			field := "max_tokens"
			if profile.Provider == "openai" || profile.Provider == "doubao" {
				field = "max_completion_tokens"
			}
			body := []byte(prefix + `,"` + field + `":256}`)
			limit := 256
			prepare := func(body []byte) (*preparedModelExecutionBatch, error) {
				return relay.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
					"public-model", "public-model", "text-fixture", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &limit, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
			}
			assertWire := func(routes *preparedModelExecutionBatch) map[string]any {
				t.Helper()
				require.Len(t, routes.routes, 1)
				upstream := routes.routes[0].upstreamRequest
				require.Equal(t, endpoint+"/chat/completions", upstream.URL.String())
				var sent map[string]any
				require.NoError(t, json.NewDecoder(upstream.Body).Decode(&sent))
				require.Equal(t, profile.UpstreamModelIDs[0], sent["model"])
				require.EqualValues(t, 256, sent[field])
				require.NotContains(t, sent, "tools")
				return sent
			}

			routes, err := prepare(body)
			if len(approval) == 0 {
				require.ErrorContains(t, err, `capability "reasoning" is not supported`)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{modelprofile.CapabilityReasoning}, routes.plan.RequiredCapabilities)
				require.NotContains(t, assertWire(routes), "thinking")
			}
			if profile.Provider == "hy3" || profile.Provider == "doubao" {
				disabled := []byte(prefix + `,"` + field + `":256,"thinking":{"type":"disabled"}}`)
				routes, err := prepare(disabled)
				require.NoError(t, err)
				require.Empty(t, routes.plan.RequiredCapabilities)
				require.Equal(t, map[string]any{"type": "disabled"}, assertWire(routes)["thinking"])
			}

			for _, extension := range []string{
				`,"stream":true`,
				`,"tools":[{"type":"function","function":{"name":"echo","parameters":{"type":"object"}}}]`,
				`,"response_format":{"type":"json_object"}`,
			} {
				_, err := validateChatExecutionProfileRequest([]byte(prefix+extension+`}`), plan)
				require.Error(t, err, extension)
			}
		})
	}
}
