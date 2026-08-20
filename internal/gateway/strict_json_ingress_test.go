package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestStrictJSONIngressRejectsChatAndEmbeddingsBeforeBusinessTouch(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		profile string
		body    string
		handle  func(*Relay, http.ResponseWriter, *http.Request)
	}{
		{
			name: "chat root model", path: "/v1/chat/completions", profile: payloadpolicy.ProfileChatRequest,
			body:   `{"model":"gpt-one","model":"private-model-marker","messages":[]}`,
			handle: func(relay *Relay, w http.ResponseWriter, r *http.Request) { relay.HandleChatCompletion(w, r) },
		},
		{
			name: "chat message", path: "/v1/chat/completions", profile: payloadpolicy.ProfileChatRequest,
			body:   `{"model":"gpt","messages":[{"role":"user","content":"private-content-marker","content":"other"}]}`,
			handle: func(relay *Relay, w http.ResponseWriter, r *http.Request) { relay.HandleChatCompletion(w, r) },
		},
		{
			name: "chat content part", path: "/v1/chat/completions", profile: payloadpolicy.ProfileChatRequest,
			body:   `{"model":"gpt","messages":[{"role":"user","content":[{"type":"text","text":"private-part-marker","text":"other"}]}]}`,
			handle: func(relay *Relay, w http.ResponseWriter, r *http.Request) { relay.HandleChatCompletion(w, r) },
		},
		{
			name: "chat tool", path: "/v1/chat/completions", profile: payloadpolicy.ProfileChatRequest,
			body:   `{"model":"gpt","messages":[],"tools":[{"type":"function","function":{"name":"private-tool-marker","name":"other"}}]}`,
			handle: func(relay *Relay, w http.ResponseWriter, r *http.Request) { relay.HandleChatCompletion(w, r) },
		},
		{
			name: "embeddings root model", path: "/v1/embeddings", profile: payloadpolicy.ProfileEmbeddingsRequest,
			body:   `{"model":"embed-one","model":"private-embedding-marker","input":"hello"}`,
			handle: func(relay *Relay, w http.ResponseWriter, r *http.Request) { relay.HandleEmbeddings(w, r) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var databaseTouches atomic.Int32
			var billingTouches atomic.Int32
			database := &testutil.MockDB{
				QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
					databaseTouches.Add(1)
					return &testutil.MockRows{}, nil
				},
				QueryRowFn: func(context.Context, string, ...any) pgx.Row {
					databaseTouches.Add(1)
					return &testutil.MockRow{}
				},
				ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					databaseTouches.Add(1)
					return pgconn.CommandTag{}, nil
				},
			}
			bill := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					billingTouches.Add(1)
					return &billing.FreezeResult{Success: true}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
					billingTouches.Add(1)
					return nil
				},
			}
			relay := newTestRelay(database, bill)
			budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
			retain := payloadpolicy.RequestMiddleware(budget, test.profile, func(context.Context) int64 { return 1 })
			handler := retain(middleware.StrictJSONIngress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				test.handle(relay, w, r)
			})))

			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(1))
			ctx = context.WithValue(ctx, middleware.CtxRequestID, "strict-json-test")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request.WithContext(ctx))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if databaseTouches.Load() != 0 || billingTouches.Load() != 0 {
				t.Fatalf("duplicate JSON reached business boundary: db=%d billing/provider=%d", databaseTouches.Load(), billingTouches.Load())
			}
			for _, marker := range []string{"private-model-marker", "private-content-marker", "private-part-marker", "private-tool-marker", "private-embedding-marker"} {
				if strings.Contains(recorder.Body.String(), marker) {
					t.Fatalf("error echoed private payload: %s", recorder.Body.String())
				}
			}
			if !strings.Contains(recorder.Body.String(), `"type":"invalid_request_error"`) ||
				!strings.Contains(recorder.Body.String(), `"code":"validation_error.invalid_request"`) {
				t.Fatalf("unexpected OpenAI error envelope: %s", recorder.Body.String())
			}
		})
	}
}
