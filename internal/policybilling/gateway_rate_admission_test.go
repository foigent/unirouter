package policybilling

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

type recordingAdmissionRuntime struct {
	reserveErr   error
	reserveErrAt map[int]error
	finalizeErr  error
	requests     []limitpolicy.AdmissionRequest
	finalizeCall int
}

type recordingEarlyRateEvidenceSink struct {
	events []EarlyRateAdmissionEvidence
}

func (s *recordingEarlyRateEvidenceSink) RecordEarlyRateAdmission(_ context.Context, evidence EarlyRateAdmissionEvidence) {
	s.events = append(s.events, evidence)
}

func (r *recordingAdmissionRuntime) Reserve(_ context.Context, request limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error) {
	r.requests = append(r.requests, request)
	if err := r.reserveErrAt[len(r.requests)]; err != nil {
		return nil, err
	}
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	return &limitpolicy.Reservation{OperationID: request.OperationID}, nil
}

func TestInvalidModelAttemptsStillConsumeUnscopedRate(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing model", body: `{"private_marker":"missing-model"}`},
		{name: "wrong model type", body: `{"model":42,"private_marker":"wrong-type"}`},
		{name: "non-object", body: `["private-non-object"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &recordingAdmissionRuntime{reserveErrAt: map[int]error{2: limitpolicy.ErrRPSExceeded}}
			service := New(&fakeProvider{}, runtime)
			var downstream atomic.Int64
			handler := gatewayRateTestHandler(t, service, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				downstream.Add(1)
			}))

			first := httptest.NewRecorder()
			handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(test.body)))
			second := httptest.NewRecorder()
			handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(test.body)))

			if first.Code != http.StatusBadRequest || second.Code != http.StatusTooManyRequests || downstream.Load() != 0 {
				t.Fatalf("first=%d second=%d downstream=%d first_body=%s second_body=%s",
					first.Code, second.Code, downstream.Load(), first.Body.String(), second.Body.String())
			}
			if len(runtime.requests) != 2 || runtime.finalizeCall != 1 {
				t.Fatalf("requests=%+v finalize=%d", runtime.requests, runtime.finalizeCall)
			}
			for _, request := range runtime.requests {
				if request.Phase != limitpolicy.AdmissionPhaseUnscopedRate || request.Subject.Model != "" {
					t.Fatalf("invalid attempt was not unscoped: %+v", request)
				}
			}
			if !strings.Contains(first.Body.String(), "validation_error.invalid_request") ||
				!strings.Contains(second.Body.String(), ErrorCodeRPSExceeded) ||
				strings.Contains(first.Body.String(), "private") || strings.Contains(second.Body.String(), "private") {
				t.Fatalf("unstable or leaking responses: first=%s second=%s", first.Body.String(), second.Body.String())
			}
		})
	}
}

func (*recordingAdmissionRuntime) Extend(context.Context, *limitpolicy.Reservation, int, int64) error {
	return nil
}

func (r *recordingAdmissionRuntime) Finalize(context.Context, *limitpolicy.Reservation, int64) error {
	return nil
}

func (r *recordingAdmissionRuntime) FinalizeRateAdmission(context.Context, *limitpolicy.Reservation, string) (limitpolicy.RateAdmissionReceipt, error) {
	r.finalizeCall++
	return limitpolicy.RateAdmissionReceipt{}, r.finalizeErr
}

func (*recordingAdmissionRuntime) Cancel(context.Context, *limitpolicy.Reservation) error { return nil }

func (*recordingAdmissionRuntime) CancelBeforeDispatch(context.Context, *limitpolicy.Reservation) error {
	return nil
}

func TestGatewayRateAdmissionDenialStopsBeforeDownstream(t *testing.T) {
	runtime := &recordingAdmissionRuntime{reserveErr: limitpolicy.ErrRPSExceeded}
	service := New(&fakeProvider{}, runtime)
	evidence := &recordingEarlyRateEvidenceSink{}
	service.SetEarlyRateAdmissionEvidenceSink(evidence)
	var downstream atomic.Int64
	handler := gatewayRateTestHandler(t, service, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		downstream.Add(1)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"private-model-marker","messages":[]}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests || downstream.Load() != 0 {
		t.Fatalf("status=%d downstream=%d body=%s", recorder.Code, downstream.Load(), recorder.Body.String())
	}
	if len(runtime.requests) != 1 || runtime.requests[0].Phase != limitpolicy.AdmissionPhaseUnscopedRate ||
		runtime.requests[0].Subject.Model != "" || runtime.requests[0].RequestedModel != "private-model-marker" ||
		runtime.finalizeCall != 0 {
		t.Fatalf("runtime requests=%+v finalize=%d", runtime.requests, runtime.finalizeCall)
	}
	if !strings.Contains(recorder.Body.String(), ErrorCodeRPSExceeded) ||
		strings.Contains(recorder.Body.String(), "private-model-marker") {
		t.Fatalf("unstable or leaking denial: %s", recorder.Body.String())
	}
	if len(evidence.events) != 1 || evidence.events[0].Result != "denied" ||
		evidence.events[0].ErrorCode != ErrorCodeRPSExceeded ||
		evidence.events[0].OperationID == "" || evidence.events[0].OrgID != 7 || evidence.events[0].APIKeyID != 13 {
		t.Fatalf("safe early evidence=%+v", evidence.events)
	}
}

func TestGatewayRateAdmissionFinalizeFailureIsFailClosed(t *testing.T) {
	runtime := &recordingAdmissionRuntime{finalizeErr: errors.New("terminal state unavailable")}
	service := New(&fakeProvider{}, runtime)
	var downstream atomic.Int64
	handler := gatewayRateTestHandler(t, service, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		downstream.Add(1)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings",
		strings.NewReader(`{"model":"embedding-public","input":"hello"}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable || downstream.Load() != 0 || runtime.finalizeCall != 1 {
		t.Fatalf("status=%d downstream=%d finalize=%d body=%s", recorder.Code, downstream.Load(), runtime.finalizeCall, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), ErrorCodeStateUnavailable) {
		t.Fatalf("missing stable state error: %s", recorder.Body.String())
	}
}

func TestGatewayRateAdmissionSplitsRateFromModelExecution(t *testing.T) {
	runtime := &recordingAdmissionRuntime{}
	provider := &fakeProvider{}
	service := New(provider, runtime)
	apiKey := gatewayRateTestAPIKey()
	var mainOperationID string
	handler := gatewayRateTestHandlerWithKey(t, service, apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainOperationID = middleware.GetRequestIdentityFromContext(r.Context()).OperationID
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxModelAlias, "public-model-alias"))
		if phase, requestedModel, _, valid := admissionPhaseFromContext(r.Context(), apiKey, mainOperationID); !valid || phase != limitpolicy.AdmissionPhaseModelExecution || requestedModel != "public-model-alias" {
			t.Fatal("trusted early-rate marker is missing")
		}
		result, err := service.Freeze(r.Context(), billing.FreezeRequest{
			RequestID: mainOperationID, OrgID: apiKey.OrgID, Model: "resolved-provider-model",
			EstimatedInputTokens: 40, ReservedCompletionTokens: 60, EstimatedTokens: 100,
		})
		if err != nil || result == nil || !result.Success {
			t.Fatalf("freeze=%+v err=%v", result, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"  public-model-alias  ","messages":[]}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent || len(runtime.requests) != 2 || provider.freezeCalls != 1 {
		t.Fatalf("status=%d requests=%+v wallet=%d body=%s", recorder.Code, runtime.requests, provider.freezeCalls, recorder.Body.String())
	}
	rateRequest, executionRequest := runtime.requests[0], runtime.requests[1]
	expectedRateOperationID, valid := limitpolicy.RateAdmissionOperationID(mainOperationID)
	if !valid {
		t.Fatalf("invalid main operation ID %q", mainOperationID)
	}
	if rateRequest.Phase != limitpolicy.AdmissionPhaseUnscopedRate ||
		rateRequest.OperationID != expectedRateOperationID ||
		rateRequest.Subject.Model != "" || rateRequest.RequestedModel != "public-model-alias" {
		t.Fatalf("rate request=%+v", rateRequest)
	}
	if executionRequest.Phase != limitpolicy.AdmissionPhaseModelExecution ||
		executionRequest.OperationID != mainOperationID ||
		executionRequest.Subject.Model != "resolved-provider-model" ||
		executionRequest.RequestedModel != "public-model-alias" ||
		executionRequest.EstimatedInput != 40 || executionRequest.ReservedOutput != 60 {
		t.Fatalf("execution request=%+v", executionRequest)
	}
	if runtime.finalizeCall != 1 {
		t.Fatalf("early rate finalize calls=%d, want 1", runtime.finalizeCall)
	}
}

func TestOrdinaryContextValueCannotForgeEarlyRateAdmission(t *testing.T) {
	runtime := &recordingAdmissionRuntime{}
	service := New(&fakeProvider{}, runtime)
	apiKey := gatewayRateTestAPIKey()
	const operationID = "op_direct_internal_call"
	ctx := gatewayRateSubjectContext(context.Background(), apiKey)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, operationID)
	ctx = context.WithValue(ctx, "early_rate_admission", map[string]any{
		"operation_id": operationID, "org_id": apiKey.OrgID, "api_key_id": apiKey.ID,
	})

	result, err := service.Freeze(ctx, billing.FreezeRequest{
		RequestID: operationID, OrgID: apiKey.OrgID, Model: "direct-model",
		EstimatedInputTokens: 1, ReservedCompletionTokens: 1, EstimatedTokens: 2,
	})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("freeze=%+v err=%v", result, err)
	}
	if len(runtime.requests) != 1 || runtime.requests[0].Phase != limitpolicy.AdmissionPhaseFull {
		t.Fatalf("ordinary context forged phase: %+v", runtime.requests)
	}
}

func TestEarlyRateMarkerIsBoundToSubjectAndOperation(t *testing.T) {
	t.Parallel()
	apiKey := gatewayRateTestAPIKey()
	const operationID = "op_marker_binding"
	ctx := gatewayRateSubjectContext(context.Background(), apiKey)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, operationID)
	ctx = context.WithValue(ctx, earlyRateAdmissionContextKey{}, earlyRateAdmissionMarker{
		operationID: operationID, requestedModel: "public-model", orgID: apiKey.OrgID, apiKeyID: apiKey.ID,
	})
	ctx = context.WithValue(ctx, middleware.CtxModelAlias, "public-model")
	if phase, requestedModel, _, valid := admissionPhaseFromContext(ctx, apiKey, operationID); !valid || phase != limitpolicy.AdmissionPhaseModelExecution || requestedModel != "public-model" {
		t.Fatal("exact marker binding was not accepted")
	}
	otherRequestedModel := context.WithValue(ctx, middleware.CtxModelAlias, "other-public-model")
	if _, _, _, valid := admissionPhaseFromContext(otherRequestedModel, apiKey, operationID); valid {
		t.Fatal("marker crossed authorized requested-model identity")
	}
	if _, _, _, valid := admissionPhaseFromContext(ctx, apiKey, operationID+"-other"); valid {
		t.Fatal("marker crossed operation identity")
	}
	otherKey := *apiKey
	otherKey.ID++
	if _, _, _, valid := admissionPhaseFromContext(ctx, &otherKey, operationID); valid {
		t.Fatal("marker crossed API-key identity")
	}
	otherOrg := *apiKey
	otherOrg.OrgID++
	if _, _, _, valid := admissionPhaseFromContext(ctx, &otherOrg, operationID); valid {
		t.Fatal("marker crossed organization identity")
	}
}

func TestMismatchedTrustedMarkerFailsClosedWithoutSecondReservation(t *testing.T) {
	t.Parallel()
	runtime := &recordingAdmissionRuntime{}
	service := New(&fakeProvider{}, runtime)
	apiKey := gatewayRateTestAPIKey()
	const operationID = "op_expected_binding"
	ctx := gatewayRateSubjectContext(context.Background(), apiKey)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, operationID)
	ctx = context.WithValue(ctx, earlyRateAdmissionContextKey{}, earlyRateAdmissionMarker{
		operationID: operationID + "-wrong", requestedModel: "direct-model", orgID: apiKey.OrgID, apiKeyID: apiKey.ID,
	})
	ctx = context.WithValue(ctx, middleware.CtxModelAlias, "direct-model")

	result, err := service.Freeze(ctx, billing.FreezeRequest{
		RequestID: operationID, OrgID: apiKey.OrgID, Model: "direct-model",
		EstimatedInputTokens: 1, ReservedCompletionTokens: 1, EstimatedTokens: 2,
	})
	if err != nil || result == nil || result.Success || result.ErrorCode != ErrorCodeStateUnavailable {
		t.Fatalf("freeze=%+v err=%v", result, err)
	}
	if len(runtime.requests) != 0 {
		t.Fatalf("mismatched marker reached runtime: %+v", runtime.requests)
	}
}

func gatewayRateTestHandler(t *testing.T, service *Billing, next http.Handler) http.Handler {
	t.Helper()
	return gatewayRateTestHandlerWithKey(t, service, gatewayRateTestAPIKey(), next)
}

func gatewayRateTestHandlerWithKey(t *testing.T, service *Billing, apiKey *models.ApiKey, next http.Handler) http.Handler {
	t.Helper()
	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	retain := payloadpolicy.RequestMiddleware(budget, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)
	admitted := middleware.StrictJSONIngress(service.GatewayRateAdmission(next))
	withSubject := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := gatewayRateSubjectContext(r.Context(), apiKey)
		retain(admitted).ServeHTTP(w, r.WithContext(ctx))
	})
	return middleware.OperationIdentity(withSubject)
}

func gatewayRateSubjectContext(ctx context.Context, apiKey *models.ApiKey) context.Context {
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, apiKey)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, apiKey.OrgID)
	ctx = context.WithValue(ctx, middleware.CtxProjectID, *apiKey.ProjectID)
	ctx = context.WithValue(ctx, middleware.CtxKeyID, apiKey.ID)
	return ctx
}

func gatewayRateTestAPIKey() *models.ApiKey {
	projectID := int64(11)
	return &models.ApiKey{ID: 13, OrgID: 7, ProjectID: &projectID, QPSLimit: 10, RPMLimit: 60}
}
