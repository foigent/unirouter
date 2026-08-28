package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const deepSeekV4FlashToolsProtocol = "protocol.deepseek-openai-chat.v3"

func TestDeepSeekV4FlashToolsCandidateDerivesTypedNonStreamInvocation(t *testing.T) {
	plan := executionProfilePlan(t, "deepseek.v4-flash.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name                string
		body                string
		wantChoiceRequest   string
		wantChoiceEffective string
		wantChoiceOrigin    string
		wantSelectedName    string
		wantReasoningEffort string
		wantErr             string
	}{
		{
			name:              "provider defaults remain omitted on wire",
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantReasoningEffort: "omitted",
		},
		{
			name:              "thinking accepts explicit required",
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":"required"}`,
			wantChoiceRequest: "required", wantChoiceEffective: "required", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantReasoningEffort: "omitted",
		},
		{
			name:              "thinking accepts named choice",
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup_weather"}}}`,
			wantChoiceRequest: "named", wantChoiceEffective: "named", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantSelectedName: "lookup_weather", wantReasoningEffort: "omitted",
		},
		{
			name:              `none is valid without tools`,
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}],"tool_choice":"none"}`,
			wantChoiceRequest: "none", wantChoiceEffective: "none", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantReasoningEffort: "omitted",
		},
		{
			name:              `low remains low`,
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"reasoning_effort":"low"}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantReasoningEffort: "low",
		},
		{
			name:              `medium compatibility maps to high`,
			body:              `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"reasoning_effort":"medium"}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantReasoningEffort: "high",
		},
		{name: "auto needs tools", body: `{"model":"deepseek-v4-flash","messages":[],"tool_choice":"auto"}`, wantErr: "requires at least one function tool"},
		{name: "named tool must be declared", body: `{"model":"deepseek-v4-flash","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"other"}}}`, wantErr: "is not declared"},
		{name: "strict belongs to beta surface", body: `{"model":"deepseek-v4-flash","messages":[],"tools":[{"type":"function","function":{"name":"lookup","strict":true}}]}`, wantErr: `function field "strict" has no bounded forwarding contract`},
		{name: "parallel tools remain undocumented on standard surface", body: `{"model":"deepseek-v4-flash","messages":[],"tools":[` + tool + `],"parallel_tool_calls":true}`, wantErr: "parallel_tool_calls is not supported"},
		{name: "null parallel tools cannot bypass standard surface", body: `{"model":"deepseek-v4-flash","messages":[],"tools":[` + tool + `],"parallel_tool_calls":null}`, wantErr: "parallel_tool_calls is not supported"},
		{name: "stream tools remain deferred", body: `{"model":"deepseek-v4-flash","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "require stream=false"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.True(t, evidence.FunctionToolInvocationModeled)
			require.Equal(t, test.wantChoiceRequest, evidence.FunctionToolChoiceRequest)
			require.Equal(t, test.wantChoiceEffective, evidence.FunctionToolChoiceEffective)
			require.Equal(t, test.wantChoiceOrigin, evidence.FunctionToolChoiceOrigin)
			require.Equal(t, test.wantSelectedName, evidence.SelectedFunctionName)
			require.Equal(t, test.wantReasoningEffort, evidence.ReasoningEffortEffective)
			if evidence.FunctionToolsRequested {
				require.Equal(t, []string{"lookup_weather"}, evidence.DeclaredFunctionNames)
			}
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDeepSeekV4FlashRequiredAndNamedUseOnePlannerCapabilitySnapshot(t *testing.T) {
	tests := []struct {
		name             string
		choiceJSON       string
		wantCapability   string
		wantSelectedName string
	}{
		{name: "required", choiceJSON: `"required"`, wantCapability: modelprofile.CapabilityToolChoiceReq},
		{name: "named", choiceJSON: `{"type":"function","function":{"name":"lookup"}}`, wantCapability: modelprofile.CapabilityToolChoiceNamed, wantSelectedName: "lookup"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", encKey, nil)
			setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.3.0")
			plan.Routes[0].Provider = "deepseek"
			plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
			planner := &executionPlannerStub{plan: plan}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

			explicitOutput := 128
			body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":` + test.choiceJSON + `,"max_tokens":128}`)
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-deepseek-flash-"+test.name,
				modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
				body, time.Now(),
			)
			require.NoError(t, err)

			constraint := toolChoiceConstraint([]string{"lookup"}, test.wantSelectedName)
			require.Contains(t, batch.plan.CapabilityRequirements, modelrouting.CapabilityRequirement{
				CapabilityID:       test.wantCapability,
				Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
				FunctionToolChoice: constraint,
			})
			require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
			require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)
			planner.mu.Lock()
			require.Equal(t, batch.plan.CapabilityRequirements, planner.requests[0].InvocationCapabilities)
			planner.mu.Unlock()

			actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(actual, &payload))
			require.JSONEq(t, test.choiceJSON, string(payload["tool_choice"]))
			require.Contains(t, string(payload["tools"]), `"name":"lookup"`)

			for index := range batch.routes[0].compat.PlanCapabilityRequirements {
				requirement := &batch.routes[0].compat.PlanCapabilityRequirements[index]
				if requirement.FunctionToolChoice != nil {
					requirement.FunctionToolChoice.DeclaredFunctionNames[0] = "mutated"
				}
			}
			for _, requirement := range batch.plan.CapabilityRequirements {
				if requirement.FunctionToolChoice != nil {
					require.Equal(t, []string{"lookup"}, requirement.FunctionToolChoice.DeclaredFunctionNames)
				}
			}
		})
	}
}

func TestDeepSeekV4FlashToolChoiceResponseUsesExactPlanConstraintForEveryChoice(t *testing.T) {
	tests := []struct {
		name            string
		capability      string
		declared        []string
		selected        string
		choiceToolNames [][]string
		wantComplete    bool
	}{
		{name: "required accepts declared tool", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}, choiceToolNames: [][]string{{"lookup"}}, wantComplete: true},
		{name: "required rejects empty choices", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}},
		{name: "required rejects plain text", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}, choiceToolNames: [][]string{nil}},
		{name: "required rejects undeclared tool", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}, choiceToolNames: [][]string{{"other"}}},
		{name: "named accepts selected tool", capability: modelprofile.CapabilityToolChoiceNamed, declared: []string{"lookup", "other"}, selected: "lookup", choiceToolNames: [][]string{{"lookup"}}, wantComplete: true},
		{name: "named rejects empty choices", capability: modelprofile.CapabilityToolChoiceNamed, declared: []string{"lookup", "other"}, selected: "lookup"},
		{name: "named rejects other declared tool", capability: modelprofile.CapabilityToolChoiceNamed, declared: []string{"lookup", "other"}, selected: "lookup", choiceToolNames: [][]string{{"other"}}},
		{name: "auto accepts plain text", capability: modelprofile.CapabilityToolChoiceAuto, declared: []string{"lookup"}, choiceToolNames: [][]string{nil}, wantComplete: true},
		{name: "auto rejects undeclared tool", capability: modelprofile.CapabilityToolChoiceAuto, declared: []string{"lookup"}, choiceToolNames: [][]string{{"other"}}},
		{name: "none accepts plain text", capability: modelprofile.CapabilityFunctionTools, declared: []string{"lookup"}, choiceToolNames: [][]string{nil}, wantComplete: true},
		{name: "none rejects provider tool call", capability: modelprofile.CapabilityFunctionTools, declared: []string{"lookup"}, choiceToolNames: [][]string{{"lookup"}}},
		{name: "required rejects one empty choice", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}, choiceToolNames: [][]string{{"lookup"}, nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := deepSeekV4FlashToolChoiceRoute(test.capability, test.declared, test.selected)
			inspection := inspectNonStreamOutputTextsForRoute(deepSeekV4FlashToolResponse(test.choiceToolNames...), route)
			require.Equal(t, test.wantComplete, inspection.complete(), inspection.incompletePaths)
		})
	}
}

func TestDeepSeekV4FlashRequiredToolChoiceLiveResponseIsHeldBeforeCustomerDelivery(t *testing.T) {
	var holds, prepares, settlements atomic.Int64
	billingStub := &mockBilling{
		holdOutputFn: func(_ context.Context, _ billing.SettleRequest, reason string) error {
			require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
			holds.Add(1)
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			prepares.Add(1)
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settlements.Add(1)
			return &billing.SettleResult{Success: true}, nil
		},
	}
	coordinator := &executionCoordinatorStub{}
	relay := newTestRelay(p6f11RelayDB(), billingStub)
	relay.modelExecution = &modelExecutionRuntime{coordinator: coordinator}
	route := deepSeekV4FlashToolChoiceRoute(modelprofile.CapabilityToolChoiceReq, []string{"lookup"}, "")
	response := deepSeekV4FlashToolResponse(nil)
	response["object"] = "chat.completion"
	response["usage"] = deepSeekV4FlashUsage()
	responseBody, err := json.Marshal(response)
	require.NoError(t, err)

	ctx := contextWithExecutionDecision(
		contextWithRouteAttempt(contextWithAttemptID(context.Background(), "att-deepseek-required-text"), 1, 1),
		modelexecution.Decision{Status: modelexecution.Applied},
	)
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, recorder, &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(responseBody))),
	}, route, "req-deepseek-required-text", 1, 2, "deepseek-v4-flash", "deepseek-v4-flash", "127.0.0.1",
		logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 1, 1, "upstream-required-text")

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "server_error.upstream_error")
	require.NotContains(t, recorder.Body.String(), "plain answer")
	require.EqualValues(t, 1, holds.Load())
	require.Zero(t, prepares.Load())
	require.Zero(t, settlements.Load())
	require.EqualValues(t, 1, coordinator.providerCount.Load())
	require.Zero(t, coordinator.successCount.Load())
}

func TestDeepSeekV4FlashRequiredToolChoiceCacheHitCancelsBeforeCustomerDelivery(t *testing.T) {
	var prepares, cancels atomic.Int64
	relay := newTestRelay(p6f11RelayDB(), &mockBilling{
		prepareCacheDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			prepares.Add(1)
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancels.Add(1)
			return nil
		},
	})
	response := deepSeekV4FlashToolResponse(nil)
	response["object"] = "chat.completion"
	response["usage"] = deepSeekV4FlashUsage()
	responseBody, err := json.Marshal(response)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	relay.serveResponseCacheHit(
		context.Background(), recorder,
		deepSeekV4FlashToolChoiceRoute(modelprofile.CapabilityToolChoiceReq, []string{"lookup"}, ""),
		&responsecache.Hit{SourceResponse: responseBody}, responsecache.Identity{}, invocationopt.Policy{},
		"req-deepseek-required-cache", 1, 2, "deepseek-v4-flash", "deepseek-v4-flash", "127.0.0.1", time.Now(), logger.New(logger.Fields{}),
	)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "response_cache.integrity_error")
	require.NotContains(t, recorder.Body.String(), "plain answer")
	require.Zero(t, prepares.Load())
	require.EqualValues(t, 1, cancels.Load())
}

func deepSeekV4FlashToolResponse(choiceToolNames ...[]string) map[string]interface{} {
	choices := make([]interface{}, 0, len(choiceToolNames))
	for choiceIndex, names := range choiceToolNames {
		message := map[string]interface{}{"role": "assistant", "reasoning_content": "reason"}
		finishReason := "stop"
		if len(names) == 0 {
			message["content"] = "plain answer"
		} else {
			message["content"] = nil
			finishReason = "tool_calls"
			calls := make([]interface{}, 0, len(names))
			for toolIndex, name := range names {
				calls = append(calls, map[string]interface{}{
					"id": fmt.Sprintf("call_%d_%d", choiceIndex, toolIndex), "type": "function",
					"function": map[string]interface{}{"name": name, "arguments": `{}`},
				})
			}
			message["tool_calls"] = calls
		}
		choices = append(choices, map[string]interface{}{
			"index": json.Number(fmt.Sprintf("%d", choiceIndex)), "finish_reason": finishReason, "message": message,
		})
	}
	return map[string]interface{}{
		"id": "chatcmpl-deepseek-flash-tools", "model": "deepseek-v4-flash", "choices": choices,
	}
}

func deepSeekV4FlashUsage() map[string]interface{} {
	return map[string]interface{}{
		"prompt_tokens": json.Number("2"), "completion_tokens": json.Number("1"), "total_tokens": json.Number("3"),
		"prompt_cache_hit_tokens": json.Number("1"), "prompt_cache_miss_tokens": json.Number("1"),
	}
}

func deepSeekV4FlashToolChoiceRoute(capability string, declared []string, selected string) *RouteResult {
	return &RouteResult{
		Channel:     models.Channel{ID: 1, Name: "deepseek", Provider: "deepseek"},
		ChannelKey:  models.ChannelKey{ID: 2},
		MappedModel: "deepseek-v4-flash", ModelProtocolProfile: deepSeekV4FlashToolsProtocol,
		CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       capability,
			Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
			FunctionToolChoice: toolChoiceConstraint(declared, selected),
		}},
	}
}
