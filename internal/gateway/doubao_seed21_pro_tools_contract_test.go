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

const doubaoSeed21ProToolsProtocol = "protocol.doubao-seed-2.1-pro-openai-chat.v3"

func TestDoubaoSeed21ProToolsCandidateDerivesTypedNonStreamInvocation(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.3.0")
	tool := `{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object"}}}`
	tests := []struct {
		name                string
		body                string
		wantChoiceRequest   string
		wantChoiceEffective string
		wantChoiceOrigin    string
		wantSelectedName    string
		wantParallelRequest string
		wantParallel        bool
		wantParallelOrigin  string
		wantErr             string
	}{
		{
			name:              "provider defaults remain omitted on wire",
			body:              `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `]}`,
			wantChoiceRequest: "omitted", wantChoiceEffective: "auto", wantChoiceOrigin: modelrouting.CapabilityOriginProviderDefault,
			wantParallelRequest: "omitted", wantParallel: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:              "explicit required and serial",
			body:              `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":"required","parallel_tool_calls":false}`,
			wantChoiceRequest: "required", wantChoiceEffective: "required", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelRequest: "false", wantParallelOrigin: modelrouting.CapabilityOriginExplicitRequest,
		},
		{
			name:              "named choice",
			body:              `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"weather"}],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"lookup_weather"}}}`,
			wantChoiceRequest: "named", wantChoiceEffective: "named", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantSelectedName:    "lookup_weather",
			wantParallelRequest: "omitted", wantParallel: true, wantParallelOrigin: modelrouting.CapabilityOriginProviderDefault,
		},
		{
			name:              "none without tools",
			body:              `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"user","content":"hello"}],"tool_choice":"none"}`,
			wantChoiceRequest: "none", wantChoiceEffective: "none", wantChoiceOrigin: modelrouting.CapabilityOriginExplicitRequest,
			wantParallelRequest: "omitted",
		},
		{name: "auto needs tools", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"tool_choice":"auto"}`, wantErr: "requires at least one function tool"},
		{name: "named tool must be declared", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"tools":[` + tool + `],"tool_choice":{"type":"function","function":{"name":"other"}}}`, wantErr: "is not declared"},
		{name: "parallel needs callable tools", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"parallel_tool_calls":true}`, wantErr: "requires callable function tools"},
		{name: "stream tools remain deferred", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"stream":true,"tools":[` + tool + `]}`, wantErr: "require stream=false"},
		{name: "strict tool schema remains fail closed", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"tools":[{"type":"function","function":{"name":"lookup","strict":true}}]}`, wantErr: `function field "strict" has no bounded forwarding contract`},
		{name: "parameters must be object", body: `{"model":"doubao-seed-2-1-pro-260628","messages":[],"tools":[{"type":"function","function":{"name":"lookup","parameters":true}}]}`, wantErr: "parameters must be a JSON schema object"},
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
			if evidence.FunctionToolsRequested {
				require.Equal(t, []string{"lookup_weather"}, evidence.DeclaredFunctionNames)
			} else {
				require.Empty(t, evidence.DeclaredFunctionNames)
			}
			require.Equal(t, test.wantSelectedName, evidence.SelectedFunctionName)
			require.Equal(t, test.wantParallelRequest, evidence.ParallelToolCallsRequest)
			require.Equal(t, test.wantParallel, evidence.ParallelToolCallsEffective)
			require.Equal(t, test.wantParallelOrigin, evidence.ParallelToolCallsOrigin)
			_, err = estimateChatAdmissionForPlan([]byte(test.body), plan)
			require.NoError(t, err)
		})
	}
}

func TestDoubaoSeed21ProToolsCandidateRequiresOpaqueReasoningReplay(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.3.0")
	toolHistory := `{"model":"doubao-seed-2-1-pro-260628","messages":[{"role":"assistant","content":null,%s"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]%s}`

	_, err := validateChatExecutionProfileRequest([]byte(fmt.Sprintf(toolHistory, "", "")), plan)
	require.ErrorContains(t, err, "must preserve encrypted_content")

	body := []byte(fmt.Sprintf(toolHistory, `"encrypted_content":"opaque-signature",`, ""))
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.Empty(t, evidence.ProviderRequestNormalizations)
	require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan(body, plan); return err }())

	disabled := []byte(fmt.Sprintf(toolHistory, "", `,"thinking":{"type":"disabled"}`))
	_, err = validateChatExecutionProfileRequest(disabled, plan)
	require.NoError(t, err, "disabled reasoning does not manufacture opaque provider state")

	historical := executionProfilePlan(t, "doubao.seed-2.1-pro.api", "1.2.0")
	_, err = validateChatExecutionProfileRequest(body, historical)
	require.ErrorContains(t, err, "encrypted_content history is not supported")
}

func TestDoubaoSeed21ProToolsCandidateUsesOnePlannerCapabilitySnapshot(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &plan, "doubao.seed-2.1-pro.api", "1.3.0")
	plan.Routes[0].Provider = "doubao"
	plan.Routes[0].UpstreamModel = "doubao-seed-2-1-pro-260628"
	planner := &executionPlannerStub{plan: plan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":128}`)
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-pro-tools",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
		{CapabilityID: modelprofile.CapabilityParallelTools, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityReasoning, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityToolChoiceAuto, Origins: []string{modelrouting.CapabilityOriginProviderDefault}, FunctionToolChoice: toolChoiceConstraint([]string{"lookup"}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actual, &payload))
	require.NotContains(t, payload, "tool_choice")
	require.NotContains(t, payload, "parallel_tool_calls")
	var messages []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload["messages"], &messages))
	require.Equal(t, "weather", strings.Trim(string(messages[0]["content"]), `"`))

	historyBody := []byte(`{"model":"public-model","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}],"thinking":{"type":"disabled"},"max_tokens":128}`)
	historyBatch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-seed21-pro-tool-history",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		historyBody, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityFunctionTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest, modelrouting.CapabilityOriginProviderDefault}, FunctionToolChoice: toolChoiceConstraint([]string{}, "")},
		{CapabilityID: modelprofile.CapabilityTools, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, historyBatch.plan.CapabilityRequirements)
	require.Equal(t, historyBatch.plan.CapabilityRequirements, historyBatch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, historyBatch.plan.SnapshotHash, historyBatch.routes[0].compat.SnapshotVersion)
}

func TestDoubaoSeed21ProRequiredAndNamedToolChoiceUseOnePlannerCapabilitySnapshot(t *testing.T) {
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
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
			setExecutionTestProfile(t, &plan, "doubao.seed-2.1-pro.api", "1.3.0")
			plan.Routes[0].Provider = "doubao"
			plan.Routes[0].UpstreamModel = "doubao-seed-2-1-pro-260628"
			planner := &executionPlannerStub{plan: plan}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

			explicitOutput := 128
			body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":` + test.choiceJSON + `,"max_tokens":128}`)
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-doubao-seed21-pro-"+test.name,
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

func TestDoubaoSeed21ProToolsCandidateAcceptsOnlyExactToolCallResponse(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-pro-tools", "model": "doubao-seed-2-1-pro-260628",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "tool_calls",
			"message": map[string]interface{}{
				"role": "assistant", "content": nil, "reasoning_content": "need lookup", "encrypted_content": "opaque",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
				}},
			},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21ProToolsProtocol).complete())

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["function_call"] = map[string]interface{}{"name": "lookup", "arguments": `{}`}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21ProToolsProtocol).complete())
	delete(message, "function_call")
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, doubaoSeed21ProToolsProtocol).complete())
}

func TestDoubaoSeed21ProToolChoiceResponseUsesExactPlanConstraintForEveryChoice(t *testing.T) {
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
		{name: "required rejects one empty choice", capability: modelprofile.CapabilityToolChoiceReq, declared: []string{"lookup"}, choiceToolNames: [][]string{{"lookup"}, nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := doubaoSeed21ProToolChoiceRoute(test.capability, test.declared, test.selected)
			inspection := inspectNonStreamOutputTextsForRoute(doubaoSeed21ProToolResponse(test.choiceToolNames...), route)
			require.Equal(t, test.wantComplete, inspection.complete(), inspection.incompletePaths)
		})
	}
}

func TestDoubaoSeed21ProRequiredToolChoiceLiveResponseIsHeldBeforeCustomerDelivery(t *testing.T) {
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
	route := doubaoSeed21ProToolChoiceRoute(modelprofile.CapabilityToolChoiceReq, []string{"lookup"}, "")
	response := doubaoSeed21ProToolResponse(nil)
	response["object"] = "chat.completion"
	response["usage"] = map[string]interface{}{
		"prompt_tokens": json.Number("1"), "completion_tokens": json.Number("1"), "total_tokens": json.Number("2"),
	}
	responseBody, err := json.Marshal(response)
	require.NoError(t, err)

	ctx := contextWithExecutionDecision(
		contextWithRouteAttempt(contextWithAttemptID(context.Background(), "att-doubao-required-text"), 1, 1),
		modelexecution.Decision{Status: modelexecution.Applied},
	)
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, recorder, &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(responseBody))),
	}, route, "req-doubao-required-text", 1, 2, "doubao-seed-2.1-pro", "doubao-seed-2.1-pro", "127.0.0.1",
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

func TestDoubaoSeed21ProRequiredToolChoiceCacheHitCancelsBeforeCustomerDelivery(t *testing.T) {
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
	response := doubaoSeed21ProToolResponse(nil)
	response["object"] = "chat.completion"
	response["usage"] = map[string]interface{}{
		"prompt_tokens": json.Number("1"), "completion_tokens": json.Number("1"), "total_tokens": json.Number("2"),
	}
	responseBody, err := json.Marshal(response)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	relay.serveResponseCacheHit(
		context.Background(), recorder,
		doubaoSeed21ProToolChoiceRoute(modelprofile.CapabilityToolChoiceReq, []string{"lookup"}, ""),
		&responsecache.Hit{SourceResponse: responseBody}, responsecache.Identity{}, invocationopt.Policy{},
		"req-doubao-required-cache", 1, 2, "doubao-seed-2.1-pro", "doubao-seed-2.1-pro", "127.0.0.1", time.Now(), logger.New(logger.Fields{}),
	)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "response_cache.integrity_error")
	require.NotContains(t, recorder.Body.String(), "plain answer")
	require.Zero(t, prepares.Load())
	require.EqualValues(t, 1, cancels.Load())
}

func doubaoSeed21ProToolResponse(choiceToolNames ...[]string) map[string]interface{} {
	choices := make([]interface{}, 0, len(choiceToolNames))
	for choiceIndex, names := range choiceToolNames {
		message := map[string]interface{}{
			"role": "assistant", "reasoning_content": "reason", "encrypted_content": "opaque",
		}
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
		"id": "chatcmpl-doubao-pro-tools", "model": "doubao-seed-2-1-pro-260628", "choices": choices,
	}
}

func doubaoSeed21ProToolChoiceRoute(capability string, declared []string, selected string) *RouteResult {
	return &RouteResult{
		Channel:     models.Channel{ID: 1, Name: "doubao", Provider: "doubao"},
		ChannelKey:  models.ChannelKey{ID: 2},
		MappedModel: "doubao-seed-2-1-pro-260628", ModelProtocolProfile: doubaoSeed21ProToolsProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       capability,
			Origins:            []string{modelrouting.CapabilityOriginExplicitRequest},
			FunctionToolChoice: toolChoiceConstraint(declared, selected),
		}},
	}
}
