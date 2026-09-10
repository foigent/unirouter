package gateway

import (
	"context"
	"errors"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/guardrail"
	"io"
	"net"
	"net/http"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

var (
	ErrPublicNonStreamExecutorInvalid  = errors.New("gateway: public nonstream executor invalid")
	ErrPublicNonStreamTransport        = errors.New("gateway: public nonstream provider transport failed")
	ErrPublicNonStreamResponseRead     = errors.New("gateway: public nonstream provider response read failed")
	ErrPublicNonStreamResponseTooLarge = errors.New("gateway: public nonstream provider response too large")
	ErrPublicNonStreamOutputGuard      = errors.New("gateway: public nonstream output guard failed")
	ErrPublicNonStreamOutputWithheld   = errors.New("gateway: public nonstream output withheld")
)

type PublicNonStreamFailureStage string

const (
	PublicNonStreamStagePreflight        PublicNonStreamFailureStage = "preflight"
	PublicNonStreamStageProviderNetwork  PublicNonStreamFailureStage = "provider_network"
	PublicNonStreamStageProviderContract PublicNonStreamFailureStage = "provider_contract"
	PublicNonStreamStageOutputGuardrail  PublicNonStreamFailureStage = "output_guardrail"
)

type PublicNonStreamFailureSource string

const (
	PublicNonStreamSourceGateway           PublicNonStreamFailureSource = "gateway"
	PublicNonStreamSourceClient            PublicNonStreamFailureSource = "client"
	PublicNonStreamSourceProviderTransport PublicNonStreamFailureSource = "provider_transport"
	PublicNonStreamSourceProviderHTTP      PublicNonStreamFailureSource = "provider_http"
	PublicNonStreamSourceProviderResponse  PublicNonStreamFailureSource = "provider_response"
	PublicNonStreamSourceLocalPolicy       PublicNonStreamFailureSource = "local_policy"
)

type PublicNonStreamHealthAction string

const (
	PublicNonStreamHealthNone                  PublicNonStreamHealthAction = "none"
	PublicNonStreamHealthSuccess               PublicNonStreamHealthAction = "success"
	PublicNonStreamHealthProviderFailure       PublicNonStreamHealthAction = "provider_failure"
	PublicNonStreamHealthCredentialAuthFailure PublicNonStreamHealthAction = "credential_auth_failure"
	PublicNonStreamHealthCredentialRateLimited PublicNonStreamHealthAction = "credential_rate_limited"
)

// PublicNonStreamExecutionError carries only sanitized failure metadata and a
// stable typed cause. It never includes provider response bytes, raw transport
// details, credentials or mapped model identity. The stage and source let the
// coordinator distinguish a local output-policy hold from a provider health
// failure.
type PublicNonStreamExecutionError struct {
	Stage          PublicNonStreamFailureStage
	Source         PublicNonStreamFailureSource
	ProviderStatus int
	FailureKind    string
	Retryable      bool
	err            error
}

func (failure *PublicNonStreamExecutionError) Error() string {
	if failure == nil {
		return "gateway: public nonstream execution failed"
	}
	return "gateway: public nonstream " + string(failure.Stage) + "/" +
		string(failure.Source) + "/" + failure.FailureKind
}

func (failure *PublicNonStreamExecutionError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.err
}

// HealthAction is the only post-dispatch health transition implied by this
// sanitized failure. A strict provider response followed by a local output
// hold is provider success; client mistakes/cancellation never mutate provider
// health.
func (failure *PublicNonStreamExecutionError) HealthAction() PublicNonStreamHealthAction {
	if failure == nil {
		return PublicNonStreamHealthNone
	}
	if failure.Stage == PublicNonStreamStageOutputGuardrail && failure.ProviderStatus == http.StatusOK {
		return PublicNonStreamHealthSuccess
	}
	switch failure.Source {
	case PublicNonStreamSourceProviderTransport, PublicNonStreamSourceProviderResponse:
		return PublicNonStreamHealthProviderFailure
	case PublicNonStreamSourceProviderHTTP:
		switch failure.FailureKind {
		case "auth":
			return PublicNonStreamHealthCredentialAuthFailure
		case "rate_limit":
			return PublicNonStreamHealthCredentialRateLimited
		case "channel_failure":
			return PublicNonStreamHealthProviderFailure
		}
	}
	return PublicNonStreamHealthNone
}

func (failure *PublicNonStreamExecutionError) ProviderHealthEligible() bool {
	return failure != nil && failure.HealthAction() != PublicNonStreamHealthNone
}

type PublicNonStreamHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PublicOutputGuardAction string

const (
	PublicOutputGuardAllow   PublicOutputGuardAction = "allow"
	PublicOutputGuardReplace PublicOutputGuardAction = "replace"
	PublicOutputGuardBlock   PublicOutputGuardAction = "block"
)

type PublicOutputGuardInput struct {
	EndpointKind endpointprotocol.EndpointKind
	Texts        []endpointprotocol.PublicText
}

type PublicOutputGuardDecision struct {
	Action       PublicOutputGuardAction
	Replacements []string
}

// PublicNonStreamOutputGuard receives only the strict, normalized text leaves.
// It cannot inspect or return raw provider bytes.
type PublicNonStreamOutputGuard interface {
	Evaluate(context.Context, PublicOutputGuardInput) (PublicOutputGuardDecision, error)
}

// PublicNonStreamExecutor is the narrow provider-I/O leaf for the M77 public
// path. It has no Router, adapter registry, response cache, planner, ledger or
// billing dependency. Those authorities must complete before/after this leaf
// in the gateway orchestration layer.
type PublicNonStreamExecutor struct {
	doer             PublicNonStreamHTTPDoer
	outputGuard      PublicNonStreamOutputGuard
	maxResponseBytes int64
}

func NewPublicNonStreamExecutor(
	doer PublicNonStreamHTTPDoer,
	outputGuard PublicNonStreamOutputGuard,
) (*PublicNonStreamExecutor, error) {
	if doer == nil || outputGuard == nil {
		return nil, ErrPublicNonStreamExecutorInvalid
	}
	return &PublicNonStreamExecutor{
		doer:             doer,
		outputGuard:      outputGuard,
		maxResponseBytes: endpointprotocol.PublicNonStreamMaxResponseBytes,
	}, nil
}

// When Execute also returns a non-nil output-guard error, Delivery is a
// provider-evidence carrier only. Callers may read CustomerUsage to retain the
// accepted provider liability, but must not prepare, record, settle, cache, or
// write that body to a customer.
type PublicNonStreamExecution struct {
	StatusCode int
	Delivery   endpointprotocol.PublicDelivery
}

func (execution PublicNonStreamExecution) CachePolicy() string {
	return endpointprotocol.PublicCachePolicyBypass
}

func (execution PublicNonStreamExecution) ProviderHealthAction() PublicNonStreamHealthAction {
	return PublicNonStreamHealthSuccess
}

func (executor *PublicNonStreamExecutor) Execute(
	ctx context.Context,
	upstream *endpointprotocol.PublicUpstreamRequest,
) (PublicNonStreamExecution, error) {
	if executor == nil || executor.doer == nil || executor.outputGuard == nil ||
		executor.maxResponseBytes <= 0 || upstream == nil {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStagePreflight,
			PublicNonStreamSourceGateway,
			0,
			"executor_invalid",
			false,
			ErrPublicNonStreamExecutorInvalid,
		)
	}
	request, err := upstream.NewHTTPRequest(ctx)
	if err != nil {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStagePreflight,
			PublicNonStreamSourceGateway,
			0,
			"request_invalid",
			false,
			err,
		)
	}
	finishMeasurement := executionmeasurement.HTTPStart(ctx, "")
	response, err := executor.doer.Do(request)
	observedHTTPStatus := 0
	if response != nil {
		observedHTTPStatus = response.StatusCode
	}
	finishMeasurement(observedHTTPStatus)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		source, failureKind, retryable, sanitized := classifyPublicNonStreamTransport(ctx, err)
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderNetwork, source, 0, failureKind, retryable, sanitized,
		)
	}
	if response == nil {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderNetwork,
			PublicNonStreamSourceProviderTransport,
			0,
			"connection",
			true,
			ErrPublicNonStreamTransport,
		)
	}
	providerStatus := sanitizePublicProviderStatus(response.StatusCode)
	// Once a valid non-200 status is available it is the authoritative provider
	// outcome. Error-body read failures or oversized diagnostic bodies must not
	// replace auth, rate-limit, client-request, or channel-failure classification.
	if providerStatus != 0 && providerStatus != http.StatusOK {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		source, failureKind, retryable := publicNonStreamProviderFailure(upstream, providerStatus)
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderContract,
			source,
			providerStatus,
			failureKind,
			retryable,
			endpointprotocol.ErrUpstreamResponse,
		)
	}
	if response.Body == nil {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderNetwork,
			PublicNonStreamSourceProviderTransport,
			0,
			"connection",
			true,
			ErrPublicNonStreamTransport,
		)
	}
	defer response.Body.Close()
	if providerStatus == 0 {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderContract,
			PublicNonStreamSourceProviderResponse,
			0,
			"invalid_status",
			true,
			endpointprotocol.ErrInvalidResponse,
		)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, executor.maxResponseBytes+1))
	if err != nil {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderContract,
			PublicNonStreamSourceProviderResponse,
			providerStatus,
			"response_read",
			true,
			ErrPublicNonStreamResponseRead,
		)
	}
	if int64(len(body)) > executor.maxResponseBytes {
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderContract,
			PublicNonStreamSourceProviderResponse,
			providerStatus,
			"response_too_large",
			true,
			ErrPublicNonStreamResponseTooLarge,
		)
	}
	delivery, err := upstream.ParseDelivery(ctx, providerStatus, body)
	if err != nil {
		source, failureKind, retryable := publicNonStreamProviderFailure(upstream, providerStatus)
		return PublicNonStreamExecution{}, publicNonStreamFailure(
			PublicNonStreamStageProviderContract,
			source,
			providerStatus,
			failureKind,
			retryable,
			err,
		)
	}
	// From this point onward the provider response and customer usage have
	// passed the exact public delivery contract. Preserve that normalized
	// authority on local output-policy failures so the orchestrator can Hold
	// the provider liability without retaining or exposing raw provider bytes.
	parsed := PublicNonStreamExecution{StatusCode: http.StatusOK, Delivery: delivery}

	decision, err := executor.outputGuard.Evaluate(ctx, PublicOutputGuardInput{
		EndpointKind: delivery.EndpointKind(),
		Texts:        delivery.Texts(),
	})
	if err != nil {
		return parsed, publicNonStreamFailure(
			PublicNonStreamStageOutputGuardrail,
			PublicNonStreamSourceLocalPolicy,
			http.StatusOK,
			"output_guard_error",
			false,
			ErrPublicNonStreamOutputGuard,
		)
	}
	switch decision.Action {
	case PublicOutputGuardAllow:
		if len(decision.Replacements) != 0 {
			return parsed, publicNonStreamFailure(
				PublicNonStreamStageOutputGuardrail,
				PublicNonStreamSourceLocalPolicy,
				http.StatusOK,
				"output_guard_invalid",
				false,
				ErrPublicNonStreamOutputGuard,
			)
		}
	case PublicOutputGuardReplace:
		delivery, err = delivery.WithTexts(decision.Replacements)
		if err != nil {
			return parsed, publicNonStreamFailure(
				PublicNonStreamStageOutputGuardrail,
				PublicNonStreamSourceLocalPolicy,
				http.StatusOK,
				"output_guard_invalid",
				false,
				ErrPublicNonStreamOutputGuard,
			)
		}
		guardrail.MarkSourceReplacement(ctx, guardrail.StageNonStreamOutput)
	case PublicOutputGuardBlock:
		return parsed, publicNonStreamFailure(
			PublicNonStreamStageOutputGuardrail,
			PublicNonStreamSourceLocalPolicy,
			http.StatusOK,
			"output_withheld",
			false,
			ErrPublicNonStreamOutputWithheld,
		)
	default:
		return parsed, publicNonStreamFailure(
			PublicNonStreamStageOutputGuardrail,
			PublicNonStreamSourceLocalPolicy,
			http.StatusOK,
			"output_guard_invalid",
			false,
			ErrPublicNonStreamOutputGuard,
		)
	}
	// A successfully parsed public non-stream delivery is always a synchronous
	// HTTP 200 contract. Provider-specific 2xx statuses were rejected above and
	// are never reflected to the customer boundary.
	parsed.Delivery = delivery
	return parsed, nil
}

func publicNonStreamFailure(
	stage PublicNonStreamFailureStage,
	source PublicNonStreamFailureSource,
	providerStatus int,
	failureKind string,
	retryable bool,
	err error,
) error {
	return &PublicNonStreamExecutionError{
		Stage: stage, Source: source, ProviderStatus: providerStatus,
		FailureKind: failureKind, Retryable: retryable, err: err,
	}
}

func classifyPublicNonStreamTransport(
	ctx context.Context,
	err error,
) (PublicNonStreamFailureSource, string, bool, error) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return PublicNonStreamSourceClient, "client_cancelled", false, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return PublicNonStreamSourceProviderTransport, "deadline", true, context.DeadlineExceeded
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return PublicNonStreamSourceProviderTransport, "deadline", true, context.DeadlineExceeded
	}
	return PublicNonStreamSourceProviderTransport, "connection", true, ErrPublicNonStreamTransport
}

func sanitizePublicProviderStatus(status int) int {
	if status < 100 || status > 599 {
		return 0
	}
	return status
}

func publicNonStreamProviderFailure(
	upstream *endpointprotocol.PublicUpstreamRequest,
	providerStatus int,
) (PublicNonStreamFailureSource, string, bool) {
	if providerStatus == http.StatusOK {
		return PublicNonStreamSourceProviderResponse, "invalid_response", true
	}
	if providerStatus >= http.StatusOK && providerStatus < http.StatusMultipleChoices {
		return PublicNonStreamSourceProviderResponse, "unexpected_success_status", true
	}
	failureKind, retryable := upstream.ClassifyHTTPStatus(providerStatus)
	return PublicNonStreamSourceProviderHTTP, failureKind, retryable
}
