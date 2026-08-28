package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type publicHandlerCoordinator struct {
	*executionCoordinatorStub
	publicCalls atomic.Int64
}

func (coordinator *publicHandlerCoordinator) AuthorizePublic(
	_ context.Context,
	_ modelrouting.Plan,
	_ modelrouting.Route,
	identity modelexecution.AttemptIdentity,
) (modelexecution.Decision, error) {
	coordinator.publicCalls.Add(1)
	return modelexecution.Decision{
		Status: modelexecution.Applied,
		ProviderAttemptKey: modelexecution.ProviderAttemptKey{
			OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
		},
	}, nil
}

func TestHandlePublicNonStreamUsesNarrowRuntimeAndCanonicalDelivery(t *testing.T) {
	const requestedModel = "public-text"
	const resolvedModel = "catalog-text"
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), key)
	if err != nil {
		t.Fatal(err)
	}
	plan := publicRuntimePlan(t, 1)
	plan.PublicModel = resolvedModel
	plan.Routes[0].EncryptedSecret = encrypted
	planner := &executionPlannerStub{plan: plan}
	coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
	var freezeCalls, completionCalls, deliveryCalls, settleCalls atomic.Int64
	billingService := &mockBilling{
		freezeFn: func(_ context.Context, request billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezeCalls.Add(1)
			if request.Model != resolvedModel {
				t.Fatalf("freeze model=%q", request.Model)
			}
			return &billing.FreezeResult{Success: true}, nil
		},
		completeAttemptFn: func(_ context.Context, request billing.ProviderAttemptCompletion) error {
			completionCalls.Add(1)
			if request.Outcome != billing.ProviderAttemptOutcomeResponseAccepted {
				t.Fatalf("completion=%+v", request)
			}
			return nil
		},
		recordDeliveryFn: func(context.Context, billing.DeliveryEvent) error {
			deliveryCalls.Add(1)
			return nil
		},
		settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls.Add(1)
			if request.PromptTokens != 10 || request.CompletionTokens != 4 {
				t.Fatalf("settlement=%+v", request)
			}
			return &billing.SettleResult{Success: true}, nil
		},
	}
	relay := NewRelay(executionTestDB(), nil, nil, billingService, nil, key)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	service, err := guardrail.NewService(&testutil.MockDB{}, nil, []byte("public-handler-guardrail-secret"), guardrail.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	relay.SetGuardrail(service)
	var providerBody string
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		providerBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", "")))}, nil
	})

	recorder := httptest.NewRecorder()
	publicHandlerRequestModels(t, relay, modeloperation.IDResponses, requestedModel, resolvedModel, recorder)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"model":"`+requestedModel+`"`) ||
		strings.Contains(recorder.Body.String(), resolvedModel) ||
		strings.Contains(recorder.Body.String(), "provider-private-model") ||
		!strings.Contains(providerBody, `"model":"provider-private-model"`) || strings.Contains(providerBody, resolvedModel) {
		t.Fatalf("status=%d customer=%s provider=%s", recorder.Code, recorder.Body.String(), providerBody)
	}
	planner.mu.Lock()
	planRequests := append([]modelrouting.Request(nil), planner.requests...)
	planner.mu.Unlock()
	if len(planRequests) != 1 || planRequests[0].PublicModel != resolvedModel {
		t.Fatalf("planner requests=%+v", planRequests)
	}
	if coordinator.publicCalls.Load() != 1 || coordinator.authorizeCount.Load() != 0 ||
		freezeCalls.Load() != 1 || completionCalls.Load() != 1 || deliveryCalls.Load() != 2 || settleCalls.Load() != 1 {
		t.Fatalf("public=%d legacy=%d freeze=%d complete=%d delivery=%d settle=%d",
			coordinator.publicCalls.Load(), coordinator.authorizeCount.Load(), freezeCalls.Load(),
			completionCalls.Load(), deliveryCalls.Load(), settleCalls.Load())
	}
}

func TestHandlePublicNonStreamDispatchesExactResponsesV2StreamWithoutMappedModelDisclosure(t *testing.T) {
	const requestedModel = "public-text"
	const resolvedModel = "catalog-text"
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), key)
	if err != nil {
		t.Fatal(err)
	}
	plan := publicRuntimePlanForProfile(t, 1, "openai.gpt-5.6-sol.responses", "1.3.0")
	plan.PublicModel = resolvedModel
	plan.Routes[0].EncryptedSecret = encrypted
	planner := &executionPlannerStub{plan: plan}
	coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
	var begins, persists, finishes atomic.Int64
	billingService := &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Success: true}, nil
		},
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			begins.Add(1)
			return nil
		},
		persistUsageFn: func(_ context.Context, request billing.SettleRequest) error {
			persists.Add(1)
			if request.StreamFence == nil || request.UsageAuthority != "provider_reported" {
				t.Fatalf("usage=%+v", request)
			}
			return nil
		},
		finishStreamFn: func(_ context.Context, _ billing.StreamDeliveryStart, state, _ string) error {
			finishes.Add(1)
			if state != billing.StreamLeaseStateCompleted {
				t.Fatalf("state=%s", state)
			}
			return nil
		},
	}
	relay := NewRelay(executionTestDB(), nil, nil, billingService, nil, key)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	service, err := guardrail.NewService(&testutil.MockDB{}, nil, []byte("public-handler-stream-guardrail-secret"), guardrail.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	relay.SetGuardrail(service)
	var providerBody string
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		providerBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(publicResponsesSSE("safe", " answer"))),
		}, nil
	})

	recorder := httptest.NewRecorder()
	publicHandlerStreamRequest(t, relay, requestedModel, modelauthz.Decision{
		ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses,
		RequestedModel: requestedModel, ResolvedModel: resolvedModel, RuntimeMutation: "none",
	}, recorder)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" ||
		!strings.Contains(recorder.Body.String(), "event: response.completed") ||
		!strings.Contains(recorder.Body.String(), `"model":"`+requestedModel+`"`) ||
		strings.Contains(recorder.Body.String(), "provider-private-model") ||
		!strings.Contains(providerBody, `"stream":true`) || !strings.Contains(providerBody, `"model":"provider-private-model"`) ||
		begins.Load() != 1 || persists.Load() != 1 || finishes.Load() != 1 {
		t.Fatalf("status=%d headers=%v customer=%s provider=%s begin=%d persist=%d finish=%d",
			recorder.Code, recorder.Header(), recorder.Body.String(), providerBody, begins.Load(), persists.Load(), finishes.Load())
	}
}

func TestHandlePublicNonStreamDispatchesExactMessagesV2StreamWithoutMappedModelDisclosure(t *testing.T) {
	const requestedModel = "public-text"
	const resolvedModel = "catalog-text"
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), key)
	if err != nil {
		t.Fatal(err)
	}
	plan := publicRuntimePlanForProfileOperation(
		t, 1, "anthropic.claude-sonnet-5.api", "1.3.0", modeloperation.IDMessages,
	)
	plan.PublicModel = resolvedModel
	plan.Routes[0].EncryptedSecret = encrypted
	planner := &executionPlannerStub{plan: plan}
	coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
	var begins, persists, finishes atomic.Int64
	billingService := &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{Success: true}, nil
		},
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			begins.Add(1)
			return nil
		},
		persistUsageFn: func(_ context.Context, request billing.SettleRequest) error {
			persists.Add(1)
			if request.StreamFence == nil || request.UsageAuthority != "provider_reported" ||
				request.CacheUsageSchema != "anthropic.prompt_cache.v1" {
				t.Fatalf("usage=%+v", request)
			}
			return nil
		},
		finishStreamFn: func(_ context.Context, _ billing.StreamDeliveryStart, state, reason string) error {
			finishes.Add(1)
			if state != billing.StreamLeaseStateCompleted || reason != "message_completed" {
				t.Fatalf("state=%s reason=%s", state, reason)
			}
			return nil
		},
	}
	relay := NewRelay(executionTestDB(), nil, nil, billingService, nil, key)
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	service, err := guardrail.NewService(
		&testutil.MockDB{}, nil, []byte("public-handler-messages-stream-guardrail-secret"), guardrail.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	relay.SetGuardrail(service)
	var providerBody string
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		providerBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(publicAnthropicMessagesSSE("safe", " answer"))),
		}, nil
	})

	recorder := httptest.NewRecorder()
	publicHandlerOperationStreamRequest(t, relay, modeloperation.IDMessages, requestedModel, modelauthz.Decision{
		ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathMessages,
		RequestedModel: requestedModel, ResolvedModel: resolvedModel, RuntimeMutation: "none",
	}, `true`, recorder)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream" ||
		!strings.Contains(recorder.Body.String(), "event: message_stop") ||
		!strings.Contains(recorder.Body.String(), `"model":"`+requestedModel+`"`) ||
		strings.Contains(recorder.Body.String(), "provider-private-model") ||
		!strings.Contains(providerBody, `"stream":true`) ||
		!strings.Contains(providerBody, `"model":"provider-private-model"`) ||
		begins.Load() != 1 || persists.Load() != 1 || finishes.Load() != 1 {
		t.Fatalf("status=%d headers=%v customer=%s provider=%s begin=%d persist=%d finish=%d",
			recorder.Code, recorder.Header(), recorder.Body.String(), providerBody,
			begins.Load(), persists.Load(), finishes.Load())
	}
}

func TestHandlePublicNonStreamRejectsInvalidMessagesStreamTypeBeforePlanOrFreeze(t *testing.T) {
	planner := &executionPlannerStub{plan: publicRuntimePlanForProfileOperation(
		t, 1, "anthropic.claude-sonnet-5.api", "1.3.0", modeloperation.IDMessages,
	)}
	coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
	var freezes, providerCalls atomic.Int64
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezes.Add(1)
			return &billing.FreezeResult{Success: true}, nil
		},
	}, nil, []byte("0123456789abcdef0123456789abcdef"))
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, nil
	})
	recorder := httptest.NewRecorder()
	publicHandlerOperationStreamRequest(t, relay, modeloperation.IDMessages, "public-text", modelauthz.Decision{
		ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathMessages,
		RequestedModel: "public-text", ResolvedModel: "public-text", RuntimeMutation: "none",
	}, `"true"`, recorder)
	planner.mu.Lock()
	plans := len(planner.requests)
	planner.mu.Unlock()
	if recorder.Code != http.StatusBadRequest || plans != 0 || freezes.Load() != 0 || providerCalls.Load() != 0 ||
		!strings.Contains(recorder.Body.String(), `"type":"invalid_request_error"`) {
		t.Fatalf("status=%d plans=%d freeze=%d provider=%d body=%s",
			recorder.Code, plans, freezes.Load(), providerCalls.Load(), recorder.Body.String())
	}
}

func TestHandlePublicNonStreamPreservesHistoricalMessagesV1StreamRejection(t *testing.T) {
	planner := &executionPlannerStub{plan: publicRuntimePlanForProfileOperation(
		t, 1, "anthropic.claude-sonnet-5.api", "1.2.0", modeloperation.IDMessages,
	)}
	coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
	var freezes, providerCalls atomic.Int64
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
		freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
			freezes.Add(1)
			return &billing.FreezeResult{Success: true}, nil
		},
	}, nil, []byte("0123456789abcdef0123456789abcdef"))
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, nil
	})
	recorder := httptest.NewRecorder()
	publicHandlerOperationStreamRequest(t, relay, modeloperation.IDMessages, "public-text", modelauthz.Decision{
		ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathMessages,
		RequestedModel: "public-text", ResolvedModel: "public-text", RuntimeMutation: "none",
	}, `true`, recorder)
	if recorder.Code != http.StatusBadRequest || freezes.Load() != 0 || providerCalls.Load() != 0 ||
		coordinator.publicCalls.Load() != 0 ||
		!strings.Contains(recorder.Body.String(), `"type":"invalid_request_error"`) {
		t.Fatalf("status=%d freeze=%d provider=%d authorize=%d body=%s",
			recorder.Code, freezes.Load(), providerCalls.Load(), coordinator.publicCalls.Load(), recorder.Body.String())
	}
}

func TestHandlePublicNonStreamExactRuntimeFencePrecedesPlanFreezeAndProvider(t *testing.T) {
	planner := &executionPlannerStub{plan: publicRuntimePlan(t, 1)}
	legacyOnlyCoordinator := &executionCoordinatorStub{}
	var freezes, providerCalls atomic.Int64
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
		freezes.Add(1)
		return &billing.FreezeResult{Success: true}, nil
	}}, nil, []byte("0123456789abcdef0123456789abcdef"))
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, legacyOnlyCoordinator, routingpolicy.Default()); err != nil {
		t.Fatal(err)
	}
	relay.providerHTTPDoer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		return nil, nil
	})
	recorder := httptest.NewRecorder()
	publicHandlerRequest(t, relay, modeloperation.IDMessages, recorder)
	planner.mu.Lock()
	plans := len(planner.requests)
	planner.mu.Unlock()
	if recorder.Code != http.StatusServiceUnavailable || plans != 0 || freezes.Load() != 0 || providerCalls.Load() != 0 ||
		!strings.Contains(recorder.Body.String(), `"type":"error"`) ||
		!strings.Contains(recorder.Body.String(), `"request_id":"req-public-handler"`) {
		t.Fatalf("status=%d plans=%d freeze=%d provider=%d body=%s",
			recorder.Code, plans, freezes.Load(), providerCalls.Load(), recorder.Body.String())
	}
}

func TestHandlePublicNonStreamRequiresExactServerAuthorizedModelIdentity(t *testing.T) {
	tests := []struct {
		name     string
		decision modelauthz.Decision
	}{
		{name: "missing decision"},
		{name: "wrong contract", decision: modelauthz.Decision{ContractVersion: "wrong", Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: "catalog-text", RuntimeMutation: "none"}},
		{name: "denied", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: "catalog-text", RuntimeMutation: "none"}},
		{name: "wrong endpoint", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathMessages, RequestedModel: "public-text", ResolvedModel: "catalog-text", RuntimeMutation: "none"}},
		{name: "wrong requested model", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "other", ResolvedModel: "catalog-text", RuntimeMutation: "none"}},
		{name: "blank resolved model", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", RuntimeMutation: "none"}},
		{name: "non canonical resolved model", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: " catalog-text", RuntimeMutation: "none"}},
		{name: "resolved model too long", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: strings.Repeat("m", 161), RuntimeMutation: "none"}},
		{name: "resolved model control", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: "catalog\ntext", RuntimeMutation: "none"}},
		{name: "runtime mutation", decision: modelauthz.Decision{ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: modeloperation.PathResponses, RequestedModel: "public-text", ResolvedModel: "catalog-text", RuntimeMutation: "rewrite"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &executionPlannerStub{plan: publicRuntimePlan(t, 1)}
			coordinator := &publicHandlerCoordinator{executionCoordinatorStub: &executionCoordinatorStub{}}
			var freezes, providerCalls atomic.Int64
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
				freezes.Add(1)
				return &billing.FreezeResult{Success: true}, nil
			}}, nil, []byte("0123456789abcdef0123456789abcdef"))
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			relay.providerHTTPDoer = publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				providerCalls.Add(1)
				return nil, nil
			})

			recorder := httptest.NewRecorder()
			publicHandlerRequestWithDecision(t, relay, modeloperation.IDResponses, "public-text", test.decision, recorder)
			planner.mu.Lock()
			plans := len(planner.requests)
			planner.mu.Unlock()
			if recorder.Code != http.StatusServiceUnavailable || plans != 0 || freezes.Load() != 0 ||
				coordinator.publicCalls.Load() != 0 || providerCalls.Load() != 0 {
				t.Fatalf("status=%d plans=%d freeze=%d authorize=%d provider=%d body=%s",
					recorder.Code, plans, freezes.Load(), coordinator.publicCalls.Load(), providerCalls.Load(), recorder.Body.String())
			}
		})
	}
}

func publicHandlerRequest(t *testing.T, relay *Relay, operation modeloperation.ID, recorder http.ResponseWriter) {
	t.Helper()
	publicHandlerRequestModels(t, relay, operation, "public-text", "public-text", recorder)
}

func publicHandlerRequestModels(
	t *testing.T,
	relay *Relay,
	operation modeloperation.ID,
	requestedModel string,
	resolvedModel string,
	recorder http.ResponseWriter,
) {
	t.Helper()
	publicHandlerRequestWithDecision(t, relay, operation, requestedModel, modelauthz.Decision{
		ContractVersion: modelauthz.ContractVersion, Allowed: true,
		Endpoint: func() string {
			definition, _ := modeloperation.LookupID(operation)
			return definition.Path
		}(),
		RequestedModel: requestedModel, ResolvedModel: resolvedModel, RuntimeMutation: "none",
	}, recorder)
}

func publicHandlerRequestWithDecision(
	t *testing.T,
	relay *Relay,
	operation modeloperation.ID,
	requestedModel string,
	decision modelauthz.Decision,
	recorder http.ResponseWriter,
) {
	t.Helper()
	definition, known := modeloperation.LookupID(operation)
	if !known {
		t.Fatalf("unknown operation %s", operation)
	}
	body := `{"model":"` + requestedModel + `","input":"hello","max_output_tokens":64}`
	if operation == modeloperation.IDMessages {
		body = `{"model":"` + requestedModel + `","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`
	}
	request := httptest.NewRequest(http.MethodPost, definition.Path, strings.NewReader(body))
	apiKey := &models.ApiKey{ID: 13, OrgID: 11, Status: "active"}
	ctx := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(11))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(13))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-public-handler")
	ctx = context.WithValue(ctx, middleware.CtxModelAlias, requestedModel)
	if decision.ContractVersion != "" || decision.Allowed || decision.Endpoint != "" ||
		decision.RequestedModel != "" || decision.ResolvedModel != "" || decision.RuntimeMutation != "" {
		ctx = modelauthz.WithDecision(ctx, decision)
	}
	request = request.WithContext(ctx)
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	handler := payloadpolicy.RequestMiddlewareWithMax(budget, 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relay.HandlePublicNonStream(w, r, operation)
	}))
	handler.ServeHTTP(recorder, request)
}

func publicHandlerStreamRequest(
	t *testing.T,
	relay *Relay,
	requestedModel string,
	decision modelauthz.Decision,
	recorder http.ResponseWriter,
) {
	t.Helper()
	publicHandlerOperationStreamRequest(
		t, relay, modeloperation.IDResponses, requestedModel, decision, `true`, recorder,
	)
}

func publicHandlerOperationStreamRequest(
	t *testing.T,
	relay *Relay,
	operation modeloperation.ID,
	requestedModel string,
	decision modelauthz.Decision,
	streamValue string,
	recorder http.ResponseWriter,
) {
	t.Helper()
	definition, known := modeloperation.LookupID(operation)
	if !known {
		t.Fatalf("unknown operation %s", operation)
	}
	body := `{"model":"` + requestedModel + `","input":"hello","max_output_tokens":64,"stream":` + streamValue + `}`
	if operation == modeloperation.IDMessages {
		body = `{"model":"` + requestedModel + `","max_tokens":64,"stream":` + streamValue + `,"messages":[{"role":"user","content":"hello"}]}`
	}
	request := httptest.NewRequest(http.MethodPost, definition.Path, strings.NewReader(body))
	apiKey := &models.ApiKey{ID: 13, OrgID: 11, Status: "active"}
	ctx := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(11))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(13))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req-public-handler-stream")
	ctx = context.WithValue(ctx, middleware.CtxModelAlias, requestedModel)
	ctx = modelauthz.WithDecision(ctx, decision)
	request = request.WithContext(ctx)
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	handler := payloadpolicy.RequestMiddlewareWithMax(
		budget, 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relay.HandlePublicNonStream(w, r, operation)
	}))
	handler.ServeHTTP(recorder, request)
}
