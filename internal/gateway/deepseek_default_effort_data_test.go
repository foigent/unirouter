package gateway

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestDeepSeekFlashDataProfileOriginalToolShapeNormalizesAutoAndRequiresDefaultEffort(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/deepseek-streamed-tools.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	var d modelprofile.Definition
	for _, raw := range bundle.Definitions {
		candidate, e := modelprofile.DecodeDefinition(raw)
		require.NoError(t, e)
		if candidate.Profile().ID == "deepseek.v4-flash.api" {
			d = candidate
		}
	}
	require.Equal(t, "1.6.0", d.Profile().Version)
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", key, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
	p := d.Profile()
	plan.Definition, plan.ExecutionProfileVersion = d, p.Version
	plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
	plan.PublicOutputLimit = p.ProviderMaxOutput
	plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = "deepseek", "deepseek-v4-flash"
	plan.Routes[0].DefaultMaxOutput = 2048
	tool := func(name string) map[string]any {
		return map[string]any{"type": "function", "function": map[string]any{"name": name, "parameters": map[string]any{"type": "object"}}}
	}
	request := map[string]any{"model": "public-model", "messages": []map[string]string{{"role": "system", "content": "Use the available functions."}, {"role": "user", "content": "Read a file"}},
		"stream": true, "tool_choice": "auto", "max_tokens": 2048,
		"tools": []any{tool("apply_patch"), tool("edit"), tool("read"), tool("write")}}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	capabilities, err := invocationCapabilityRequirementsForChat(p, evidence, body)
	require.NoError(t, err)
	ids := make([]string, 0, len(capabilities))
	for _, requirement := range capabilities {
		ids = append(ids, requirement.CapabilityID)
		if requirement.CapabilityID == modelprofile.CapabilityReasoningEffort {
			require.Contains(t, requirement.Origins, modelrouting.CapabilityOriginProviderDefault)
		}
	}
	slices.Sort(ids)
	require.Equal(t, []string{
		modelprofile.CapabilityFunctionTools, modelprofile.CapabilityReasoningEffort,
		modelprofile.CapabilityStreaming, modelprofile.CapabilityToolChoiceAuto,
		modelprofile.CapabilityTools,
	}, ids)
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
	n := 2048
	batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "deepseek-original-shape", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &n, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
	require.NoError(t, err)
	providerWire, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var sent map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(providerWire, &sent))
	require.NotContains(t, sent, "tool_choice")
	require.NotContains(t, sent, "thinking")
	require.NotContains(t, sent, "reasoning_effort")
	require.JSONEq(t, string(deepSeekShapeJSON(t, request["tools"])), string(sent["tools"]))
	require.JSONEq(t, `true`, string(sent["stream"]))
	require.JSONEq(t, `2048`, string(sent["max_tokens"]))
	for _, choice := range []any{"required", map[string]any{"type": "function", "function": map[string]any{"name": "read"}}} {
		request["tool_choice"] = choice
		bad := deepSeekShapeJSON(t, request)
		_, err := validateChatExecutionProfileRequest(bad, plan)
		require.Error(t, err)
	}
	request["tool_choice"] = "auto"
	delete(request, "tools")
	_, err = validateChatExecutionProfileRequest(deepSeekShapeJSON(t, request), plan)
	require.Error(t, err)
}

func deepSeekShapeJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.Marshal(value)
	require.NoError(t, err)
	return wire
}
