package mediajob

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/models"
)

type imageRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f imageRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type imageRouterFixture struct {
	route          gateway.RouteResult
	routes         []gateway.RouteResult
	mu             sync.Mutex
	channelSuccess int
	channelFail    int
	keySuccess     int
	keyCooldown    int
	keyAuthFail    int
	keyFail        int
}

func (r *imageRouterFixture) RouteAllWithKey(
	context.Context,
	string,
	string,
) ([]gateway.RouteResult, error) {
	if len(r.routes) > 0 {
		return append([]gateway.RouteResult(nil), r.routes...), nil
	}
	return []gateway.RouteResult{r.route}, nil
}

func (r *imageRouterFixture) MarkChannelSuccess(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channelSuccess++
	return nil
}
func (r *imageRouterFixture) MarkChannelFail(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channelFail++
	return nil
}
func (r *imageRouterFixture) MarkKeySuccess(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keySuccess++
	return nil
}
func (r *imageRouterFixture) MarkKeyRateLimited(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keyCooldown++
	return nil
}
func (r *imageRouterFixture) MarkKeyAuthFailure(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keyAuthFail++
	return nil
}
func (r *imageRouterFixture) MarkKeyFail(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keyFail++
	return nil
}

func TestGPTImageProviderUsesFrozenProfileAndReturnsValidatedBytes(t *testing.T) {
	image := fixturePNG()
	var received struct {
		Authorization string
		Path          string
		RequestID     string
		Body          map[string]any
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Authorization = r.Header.Get("Authorization")
		received.Path = r.URL.Path
		received.RequestID = r.Header.Get("X-Client-Request-Id")
		_ = json.NewDecoder(r.Body).Decode(&received.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` +
			base64.StdEncoding.EncodeToString(image) +
			`"}],"usage":{"input_tokens":3,"input_tokens_details":{"text_tokens":3,"image_tokens":0},"output_tokens":5,"total_tokens":8}}`))
	}))
	defer server.Close()
	encKey := []byte(strings.Repeat("e", 32))
	encrypted, err := auth.EncryptKey([]byte("fixture-provider-key"), encKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	router := &imageRouterFixture{route: gateway.RouteResult{
		Channel: models.Channel{
			ID: 4, BaseURL: server.URL, AdapterType: adapters.TypeOpenAICompatible,
		},
		ChannelKey:  models.ChannelKey{ID: 5, KeyEnc: encrypted},
		MappedModel: "gpt-image-1",
	}}
	provider, err := NewGPTImageProvider(router, encKey, GPTImageProviderOptions{
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	job := imageTestJob(Scope{OrgID: 1, ProjectID: 2}, "mediajob_0123456789abcdef0123456789abcdef")
	job.RequestedModel = "company-image"
	job.RoutingModel = "company-image"
	job.Attempt = 1
	result, err := provider.Generate(context.Background(), job, NormalizedRequest{
		Model: "company-image", Prompt: "fixture prompt", N: 1,
		OutputFormat: "png", Stream: false,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.EqualFold(result.MediaType, "image/png") ||
		string(result.Bytes) != string(image) ||
		result.Outcome.ProviderState != "completed" ||
		len(result.Outcome.Units) != 4 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if received.Authorization != "Bearer fixture-provider-key" ||
		received.Path != "/v1/images/generations" ||
		received.RequestID != job.JobID ||
		received.Body["model"] != "gpt-image-1" ||
		received.Body["stream"] != false {
		t.Fatalf("unexpected upstream request: %+v", received)
	}
	if router.channelSuccess != 1 || router.keySuccess != 1 {
		t.Fatalf("success health not recorded: %+v", router)
	}
}

func TestGPTImageProviderClassifiesRateLimitWithoutLeakingBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"secret":"must-not-escape"}`))
	}))
	defer server.Close()
	encKey := []byte(strings.Repeat("e", 32))
	encrypted, _ := auth.EncryptKey([]byte("fixture-provider-key"), encKey)
	router := &imageRouterFixture{route: gateway.RouteResult{
		Channel: models.Channel{
			ID: 4, BaseURL: server.URL, AdapterType: adapters.TypeOpenAICompatible,
		},
		ChannelKey:  models.ChannelKey{ID: 5, KeyEnc: encrypted},
		MappedModel: "gpt-image-1",
	}}
	provider, err := NewGPTImageProvider(router, encKey, GPTImageProviderOptions{
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	job := imageTestJob(Scope{OrgID: 1, ProjectID: 2}, "mediajob_0123456789abcdef0123456789abcdef")
	job.RequestedModel = "company-image"
	job.RoutingModel = "company-image"
	_, err = provider.Generate(context.Background(), job, NormalizedRequest{
		Model: "company-image", Prompt: "fixture prompt", N: 1,
	})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Retryable ||
		!strings.Contains(providerErr.Code, "429_rate_limit") ||
		strings.Contains(err.Error(), "must-not-escape") {
		t.Fatalf("error = %#v", err)
	}
	if router.keyCooldown != 1 {
		t.Fatalf("rate-limit cooldown count = %d", router.keyCooldown)
	}
}

func TestGPTImageProviderFailsClosedOnAmbiguousTransportOutcome(t *testing.T) {
	encKey := []byte(strings.Repeat("e", 32))
	encrypted, _ := auth.EncryptKey([]byte("fixture-provider-key"), encKey)
	router := &imageRouterFixture{route: gateway.RouteResult{
		Channel: models.Channel{
			ID: 4, BaseURL: "https://provider.example.test",
			AdapterType: adapters.TypeOpenAICompatible,
		},
		ChannelKey:  models.ChannelKey{ID: 5, KeyEnc: encrypted},
		MappedModel: "gpt-image-1",
	}}
	provider, err := NewGPTImageProvider(router, encKey, GPTImageProviderOptions{
		HTTPClient: &http.Client{Transport: imageRoundTripperFunc(
			func(*http.Request) (*http.Response, error) {
				return nil, errors.New("ambiguous network failure")
			},
		)},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	job := imageTestJob(
		Scope{OrgID: 1, ProjectID: 2},
		"mediajob_0123456789abcdef0123456789abcdef",
	)
	job.RequestedModel = "company-image"
	job.RoutingModel = "company-image"
	_, err = provider.Generate(context.Background(), job, NormalizedRequest{
		Model: "company-image", Prompt: "fixture prompt", N: 1,
	})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) ||
		!providerErr.OutcomeUnknown ||
		providerErr.Retryable ||
		providerErr.Code != "provider_transport_outcome_unknown" ||
		strings.Contains(err.Error(), "ambiguous network failure") {
		t.Fatalf("error = %#v", err)
	}
}

func TestGPTImageProviderNeverFollowsRedirects(t *testing.T) {
	encKey := []byte(strings.Repeat("e", 32))
	encrypted, _ := auth.EncryptKey([]byte("fixture-provider-key"), encKey)
	router := &imageRouterFixture{route: gateway.RouteResult{
		Channel: models.Channel{
			ID: 4, BaseURL: "https://provider.example.test",
			AdapterType: adapters.TypeOpenAICompatible,
		},
		ChannelKey:  models.ChannelKey{ID: 5, KeyEnc: encrypted},
		MappedModel: "gpt-image-1",
	}}
	var calls int
	provider, err := NewGPTImageProvider(router, encKey, GPTImageProviderOptions{
		HTTPClient: &http.Client{
			Transport: imageRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatalf(
						"redirect followed to %s with authorization=%q",
						request.URL,
						request.Header.Get("Authorization"),
					)
				}
				return &http.Response{
					StatusCode: http.StatusTemporaryRedirect,
					Header: http.Header{
						"Location": []string{"https://attacker.example.test/capture"},
					},
					Body: http.NoBody,
				}, nil
			}),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	job := imageTestJob(
		Scope{OrgID: 1, ProjectID: 2},
		"mediajob_0123456789abcdef0123456789abcdef",
	)
	job.RequestedModel = "company-image"
	job.RoutingModel = "company-image"
	_, err = provider.Generate(context.Background(), job, NormalizedRequest{
		Model: "company-image", Prompt: "fixture prompt", N: 1,
	})
	if err == nil || calls != 1 {
		t.Fatalf("redirect result err=%v calls=%d", err, calls)
	}
}

func TestGPTImageProviderUsesOneTimeoutBudgetAcrossFailoverRoutes(t *testing.T) {
	encKey := []byte(strings.Repeat("e", 32))
	encrypted, _ := auth.EncryptKey([]byte("fixture-provider-key"), encKey)
	route := gateway.RouteResult{
		Channel: models.Channel{
			ID: 4, BaseURL: "https://provider.example.test",
			AdapterType: adapters.TypeOpenAICompatible,
		},
		ChannelKey:  models.ChannelKey{ID: 5, KeyEnc: encrypted},
		MappedModel: "gpt-image-1",
	}
	second := route
	second.Channel.ID = 6
	second.ChannelKey.ID = 7
	router := &imageRouterFixture{routes: []gateway.RouteResult{route, second}}
	var deadlines []time.Time
	provider, err := NewGPTImageProvider(router, encKey, GPTImageProviderOptions{
		Timeout: time.Second,
		HTTPClient: &http.Client{Transport: imageRoundTripperFunc(
			func(request *http.Request) (*http.Response, error) {
				deadline, ok := request.Context().Deadline()
				if !ok {
					t.Fatal("provider request has no timeout deadline")
				}
				deadlines = append(deadlines, deadline)
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     make(http.Header),
					Body:       http.NoBody,
				}, nil
			},
		)},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	job := imageTestJob(
		Scope{OrgID: 1, ProjectID: 2},
		"mediajob_0123456789abcdef0123456789abcdef",
	)
	job.RequestedModel = "company-image"
	job.RoutingModel = "company-image"
	_, err = provider.Generate(context.Background(), job, NormalizedRequest{
		Model: "company-image", Prompt: "fixture prompt", N: 1,
	})
	if err == nil || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("err=%v deadlines=%v", err, deadlines)
	}
}

func TestGPTImageProviderBoundedFirstDispatch(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		status       int
		attemptMax   int
		single       bool
		wantDownload bool
		wantFallback int32
	}{
		{name: "success", status: 200, attemptMax: 1, single: true, wantDownload: true},
		{name: "rate-limited", status: 429, attemptMax: 1, single: true},
		{name: "redirect", status: 307, attemptMax: 1, single: true},
		{name: "connection lost after full body", attemptMax: 1, single: true},
		{name: "ordinary failover remains available", status: 429, attemptMax: 3, wantDownload: true, wantFallback: 1},
		{name: "legacy default one still allows failover", status: 429, attemptMax: 1, wantDownload: true, wantFallback: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var primaryCalls, fallbackCalls, nonHTTP1Calls atomic.Int32
			respond := func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` +
					base64.StdEncoding.EncodeToString(fixturePNG()) +
					`"}],"usage":{"input_tokens":3,"input_tokens_details":{"text_tokens":3,"image_tokens":0},"output_tokens":5,"total_tokens":8}}`))
			}
			fallback := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || !strings.Contains(string(body), "unique synthetic prompt") {
					http.Error(w, "invalid fixture body", 400)
					return
				}
				respond(w)
			}))
			fallback.EnableHTTP2 = true
			fallback.StartTLS()
			t.Cleanup(fallback.Close)
			primary := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				primaryCalls.Add(1)
				if r.ProtoMajor != 1 {
					nonHTTP1Calls.Add(1)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != http.MethodPost || !strings.Contains(string(body), "unique synthetic prompt") {
					http.Error(w, "invalid fixture body", 400)
					return
				}
				switch scenario.status {
				case 200:
					respond(w)
				case 0:
					h, ok := w.(http.Hijacker)
					if !ok {
						http.Error(w, "provider cannot model drop", 500)
						return
					}
					conn, _, err := h.Hijack()
					if err == nil {
						_ = conn.Close()
					}
				default:
					w.Header().Set("Location", fallback.URL)
					w.WriteHeader(scenario.status)
					_, _ = w.Write([]byte(`{"error":{"message":"fixture"}}`))
				}
			}))
			primary.EnableHTTP2 = true
			primary.StartTLS()
			t.Cleanup(primary.Close)
			key := []byte(strings.Repeat("e", 32))
			encrypted, err := auth.EncryptKey([]byte("fixture-provider-key"), key)
			require.NoError(t, err)
			route := func(base, adapter string) gateway.RouteResult {
				return gateway.RouteResult{Channel: models.Channel{BaseURL: base, AdapterType: adapter},
					ChannelKey: models.ChannelKey{KeyEnc: encrypted}, MappedModel: "gpt-image-2.6-flare"}
			}
			router := &imageRouterFixture{routes: []gateway.RouteResult{
				route(fallback.URL, "anthropic"), route(primary.URL, adapters.TypeOpenAICompatible), route(fallback.URL, adapters.TypeOpenAICompatible),
			}}
			provider, err := NewGPTImageProvider(router, key, GPTImageProviderOptions{HTTPClient: primary.Client()})
			require.NoError(t, err)
			job := imageTestJob(Scope{OrgID: 1, ProjectID: 2}, "mediajob_0123456789abcdef0123456789abcdef")
			job.RequestedModel, job.RoutingModel = "company-image", "company-image"
			job.MaxAttempts, job.Attempt, job.Lifecycle.Outcome = scenario.attemptMax, 1, OutcomePending
			job.SingleUpstreamAttempt = scenario.single
			request := NormalizedRequest{Model: "company-image", Prompt: "unique synthetic prompt", N: 1}
			result, err := provider.Generate(t.Context(), job, request)
			if scenario.wantDownload {
				require.NoError(t, err)
				require.Equal(t, "image/png", result.MediaType)
			} else {
				var failure *ProviderError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, scenario.status == 0, failure.OutcomeUnknown)
			}
			require.EqualValues(t, 1, primaryCalls.Load())
			require.Equal(t, scenario.wantFallback, fallbackCalls.Load())
			if scenario.single {
				require.Zero(t, nonHTTP1Calls.Load())
				job.Lifecycle.Outcome = OutcomeSucceeded
				_, err = provider.Generate(t.Context(), job, request)
				require.ErrorContains(t, err, "single_submission_requires_first_dispatch")
				job.Lifecycle.Outcome = OutcomePending
				job.Attempt++
				_, err = provider.Generate(t.Context(), job, request)
				require.ErrorContains(t, err, "single_submission_requires_first_dispatch")
				require.EqualValues(t, 1, primaryCalls.Load())
				require.Zero(t, fallbackCalls.Load())
			}
		})
	}
}
