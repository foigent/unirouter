package gateway

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const doubaoEvolvingStrictJSONSchemaProtocol = "protocol.doubao-seed-evolving-openai-chat.v5"

func TestDoubaoSeedEvolvingStrictJSONSchemaRequestIsExactAndPlanBound(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.6.0")
	body := []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"return coordinates"}],"response_format":{"type":"json_schema","json_schema":{"name":"coordinates","description":"A coordinate pair","strict":true,"schema":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"],"additionalProperties":false}}}}`)
	evidence, err := validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	require.NoError(t, func() error { _, err := estimateChatAdmissionForPlan(body, plan); return err }())

	profile, ok := modelprofile.Lookup("doubao.seed-evolving.api", "1.6.0")
	require.True(t, ok)
	require.Contains(t, profile.Capabilities, modelprofile.CapabilityStrictJSONSchema)
	require.Equal(t, doubaoEvolvingStrictJSONSchemaProtocol, profile.ProtocolProfile)
	requirements, err := invocationCapabilityRequirementsForChat(profile, evidence, body)
	require.NoError(t, err)
	require.Contains(t, requirements, modelrouting.CapabilityRequirement{
		CapabilityID: modelprofile.CapabilityStrictJSONSchema,
		Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
	})

	encKey := []byte("0123456789abcdef0123456789abcdef")
	executionPlan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://ark.cn-beijing.volces.com/api/v3", encKey, nil)
	setExecutionTestProfile(t, &executionPlan, "doubao.seed-evolving.api", "1.6.0")
	executionPlan.Routes[0].Provider = "doubao"
	executionPlan.Routes[0].UpstreamModel = "doubao-seed-evolving"
	planner := &executionPlannerStub{plan: executionPlan}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()))

	explicitOutput := 128
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-doubao-evolving-strict-json",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicitOutput, EstimateVersion: chatAdmissionEstimateVersion},
		body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, []modelrouting.CapabilityRequirement{
		{CapabilityID: modelprofile.CapabilityReasoningEffort, Origins: []string{modelrouting.CapabilityOriginProviderDefault}},
		{CapabilityID: modelprofile.CapabilityStrictJSONSchema, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}},
	}, batch.plan.CapabilityRequirements)
	require.Equal(t, batch.plan.CapabilityRequirements, batch.routes[0].compat.PlanCapabilityRequirements)
	require.Equal(t, batch.plan.SnapshotHash, batch.routes[0].compat.SnapshotVersion)

	actual, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var want, got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &want))
	require.NoError(t, json.Unmarshal(actual, &got))
	want["model"] = json.RawMessage(`"doubao-seed-evolving"`)
	require.Equal(t, want, got)
}

func TestDoubaoSeedEvolvingStrictJSONSchemaRejectsUnprovenCombinations(t *testing.T) {
	plan := executionProfilePlan(t, "doubao.seed-evolving.api", "1.6.0")
	definition := `"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}`
	tool := `{"type":"function","function":{"name":"lookup"}}`
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "json object remains closed", body: `{"model":"m","messages":[{"role":"user","content":"json"}],"response_format":{"type":"json_object"}}`, wantErr: "must be one of text, json_schema"},
		{name: "strict false", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":false,"schema":{}}}}`, wantErr: "strict must be true"},
		{name: "strict omitted", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{}}}}`, wantErr: "strict must be true"},
		{name: "schema must be object", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":true}}}`, wantErr: "schema must be a JSON schema object"},
		{name: "description null is not string", body: `{"model":"m","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"answer","description":null,"strict":true,"schema":{}}}}`, wantErr: "description must be a string when present"},
		{name: "stream remains unproven", body: `{"model":"m","messages":[],"stream":true,` + definition + `}`, wantErr: "requires stream=false"},
		{name: "tools remain unproven", body: `{"model":"m","messages":[],"tools":[` + tool + `],` + definition + `}`, wantErr: "cannot be combined with function tools"},
		{name: "tool choice remains unproven", body: `{"model":"m","messages":[],"tool_choice":"none",` + definition + `}`, wantErr: "cannot be combined with tool_choice"},
		{name: "frequency penalty remains unproven", body: `{"model":"m","messages":[],"frequency_penalty":0,` + definition + `}`, wantErr: "cannot be combined with frequency_penalty"},
		{name: "presence penalty remains unproven", body: `{"model":"m","messages":[],"presence_penalty":0,` + definition + `}`, wantErr: "cannot be combined with presence_penalty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.ErrorContains(t, err, test.wantErr)
		})
	}

	plainStream := []byte(`{"model":"m","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	_, err := validateChatExecutionProfileRequest(plainStream, plan)
	require.NoError(t, err, "v5 must retain the v4 plain streaming contract")
	plainTools := []byte(`{"model":"m","messages":[],"tools":[` + tool + `]}`)
	_, err = validateChatExecutionProfileRequest(plainTools, plan)
	require.NoError(t, err, "v5 must retain the v4 non-stream tools contract")

	historical := executionProfilePlan(t, "doubao.seed-evolving.api", "1.5.0")
	_, err = validateChatExecutionProfileRequest([]byte(`{"model":"m","messages":[],`+definition+`}`), historical)
	require.ErrorContains(t, err, "must be one of text")
}

func TestDoubaoSeedEvolvingStrictJSONSchemaResponseUsesInvocationSnapshot(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-json", "model": "doubao-seed-evolving",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": `{"x":1,"y":2}`},
		}},
	}
	strictRoute := &RouteResult{
		ModelProtocolProfile: doubaoEvolvingStrictJSONSchemaProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID: modelprofile.CapabilityStrictJSONSchema,
			Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
		}},
	}
	require.True(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["content"] = "not json"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())

	plainRoute := &RouteResult{ModelProtocolProfile: doubaoEvolvingStrictJSONSchemaProtocol}
	require.True(t, inspectNonStreamOutputTextsForRoute(response, plainRoute).complete(), "plain v5 invocation must not inherit strict JSON response semantics")

	message["content"] = `{"x":1}`
	message["refusal"] = "unmodeled refusal"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())
	delete(message, "refusal")
	choice := response["choices"].([]interface{})[0].(map[string]interface{})
	choice["finish_reason"] = "length"
	require.False(t, inspectNonStreamOutputTextsForRoute(response, strictRoute).complete())
}

func TestDoubaoSeedEvolvingStrictJSONSchemaGuardrailTransformMustRemainJSON(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": `{"answer":"safe"}`},
		}},
	}
	route := &RouteResult{
		ModelProtocolProfile: doubaoEvolvingStrictJSONSchemaProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID: modelprofile.CapabilityStrictJSONSchema,
			Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
		}},
	}
	inspection := inspectNonStreamOutputTextsForRoute(response, route)
	require.True(t, inspection.complete())
	transformed := append([]guardrail.Text(nil), inspection.texts...)
	for index := range transformed {
		if transformed[index].Path == "choices[0].message.content" {
			transformed[index].Content = "[REDACTED]"
		}
	}
	err := applyProfileAwareOutputGuardrailTransform(route, guardrail.Result{Transformed: true, Texts: transformed}, inspection.refs)
	require.ErrorContains(t, err, "invalidated strict JSON content")
	require.Equal(t, `{"answer":"safe"}`, response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"])

	for index := range transformed {
		if transformed[index].Path == "choices[0].message.content" {
			transformed[index].Content = `{"answer":"[REDACTED]"}`
		}
	}
	require.NoError(t, applyProfileAwareOutputGuardrailTransform(route, guardrail.Result{Transformed: true, Texts: transformed}, inspection.refs))
	require.Equal(t, `{"answer":"[REDACTED]"}`, response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"])
}

type strictJSONCacheBackend struct {
	commits  atomic.Int64
	releases atomic.Int64
}

func (*strictJSONCacheBackend) Generation(context.Context, string) (uint64, error) { return 0, nil }
func (*strictJSONCacheBackend) Read(context.Context, string) ([]byte, error) {
	return nil, responsecache.ErrCacheMiss
}
func (*strictJSONCacheBackend) Acquire(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (backend *strictJSONCacheBackend) Commit(context.Context, string, string, string, []byte, time.Duration) error {
	backend.commits.Add(1)
	return nil
}
func (backend *strictJSONCacheBackend) Release(context.Context, string, string) error {
	backend.releases.Add(1)
	return nil
}
func (*strictJSONCacheBackend) Invalidate(context.Context, string) error { return nil }
func (*strictJSONCacheBackend) Purge(context.Context, string) (uint64, error) {
	return 0, nil
}

func TestDoubaoSeedEvolvingStrictJSONSchemaLiveRefusalIsHeldBeforeDeliveryAndCacheCommit(t *testing.T) {
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
	backend := &strictJSONCacheBackend{}
	keyring, err := responsecache.NewStaticKeyring("strict-json-v1", map[string][]byte{
		"strict-json-v1": bytes.Repeat([]byte{'k'}, 32),
	})
	require.NoError(t, err)
	cacheService, err := responsecache.NewService(
		backend, bytes.Repeat([]byte{'i'}, 32), keyring, responsecache.DefaultConfig(),
	)
	require.NoError(t, err)
	identity := strictJSONResponseCacheIdentity()
	owner := cacheService.Begin(context.Background(), responsecache.Request{
		Identity: identity, EffectiveBody: []byte(`{"model":"doubao-seed-evolving","messages":[{"role":"user","content":"json"}]}`),
		OptIn: true, PersistenceAllowed: true,
	})
	require.Equal(t, responsecache.DecisionOwner, owner.Kind)
	require.NotNil(t, owner.Reservation)

	relay := newTestRelay(p6f11RelayDB(), billingStub)
	relay.responseCache = cacheService
	relay.modelExecution = &modelExecutionRuntime{coordinator: &executionCoordinatorStub{}}
	route := strictJSONSchemaRoute()
	ctx := contextWithResponseCacheMiss(context.Background(), &responseCacheMissState{
		reservation: owner.Reservation,
		identity:    identity,
	})
	ctx = contextWithRouteAttempt(contextWithAttemptID(ctx, "att-doubao-strict-refusal"), 1, 1)
	recorder := httptest.NewRecorder()
	responseBody := `{"id":"chatcmpl-strict-refusal","object":"chat.completion","model":"doubao-seed-evolving","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}","refusal":"unmodeled"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	relay.relayNonStreamRequest(ctx, recorder, &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(responseBody)),
	}, route, "req-doubao-strict-refusal", 1, 2, "doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1",
		logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 1, 1, "upstream-strict-refusal")

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "server_error.upstream_error")
	require.NotContains(t, recorder.Body.String(), "unmodeled")
	require.EqualValues(t, 1, holds.Load())
	require.Zero(t, prepares.Load())
	require.Zero(t, settlements.Load())
	require.Zero(t, backend.commits.Load())
	require.EqualValues(t, 1, backend.releases.Load())
}

func TestDoubaoSeedEvolvingStrictJSONSchemaCacheHitInvalidContentCancelsBeforeDelivery(t *testing.T) {
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
	recorder := httptest.NewRecorder()
	relay.serveResponseCacheHit(
		context.Background(), recorder, strictJSONSchemaRoute(),
		&responsecache.Hit{SourceResponse: []byte(`{"id":"chatcmpl-strict-cache","object":"chat.completion","model":"doubao-seed-evolving","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"not json"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)},
		responsecache.Identity{}, invocationopt.Policy{}, "req-doubao-strict-cache", 1, 2,
		"doubao-seed-evolving", "doubao-seed-evolving", "127.0.0.1", time.Now(), logger.New(logger.Fields{}),
	)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "response_cache.integrity_error")
	require.NotContains(t, recorder.Body.String(), "not json")
	require.Zero(t, prepares.Load())
	require.EqualValues(t, 1, cancels.Load())
}

func strictJSONSchemaRoute() *RouteResult {
	return &RouteResult{
		Channel:     models.Channel{ID: 1, Name: "doubao", Provider: "doubao"},
		ChannelKey:  models.ChannelKey{ID: 2},
		MappedModel: "doubao-seed-evolving", ModelProtocolProfile: doubaoEvolvingStrictJSONSchemaProtocol,
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID: modelprofile.CapabilityStrictJSONSchema,
			Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
		}},
	}
}

func strictJSONResponseCacheIdentity() responsecache.Identity {
	return responsecache.Identity{
		Scope:    responsecache.Scope{OrgID: 1, ProjectID: 0, Region: "cn-beijing", Residency: "cn"},
		APIKeyID: 2, AuthorizationEpoch: "auth-v1", Endpoint: responsecache.EndpointChatCompletions,
		RequestedModel: "doubao-seed-evolving", ResolvedModel: "doubao-seed-evolving", Provider: "doubao",
		ChannelID: 1, ChannelRoutingEpoch: 1, ChannelCircuitEpoch: 1, CredentialID: 2, CredentialEpoch: 1,
		AdapterProfileVersion: "openai-compatible-v1", RoutePolicyVersion: "route-v1",
		RuntimeReleaseRef: "release-v1", RuntimeStage: "stable", RuntimeAssignmentID: "assignment-v1",
		GuardrailPolicyVersion: "guardrail-v1", OptimizationPolicyVersion: "optimization-v1",
		CustomerPricingVersion: "pricing-v1", ResponseCachePriceVersion: "zero-charge-v1",
		RequestSchemaVersion: responsecache.RequestSchemaChatV1, ResponseSchemaVersion: responsecache.ResponseSchemaChatV1,
	}
}
