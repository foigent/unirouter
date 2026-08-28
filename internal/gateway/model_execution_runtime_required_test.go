package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestChatAndEmbeddingsFailClosedWhenModelExecutionRuntimeIsMissing(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		handle func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{
			name: "chat", path: "/v1/chat/completions",
			body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`,
			handle: func(relay *Relay, writer http.ResponseWriter, request *http.Request) {
				relay.HandleChatCompletion(writer, request)
			},
		},
		{
			name: "embeddings", path: "/v1/embeddings",
			body: `{"model":"public-model","input":"hello"}`,
			handle: func(relay *Relay, writer http.ResponseWriter, request *http.Request) {
				relay.HandleEmbeddings(writer, request)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			databaseCalls := 0
			freezeCalls := 0
			providerCalls := 0
			database := &testutil.MockDB{
				QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
					databaseCalls++
					return &testutil.MockRows{}, nil
				},
				QueryRowFn: func(context.Context, string, ...any) pgx.Row {
					databaseCalls++
					return &testutil.MockRow{}
				},
				ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					databaseCalls++
					return pgconn.CommandTag{}, nil
				},
			}
			relay := NewRelay(database, nil, NewRouter(database), &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezeCalls++
					return &billing.FreezeResult{Success: true}, nil
				},
			}, nil, []byte("invalid-secret-key"))
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				providerCalls++
				return &http.Response{StatusCode: http.StatusOK}, nil
			})}

			writer := httptest.NewRecorder()
			test.handle(relay, writer, executionRequest(http.MethodPost, test.path, "req-runtime-missing-"+test.name, test.body))

			if writer.Code != http.StatusServiceUnavailable || writer.Header().Get("Retry-After") != "1" {
				t.Fatalf("missing runtime response = status %d retry-after %q body %s", writer.Code, writer.Header().Get("Retry-After"), writer.Body.String())
			}
			if !strings.Contains(writer.Body.String(), `"code":"server_error.service_unavailable"`) {
				t.Fatalf("missing runtime response did not use the bounded public error: %s", writer.Body.String())
			}
			if databaseCalls != 0 || freezeCalls != 0 || providerCalls != 0 {
				t.Fatalf("missing runtime crossed a business boundary: db=%d freeze=%d provider=%d", databaseCalls, freezeCalls, providerCalls)
			}
		})
	}
}

func TestChatAndEmbeddingsHaveNoLegacyRouterFallbackSource(t *testing.T) {
	source, err := os.ReadFile("relay.go")
	if err != nil {
		t.Fatalf("read relay source: %v", err)
	}
	laterByHandler := map[string][]string{
		"HandleChatCompletion": {"pinGuardrail", "prepareModelExecutionPlan", "materializeModelExecutionRoutes", "billing.Freeze", "client.Do"},
		"HandleEmbeddings":     {"pinGuardrail", "prepareModelExecutionRoutes", "billing.Freeze", "client.Do"},
	}
	for _, name := range []string{"HandleChatCompletion", "HandleEmbeddings"} {
		body := relayMethodSource(t, string(source), name)
		guard := strings.Index(body, "requireModelExecutionRuntime")
		if guard < 0 {
			t.Fatalf("%s has no model execution runtime guard", name)
		}
		for _, forbidden := range []string{
			"RouteAllWithKey", "executionBatch == nil", "executionBatch != nil",
			"router.PrepareRoute", "router.MaintainProbeLease", "contextWithDurableLegacyProviderAttempt",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s retained legacy routing source %q", name, forbidden)
			}
		}
		for _, later := range laterByHandler[name] {
			position := strings.Index(body, later)
			if position < 0 || position < guard {
				t.Fatalf("%s runtime guard does not precede %q", name, later)
			}
		}
	}
	runtimeSource, err := os.ReadFile("model_execution_runtime.go")
	if err != nil {
		t.Fatalf("read model execution runtime source: %v", err)
	}
	for _, forbidden := range []string{"relay.router", "compatibilityRoutes"} {
		if strings.Contains(string(runtimeSource), forbidden) {
			t.Fatalf("model execution runtime retained legacy Router dependency %q", forbidden)
		}
	}
}

func relayMethodSource(t *testing.T, source, name string) string {
	t.Helper()
	marker := "func (relay *Relay) " + name + "("
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("relay method %s not found", name)
	}
	remainder := source[start+len(marker):]
	end := strings.Index(remainder, "\nfunc ")
	if end < 0 {
		return remainder
	}
	return remainder[:end]
}
