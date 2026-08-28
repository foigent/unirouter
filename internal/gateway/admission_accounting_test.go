package gateway

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/guardrail"
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

func TestAdmissionEstimateCoversCompleteFinalChatEnvelope(t *testing.T) {
	arguments := strings.Repeat("a", 8_192)
	body := []byte(`{"model":"public-model","messages":[{"role":"assistant","content":null,"name":"worker","tool_call_id":"call_1","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"` + arguments + `"}}]}],"tools":[{"type":"function","function":{"name":"lookup","description":"` + strings.Repeat("d", 2_048) + `","parameters":{"type":"object"}}}],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},"prediction":{"type":"content","content":"predicted"},"reasoning":{"effort":"high"},"guided_grammar":"` + strings.Repeat("x", 1_024) + `"}`)
	estimate, err := estimateChatAdmission(body)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Version != chatAdmissionEstimateVersion || estimate.InputTokens < len(body) {
		t.Fatalf("complete envelope was underestimated: estimate=%+v bytes=%d", estimate, len(body))
	}

	for _, unsafe := range []string{
		`{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`,
		`{"model":"public-model","messages":[],"modalities":["text","audio"],"audio":{"voice":"alloy"}}`,
		`{"model":"public-model","messages":[],"prediction":{"type":"content","content":[{"type":"input_audio","audio":"opaque"}]}}`,
	} {
		if _, err := estimateChatAdmission([]byte(unsafe)); err == nil {
			t.Fatalf("non-text envelope was not failed closed: %s", unsafe)
		}
	}
}

func TestVerifiedMiniMaxTokenizerProfileReplacesByteUpperWithoutClaimingProviderUsage(t *testing.T) {
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"` + strings.Repeat("a", 100_000) + `"}],"max_tokens":1}`)
	plan := modelrouting.Plan{
		ExecutionProfileID: "minimax.m2.7.token-plan", ExecutionProfileVersion: "1.0.0",
		TokenizerProfileID: modelprofile.TokenizerMiniMaxM27ID, TokenizerProfileVersion: modelprofile.TokenizerMiniMaxM27Version,
	}
	byteUpper, err := estimateChatAdmission(body)
	if err != nil {
		t.Fatal(err)
	}
	profileEstimate, err := estimateChatAdmissionForPlan(body, plan)
	if err != nil {
		t.Fatal(err)
	}
	if profileEstimate.Version != miniMaxM27ChatEstimateVersion || profileEstimate.InputTokens >= byteUpper.InputTokens || profileEstimate.InputTokens >= 20_000 {
		t.Fatalf("verified tokenizer estimate did not narrow conservatively: profile=%+v byte=%+v", profileEstimate, byteUpper)
	}

	plan.TokenizerProfileID = modelprofile.TokenizerByteUpperID
	plan.TokenizerProfileVersion = modelprofile.TokenizerByteUpperVersion
	fallback, err := estimateChatAdmissionForPlan(body, plan)
	if err != nil {
		t.Fatal(err)
	}
	if fallback != byteUpper {
		t.Fatalf("unverified byte upper changed: got=%+v want=%+v", fallback, byteUpper)
	}

	plan.TokenizerProfileID = "tokenizer.unknown"
	if _, err := estimateChatAdmissionForPlan(body, plan); err == nil {
		t.Fatal("unknown tokenizer profile was not failed closed")
	}
}

func TestVerifiedKimiK26TokenizerProfileReplacesByteUpperWithoutClaimingProviderUsage(t *testing.T) {
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"` + strings.Repeat("a", 100_000) + `"}],"max_tokens":1}`)
	plan := modelrouting.Plan{
		ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.3.0",
		TokenizerProfileID: modelprofile.TokenizerKimiK26ID, TokenizerProfileVersion: modelprofile.TokenizerKimiK26Version,
	}
	byteUpper, err := estimateChatAdmission(body)
	require.NoError(t, err)
	profileEstimate, err := estimateChatAdmissionForPlan(body, plan)
	require.NoError(t, err)
	require.Equal(t, kimiK26ChatEstimateVersion, profileEstimate.Version)
	require.Less(t, profileEstimate.InputTokens, byteUpper.InputTokens)
	require.Less(t, profileEstimate.InputTokens, 30_000)
	providerEstimate, err := estimateProviderJSONAdmissionForPlan(body, plan)
	require.NoError(t, err)
	require.Equal(t, kimiK26ProviderEstimateVersion, providerEstimate.Version)
	require.Equal(t, profileEstimate.InputTokens, providerEstimate.InputTokens)
}

func TestVerifiedTokenizerProfilesAdmitLargeToolEnvelopeBeforeFreeze(t *testing.T) {
	tests := []struct {
		name            string
		profileID       string
		profileVersion  string
		estimateVersion string
		contentBytes    int
		contextLimit    int
	}{
		{name: "Kimi K2.6", profileID: "kimi.k2.6.api", profileVersion: "1.3.0", estimateVersion: kimiK26ChatEstimateVersion, contentBytes: 262_000, contextLimit: 262_144},
		{name: "MiniMax M2.7", profileID: "minimax.m2.7.token-plan", profileVersion: "1.0.0", estimateVersion: miniMaxM27ProviderEstimateVersion, contentBytes: 100_000, contextLimit: 20_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
			setExecutionTestProfile(t, &plan, test.profileID, test.profileVersion)
			plan.PublicContextLimit = test.contextLimit
			plan.Routes[0].MaxContext = test.contextLimit
			body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"` + strings.Repeat("a", test.contentBytes) + `"}],"max_tokens":1}`)
			byteUpper, err := estimateChatAdmission(body)
			require.NoError(t, err)
			require.Greater(t, byteUpper.InputTokens, plan.PublicContextLimit)

			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-verified-tokenizer-profile",
				modelExecutionRequestLimits{EstimatedInputTokens: byteUpper.InputTokens, ExplicitOutputTokens: intPointer(1), EstimateVersion: byteUpper.Version},
				body, time.Now(),
			)
			require.NoError(t, err)
			require.Len(t, batch.routes, 1)
			require.Less(t, batch.limits.EstimatedInputTokens, plan.PublicContextLimit)
			require.Equal(t, test.estimateVersion, batch.limits.EstimateVersion)
		})
	}
}

func TestAdmissionUnknownResourceFieldsFailBeforePlannerOrFreeze(t *testing.T) {
	tests := []string{
		`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"file_url":"https://example.test/large.bin"}`,
		`{"model":"public-model","messages":[{"role":"user","content":"hello","attachments":[{"file_id":"file-1"}]}]}`,
		`{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"hello","file_url":"https://example.test/large.bin"}]}]}`,
	}
	for index, body := range tests {
		encKey := []byte("0123456789abcdef0123456789abcdef")
		plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
		planner := &executionPlannerStub{plan: plan}
		coordinator := &executionCoordinatorStub{}
		var freezes, attempts, network atomic.Int64
		relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
			freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			},
			beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error { attempts.Add(1); return nil },
		}, nil, encKey)
		relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
			network.Add(1)
			return nil, io.ErrUnexpectedEOF
		})}
		if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", fmt.Sprintf("req-unknown-resource-%d", index), body))
		planner.mu.Lock()
		plans := len(planner.requests)
		planner.mu.Unlock()
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"validation_error.unsupported_semantics"`) {
			t.Fatalf("case %d status=%d body=%s", index, recorder.Code, recorder.Body.String())
		}
		if plans != 0 || freezes.Load() != 0 || attempts.Load() != 0 || coordinator.authorizeCount.Load() != 0 || network.Load() != 0 {
			t.Fatalf("case %d crossed pre-planner boundary: plans=%d freeze=%d attempt=%d authorize=%d network=%d", index, plans, freezes.Load(), attempts.Load(), coordinator.authorizeCount.Load(), network.Load())
		}
	}
}

func TestInputGuardrailCoversToolIdentifiersSchemasAndArgumentsBeforePlanner(t *testing.T) {
	const marker = "token_abcdefghijkl"
	tests := []struct {
		name   string
		action string
		body   string
		code   string
	}{
		{
			name: "function name", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"` + marker + `","parameters":{"type":"object"}}}]}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "schema property key", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"safe_name","parameters":{"type":"object","properties":{"` + marker + `":{"type":"string"}}}}}]}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "schema required item", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"safe_name","parameters":{"type":"object","required":["` + marker + `"]}}}]}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "schema ref", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"safe_name","parameters":{"$ref":"#/$defs/` + marker + `"}}}]}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "tool arguments", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"safe_name","arguments":"{\"secret\":\"` + marker + `\"}"}}]}]}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "selected tool name", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"safe_name","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"` + marker + `"}}}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "guided schema string", action: guardrail.ActionBlock,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"guided_json":"{\"type\":\"object\",\"title\":\"` + marker + `\"}"}`,
			code: "guardrail_error.input_blocked",
		},
		{
			name: "structured identifier redaction fails closed", action: guardrail.ActionRedact,
			body: `{"model":"public-model","messages":[{"role":"user","content":"safe"}],"tools":[{"type":"function","function":{"name":"` + marker + `","parameters":{"type":"object"}}}]}`,
			code: "guardrail_error.transform_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newP6F10GuardrailService(t, guardrail.Policy{
				ID: 91, Name: "tool boundary", Version: 1, Stage: guardrail.StageInput,
				DetectorRef: guardrail.DetectorPII, Action: test.action,
				Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
				Mandatory: true, Config: json.RawMessage(`{"entities":["secret"]}`),
			})
			encKey := []byte("0123456789abcdef0123456789abcdef")
			planner := &executionPlannerStub{plan: executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)}
			var freezes atomic.Int64
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			}}, nil, encKey)
			relay.SetGuardrail(service)
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-tool-guardrail", test.body))
			planner.mu.Lock()
			plans := len(planner.requests)
			planner.mu.Unlock()
			if !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if plans != 0 || freezes.Load() != 0 {
				t.Fatalf("tool policy crossed pre-planner boundary: plans=%d freezes=%d", plans, freezes.Load())
			}
		})
	}
}

func TestEmbeddingsAdmissionShapesAndExactTokenIDs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
		ok   bool
	}{
		{name: "string", body: `{"model":"e","input":"hello"}`, want: 5, ok: true},
		{name: "strings", body: `{"model":"e","input":["hello","世界"]}`, want: 11, ok: true},
		{name: "token ids", body: `{"model":"e","input":[1,2,3,2147483647]}`, want: 4, ok: true},
		{name: "token matrix", body: `{"model":"e","input":[[1,2],[3,4,5]]}`, want: 5, ok: true},
		{name: "fraction", body: `{"model":"e","input":[1.0]}`},
		{name: "exponent", body: `{"model":"e","input":[1e3]}`},
		{name: "negative", body: `{"model":"e","input":[-1]}`},
		{name: "negative zero", body: `{"model":"e","input":[-0]}`},
		{name: "range", body: `{"model":"e","input":[2147483648]}`},
		{name: "mixed", body: `{"model":"e","input":["one",2]}`},
		{name: "nested strings", body: `{"model":"e","input":[["one"]]}`},
		{name: "deep", body: `{"model":"e","input":[[[1]]]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			estimate, err := estimateEmbeddingsAdmission([]byte(test.body))
			if test.ok {
				if err != nil || estimate.InputTokens != test.want || estimate.Version != embeddingsAdmissionEstimateVersion {
					t.Fatalf("estimate=%+v err=%v, want=%d", estimate, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("unsupported embedding shape admitted: %+v", estimate)
			}
		})
	}
	providerEstimate, err := estimateProviderEmbeddingsAdmission([]byte(`{"input":[1000,2000,3000],"encoding_format":"float"}`))
	if err != nil || providerEstimate.InputTokens != 3 || providerEstimate.Version != embeddingsAdmissionEstimateVersion {
		t.Fatalf("provider token IDs were not counted exactly: estimate=%+v err=%v", providerEstimate, err)
	}
	if _, err := estimateProviderEmbeddingsAdmission([]byte(`{"input":[1,2],"provider_file":{"url":"https://example.test/input"}}`)); err == nil {
		t.Fatal("unknown provider embeddings resource shape was admitted")
	}
}

func TestAdmissionRejectsLargeStructuredInputsBeforeFreezeOrDispatch(t *testing.T) {
	tokenVector := "[" + strings.TrimSuffix(strings.Repeat("1,", 8_192), ",") + "]"
	tests := []struct {
		name      string
		operation modeloperation.ID
		path      string
		body      string
		maxCtx    int
		editPlan  func(*modelrouting.Plan)
		handle    func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{
			name: "large tool arguments", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", maxCtx: 4_096,
			body:   `{"model":"public-model","messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"work","arguments":"` + strings.Repeat("x", 8_192) + `"}}]}],"max_tokens":1}`,
			handle: (*Relay).HandleChatCompletion,
		},
		{
			name: "eight thousand token ids", operation: modeloperation.IDEmbeddings, path: "/v1/embeddings", maxCtx: 8_000,
			body: `{"model":"public-model","input":` + tokenVector + `}`, handle: (*Relay).HandleEmbeddings,
		},
		{
			name: "adapter mapped model expansion", operation: modeloperation.IDChatCompletions, path: "/v1/chat/completions", maxCtx: 4_096,
			body: `{"model":"public-model","messages":[{"role":"user","content":"short"}],"max_tokens":1}`,
			editPlan: func(plan *modelrouting.Plan) {
				plan.Routes[0].UpstreamModel = strings.Repeat("m", 8_192)
			},
			handle: (*Relay).HandleChatCompletion,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, test.operation, "https://provider.test", encKey, nil)
			plan.Routes[0].MaxContext = test.maxCtx
			if test.editPlan != nil {
				test.editPlan(&plan)
			}
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var freezes, attempts, network atomic.Int64
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezes.Add(1)
					return &billing.FreezeResult{Success: true}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error { attempts.Add(1); return nil },
			}, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, io.ErrUnexpectedEOF
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			test.handle(relay, recorder, executionRequest(http.MethodPost, test.path, "req-admission-"+strings.ReplaceAll(test.name, " ", "-"), test.body))
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"validation_error.context_length_exceeded"`) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if freezes.Load() != 0 || attempts.Load() != 0 || coordinator.authorizeCount.Load() != 0 || network.Load() != 0 {
				t.Fatalf("rejection crossed pre-freeze boundary: freeze=%d attempt=%d authorize=%d network=%d", freezes.Load(), attempts.Load(), coordinator.authorizeCount.Load(), network.Load())
			}
		})
	}
}

func TestPlannerRequestDefaultOutputLimitIsTheImplicitHardReservation(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
	plan.PublicOutputLimit = 4_321
	plan.RequestDefaultOutputLimit = 1_234
	planner := &executionPlannerStub{plan: plan}
	coordinator := &executionCoordinatorStub{}
	var frozen billing.FreezeRequest
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
		freezeFn: func(_ context.Context, request billing.FreezeRequest) (*billing.FreezeResult, error) {
			frozen = request
			return &billing.FreezeResult{Success: true}, nil
		},
	}, nil, encKey)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-public-output", `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	if frozen.ReservedCompletionTokens != plan.RequestDefaultOutputLimit || frozen.EstimatedTokens != frozen.EstimatedInputTokens+plan.RequestDefaultOutputLimit {
		t.Fatalf("planner request default was not the Freeze hard bound: %+v", frozen)
	}
	if coordinator.authorizeCount.Load() != 1 {
		t.Fatalf("short text did not pass normal admission: authorize=%d status=%d body=%s", coordinator.authorizeCount.Load(), recorder.Code, recorder.Body.String())
	}

	legacy := executionTestPlan(t, modelrouting.ModeLegacy, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
	legacy.PublicOutputLimit = 0
	legacy.Routes[0].ProtocolProfileID = "legacy.channel_model_probe"
	legacy.Routes[0].ProtocolProfileVersion = "v1"
	legacy.Routes[0].DefaultMaxOutput = 77
	legacyPlanner := &executionPlannerStub{plan: legacy}
	legacyCoordinator := &executionCoordinatorStub{}
	var legacyFrozen billing.FreezeRequest
	legacyRelay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
		freezeFn: func(_ context.Context, request billing.FreezeRequest) (*billing.FreezeResult, error) {
			legacyFrozen = request
			return &billing.FreezeResult{Success: true}, nil
		},
	}, nil, encKey)
	if err := legacyRelay.SetModelExecutionRuntime(modelrouting.ModeLegacy, legacyPlanner, legacyCoordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	legacyRecorder := httptest.NewRecorder()
	legacyRelay.HandleChatCompletion(legacyRecorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-legacy-output", `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	if legacyFrozen.ReservedCompletionTokens != legacyImplicitOutputLimitV1 || legacyCoordinator.authorizeCount.Load() != 1 {
		t.Fatalf("legacy compatibility bound mismatch: freeze=%+v authorize=%d status=%d body=%s", legacyFrozen, legacyCoordinator.authorizeCount.Load(), legacyRecorder.Code, legacyRecorder.Body.String())
	}
	if legacyFrozen.ReservedCompletionTokens == legacy.Routes[0].DefaultMaxOutput {
		t.Fatal("route default output was incorrectly promoted to a hard bound")
	}
	legacyBatch, err := legacyRelay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-legacy-output-wire",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	legacyBody, err := providerRequestBody(legacyBatch.routes[0].upstreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	var legacyPayload map[string]json.RawMessage
	if err := json.Unmarshal(legacyBody, &legacyPayload); err != nil {
		t.Fatal(err)
	}
	if string(legacyPayload["max_tokens"]) != "1000" || legacyBatch.limits.ProviderOutputLimit != legacyImplicitOutputLimitV1 {
		t.Fatalf("legacy adapter compatibility cap did not bind provider liability: limits=%+v body=%s", legacyBatch.limits, legacyBody)
	}
}

func TestImplicitOutputLimitUsesExactOpenAICompatibleWireContract(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	newPlan := func(mode modelrouting.Mode) modelrouting.Plan {
		plan := executionTestPlan(t, mode, modeloperation.IDChatCompletions, "https://api.deepseek.example", encKey, nil)
		setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.0.0")
		plan.PublicOutputLimit = 384_000
		plan.RequestDefaultOutputLimit = 32_768
		plan.Routes[0].Provider = "deepseek"
		plan.Routes[0].UpstreamModel = "deepseek-v4-flash"
		plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
		plan.Routes[0].ProtocolProfileVersion = "1.0.0"
		return plan
	}
	probePlan := newPlan(modelrouting.ModeCanonical)
	setExecutionTestProfile(t, &probePlan, modelprofile.UnverifiedProfileID, modelprofile.UnverifiedProfileVersion)
	probeOnly := probePlan.Routes[0]
	probeOnly.ProtocolProfileID = "legacy.channel_model_probe"
	probeOnly.ProtocolProfileVersion = "v1"
	if _, supported := providerOutputLimitField(probePlan, probeOnly, nil); supported {
		t.Fatal("probe profile was incorrectly promoted to an execution wire contract")
	}
	for _, mode := range []modelrouting.Mode{modelrouting.ModeCanonical, modelrouting.ModeVerify} {
		t.Run(string(mode), func(t *testing.T) {
			plan := newPlan(mode)
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			if err := relay.SetModelExecutionRuntime(mode, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-deepseek-output-contract",
				modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
				[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if batch.limits.EstimatedOutputTokens != plan.RequestDefaultOutputLimit || batch.limits.ProviderOutputLimit != plan.RequestDefaultOutputLimit {
				t.Fatalf("request default output was not reserved: %+v", batch.limits)
			}
			actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(actualBody, &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["max_tokens"]) != "32768" || payload["max_completion_tokens"] != nil || string(payload["model"]) != `"deepseek-v4-flash"` {
				t.Fatalf("DeepSeek/OpenAI-compatible hard cap contract mismatch: %s", actualBody)
			}
		})
	}
	plan := newPlan(modelrouting.ModeCanonical)
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}

	_, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-reasoning-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`), time.Now(),
	)
	if !errors.Is(err, modelrouting.ErrInvalidRequest) {
		t.Fatalf("historical reasoning profile field was accepted instead of failing closed: %v", err)
	}

	explicit := 128
	_, err = relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-reasoning-explicit-output",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicit, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high","max_completion_tokens":128}`), time.Now(),
	)
	if !errors.Is(err, modelrouting.ErrInvalidRequest) {
		t.Fatalf("explicit output must not retroactively enable historical reasoning semantics: %v", err)
	}

	current := newPlan(modelrouting.ModeCanonical)
	setExecutionTestProfile(t, &current, "deepseek.v4-flash.api", "1.2.0")
	currentRelay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	if err := currentRelay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: current}, &executionCoordinatorStub{}, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	currentBatch, err := currentRelay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "req-current-reasoning-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`), time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	currentBody, err := providerRequestBody(currentBatch.routes[0].upstreamRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(currentBody), `"max_tokens":32768`) || strings.Contains(string(currentBody), `"max_completion_tokens"`) {
		t.Fatalf("current typed reasoning request default was not injected: %s", currentBody)
	}
}

func TestMiniMaxCurrentVerifiedProtocolInjectsOfficialCompletionLimit(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "minimax.m2.7.token-plan", "1.2.0")
	plan.PublicContextLimit = 204_800
	plan.PublicOutputLimit = 204_800
	plan.Routes[0].Provider = "minimax"
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	plan.Routes[0].UpstreamModel = "MiniMax-M2.7"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 204_800

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"MiniMax-M2.7", "MiniMax-M2.7", "req-minimax-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"MiniMax-M2.7","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 65_536, batch.limits.ProviderOutputLimit)
	require.Len(t, batch.routes, 1)
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "65536", string(payload["max_completion_tokens"]))
	require.Nil(t, payload["max_tokens"])
}

func TestCurrentMiniMaxProfilesPrepareExactOutputFieldsAndOfficialURL(t *testing.T) {
	for _, test := range []struct {
		name, profileID, upstreamModel   string
		context, maximum, requestDefault int
	}{
		{name: "M2.7", profileID: "minimax.m2.7.token-plan", upstreamModel: "MiniMax-M2.7", context: 204_800, maximum: 204_800, requestDefault: 65_536},
		{name: "M2.5 highspeed", profileID: "minimax.m2.5-highspeed.token-plan", upstreamModel: "MiniMax-M2.5-highspeed", context: 204_800, maximum: 204_800, requestDefault: 65_536},
		{name: "M3", profileID: "minimax.m3.token-plan", upstreamModel: "MiniMax-M3", context: 1_000_000, maximum: 524_288, requestDefault: 131_072},
	} {
		t.Run(test.name, func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimaxi.com/v1", encKey, nil)
			setExecutionTestProfile(t, &plan, test.profileID, "1.2.0")
			plan.PublicContextLimit = test.context
			plan.PublicOutputLimit = test.maximum
			plan.Routes[0].Provider = "minimax"
			plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
			plan.Routes[0].UpstreamModel = test.upstreamModel
			plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
			plan.Routes[0].ProtocolProfileVersion = "1.0.0"
			plan.Routes[0].MaxContext = test.context
			plan.Routes[0].DefaultMaxOutput = test.requestDefault

			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
			require.NoError(t, relay.SetModelExecutionRuntime(
				modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
			))

			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
				test.upstreamModel, test.upstreamModel, "req-minimax-current-default-"+test.name,
				modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
				[]byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, test.upstreamModel)), time.Now(),
			)
			require.NoError(t, err)
			require.Equal(t, test.requestDefault, batch.limits.ProviderOutputLimit)
			require.Len(t, batch.routes, 1)
			actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(actualBody, &payload))
			require.Equal(t, fmt.Sprintf("%d", test.requestDefault), string(payload["max_completion_tokens"]))
			require.Nil(t, payload["max_tokens"])
			require.Equal(t, "https://api.minimaxi.com/v1/chat/completions", batch.routes[0].upstreamRequest.URL.String())

			for _, field := range []string{"max_tokens", "max_completion_tokens"} {
				// The typed contract test covers the provider field maximum. The
				// complete route must also reserve non-zero prompt tokens inside the
				// total context budget, so use a distinct above-default value here.
				explicit := test.requestDefault + 1
				body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],%q:%d}`, test.upstreamModel, field, explicit))
				explicitBatch, err := relay.prepareModelExecutionRoutes(
					context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
					test.upstreamModel, test.upstreamModel, "req-minimax-current-explicit-"+field+test.name,
					modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicit, EstimateVersion: chatAdmissionEstimateVersion},
					body, time.Now(),
				)
				require.NoError(t, err, field)
				require.Equal(t, explicit, explicitBatch.limits.EstimatedOutputTokens)
				require.Zero(t, explicitBatch.limits.ProviderOutputLimit)
				explicitBody, err := providerRequestBody(explicitBatch.routes[0].upstreamRequest)
				require.NoError(t, err)
				var explicitPayload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(explicitBody, &explicitPayload))
				require.Equal(t, fmt.Sprintf("%d", explicit), string(explicitPayload[field]))
			}
		})
	}
}

func TestGrok46CoreChatInjectsPolicyDefaultAndUsesExactOfficialURL(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.x.ai/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "xai.grok-4.6.api", "1.1.0")
	plan.PublicContextLimit = 500_000
	plan.PublicOutputLimit = 131_072
	plan.Routes[0].Provider = "xai"
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	plan.Routes[0].UpstreamModel = "grok-4.6"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 500_000
	plan.Routes[0].DefaultMaxOutput = 32_768

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"grok-4.6", "grok-4.6", "req-grok-core-default",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 32_768, batch.limits.ProviderOutputLimit)
	require.Equal(t, "https://api.x.ai/v1/chat/completions", batch.routes[0].upstreamRequest.URL.String())
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "32768", string(payload["max_tokens"]))
	require.Nil(t, payload["max_completion_tokens"])

	explicit := 131_072
	explicitBatch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"grok-4.6", "grok-4.6", "req-grok-core-explicit",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &explicit, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":131072}`), time.Now(),
	)
	require.NoError(t, err)
	explicitBody, err := providerRequestBody(explicitBatch.routes[0].upstreamRequest)
	require.NoError(t, err)
	require.Contains(t, string(explicitBody), `"max_tokens":131072`)
	require.NotContains(t, string(explicitBody), "max_completion_tokens")
}

func TestQwen38MaxVerifiedProtocolInjectsOfficialDefaultCompletionLimit(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://dashscope.aliyuncs.com/compatible-mode/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "qwen.3.8-max.api", "1.1.0")
	plan.PublicContextLimit = 1_000_000
	plan.PublicOutputLimit = 131_072
	plan.Routes[0].Provider = "dashscope"
	plan.Routes[0].UpstreamModel = "qwen3.8-max"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 1_000_000

	field, supported := providerOutputLimitField(plan, plan.Routes[0], nil)
	require.True(t, supported)
	require.Equal(t, "max_completion_tokens", field)

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"qwen3.8-max", "qwen3.8-max", "req-qwen-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"qwen3.8-max","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 131_072, batch.limits.ProviderOutputLimit)
	require.Len(t, batch.routes, 1)
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "131072", string(payload["max_completion_tokens"]))
	require.Nil(t, payload["max_tokens"])
}

func TestQwen38ExtensionsRemainInProviderPayloadAndAdmissionCount(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://dashscope.aliyuncs.com/compatible-mode/v1", encKey, nil)
	setExecutionTestProfile(t, &plan, "qwen.3.8-max.api", "1.8.0")
	plan.PublicContextLimit = 1_000_000
	plan.PublicOutputLimit = 131_072
	plan.Routes[0].Provider = "dashscope"
	plan.Routes[0].UpstreamModel = "qwen3.8-max"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 1_000_000

	body := []byte(`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":"complete chain"},{"role":"user","content":"hello"}],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup"}}],"tool_stream":true,"enable_thinking":true,"thinking_budget":4096,"preserve_thinking":true,"max_tokens":131072}`)
	expectedAdmission, err := estimateChatAdmissionForPlan(body, plan)
	require.NoError(t, err)
	withoutExtensions, err := estimateChatAdmissionForPlan([]byte(`{"model":"qwen3.8-max","messages":[{"role":"assistant","content":"answer","reasoning_content":"complete chain"},{"role":"user","content":"hello"}],"stream":true,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup"}}],"max_tokens":131072}`), plan)
	require.NoError(t, err)
	require.Greater(t, expectedAdmission.InputTokens, withoutExtensions.InputTokens)

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	explicitOutput := 131_072
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"qwen3.8-max", "qwen3.8-max", "req-qwen-extension-payload",
		modelExecutionRequestLimits{ExplicitOutputTokens: &explicitOutput}, body, time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, expectedAdmission.InputTokens, batch.limits.EstimatedInputTokens)
	require.Len(t, batch.routes, 1)
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "true", string(payload["enable_thinking"]))
	require.Equal(t, "4096", string(payload["thinking_budget"]))
	require.Equal(t, "true", string(payload["preserve_thinking"]))
	require.Equal(t, "true", string(payload["tool_stream"]))
	require.Equal(t, "131072", string(payload["max_tokens"]))
}

func TestGLM53VerifiedProtocolInjectsOfficialDefaultMaxTokens(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://open.bigmodel.cn/api/paas/v4", encKey, nil)
	setExecutionTestProfile(t, &plan, "glm.5.3.api", "1.3.0")
	plan.PublicContextLimit = 1_000_000
	plan.PublicOutputLimit = 65_536
	plan.Routes[0].Provider = "glm"
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	plan.Routes[0].UpstreamModel = "glm-5.3"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 1_000_000

	field, supported := providerOutputLimitField(plan, plan.Routes[0], nil)
	require.True(t, supported)
	require.Equal(t, "max_tokens", field)

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"glm-5.3", "glm-5.3", "req-glm-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 65_536, batch.limits.ProviderOutputLimit)
	require.Len(t, batch.routes, 1)
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "65536", string(payload["max_tokens"]))
	require.Nil(t, payload["max_completion_tokens"])
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", batch.routes[0].upstreamRequest.URL.String())
}

func TestGLM5VTurboVerifiedProtocolInjectsOfficialDefaultMaxTokens(t *testing.T) {
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://open.bigmodel.cn/api/paas/v4", encKey, nil)
	setExecutionTestProfile(t, &plan, "glm.5v-turbo.api", "1.3.0")
	plan.PublicContextLimit = 200_000
	plan.PublicOutputLimit = 65_536
	plan.Routes[0].Provider = "glm"
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	plan.Routes[0].UpstreamModel = "glm-5v-turbo"
	plan.Routes[0].ProtocolProfileID = "openai.chat_completions"
	plan.Routes[0].ProtocolProfileVersion = "1.0.0"
	plan.Routes[0].MaxContext = 200_000

	field, supported := providerOutputLimitField(plan, plan.Routes[0], nil)
	require.True(t, supported)
	require.Equal(t, "max_tokens", field)

	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(
		modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default(),
	))
	batch, err := relay.prepareModelExecutionRoutes(
		context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"glm-5v-turbo", "glm-5v-turbo", "req-glm5v-output-contract",
		modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
		[]byte(`{"model":"glm-5v-turbo","messages":[{"role":"user","content":"hello"}]}`), time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 65_536, batch.limits.ProviderOutputLimit)
	require.Len(t, batch.routes, 1)
	actualBody, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(actualBody, &payload))
	require.Equal(t, "65536", string(payload["max_tokens"]))
	require.Nil(t, payload["max_completion_tokens"])
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", batch.routes[0].upstreamRequest.URL.String())
}

func TestChatProviderFailuresHaveOneTopologyNeutralExhaustedResponse(t *testing.T) {
	var canonicalBody string
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://provider.test", encKey, nil)
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
				digest, err := executionevidence.Digest(route.Evidence)
				if err != nil {
					return modelexecution.Decision{}, err
				}
				return modelexecution.Decision{
					Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
					ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
				}, nil
			}}
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true}, nil
				},
			}, nil, encKey)
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"provider-secret","status":"` + http.StatusText(status) + `"}}`)),
				}, nil
			})}
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			relay.HandleChatCompletion(recorder, executionRequest(http.MethodPost, "/v1/chat/completions", "req-provider-failure", `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":1}`))
			if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "provider-secret") || strings.Contains(recorder.Body.String(), http.StatusText(status)) {
				t.Fatalf("provider topology escaped: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if canonicalBody == "" {
				canonicalBody = recorder.Body.String()
			} else if recorder.Body.String() != canonicalBody {
				t.Fatalf("provider status changed public error: first=%s current=%s", canonicalBody, recorder.Body.String())
			}
		})
	}
}

func TestJSONMutationPathsPreserveLargeIntegers(t *testing.T) {
	const large = "9007199254740993123456789"
	const streamLarge = "9007199254740993"
	embeddingBody := []byte(`{"model":"e","input":"secret","provider_counter":` + large + `}`)
	transformed, err := applyEmbeddingsGuardrailTransform(embeddingBody, &EmbeddingsRequest{Model: "e", Input: "secret"}, guardrail.Result{
		Transformed: true, Texts: []guardrail.Text{{Path: "input", Content: "safe"}},
	})
	if err != nil || !strings.Contains(string(transformed), `"provider_counter":`+large) {
		t.Fatalf("embedding transform rounded integer: body=%s err=%v", transformed, err)
	}

	frame, err := parseGuardedStreamFrame([]byte(`{"created":` + streamLarge + `,"choices":[{"index":0,"delta":{"content":"safe"}}]}`))
	if err != nil || frame.unsupported {
		t.Fatalf("normal stream index rejected: frame=%+v err=%v", frame, err)
	}
	frame.transformed = true
	encoded, err := frame.data()
	if err != nil || !strings.Contains(string(encoded), `"created":`+streamLarge) {
		t.Fatalf("stream transform rounded integer: body=%s err=%v", encoded, err)
	}

	relay := newTestRelay(executionTestDB(), &mockBilling{})
	nonStreamResponse := &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"source","object":"chat.completion","created":` + streamLarge + `,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
	}
	nonStreamRecorder := httptest.NewRecorder()
	nonStreamContext := contextWithRouteAttempt(contextWithAttemptID(context.Background(), "att-large-number"), 1, 1)
	relay.relayNonStreamRequest(nonStreamContext, nonStreamRecorder, nonStreamResponse,
		&RouteResult{Channel: models.Channel{ID: 1, Name: "provider", Provider: "openai"}},
		"req-nonstream-number", 1, 2, "public-model", "public-model", "127.0.0.1",
		logger.New(logger.Fields{RequestID: "req-nonstream-number", OrgID: 1}), time.Now(), adapters.NewOpenAICompatible(), 1, 1, "")
	if nonStreamRecorder.Code != http.StatusOK || !strings.Contains(nonStreamRecorder.Body.String(), `"created":`+streamLarge) {
		t.Fatalf("non-stream delivery rounded integer: status=%d body=%s", nonStreamRecorder.Code, nonStreamRecorder.Body.String())
	}

	cacheSource := []byte(`{"id":"source","object":"chat.completion","created":1,"metadata":{"provider_counter":` + large + `},"choices":[{"index":0,"message":{"role":"assistant","content":"cached"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	recorder := httptest.NewRecorder()
	relay.serveResponseCacheHit(
		context.Background(), recorder,
		&RouteResult{Channel: models.Channel{ID: 1, Name: "provider", Provider: "openai"}, ChannelKey: models.ChannelKey{ID: 2}},
		&responsecache.Hit{SourceResponse: cacheSource}, responsecache.Identity{}, invocationopt.Policy{},
		"req-cache-number", 1, 2, "public-model", "public-model", "127.0.0.1", time.Now(),
		logger.New(logger.Fields{RequestID: "req-cache-number", OrgID: 1}),
	)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"provider_counter":`+large) {
		t.Fatalf("cache delivery rounded integer: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := decodeJSONUseNumber(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	metadata, ok := response["metadata"].(map[string]interface{})
	if !ok || metadata["provider_counter"] != json.Number(large) {
		t.Fatalf("cache number=%v (%T), want exact json.Number", response["metadata"], response["metadata"])
	}
}

func TestUnknownNonStreamAndCachedOutputFieldsAreNeverDelivered(t *testing.T) {
	const marker = "provider-private-marker"
	responseBody := `{"id":"source","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop","private_output":"` + marker + `"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	var holds, prepares, completions atomic.Int64
	holdReason := ""
	billingStub := &mockBilling{
		completeAttemptFn: func(context.Context, billing.ProviderAttemptCompletion) error {
			completions.Add(1)
			return nil
		},
		holdOutputFn: func(_ context.Context, _ billing.SettleRequest, reason string) error {
			holds.Add(1)
			holdReason = reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			prepares.Add(1)
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
	}
	coordinator := &executionCoordinatorStub{}
	relay := newTestRelay(p6f11RelayDB(), billingStub)
	relay.modelExecution = &modelExecutionRuntime{coordinator: coordinator}
	ctx := contextWithExecutionDecision(contextWithRouteAttempt(contextWithAttemptID(context.Background(), "att-private-output"), 1, 1), modelexecution.Decision{Status: modelexecution.Applied})
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, recorder, &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody)),
	}, &RouteResult{Channel: models.Channel{ID: 1, Name: "provider", Provider: "provider"}},
		"req-private-output", 1, 2, "public-model", "public-model", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 1, 1, "")
	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), marker) {
		t.Fatalf("unknown provider field escaped: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if holds.Load() != 1 || holdReason != billing.OutputHoldReasonProviderResponseUnusable || completions.Load() != 0 || prepares.Load() != 0 || coordinator.providerCount.Load() != 1 || coordinator.successCount.Load() != 0 {
		t.Fatalf("unknown response terminal mismatch: holds=%d reason=%s completions=%d prepares=%d provider_failure=%d success=%d", holds.Load(), holdReason, completions.Load(), prepares.Load(), coordinator.providerCount.Load(), coordinator.successCount.Load())
	}

	var cachePrepares, cacheCancels atomic.Int64
	cacheRelay := newTestRelay(executionTestDB(), &mockBilling{
		prepareCacheDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			cachePrepares.Add(1)
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
		cancelFn: func(context.Context, string, int64) error { cacheCancels.Add(1); return nil },
	})
	cacheRecorder := httptest.NewRecorder()
	cacheRelay.serveResponseCacheHit(
		context.Background(), cacheRecorder,
		&RouteResult{Channel: models.Channel{ID: 1, Name: "provider", Provider: "provider"}},
		&responsecache.Hit{SourceResponse: []byte(responseBody)}, responsecache.Identity{}, invocationopt.Policy{},
		"req-private-cache", 1, 2, "public-model", "public-model", "127.0.0.1", time.Now(), logger.New(logger.Fields{}),
	)
	if cacheRecorder.Code != http.StatusInternalServerError || !strings.Contains(cacheRecorder.Body.String(), "response_cache.integrity_error") || strings.Contains(cacheRecorder.Body.String(), marker) {
		t.Fatalf("unknown cached field escaped: status=%d body=%s", cacheRecorder.Code, cacheRecorder.Body.String())
	}
	if cachePrepares.Load() != 0 || cacheCancels.Load() != 1 {
		t.Fatalf("unknown cache output crossed delivery: prepares=%d cancels=%d", cachePrepares.Load(), cacheCancels.Load())
	}
}

func TestEnforcedNonStreamGuardrailFailsClosedOnMalformedToolOutput(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 92, Name: "output completeness", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionRedact,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "public-model", Endpoint: "chat"})
	for _, body := range []string{
		`{"choices":[{"message":{"role":"assistant","content":"safe","function_call":{"name":"f","arguments":{"opaque":"alice@example.com"}}}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"safe","tool_calls":{"function":{"arguments":"alice@example.com"}}}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"safe","tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":["alice@example.com"]}}]}}]}`,
	} {
		var response map[string]interface{}
		if err := decodeJSONUseNumber([]byte(body), &response); err != nil {
			t.Fatal(err)
		}
		inspection := inspectNonStreamOutputTexts(response)
		if inspection.complete() {
			t.Fatalf("malformed modeled output was treated as complete: %s", body)
		}
		result, _ := evaluateNonStreamOutputGuardrail(context.Background(), pinned, response)
		if result.Decision != guardrail.DecisionBlock || !result.Degraded {
			t.Fatalf("enforced output did not fail closed: result=%+v body=%s", result, body)
		}
	}
}

func TestStreamWhitelistAndProviderErrorsNeverDeliverUnknownFrames(t *testing.T) {
	tests := []struct {
		name          string
		stream        string
		adapter       adapters.Adapter
		wantDelivery  bool
		wantSettle    bool
		forbiddenText string
	}{
		{
			name: "unknown frame", stream: "data: {\"choices\":[{\"delta\":{\"audio\":\"opaque-audio\"}}]}\n\ndata: [DONE]\n\n",
			forbiddenText: "opaque-audio",
		},
		{
			name: "openai error then done", stream: "data: {\"error\":{\"message\":\"provider-secret\"}}\n\ndata: [DONE]\n\n",
			forbiddenText: "provider-secret",
		},
		{
			name: "anthropic error", stream: "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"anthropic-secret\"}}\n\n",
			adapter: adapters.NewAnthropicNative(), forbiddenText: "anthropic-secret",
		},
		{
			name: "partial then error", stream: "data: {\"choices\":[{\"delta\":{\"content\":\"safe-partial\"}}]}\n\ndata: {\"error\":{\"message\":\"provider-secret\"}}\n\ndata: [DONE]\n\n",
			wantDelivery: true, wantSettle: true, forbiddenText: "provider-secret",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var begins, deliveries, settles, cancels, settledCompletion atomic.Int64
			billingStub := &mockBilling{
				beginStreamFn:    func(context.Context, billing.StreamDeliveryStart) error { begins.Add(1); return nil },
				recordDeliveryFn: func(context.Context, billing.DeliveryEvent) error { deliveries.Add(1); return nil },
				settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
					settles.Add(1)
					settledCompletion.Store(int64(request.CompletionTokens))
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				},
				cancelFn: func(context.Context, string, int64) error { cancels.Add(1); return nil },
			}
			coordinator := &executionCoordinatorStub{}
			relay := newTestRelay(p6f11RelayDB(), billingStub)
			relay.modelExecution = &modelExecutionRuntime{coordinator: coordinator}
			ctx := contextWithExecutionDecision(p6f11StreamContext(nil, "req-"+strings.ReplaceAll(test.name, " ", "-")), modelexecution.Decision{Status: modelexecution.Applied})
			response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.stream))}
			recorder := httptest.NewRecorder()
			relay.relayStreamRequest(ctx, recorder, response, &RouteResult{Channel: models.Channel{Name: "provider", Provider: "provider"}},
				"req-"+strings.ReplaceAll(test.name, " ", "-"), 1, 2, "public-model", "public-model", "127.0.0.1", nil,
				logger.New(logger.Fields{}), time.Now(), 1_000, test.adapter, "")
			if strings.Contains(recorder.Body.String(), test.forbiddenText) {
				t.Fatalf("unmodeled/provider error text was disclosed: %s", recorder.Body.String())
			}
			if coordinator.providerCount.Load() != 1 || coordinator.successCount.Load() != 0 {
				t.Fatalf("provider terminal health mismatch: failure=%d success=%d", coordinator.providerCount.Load(), coordinator.successCount.Load())
			}
			if (begins.Load() > 0) != test.wantDelivery || (deliveries.Load() > 0) != test.wantDelivery || (settles.Load() > 0) != test.wantSettle {
				t.Fatalf("delivery mismatch: begin=%d delivery=%d settle=%d cancel=%d body=%s", begins.Load(), deliveries.Load(), settles.Load(), cancels.Load(), recorder.Body.String())
			}
			if !test.wantDelivery && cancels.Load() != 1 {
				t.Fatalf("error-only stream did not cancel cleanly: cancel=%d", cancels.Load())
			}
			if test.wantSettle && settledCompletion.Load() <= 0 {
				t.Fatalf("partial provider failure did not settle only the delivered model delta: completion=%d", settledCompletion.Load())
			}
			if !test.wantSettle && settledCompletion.Load() != 0 {
				t.Fatalf("undelivered provider failure created customer completion debt: completion=%d", settledCompletion.Load())
			}
		})
	}
}

func TestStreamPublicHardCapCountsReasoningAndToolArguments(t *testing.T) {
	for _, frame := range []string{
		`{"choices":[{"delta":{"reasoning_content":"12345678"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"f","arguments":"12345678"}}]}}]}`,
	} {
		var begins, cancels atomic.Int64
		relay := newTestRelay(p6f11RelayDB(), &mockBilling{
			beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error { begins.Add(1); return nil },
			cancelFn:      func(context.Context, string, int64) error { cancels.Add(1); return nil },
		})
		ctx := contextWithStreamReservation(p6f11StreamContext(nil, "req-stream-cap"), 4, false)
		response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
		recorder := httptest.NewRecorder()
		relay.relayStreamRequest(ctx, recorder, response, &RouteResult{Channel: models.Channel{Name: "provider", Provider: "provider"}},
			"req-stream-cap", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 4, nil, "")
		if begins.Load() != 0 || cancels.Load() != 1 || strings.Contains(recorder.Body.String(), "12345678") || !strings.Contains(recorder.Body.String(), "stream_error.output_limit_exceeded") {
			t.Fatalf("hard cap did not withhold measured delta: begin=%d cancel=%d body=%s", begins.Load(), cancels.Load(), recorder.Body.String())
		}
	}
}
