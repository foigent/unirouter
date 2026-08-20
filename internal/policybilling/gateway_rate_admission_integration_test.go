//go:build integration

package policybilling

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/limitpolicy"
)

func TestGatewayRateDenialPublishesExactPolicyVersionHeader(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	runtime, err := limitpolicy.NewService(nil, client, limitpolicy.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	apiKey := gatewayRateTestAPIKey()
	apiKey.QPSLimit = 1
	billingService := New(&fakeProvider{}, runtime)
	handler := gatewayRateTestHandlerWithKey(t, billingService, apiKey, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"public-model","messages":[]}`)))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"private-topology-marker","messages":[]}`)))
	if second.Code != http.StatusTooManyRequests ||
		second.Header().Get("X-UniRouter-Limit-Policy-Version") == "" ||
		!strings.Contains(second.Body.String(), ErrorCodeRPSExceeded) {
		t.Fatalf("second status=%d headers=%v body=%s", second.Code, second.Header(), second.Body.String())
	}
	if strings.Contains(second.Body.String(), "private-topology-marker") {
		t.Fatalf("early denial leaked model identity: %s", second.Body.String())
	}
}

func TestInvalidModelAttemptIsRateLimitedBeforeValidationResponse(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	runtime, err := limitpolicy.NewService(nil, client, limitpolicy.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	apiKey := gatewayRateTestAPIKey()
	apiKey.QPSLimit = 1
	billingService := New(&fakeProvider{}, runtime)
	var downstreamCalls int
	handler := gatewayRateTestHandlerWithKey(t, billingService, apiKey, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamCalls++
		w.WriteHeader(http.StatusNoContent)
	}))

	const body = `{"private_marker":"missing-model"}`
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))

	if first.Code != http.StatusBadRequest || second.Code != http.StatusTooManyRequests || downstreamCalls != 0 ||
		!strings.Contains(second.Body.String(), ErrorCodeRPSExceeded) {
		t.Fatalf("first=%d second=%d downstream=%d first_body=%s second_body=%s",
			first.Code, second.Code, downstreamCalls, first.Body.String(), second.Body.String())
	}
	if strings.Contains(first.Body.String(), "private_marker") || strings.Contains(second.Body.String(), "private_marker") {
		t.Fatalf("invalid model response leaked body: first=%s second=%s", first.Body.String(), second.Body.String())
	}
}
