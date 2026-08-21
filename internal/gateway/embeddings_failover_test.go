package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

const embeddingsTestModel = "text-embedding-3-small"

func embeddingsRouteRow(id int64, name, provider, baseURL, encryptedKey, adapterType string, weight int) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*int64) = id
		*dest[1].(*string) = name
		*dest[2].(*string) = provider
		*dest[3].(*string) = baseURL
		*dest[4].(*string) = embeddingsTestModel
		*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
		*dest[6].(*string) = adapterType
		*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
		*dest[8].(*int) = 8192
		*dest[9].(*int) = 0
		*dest[10].(*int) = weight
		*dest[11].(*string) = "active"
		*dest[12].(*string) = "healthy"
		*dest[13].(**time.Time) = nil
		*dest[14].(*int) = 0
		*dest[15].(*int64) = id
		*dest[16].(*string) = encryptedKey
		*dest[17].(*string) = "active"
		*dest[18].(**time.Time) = nil
		return nil
	}
}

func embeddingsSuccessBody() string {
	return `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`
}

func embeddingsRequest(requestID string) *http.Request {
	body := `{"model":"text-embedding-3-small","input":"hello world"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	ctx = context.WithValue(ctx, middleware.CtxAPIKey, &models.ApiKey{ID: 1, OrgID: 1, Status: "active"})
	return req.WithContext(ctx)
}

func TestHandleEmbeddings_RetryClassificationAndFailover(t *testing.T) {
	tests := []struct {
		name             string
		firstStatus      int
		expectedStatus   int
		expectedRetry    bool
		expectedDecision string
	}{
		{name: "client 400 stops", firstStatus: http.StatusBadRequest, expectedStatus: http.StatusBadRequest, expectedDecision: "stop"},
		{name: "provider auth 401 stops safely", firstStatus: http.StatusUnauthorized, expectedStatus: http.StatusBadGateway, expectedDecision: "stop"},
		{name: "rate limit 429 fails over", firstStatus: http.StatusTooManyRequests, expectedStatus: http.StatusOK, expectedRetry: true, expectedDecision: "retry"},
		{name: "transient 503 fails over", firstStatus: http.StatusServiceUnavailable, expectedStatus: http.StatusOK, expectedRetry: true, expectedDecision: "retry"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const secretMarker = "embeddings-provider-secret"
			firstCalls := 0
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls++
				w.Header().Set("X-Request-ID", "upstream-embeddings-first")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.firstStatus)
				_, _ = w.Write([]byte(`{"error":{"message":"` + secretMarker + `"}}`))
			}))
			defer first.Close()

			secondCalls := 0
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondCalls++
				w.Header().Set("X-Request-ID", "upstream-embeddings-second")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(embeddingsSuccessBody()))
			}))
			defer second.Close()

			encKey := []byte("0123456789abcdef01234567")
			encryptedKey, err := auth.EncryptKey([]byte("sk-embeddings"), encKey)
			if err != nil {
				t.Fatalf("encrypt key: %v", err)
			}

			var rejectedMetadata map[string]interface{}
			var callArgs []any
			db := &testutil.MockDB{
				QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &testutil.MockRows{Rows: []func(dest ...any) error{
						embeddingsRouteRow(1, "embedding-primary", "openai", first.URL, encryptedKey, adapters.TypeOpenAICompatible, 1),
						embeddingsRouteRow(2, "embedding-secondary", "azure", second.URL, encryptedKey, adapters.TypeOpenAICompatible, 2),
					}}, nil
				},
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int) = 1
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					switch {
					case strings.Contains(sql, "INSERT INTO request_trace_events") && len(args) == traceInsertArgCount && args[8] == "provider.response_rejected":
						if err := json.Unmarshal([]byte(args[17].(string)), &rejectedMetadata); err != nil {
							t.Fatalf("decode rejected metadata: %v", err)
						}
					case strings.Contains(sql, "INSERT INTO call_logs"):
						callArgs = append([]any(nil), args...)
					}
					return pgconn.CommandTag{}, nil
				},
			}

			freezeCalls := 0
			settleCalls := 0
			cancelCalls := 0
			b := &mockBilling{
				freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezeCalls++
					return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
				},
				settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settleCalls++
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				},
				cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
					cancelCalls++
					return nil
				},
			}
			relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
			installLegacyHandlerTestRuntime(relay)
			rec := httptest.NewRecorder()

			relay.HandleEmbeddings(rec, embeddingsRequest("req-embeddings-retry"))

			if rec.Code != test.expectedStatus {
				t.Fatalf("expected %d, got %d: %s", test.expectedStatus, rec.Code, rec.Body.String())
			}
			if firstCalls != 1 || secondCalls != boolInt(test.expectedRetry) {
				t.Fatalf("unexpected route attempts: first=%d second=%d retry=%t", firstCalls, secondCalls, test.expectedRetry)
			}
			if freezeCalls != 1 {
				t.Fatalf("expected one Freeze, got %d", freezeCalls)
			}
			if test.expectedRetry {
				if settleCalls != 1 || cancelCalls != 0 {
					t.Fatalf("retry-success terminal invariant failed: settle=%d cancel=%d", settleCalls, cancelCalls)
				}
				if len(callArgs) != callLogInsertArgCount || callArgs[callLogArgProvider] != "azure" || callArgs[callLogArgStatusCode] != http.StatusOK {
					t.Fatalf("success log does not identify the fallback route: %#v", callArgs)
				}
				var body map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				extension := body["_uniroute"].(map[string]interface{})
				if extension["fallback"] != true || extension["attempt_count"] != float64(2) || extension["route_count"] != float64(2) {
					t.Fatalf("unexpected fallback evidence: %#v", extension)
				}
			} else {
				if settleCalls != 0 || cancelCalls != 1 {
					t.Fatalf("terminal failure invariant failed: settle=%d cancel=%d", settleCalls, cancelCalls)
				}
				if len(callArgs) != callLogInsertArgCount || callArgs[callLogArgProvider] != "openai" || callArgs[callLogArgStatusCode] != test.firstStatus {
					t.Fatalf("terminal failure log is not the first route: %#v", callArgs)
				}
				if strings.Contains(rec.Body.String(), secretMarker) {
					t.Fatalf("provider error leaked downstream: %s", rec.Body.String())
				}
			}
			if rejectedMetadata["retryable"] != test.expectedRetry || rejectedMetadata["retry_decision"] != test.expectedDecision {
				t.Fatalf("unexpected retry trace metadata: %#v", rejectedMetadata)
			}
		})
	}
}

func TestHandleEmbeddings_AllRoutesFailBounded(t *testing.T) {
	statuses := []int{http.StatusServiceUnavailable, http.StatusTooManyRequests}
	servers := make([]*httptest.Server, 0, len(statuses))
	calls := make([]int, len(statuses))
	for index, status := range statuses {
		index, status := index, status
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[index]++
			w.Header().Set("X-Request-ID", "upstream-bounded-"+string(rune('1'+index)))
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"bounded-secret"}}`))
		}))
		servers = append(servers, server)
		defer server.Close()
	}

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, _ := auth.EncryptKey([]byte("sk-embeddings"), encKey)
	var callArgs []any
	var retryDecisions []string
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				embeddingsRouteRow(1, "embedding-primary", "openai", servers[0].URL, encryptedKey, adapters.TypeOpenAICompatible, 1),
				embeddingsRouteRow(2, "embedding-secondary", "azure", servers[1].URL, encryptedKey, adapters.TypeOpenAICompatible, 2),
			}}, nil
		},
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO request_trace_events") && len(args) == traceInsertArgCount && args[8] == "provider.response_rejected":
				var metadata map[string]interface{}
				if err := json.Unmarshal([]byte(args[17].(string)), &metadata); err != nil {
					t.Fatalf("decode rejected metadata: %v", err)
				}
				retryDecisions = append(retryDecisions, metadata["retry_decision"].(string))
			case strings.Contains(sql, "INSERT INTO call_logs"):
				callArgs = append([]any(nil), args...)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	cancelCalls := 0
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
	}
	relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
	installLegacyHandlerTestRuntime(relay)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, embeddingsRequest("req-embeddings-bounded"))

	if rec.Code != http.StatusBadGateway || calls[0] != 1 || calls[1] != 1 || cancelCalls != 1 {
		t.Fatalf("bounded exhaustion failed: status=%d calls=%v cancel=%d body=%s", rec.Code, calls, cancelCalls, rec.Body.String())
	}
	if len(callArgs) != callLogInsertArgCount || callArgs[callLogArgProvider] != "azure" || callArgs[callLogArgStatusCode] != http.StatusTooManyRequests || callArgs[callLogArgUpstreamRequestID] != "upstream-bounded-2" {
		t.Fatalf("final failure log does not identify the final attempt: %#v", callArgs)
	}
	if strings.Contains(rec.Body.String(), "bounded-secret") {
		t.Fatalf("provider error leaked downstream: %s", rec.Body.String())
	}
	if len(retryDecisions) != 2 || retryDecisions[0] != "retry" || retryDecisions[1] != "stop" {
		t.Fatalf("expected actual retry then exhausted stop decisions, got %#v", retryDecisions)
	}
}

func TestP6F40BHandleEmbeddingsSkipsUnsupportedAdapterButHoldsAmbiguousTransport(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(embeddingsSuccessBody()))
	}))
	defer second.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, _ := auth.EncryptKey([]byte("sk-embeddings"), encKey)
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				embeddingsRouteRow(1, "unsupported", "anthropic", "https://example.invalid", encryptedKey, adapters.TypeAnthropicNative, 1),
				embeddingsRouteRow(2, "dead", "openai", deadURL, encryptedKey, adapters.TypeOpenAICompatible, 2),
				embeddingsRouteRow(3, "healthy", "azure", second.URL, encryptedKey, adapters.TypeOpenAICompatible, 3),
			}}, nil
		},
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	settleCalls := 0
	var completions []billing.ProviderAttemptCompletion
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
		completeAttemptFn: func(ctx context.Context, req billing.ProviderAttemptCompletion) error {
			completions = append(completions, req)
			return nil
		},
	}
	relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
	installLegacyHandlerTestRuntime(relay)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, embeddingsRequest("req-embeddings-transport"))

	if rec.Code != http.StatusBadGateway || secondCalls != 0 || settleCalls != 0 {
		t.Fatalf("ambiguous transport must stop before a second paid dispatch: status=%d second=%d settle=%d body=%s", rec.Code, secondCalls, settleCalls, rec.Body.String())
	}
	if len(completions) != 1 || completions[0].Outcome != billing.ProviderAttemptOutcomeUnknown || completions[0].RetryAuthorized {
		t.Fatalf("expected one financially ambiguous terminal attempt, got %#v", completions)
	}
}

func TestP6F40BHandleEmbeddingsMalformedSuccessRetainsProviderLiability(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer upstream.Close()

	encKey := []byte("0123456789abcdef01234567")
	encryptedKey, _ := auth.EncryptKey([]byte("sk-embeddings"), encKey)
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				embeddingsRouteRow(1, "malformed", "openai", upstream.URL, encryptedKey, adapters.TypeOpenAICompatible, 1),
			}}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	settleCalls := 0
	cancelCalls := 0
	var completions []billing.ProviderAttemptCompletion
	b := &mockBilling{
		freezeFn: func(ctx context.Context, req billing.FreezeRequest) (*billing.FreezeResult, error) {
			return &billing.FreezeResult{FreezeID: 1, AmountMicros: 100, Success: true}, nil
		},
		settleFn: func(ctx context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{SettleID: 1, Success: true}, nil
		},
		cancelFn: func(ctx context.Context, requestID string, orgID int64) error {
			cancelCalls++
			return nil
		},
		completeAttemptFn: func(ctx context.Context, req billing.ProviderAttemptCompletion) error {
			completions = append(completions, req)
			return nil
		},
	}
	relay := NewRelay(db, nil, NewRouter(db), b, nil, encKey)
	installLegacyHandlerTestRuntime(relay)
	rec := httptest.NewRecorder()

	relay.HandleEmbeddings(rec, embeddingsRequest("req-embeddings-malformed"))

	if rec.Code != http.StatusInternalServerError || settleCalls != 0 || cancelCalls != 0 {
		t.Fatalf("malformed success invariant failed: status=%d settle=%d cancel=%d body=%s", rec.Code, settleCalls, cancelCalls, rec.Body.String())
	}
	if len(completions) != 2 || completions[0].Outcome != billing.ProviderAttemptOutcomeResponseAccepted || completions[1].Outcome != billing.ProviderAttemptOutcomeResponseUnusable {
		t.Fatalf("malformed 200 must retain provider liability, got %#v", completions)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
