package gateway

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestEmbeddingsResponseInspectorAcceptsOnlyTheBoundedV1Envelope(t *testing.T) {
	expectation := embeddingsResponseExpectation{encodingFormat: embeddingsEncodingFloat, itemCount: 2, dimensions: 3}
	valid := decodeEmbeddingsResponseFixture(t, `{
		"object":"list",
		"data":[
			{"object":"embedding","index":0,"embedding":[0.1,-0.2,3]},
			{"object":"embedding","index":1,"embedding":[0,0.5,1e-6]}
		],
		"model":"text-embedding-v1",
		"usage":{"prompt_tokens":2,"total_tokens":2}
	}`)
	if inspection := inspectEmbeddingsResponseEnvelope(valid, expectation); !inspection.complete() {
		t.Fatalf("valid embeddings response failed at %s", inspection.failurePath)
	}

	oneFloat := embeddingsResponseExpectation{encodingFormat: embeddingsEncodingFloat, itemCount: 1, dimensions: 1}
	invalid := map[string]map[string]interface{}{
		"unknown root field":  decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","private":"hidden"}`),
		"unknown item field":  decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1],"private":"hidden"}],"model":"m"}`),
		"wrong data type":     decodeEmbeddingsResponseFixture(t, `{"object":"list","data":"not-an-array","model":"m"}`),
		"wrong root object":   decodeEmbeddingsResponseFixture(t, `{"object":"embedding","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m"}`),
		"wrong model type":    decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":7}`),
		"wrong usage type":    decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","usage":"one"}`),
		"unknown usage field": decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","usage":{"prompt_tokens":1,"private":1}}`),
		"negative usage":      decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"m","usage":{"prompt_tokens":-1}}`),
		"missing item object": decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"index":0,"embedding":[0.1]}],"model":"m"}`),
		"wrong item object":   decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"vector","index":0,"embedding":[0.1]}],"model":"m"}`),
		"missing index":       decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","embedding":[0.1]}],"model":"m"}`),
		"noninteger index":    decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0.5,"embedding":[0.1]}],"model":"m"}`),
		"noncontinuous index": decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":1,"embedding":[0.1]}],"model":"m"}`),
		"wrong vector type":   decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":"base64"}],"model":"m"}`),
		"nonnumeric vector":   decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":["0.1"]}],"model":"m"}`),
		"empty vector":        decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[]}],"model":"m"}`),
		"dimension mismatch":  decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]},{"object":"embedding","index":1,"embedding":[0.1,0.2]}],"model":"m"}`),
	}
	for name, response := range invalid {
		t.Run(name, func(t *testing.T) {
			if inspection := inspectEmbeddingsResponseEnvelope(response, oneFloat); inspection.complete() {
				t.Fatalf("unsupported embeddings response was accepted: %#v", response)
			}
		})
	}

	for _, number := range []json.Number{"NaN", "Infinity", "-Infinity", "1e9999"} {
		t.Run("unacceptable number "+number.String(), func(t *testing.T) {
			response := embeddingResponseWithVector([]interface{}{number})
			if inspection := inspectEmbeddingsResponseEnvelope(response, oneFloat); inspection.complete() {
				t.Fatalf("unacceptable embedding number %q was accepted", number)
			}
		})
	}

	oversized := make([]interface{}, maxEmbeddingVectorDimensions+1)
	if inspection := inspectEmbeddingsResponseEnvelope(embeddingResponseWithVector(oversized), oneFloat); inspection.complete() {
		t.Fatalf("embedding dimension above %d was accepted", maxEmbeddingVectorDimensions)
	}
}

func TestEmbeddingsResponseInspectorBindsRequestCountDimensionsAndEncoding(t *testing.T) {
	floatExpectation, err := embeddingsResponseExpectationFromRequest([]byte(`{"model":"m","input":["a","b"],"encoding_format":"float","dimensions":3}`))
	if err != nil {
		t.Fatalf("freeze float expectation: %v", err)
	}
	validFloat := decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]},{"object":"embedding","index":1,"embedding":[0.4,0.5,0.6]}],"model":"m"}`)
	if inspection := inspectEmbeddingsResponseEnvelope(validFloat, floatExpectation); !inspection.complete() {
		t.Fatalf("request-bound float response failed at %s", inspection.failurePath)
	}
	for name, response := range map[string]map[string]interface{}{
		"missing requested item":     decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"m"}`),
		"wrong requested dimensions": decodeEmbeddingsResponseFixture(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]},{"object":"embedding","index":1,"embedding":[0.3,0.4]}],"model":"m"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if inspection := inspectEmbeddingsResponseEnvelope(response, floatExpectation); inspection.complete() {
				t.Fatalf("request contract drift was accepted: %#v", response)
			}
		})
	}

	base64Expectation, err := embeddingsResponseExpectationFromRequest([]byte(`{"model":"m","input":["a","b"],"encoding_format":"base64","dimensions":3}`))
	if err != nil {
		t.Fatalf("freeze base64 expectation: %v", err)
	}
	validBase64 := map[string]interface{}{
		"object": "list", "model": "m",
		"data": []interface{}{
			map[string]interface{}{"object": "embedding", "index": json.Number("0"), "embedding": encodeBase64Embedding(0.1, -0.2, 0.3)},
			map[string]interface{}{"object": "embedding", "index": json.Number("1"), "embedding": encodeBase64Embedding(0.4, 0.5, 0.6)},
		},
	}
	if inspection := inspectEmbeddingsResponseEnvelope(validBase64, base64Expectation); !inspection.complete() {
		t.Fatalf("valid base64 response failed at %s", inspection.failurePath)
	}
	for name, embedding := range map[string]string{
		"malformed base64":       "not-base64",
		"non-float32 byte count": base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
		"non-finite float32":     encodeBase64Embedding(float32(math.NaN()), 0, 0),
		"wrong dimensions":       encodeBase64Embedding(0.1, 0.2),
	} {
		t.Run(name, func(t *testing.T) {
			response := map[string]interface{}{
				"object": "list", "model": "m",
				"data": []interface{}{
					map[string]interface{}{"object": "embedding", "index": json.Number("0"), "embedding": embedding},
					map[string]interface{}{"object": "embedding", "index": json.Number("1"), "embedding": encodeBase64Embedding(0.4, 0.5, 0.6)},
				},
			}
			if inspection := inspectEmbeddingsResponseEnvelope(response, base64Expectation); inspection.complete() {
				t.Fatalf("unsupported base64 response was accepted: %#v", response)
			}
		})
	}

	for _, request := range []string{
		`{"model":"m","input":"a","encoding_format":"hex"}`,
		`{"model":"m","input":"a","dimensions":0}`,
		`{"model":"m","input":[]}`,
	} {
		if _, err := embeddingsResponseExpectationFromRequest([]byte(request)); err == nil {
			t.Fatalf("unsupported request expectation was accepted: %s", request)
		}
	}
}

func TestEmbeddingsUnknownOutputIsHeldWithoutDeliveryOrCustomerSettlement(t *testing.T) {
	const marker = "provider-private-marker"
	responses := []struct {
		name string
		body string
	}{
		{name: "unknown root", body: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1},"private_output":"` + marker + `"}`},
		{name: "unknown item", body: `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1],"private_output":"` + marker + `"}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1}}`},
	}
	for _, response := range responses {
		t.Run(response.name, func(t *testing.T) {
			coordinator, relay, counters := newEmbeddingsResponseBoundaryFixture(t, response.body, nil, nil)
			recorder := httptest.NewRecorder()
			relay.HandleEmbeddings(recorder, executionRequest(http.MethodPost, "/v1/embeddings", "req-embeddings-private", `{"model":"public-model","input":"hello"}`))

			if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), marker) {
				t.Fatalf("unsupported field escaped: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if counters.holds.Load() != 1 || counters.holdReason != billing.OutputHoldReasonProviderResponseUnusable ||
				counters.acceptedCompletions.Load() != 1 || counters.unusableCompletions.Load() != 0 ||
				counters.prepares.Load() != 0 || counters.deliveries.Load() != 0 || counters.settles.Load() != 0 ||
				coordinator.providerCount.Load() != 1 || coordinator.successCount.Load() != 0 {
				t.Fatalf("unsupported embeddings terminal mismatch: holds=%d reason=%s accepted=%d unusable=%d prepares=%d deliveries=%d settles=%d provider_failure=%d success=%d",
					counters.holds.Load(), counters.holdReason, counters.acceptedCompletions.Load(), counters.unusableCompletions.Load(),
					counters.prepares.Load(), counters.deliveries.Load(), counters.settles.Load(), coordinator.providerCount.Load(), coordinator.successCount.Load())
			}
			if counters.held.UsagePurpose != metering.UsagePurposeProviderEvidence || counters.held.AttemptID == "" {
				t.Fatalf("hold lost provider evidence identity: %+v", counters.held)
			}
		})
	}
}

func TestEmbeddingsBase64OutputMatchesRequestAndDelivers(t *testing.T) {
	encoded := encodeBase64Embedding(0.1, -0.2, 0.3)
	coordinator, relay, counters := newEmbeddingsResponseBoundaryFixture(t,
		`{"object":"list","data":[{"object":"embedding","index":0,"embedding":"`+encoded+`"}],"model":"upstream-model","usage":{"prompt_tokens":1,"total_tokens":1}}`,
		nil, nil,
	)
	recorder := httptest.NewRecorder()
	relay.HandleEmbeddings(recorder, executionRequest(http.MethodPost, "/v1/embeddings", "req-embeddings-base64", `{"model":"public-model","input":"hello","encoding_format":"base64","dimensions":3}`))

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), encoded) {
		t.Fatalf("valid base64 response was not delivered: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if counters.holds.Load() != 0 || counters.prepares.Load() != 1 || counters.deliveries.Load() != 2 || counters.settles.Load() != 1 ||
		coordinator.successCount.Load() != 1 || coordinator.providerCount.Load() != 0 {
		t.Fatalf("base64 delivery mismatch: holds=%d prepares=%d deliveries=%d settles=%d success=%d provider_failure=%d",
			counters.holds.Load(), counters.prepares.Load(), counters.deliveries.Load(), counters.settles.Load(),
			coordinator.successCount.Load(), coordinator.providerCount.Load())
	}
}

func TestEmbeddingsUnsupportedOutputHealthFailureStillReplaysTheSameHold(t *testing.T) {
	const marker = "provider-private-marker"
	var replayed []billing.SettleRequest
	coordinator, relay, counters := newEmbeddingsResponseBoundaryFixture(t,
		`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":1,"total_tokens":1},"private_output":"`+marker+`"}`,
		func(modelexecution.Decision) (modelexecution.HealthResult, error) {
			return modelexecution.HealthResult{Status: modelexecution.Stale}, nil
		},
		func(req billing.SettleRequest, call int64) error {
			replayed = append(replayed, req)
			if call == 1 {
				return errors.New("commit outcome unavailable")
			}
			return nil
		},
	)
	recorder := httptest.NewRecorder()
	relay.HandleEmbeddings(recorder, executionRequest(http.MethodPost, "/v1/embeddings", "req-embeddings-health-stale", `{"model":"public-model","input":"hello"}`))

	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" || strings.Contains(recorder.Body.String(), marker) {
		t.Fatalf("health failure did not fail closed: status=%d retry=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	if counters.holds.Load() != 2 || len(replayed) != 2 || !reflect.DeepEqual(replayed[0], replayed[1]) ||
		counters.holdReason != billing.OutputHoldReasonProviderResponseUnusable || counters.prepares.Load() != 0 ||
		counters.deliveries.Load() != 0 || counters.settles.Load() != 0 || counters.unusableCompletions.Load() != 0 ||
		coordinator.providerCount.Load() != 1 || coordinator.successCount.Load() != 0 {
		t.Fatalf("health/hold replay mismatch: holds=%d replay=%d reason=%s prepares=%d deliveries=%d settles=%d unusable=%d provider_failure=%d success=%d",
			counters.holds.Load(), len(replayed), counters.holdReason, counters.prepares.Load(), counters.deliveries.Load(),
			counters.settles.Load(), counters.unusableCompletions.Load(), coordinator.providerCount.Load(), coordinator.successCount.Load())
	}
}

type embeddingsResponseBoundaryCounters struct {
	holds               atomic.Int64
	acceptedCompletions atomic.Int64
	unusableCompletions atomic.Int64
	prepares            atomic.Int64
	deliveries          atomic.Int64
	settles             atomic.Int64
	holdReason          string
	held                billing.SettleRequest
}

func newEmbeddingsResponseBoundaryFixture(
	t *testing.T,
	responseBody string,
	providerHealth func(modelexecution.Decision) (modelexecution.HealthResult, error),
	hold func(billing.SettleRequest, int64) error,
) (*executionCoordinatorStub, *Relay, *embeddingsResponseBoundaryCounters) {
	t.Helper()
	encKey := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDEmbeddings, "https://provider.test", encKey, nil)
	coordinator := &executionCoordinatorStub{
		providerFn: providerHealth,
		authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
			digest, err := executionevidence.Digest(route.Evidence)
			return modelexecution.Decision{
				Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
				DispatchOwnerToken: "embeddings-output-owner",
				ProviderAttemptKey: modelexecution.ProviderAttemptKey{
					OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
				},
			}, err
		},
	}
	counters := &embeddingsResponseBoundaryCounters{}
	billingStub := &mockBilling{
		completeAttemptFn: func(_ context.Context, completion billing.ProviderAttemptCompletion) error {
			switch completion.Outcome {
			case billing.ProviderAttemptOutcomeResponseAccepted:
				counters.acceptedCompletions.Add(1)
			case billing.ProviderAttemptOutcomeResponseUnusable:
				counters.unusableCompletions.Add(1)
			}
			return nil
		},
		holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
			call := counters.holds.Add(1)
			counters.held, counters.holdReason = req, reason
			if hold != nil {
				return hold(req, call)
			}
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			counters.prepares.Add(1)
			return &billing.PrepareDeliveryResult{Funded: true}, nil
		},
		recordDeliveryFn: func(context.Context, billing.DeliveryEvent) error {
			counters.deliveries.Add(1)
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			counters.settles.Add(1)
			return &billing.SettleResult{Success: true}, nil
		},
	}
	relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
	relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(responseBody)),
		}, nil
	})}
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()); err != nil {
		t.Fatalf("set execution runtime: %v", err)
	}
	return coordinator, relay, counters
}

func decodeEmbeddingsResponseFixture(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var response map[string]interface{}
	if err := decodeJSONUseNumber([]byte(body), &response); err != nil {
		t.Fatalf("decode embeddings response fixture: %v", err)
	}
	return response
}

func embeddingResponseWithVector(vector []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"object": "list",
		"model":  "m",
		"data": []interface{}{map[string]interface{}{
			"object": "embedding", "index": json.Number("0"), "embedding": vector,
		}},
	}
}

func encodeBase64Embedding(values ...float32) string {
	encoded := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(encoded[index*4:], math.Float32bits(value))
	}
	return base64.StdEncoding.EncodeToString(encoded)
}
