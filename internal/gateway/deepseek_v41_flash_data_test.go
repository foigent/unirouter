package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/middleware"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestCurrentDeepSeekFlashFormalPreflightAndRuntimePreparation(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	var d modelprofile.Definition
	for _, raw := range bundle.Definitions {
		candidate, e := modelprofile.DecodeDefinition(raw)
		require.NoError(t, e)
		if candidate.Profile().ID == "deepseek.v4.1-flash.api" {
			d = candidate
		}
	}
	require.Equal(t, "1.0.0", d.Profile().Version)
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", key, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
	plan.ExecutionProfileID = d.Profile().ID
	p := d.Profile()
	plan.Definition, plan.ExecutionProfileVersion = d, p.Version
	plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
	plan.PublicOutputLimit = p.ProviderMaxOutput
	plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = "deepseek", "deepseek-flash"
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
	for _, requirement := range capabilities {
		found := false
		for _, row := range bundle.BindingDelta {
			if row.CapabilityID == requirement.CapabilityID {
				found = true
				require.Equal(t, d.Digest(), row.ProfileDefinitionDigest)
			}
		}
		require.True(t, found, "missing binding for %s", requirement.CapabilityID)
	}
	slices.Sort(ids)
	require.Equal(t, []string{
		modelprofile.CapabilityFunctionTools, modelprofile.CapabilityReasoningEffort,
		modelprofile.CapabilityStreaming, modelprofile.CapabilityToolChoiceAuto,
		modelprofile.CapabilityTools,
	}, ids)
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
	planner, coordinator := &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
	// Formal preflight with omitted provider controls validates effective defaults
	// without authorization, credential decryption or Provider dispatch.
	req := executionRequest(http.MethodPost, "/v1/chat/completions", "current-flash-preflight", string(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.CtxModelAlias, "public-model"))
	response := httptest.NewRecorder()
	payloadpolicy.RequestMiddleware(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)(middleware.StrictJSONIngress(http.HandlerFunc(relay.HandleChatPreflight))).ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var preflight chatPreflightResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preflight))
	require.True(t, preflight.LocallyAdmissible, response.Body.String())
	require.Equal(t, p.ID, preflight.ProfileID)
	require.Contains(t, preflight.RequiredCapabilities, modelprofile.CapabilityReasoningEffort)
	require.Zero(t, coordinator.authorizeCount.Load())
	require.Zero(t, coordinator.renewCount.Load())
	require.Zero(t, coordinator.releaseCount.Load())
	n := 2048
	batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "deepseek-original-shape", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &n, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
	require.NoError(t, err)
	providerWire, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var sent map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(providerWire, &sent))
	require.JSONEq(t, `"deepseek-flash"`, string(sent["model"]))
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
