package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

type publicNonStreamDoerFunc func(*http.Request) (*http.Response, error)

func (function publicNonStreamDoerFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

type publicOutputGuardFunc func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error)

func (function publicOutputGuardFunc) Evaluate(
	ctx context.Context,
	input PublicOutputGuardInput,
) (PublicOutputGuardDecision, error) {
	return function(ctx, input)
}

type publicTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

type publicFailingBody struct {
	err    error
	closed atomic.Bool
}

func (body *publicFailingBody) Read([]byte) (int, error) { return 0, body.err }

func (body *publicFailingBody) Close() error {
	body.closed.Store(true)
	return nil
}

func (body *publicTrackingBody) Close() error {
	body.closed.Store(true)
	return nil
}

func TestPublicNonStreamExecutorStrictParseThenGuardrailReplacement(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	providerBody := &publicTrackingBody{Reader: strings.NewReader(publicResponsesProviderBody("answer", ""))}
	executor, err := NewPublicNonStreamExecutor(
		publicNonStreamDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if request.URL.String() != "https://provider.example/v1/responses" ||
				!strings.Contains(string(body), `"model":"provider-private-model"`) ||
				strings.Contains(string(body), `"model":"public-text"`) {
				t.Fatalf("unsafe upstream request url=%s body=%s", request.URL, body)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: providerBody}, nil
		}),
		publicOutputGuardFunc(func(_ context.Context, input PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			if input.EndpointKind != endpointprotocol.EndpointResponses || len(input.Texts) != 1 ||
				input.Texts[0].Path != "output[0].content[0].text" || input.Texts[0].Text != "answer" {
				t.Fatalf("guard input=%+v", input)
			}
			return PublicOutputGuardDecision{Action: PublicOutputGuardReplace, Replacements: []string{"safe answer"}}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), upstream)
	if err != nil {
		t.Fatal(err)
	}
	if !providerBody.closed.Load() {
		t.Fatal("provider response body was not closed")
	}
	if result.StatusCode != http.StatusOK || result.CachePolicy() != endpointprotocol.PublicCachePolicyBypass ||
		result.Delivery.CachePolicy() != endpointprotocol.PublicCachePolicyBypass ||
		result.ProviderHealthAction() != PublicNonStreamHealthSuccess {
		t.Fatalf("result=%+v", result)
	}
	if body := string(result.Delivery.Body()); !strings.Contains(body, `"model":"public-text"`) ||
		!strings.Contains(body, `"text":"safe answer"`) || strings.Contains(body, "provider-private-model") {
		t.Fatalf("unsafe delivery=%s", body)
	}
}

func TestPublicNonStreamExecutorRejectsProviderShapeBeforeOutputGuard(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	var guardCalls atomic.Int64
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", `,"future_root":true`))),
			}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			guardCalls.Add(1)
			return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
		}),
	)
	_, err := executor.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
	if guardCalls.Load() != 0 {
		t.Fatalf("guard reached before strict provider parse: calls=%d", guardCalls.Load())
	}
}

func TestPublicNonStreamExecutorOutputBlockIsLocalNotProviderFailure(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", "")))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			return PublicOutputGuardDecision{Action: PublicOutputGuardBlock}, nil
		}),
	)
	result, err := executor.Execute(context.Background(), upstream)
	failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageOutputGuardrail)
	if !errors.Is(err, ErrPublicNonStreamOutputWithheld) {
		t.Fatalf("error=%v", err)
	}
	if failure.Source != PublicNonStreamSourceLocalPolicy || failure.ProviderStatus != http.StatusOK ||
		failure.FailureKind != "output_withheld" || failure.Retryable ||
		failure.HealthAction() != PublicNonStreamHealthSuccess || !failure.ProviderHealthEligible() {
		t.Fatalf("failure=%+v health=%s", failure, failure.HealthAction())
	}
	assertPublicParsedLiability(t, result)
}

func TestPublicNonStreamExecutorRejectsProviderSpecificSuccessStatus(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	var guardCalls atomic.Int64
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", "")))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			guardCalls.Add(1)
			return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
		}),
	)
	_, err := executor.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
	if !errors.Is(err, endpointprotocol.ErrUpstreamResponse) || guardCalls.Load() != 0 {
		t.Fatalf("status 202 crossed public contract: error=%v guard_calls=%d", err, guardCalls.Load())
	}
}

func TestPublicNonStreamExecutorRejectsGuardrailExpansionBeyondDeliveryBound(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", "")))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			return PublicOutputGuardDecision{
				Action:       PublicOutputGuardReplace,
				Replacements: []string{strings.Repeat("x", int(endpointprotocol.PublicNonStreamMaxResponseBytes))},
			}, nil
		}),
	)
	_, err := executor.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageOutputGuardrail)
	if !errors.Is(err, ErrPublicNonStreamOutputGuard) {
		t.Fatalf("oversized replacement error=%v", err)
	}
}

func TestPublicNonStreamExecutorBoundsResponseBeforeParseOrGuard(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	var guardCalls atomic.Int64
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("123456789"))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			guardCalls.Add(1)
			return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
		}),
	)
	executor.maxResponseBytes = 8
	_, err := executor.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
	if !errors.Is(err, ErrPublicNonStreamResponseTooLarge) || guardCalls.Load() != 0 {
		t.Fatalf("error=%v guard_calls=%d", err, guardCalls.Load())
	}
}

func TestPublicNonStreamExecutorFailureStagesAndGuardDecisionValidation(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	if _, err := NewPublicNonStreamExecutor(nil, publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
		return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
	})); !errors.Is(err, ErrPublicNonStreamExecutorInvalid) {
		t.Fatalf("nil doer error=%v", err)
	}

	network := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private network detail") }),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
		}),
	)
	_, err := network.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageProviderNetwork)
	if !errors.Is(err, ErrPublicNonStreamTransport) || strings.Contains(err.Error(), "private network detail") {
		t.Fatalf("unsafe network error=%v", err)
	}

	badDecision := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", "")))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			return PublicOutputGuardDecision{Action: PublicOutputGuardAllow, Replacements: []string{"unexpected"}}, nil
		}),
	)
	_, err = badDecision.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageOutputGuardrail)
	if !errors.Is(err, ErrPublicNonStreamOutputGuard) {
		t.Fatalf("error=%v", err)
	}

	nonSuccess := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{"error":"private"}`))}, nil
		}),
		publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			t.Fatal("guard called for non-success provider response")
			return PublicOutputGuardDecision{}, nil
		}),
	)
	_, err = nonSuccess.Execute(context.Background(), upstream)
	assertPublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
	if !errors.Is(err, endpointprotocol.ErrUpstreamResponse) {
		t.Fatalf("error=%v", err)
	}
}

func TestPublicNonStreamExecutorSanitizesTransportAndResponseFailures(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	allow := publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
		return PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, nil
	})

	t.Run("client cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("secret-key provider-private-model raw cancellation")
			}),
			allow,
		)
		_, err := executor.Execute(ctx, upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderNetwork)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceClient, 0, "client_cancelled", false, PublicNonStreamHealthNone)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client cancellation type lost: %v", err)
		}
		assertPublicFailureSanitized(t, err)
	})

	t.Run("deadline", func(t *testing.T) {
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
			allow,
		)
		_, err := executor.Execute(context.Background(), upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderNetwork)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceProviderTransport, 0, "deadline", true, PublicNonStreamHealthProviderFailure)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline type lost: %v", err)
		}
		assertPublicFailureSanitized(t, err)
	})

	t.Run("connection", func(t *testing.T) {
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("secret-key provider-private-model raw dial failure")
			}),
			allow,
		)
		_, err := executor.Execute(context.Background(), upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderNetwork)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceProviderTransport, 0, "connection", true, PublicNonStreamHealthProviderFailure)
		if !errors.Is(err, ErrPublicNonStreamTransport) {
			t.Fatalf("transport type lost: %v", err)
		}
		assertPublicFailureSanitized(t, err)
	})

	t.Run("invalid 200", func(t *testing.T) {
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				body := publicResponsesProviderBody("secret-key", `,"provider-private-model-secret":true`)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			}),
			allow,
		)
		_, err := executor.Execute(context.Background(), upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceProviderResponse, http.StatusOK, "invalid_response", true, PublicNonStreamHealthProviderFailure)
		assertPublicFailureSanitized(t, err)
	})

	t.Run("response read", func(t *testing.T) {
		body := &publicFailingBody{err: errors.New("secret-key provider-private-model raw read failure")}
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			}),
			allow,
		)
		_, err := executor.Execute(context.Background(), upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceProviderResponse, http.StatusOK, "response_read", true, PublicNonStreamHealthProviderFailure)
		if !errors.Is(err, ErrPublicNonStreamResponseRead) || !body.closed.Load() {
			t.Fatalf("response read identity/close lost: error=%v closed=%t", err, body.closed.Load())
		}
		assertPublicFailureSanitized(t, err)
	})

	t.Run("output guard", func(t *testing.T) {
		executor := mustPublicExecutor(t,
			publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(publicResponsesProviderBody("answer", ""))),
				}, nil
			}),
			publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
				return PublicOutputGuardDecision{}, errors.New("secret-key provider-private-model raw guard failure")
			}),
		)
		result, err := executor.Execute(context.Background(), upstream)
		failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageOutputGuardrail)
		assertPublicFailureMetadata(t, failure, PublicNonStreamSourceLocalPolicy, http.StatusOK, "output_guard_error", false, PublicNonStreamHealthSuccess)
		if !errors.Is(err, ErrPublicNonStreamOutputGuard) {
			t.Fatalf("output guard type lost: %v", err)
		}
		assertPublicParsedLiability(t, result)
		assertPublicFailureSanitized(t, err)
	})
}

func TestPublicNonStreamExecutorHTTPFailureMetadataAndHealthEligibility(t *testing.T) {
	upstream := publicResponsesUpstream(t)
	tests := []struct {
		status    int
		kind      string
		retryable bool
		health    PublicNonStreamHealthAction
	}{
		{status: http.StatusBadRequest, kind: "client_request", retryable: false, health: PublicNonStreamHealthNone},
		{status: http.StatusUnauthorized, kind: "auth", retryable: false, health: PublicNonStreamHealthCredentialAuthFailure},
		{status: http.StatusTooManyRequests, kind: "rate_limit", retryable: true, health: PublicNonStreamHealthCredentialRateLimited},
		{status: http.StatusInternalServerError, kind: "channel_failure", retryable: true, health: PublicNonStreamHealthProviderFailure},
		{status: http.StatusServiceUnavailable, kind: "channel_failure", retryable: true, health: PublicNonStreamHealthProviderFailure},
	}
	for _, test := range tests {
		for _, bodyCase := range []string{"failing body", "oversized body"} {
			t.Run(http.StatusText(test.status)+"/"+bodyCase, func(t *testing.T) {
				var body io.ReadCloser
				var closed func() bool
				switch bodyCase {
				case "failing body":
					failing := &publicFailingBody{err: errors.New("secret-key provider-private-model raw read failure")}
					body = failing
					closed = failing.closed.Load
				case "oversized body":
					oversized := &publicTrackingBody{Reader: strings.NewReader("123456789")}
					body = oversized
					closed = oversized.closed.Load
				}
				executor := mustPublicExecutor(t,
					publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: test.status, Body: body}, nil
					}),
					publicOutputGuardFunc(func(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
						t.Fatal("HTTP failure reached output guard")
						return PublicOutputGuardDecision{}, nil
					}),
				)
				executor.maxResponseBytes = 8
				_, err := executor.Execute(context.Background(), upstream)
				failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageProviderContract)
				assertPublicFailureMetadata(
					t,
					failure,
					PublicNonStreamSourceProviderHTTP,
					test.status,
					test.kind,
					test.retryable,
					test.health,
				)
				if failure.ProviderHealthEligible() != (test.health != PublicNonStreamHealthNone) {
					t.Fatalf("health eligibility=%t action=%s", failure.ProviderHealthEligible(), test.health)
				}
				if !errors.Is(err, endpointprotocol.ErrUpstreamResponse) || !closed() {
					t.Fatalf("status authority/close lost: error=%v closed=%t", err, closed())
				}
				assertPublicFailureSanitized(t, err)
			})
		}
	}
}

func TestPublicClaudeSonnet5RefusalExplanationAlwaysCrossesOutputGuard(t *testing.T) {
	for _, test := range []struct {
		name        string
		decision    PublicOutputGuardDecision
		wantText    string
		wantBlocked bool
	}{
		{name: "allow", decision: PublicOutputGuardDecision{Action: PublicOutputGuardAllow}, wantText: "provider refusal explanation"},
		{name: "replace", decision: PublicOutputGuardDecision{Action: PublicOutputGuardReplace, Replacements: []string{"safe refusal explanation"}}, wantText: "safe refusal explanation"},
		{name: "block", decision: PublicOutputGuardDecision{Action: PublicOutputGuardBlock}, wantBlocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := publicSonnet5Upstream(t)
			executor := mustPublicExecutor(t,
				publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
  "id":"msg_refusal","type":"message","role":"assistant","model":"claude-sonnet-5",
  "content":[],"stop_reason":"refusal","stop_sequence":null,
  "stop_details":{"type":"refusal","category":"cyber","explanation":"provider refusal explanation"},
  "usage":{"input_tokens":10,"output_tokens":1}
}`))}, nil
				}),
				publicOutputGuardFunc(func(_ context.Context, input PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
					if input.EndpointKind != endpointprotocol.EndpointMessages || len(input.Texts) != 1 ||
						input.Texts[0] != (endpointprotocol.PublicText{Path: "stop_details.explanation", Text: "provider refusal explanation"}) {
						t.Fatalf("guard input=%+v", input)
					}
					return test.decision, nil
				}),
			)
			result, err := executor.Execute(context.Background(), upstream)
			if test.wantBlocked {
				failure := requirePublicExecutionFailure(t, err, PublicNonStreamStageOutputGuardrail)
				if !errors.Is(err, ErrPublicNonStreamOutputWithheld) || failure.HealthAction() != PublicNonStreamHealthSuccess ||
					result.Delivery.CustomerUsage() != (endpointprotocol.PublicCustomerUsage{InputTokens: 10, OutputTokens: 1}) {
					t.Fatalf("blocked refusal result=%+v failure=%+v err=%v", result, failure, err)
				}
				return
			}
			if err != nil || !strings.Contains(string(result.Delivery.Body()), `"explanation":"`+test.wantText+`"`) ||
				result.Delivery.CustomerUsage() != (endpointprotocol.PublicCustomerUsage{InputTokens: 10, OutputTokens: 1}) {
				t.Fatalf("refusal result=%+v body=%s err=%v", result, result.Delivery.Body(), err)
			}
		})
	}
}

func TestPublicClaudeSonnet5OmittedThinkingEvidenceBypassesTextGuardWithoutBypassingUsage(t *testing.T) {
	upstream := publicSonnet5ControlsUpstream(t)
	executor := mustPublicExecutor(t,
		publicNonStreamDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
  "id":"msg_thinking","type":"message","role":"assistant","model":"claude-sonnet-5",
  "content":[{"type":"thinking","thinking":"","signature":"opaque-signature"},{"type":"text","text":"provider answer"}],
  "stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":10,"output_tokens":7,"output_tokens_details":{"thinking_tokens":3},"inference_geo":"global","service_tier":"standard"}
}`))}, nil
		}),
		publicOutputGuardFunc(func(_ context.Context, input PublicOutputGuardInput) (PublicOutputGuardDecision, error) {
			if input.EndpointKind != endpointprotocol.EndpointMessages || len(input.Texts) != 1 ||
				input.Texts[0] != (endpointprotocol.PublicText{Path: "content[1].text", Text: "provider answer"}) {
				t.Fatalf("guard input=%+v", input)
			}
			return PublicOutputGuardDecision{Action: PublicOutputGuardReplace, Replacements: []string{"safe answer"}}, nil
		}),
	)
	result, err := executor.Execute(context.Background(), upstream)
	if err != nil || result.Delivery.CustomerUsage() != (endpointprotocol.PublicCustomerUsage{InputTokens: 10, OutputTokens: 7}) ||
		!strings.Contains(string(result.Delivery.Body()), `"signature":"opaque-signature"`) ||
		!strings.Contains(string(result.Delivery.Body()), `"inference_geo":"global"`) ||
		!strings.Contains(string(result.Delivery.Body()), `"service_tier":"standard"`) ||
		!strings.Contains(string(result.Delivery.Body()), `"text":"safe answer"`) {
		t.Fatalf("thinking result=%+v body=%s err=%v", result, result.Delivery.Body(), err)
	}
}

func publicResponsesUpstream(t *testing.T) *endpointprotocol.PublicUpstreamRequest {
	t.Helper()
	identity := endpointprotocol.PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDResponses),
		PublicPath:                 modeloperation.PathResponses,
		ExecutorContractVersion:    endpointprotocol.EndpointProfileNonStreamExecutorV1,
		ProfileID:                  endpointprotocol.ProfileOpenAIResponsesText,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    endpointprotocol.PublicDeliveryContractV1,
		ChargeContractVersion:      endpointprotocol.PublicCustomerChargeContractV1,
		IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1,
	}
	profile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-text","input":"hello","max_output_tokens":64}`),
		"public-text",
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, endpointprotocol.PublicUpstreamInput{
		BaseURL:     "https://provider.example",
		APIKey:      "secret",
		RequestID:   "req-public",
		MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	return upstream
}

func publicSonnet5Upstream(t *testing.T) *endpointprotocol.PublicUpstreamRequest {
	t.Helper()
	identity := endpointprotocol.PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDMessages),
		PublicPath:                 modeloperation.PathMessages,
		ExecutorContractVersion:    endpointprotocol.EndpointProfileNonStreamExecutorV1,
		ProfileID:                  endpointprotocol.ProfileAnthropicMessagesText,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    endpointprotocol.PublicDeliveryContractV1,
		ChargeContractVersion:      endpointprotocol.PublicCustomerChargeContractV1,
		IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1,
	}
	profile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude",
		endpointprotocol.PublicRequestContract{
			ExecutionProfileID: "anthropic.claude-sonnet-5.api", ExecutionProfileVersion: "1.1.0",
			AnthropicMessages: &endpointprotocol.PublicAnthropicMessagesContract{
				ForbiddenRootFields: []string{"temperature", "top_p"}, RequireFinalUserMessage: true,
				AllowedResponseStops: []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
				RefusalStopDetails:   true,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, endpointprotocol.PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", RequestID: "req-sonnet5",
		MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	return upstream
}

func publicSonnet5ControlsUpstream(t *testing.T) *endpointprotocol.PublicUpstreamRequest {
	t.Helper()
	identity := endpointprotocol.PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDMessages),
		PublicPath:                 modeloperation.PathMessages,
		ExecutorContractVersion:    endpointprotocol.EndpointProfileNonStreamExecutorV1,
		ProfileID:                  endpointprotocol.ProfileAnthropicMessagesText,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    endpointprotocol.PublicDeliveryContractV1,
		ChargeContractVersion:      endpointprotocol.PublicCustomerChargeContractV1,
		IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1,
	}
	profile, err := endpointprotocol.NewPublicNonStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := profile.PrepareRequest(
		[]byte(`{"model":"public-claude","max_tokens":64,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hello"}]}`),
		"public-claude",
		endpointprotocol.PublicRequestContract{
			ExecutionProfileID: "anthropic.claude-sonnet-5.api", ExecutionProfileVersion: "1.2.0",
			AnthropicMessages: &endpointprotocol.PublicAnthropicMessagesContract{
				ForbiddenRootFields: []string{"temperature", "top_p"}, RequireFinalUserMessage: true,
				AllowedResponseStops: []string{"end_turn", "max_tokens", "refusal", "stop_sequence"},
				RefusalStopDetails:   true,
				ResponseInferenceGeo: true, ResponseServiceTier: true,
				Reasoning: &endpointprotocol.PublicAnthropicReasoningRequestContract{
					ThinkingObjectField: "thinking", ThinkingTypeField: "type",
					AllowedThinkingTypes: []string{"adaptive", "disabled"}, ThinkingProviderDefault: "adaptive",
					DisabledThinkingTypes:   []string{"disabled"},
					OutputConfigObjectField: "output_config", EffortField: "effort",
					AllowedEfforts: []string{"low", "medium", "high", "xhigh", "max"}, EffortProviderDefault: "high",
					ResponseThinkingTextMode: "omitted",
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, endpointprotocol.PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", RequestID: "req-sonnet5-controls",
		MappedModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	return upstream
}

func publicResponsesProviderBody(text, rootExtra string) string {
	encoded, _ := json.Marshal(text)
	return `{
		"id":"resp_1","object":"response","status":"completed","model":"provider-private-model",
		"error":null,"incomplete_details":null` + rootExtra + `,
		"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":` + string(encoded) + `}]}],
		"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}
	}`
}

func mustPublicExecutor(
	t *testing.T,
	doer PublicNonStreamHTTPDoer,
	guard PublicNonStreamOutputGuard,
) *PublicNonStreamExecutor {
	t.Helper()
	executor, err := NewPublicNonStreamExecutor(doer, guard)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func assertPublicExecutionFailure(t *testing.T, err error, wantStage PublicNonStreamFailureStage) {
	t.Helper()
	_ = requirePublicExecutionFailure(t, err, wantStage)
}

func requirePublicExecutionFailure(
	t *testing.T,
	err error,
	wantStage PublicNonStreamFailureStage,
) *PublicNonStreamExecutionError {
	t.Helper()
	var failure *PublicNonStreamExecutionError
	if !errors.As(err, &failure) || failure.Stage != wantStage {
		t.Fatalf("error=%v failure=%+v want stage=%s", err, failure, wantStage)
	}
	return failure
}

func assertPublicFailureMetadata(
	t *testing.T,
	failure *PublicNonStreamExecutionError,
	source PublicNonStreamFailureSource,
	status int,
	kind string,
	retryable bool,
	health PublicNonStreamHealthAction,
) {
	t.Helper()
	if failure.Source != source || failure.ProviderStatus != status || failure.FailureKind != kind ||
		failure.Retryable != retryable || failure.HealthAction() != health {
		t.Fatalf(
			"failure=%+v health=%s want source=%s status=%d kind=%s retryable=%t health=%s",
			failure,
			failure.HealthAction(),
			source,
			status,
			kind,
			retryable,
			health,
		)
	}
}

func assertPublicFailureSanitized(t *testing.T, err error) {
	t.Helper()
	text := err.Error()
	for _, forbidden := range []string{"secret-key", "provider-private-model", "raw body", "raw read", "raw dial", "raw cancellation", "raw guard"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("failure leaked %q: %s", forbidden, text)
		}
	}
}

func assertPublicParsedLiability(t *testing.T, result PublicNonStreamExecution) {
	t.Helper()
	wantUsage := endpointprotocol.PublicCustomerUsage{InputTokens: 10, OutputTokens: 4}
	if result.StatusCode != http.StatusOK || result.Delivery.CustomerUsage() != wantUsage {
		t.Fatalf("parsed liability lost: result=%+v usage=%+v", result, result.Delivery.CustomerUsage())
	}
	body := string(result.Delivery.Body())
	if !strings.Contains(body, `"model":"public-text"`) ||
		strings.Contains(body, "provider-private-model") {
		t.Fatalf("parsed delivery unsafe: %s", body)
	}
}
