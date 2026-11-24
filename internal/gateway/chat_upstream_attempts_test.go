package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestChatUpstreamAttemptLimitAtProvider(t *testing.T) {
	const chatResponse = `{"id":"chatcmpl-fixture","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	for _, test := range []struct {
		name           string
		single         bool
		staleFirst     bool
		firstStatus    int
		wantStatus     int
		wantFirst      int64
		wantSecond     int64
		wantAuthorized int64
	}{
		{name: "success", single: true, firstStatus: http.StatusOK, wantStatus: http.StatusOK, wantFirst: 1, wantAuthorized: 1},
		{name: "retryable response", single: true, firstStatus: http.StatusServiceUnavailable, wantStatus: http.StatusBadGateway, wantFirst: 1, wantAuthorized: 1},
		{name: "stream terminated after POST", single: true, wantStatus: http.StatusBadGateway, wantFirst: 1, wantAuthorized: 1},
		{name: "ineligible before dispatch", single: true, staleFirst: true, firstStatus: http.StatusOK, wantStatus: http.StatusOK, wantSecond: 1, wantAuthorized: 2},
		{name: "default still fails over", firstStatus: http.StatusServiceUnavailable, wantStatus: http.StatusOK, wantFirst: 1, wantSecond: 1, wantAuthorized: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var firstPosts, secondPosts atomic.Int64
			startReceiver := func(posts *atomic.Int64, status int) *httptest.Server {
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if r.ProtoMajor != 2 || r.Method != http.MethodPost || err != nil || !strings.Contains(string(body), `"model":"upstream-model"`) {
						t.Errorf("invalid upstream request: protocol=%s method=%s read_error=%v", r.Proto, r.Method, err)
					}
					posts.Add(1)
					if status == 0 {
						panic(http.ErrAbortHandler)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == http.StatusOK {
						_, _ = io.WriteString(w, chatResponse)
					}
				}))
				server.EnableHTTP2 = true
				server.StartTLS()
				t.Cleanup(server.Close)
				return server
			}
			first := startReceiver(&firstPosts, test.firstStatus)
			second := startReceiver(&secondPosts, http.StatusOK)
			encKey := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, first.URL, encKey, nil)
			secondRoute := plan.Routes[0]
			secondRoute.BaseURL = second.URL
			secondRoute.ConnectionName = "second-connection"
			secondRoute.Evidence.DeploymentID++
			secondRoute.Evidence.ConnectionID++
			secondRoute.Evidence.CredentialBindingID++
			plan.Routes = append(plan.Routes, secondRoute)
			coordinator := &executionCoordinatorStub{
				authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					if test.staleFirst && identity.RouteSequence == 1 {
						return modelexecution.Decision{Status: modelexecution.Stale}, nil
					}
					digest, err := executionevidence.Digest(route.Evidence)
					return modelexecution.Decision{
						Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
						DispatchOwnerToken: "attempt-limit-owner",
						ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID},
					}, err
				},
			}
			var attempts []billing.ProviderAttemptCompletion
			var usage []billing.SettleRequest
			billingStub := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1}, nil
				},
				completeAttemptFn: func(_ context.Context, completed billing.ProviderAttemptCompletion) error {
					attempts = append(attempts, completed)
					return nil
				},
				prepareDeliveryFn: func(_ context.Context, req billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
					usage = append(usage, req)
					return &billing.PrepareDeliveryResult{Funded: true, Preview: billing.SettleResult{Success: true}}, nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, billingStub, nil, encKey)
			client := relay.providerClient(5 * time.Second).(*http.Client)
			client.Transport = executionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, !test.single, r.GetBody != nil, "body replay at transport handoff")
				require.Empty(t, r.Header.Get("Idempotency-Key"))
				require.Empty(t, r.Header.Get(middleware.ChatUpstreamAttemptsHeader))
				// Both local fixtures use the same httptest certificate.
				return first.Client().Transport.RoundTrip(r)
			})
			relay.providerHTTPDoer = client
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()))

			r := executionRequest(http.MethodPost, "/v1/chat/completions", "req-chat-upstream-attempt-limit",
				`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`)
			if test.single {
				r.Header.Set(middleware.ChatUpstreamAttemptsHeader, "1")
			}
			w := httptest.NewRecorder()
			middleware.ChatUpstreamAttemptPolicy(http.HandlerFunc(relay.HandleChatCompletion)).ServeHTTP(w, r)

			require.Equal(t, test.wantStatus, w.Code, w.Body.String())
			require.Equal(t, test.wantFirst, firstPosts.Load(), "first provider POST count")
			require.Equal(t, test.wantSecond, secondPosts.Load(), "second provider POST count")
			require.Equal(t, test.wantAuthorized, coordinator.authorizeCount.Load())
			require.Len(t, attempts, int(test.wantFirst+test.wantSecond))
			for _, attempt := range attempts {
				require.Equal(t, !test.single && attempt.Outcome == billing.ProviderAttemptOutcomeRejected, attempt.RetryAuthorized)
			}
			switch {
			case test.wantStatus == http.StatusOK:
				require.Equal(t, billing.ProviderAttemptOutcomeResponseAccepted, attempts[len(attempts)-1].Outcome)
				require.EqualValues(t, 1, coordinator.successCount.Load())
				require.Len(t, usage, 1)
				require.Equal(t, metering.UsageSourceUpstream, usage[0].UsageSource)
				require.Equal(t, 3, usage[0].PromptTokens)
				require.Equal(t, 1, usage[0].CompletionTokens)
			case test.firstStatus == 0:
				require.Equal(t, billing.ProviderAttemptOutcomeUnknown, attempts[0].Outcome)
			default:
				require.Equal(t, billing.ProviderAttemptOutcomeRejected, attempts[0].Outcome)
			}
		})
	}
}
