package mediajob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const maxProviderResponseBytes = endpointprotocol.MaxOpenAIImageResponseBytes

type ImageRouter interface {
	RouteAllWithKey(context.Context, string, string) ([]gateway.RouteResult, error)
	MarkChannelSuccess(context.Context, int64) error
	MarkChannelFail(context.Context, int64) error
	MarkKeySuccess(context.Context, int64) error
	MarkKeyRateLimited(context.Context, int64) error
	MarkKeyAuthFailure(context.Context, int64) error
	MarkKeyFail(context.Context, int64) error
}

type imageProviderRoute struct {
	provider, baseURL, encryptedKey, mappedModel string
	profileID, profileVersion, releaseRef        string
	legacyChannelID, legacyKeyID                 int64
	canonical                                    *executionevidence.EvidenceV2
}

type GPTImageProvider struct {
	contentObserver  MediaContentObserver
	sourceObserver   SourceMediaProviderObserver
	router           ImageRouter
	profile          endpointprotocol.Profile
	httpClient       *http.Client
	singleSendClient *http.Client
	encKey           []byte
	timeout          time.Duration
}

type GPTImageProviderOptions struct {
	HTTPClient *http.Client
	Timeout    time.Duration
}

func NewGPTImageProvider(
	router ImageRouter,
	encKey []byte,
	options GPTImageProviderOptions,
) (*GPTImageProvider, error) {
	if router == nil || len(encKey) != 32 {
		return nil, domainError(ErrUnavailable, "image_provider_dependency_invalid")
	}
	profile, err := endpointprotocol.NewRegistry().Get(endpointprotocol.ProfileOpenAIImageGPT)
	if err != nil {
		return nil, fmt.Errorf("mediajob: load image Endpoint Profile: %w", err)
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{
			Transport: http.DefaultTransport,
		}
	}
	httpClient := *options.HTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	var singleSendClient *http.Client
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	// A custom RoundTripper may replay internally. Only a standard Transport
	// can be constrained for the single provider submission contract.
	if standard, ok := transport.(*http.Transport); ok && standard != nil && standard.DialTLSContext == nil && standard.DialTLS == nil {
		bounded := standard.Clone()
		adapters.ConfigureSingleSendTransport(bounded)
		single := httpClient
		single.Transport = bounded
		singleSendClient = &single
	}
	if options.Timeout == 0 {
		options.Timeout = 3 * time.Minute
	}
	if options.Timeout < time.Second || options.Timeout > 15*time.Minute {
		return nil, domainError(ErrInvalid, "provider_timeout_invalid")
	}
	return &GPTImageProvider{
		router: router, profile: profile, httpClient: &httpClient, singleSendClient: singleSendClient,
		encKey: append([]byte(nil), encKey...), timeout: options.Timeout,
	}, nil
}

func (p *GPTImageProvider) Generate(
	ctx context.Context,
	job Job,
	request NormalizedRequest,
) (ProviderResult, error) {
	singleSend := job.SingleUpstreamAttempt
	if singleSend && (job.MaxAttempts != 1 || job.Attempt != 1 || job.Lifecycle.Outcome != OutcomePending || job.Lifecycle.Execution == ExecutionTerminal) {
		return ProviderResult{}, &ProviderError{Code: "single_submission_requires_first_dispatch"}
	}
	if singleSend && p.singleSendClient == nil {
		return ProviderResult{}, &ProviderError{Code: "single_submission_transport_unavailable"}
	}
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	requestBody, err := json.Marshal(request)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "request_encoding_failed"}
	}
	if job.CanonicalRoute != nil || job.dispatchRoute != nil {
		route := job.dispatchRoute
		if job.CanonicalRoute == nil || job.CanonicalDispatch == nil || route == nil || route.PublicEvidence == nil ||
			!job.SingleUpstreamAttempt || !modelprofile.SupportsMediaJobBinding(job.ProfileID, route.Provider, route.UpstreamModel) ||
			route.PublicEvidence.ProtocolProfileID != job.ProfileID ||
			adapters.NormalizeType(route.AdapterType) != adapters.TypeOpenAICompatible {
			return ProviderResult{}, &ProviderError{Code: "canonical_media_dispatch_binding_invalid"}
		}
		accepted, acceptedErr := executionevidence.DigestV2(*job.CanonicalDispatch)
		dispatched, dispatchedErr := executionevidence.DigestV2(*route.PublicEvidence)
		if acceptedErr != nil || dispatchedErr != nil || accepted != dispatched {
			return ProviderResult{}, &ProviderError{Code: "canonical_media_dispatch_binding_invalid"}
		}
		return p.generateRoute(callCtx, job, requestBody, imageProviderRoute{
			provider: route.Provider, baseURL: route.BaseURL, encryptedKey: route.EncryptedSecret, mappedModel: route.UpstreamModel,
			profileID: route.profileID, profileVersion: route.profileVersion,
			releaseRef: route.PublicEvidence.RuntimeReleaseRef, canonical: route.PublicEvidence,
		}, 1, 1)
	}
	routes, err := p.router.RouteAllWithKey(
		callCtx,
		job.RoutingModel,
		"async-media:"+job.JobID+":"+fmt.Sprintf("%d", job.Attempt),
	)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "route_unavailable", Retryable: true}
	}
	var lastErr error
	for index, route := range routes {
		if adapters.NormalizeType(route.Channel.AdapterType) != adapters.TypeOpenAICompatible {
			lastErr = &ProviderError{Code: "endpoint_adapter_unsupported"}
			continue
		}
		result, generateErr := p.generateRoute(callCtx, job, requestBody, imageProviderRoute{
			provider: route.Channel.Provider, baseURL: route.Channel.BaseURL, encryptedKey: route.ChannelKey.KeyEnc, mappedModel: route.MappedModel,
			profileID: route.ExecutionProfileID, profileVersion: route.ExecutionProfileVersion, releaseRef: route.RuntimeReleaseRef,
			legacyChannelID: route.Channel.ID, legacyKeyID: route.ChannelKey.ID,
		}, index+1, len(routes))
		if generateErr == nil {
			_ = p.router.MarkChannelSuccess(ctx, route.Channel.ID)
			_ = p.router.MarkKeySuccess(ctx, route.ChannelKey.ID)
			return result, nil
		}
		lastErr = generateErr
		var providerErr *ProviderError
		if singleSend || !errors.As(generateErr, &providerErr) || !providerErr.Retryable {
			break
		}
	}
	if lastErr == nil {
		lastErr = &ProviderError{Code: "compatible_route_unavailable", Retryable: true}
	}
	return ProviderResult{}, lastErr
}

func (p *GPTImageProvider) generateRoute(
	ctx context.Context,
	job Job,
	requestBody []byte,
	route imageProviderRoute,
	sequence, total int,
) (ProviderResult, error) {
	apiKey, err := auth.DecryptKey(route.encryptedKey, p.encKey)
	if err != nil {
		if route.canonical == nil {
			_ = p.router.MarkKeyFail(ctx, route.legacyKeyID)
		}
		return ProviderResult{}, &ProviderError{Code: "provider_key_unavailable"}
	}
	defer clearString(&apiKey)
	profile := p.profile
	if route.canonical != nil && job.ProfileID != profile.Descriptor().ProfileID {
		profile, err = endpointprotocol.NewRegistry().Resolve(job.ProfileID, route.canonical.ProtocolProfileVersion)
		if err != nil {
			return ProviderResult{}, &ProviderError{Code: "canonical_media_dispatch_binding_invalid"}
		}
	}
	upstreamRequest, binding, err := profile.BuildRequest(
		ctx,
		endpointprotocol.BuildInput{
			BaseURL: route.baseURL, APIKey: apiKey,
			RequestID: job.JobID, RequestedModel: job.RequestedModel,
			MappedModel: route.mappedModel, Body: requestBody, LegacyImageDelivery: route.canonical == nil,
		},
	)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err)}
	}
	var observation SourceMediaRoute
	if p.SourceObservationsEnabled() || p.contentObserver != nil {
		descriptor := profile.Descriptor()
		observation = SourceMediaRoute{Sequence: sequence, Total: total, ChannelID: route.legacyChannelID, ChannelKeyID: route.legacyKeyID, Provider: route.provider, Origin: upstreamRequest.URL.Scheme + "://" + upstreamRequest.URL.Host, Model: job.RoutingModel, Operation: job.Operation, ProtocolProfile: descriptor.ProfileID, ProtocolVersion: descriptor.ProfileVersion, Surface: descriptor.UpstreamEndpoint, ExecutionProfile: route.profileID, ExecutionVersion: route.profileVersion, ReleaseRef: route.releaseRef, Canonical: route.canonical}
	}
	if p.SourceObservationsEnabled() {
		if err := p.sourceObserver.BeforeMediaRoute(ctx, job, observation); err != nil {
			return ProviderResult{}, &ProviderError{Code: "source_dispatch_unavailable"}
		}
	}
	observeResponse := func(*http.Response, error) {}
	if p.contentObserver != nil {
		upstreamRequest, observeResponse = p.contentObserver.ObserveMediaProvider(ctx, job, observation, upstreamRequest)
	}
	var started time.Time
	if p.SourceObservationsEnabled() {
		started = time.Now()
	}
	client := p.httpClient
	if job.SingleUpstreamAttempt {
		upstreamRequest.GetBody = nil
		client = p.singleSendClient
	}
	response, err := client.Do(upstreamRequest)
	if p.SourceObservationsEnabled() {
		duration := time.Since(started).Nanoseconds()
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		recordSourceMediaHTTP(ctx, SourceMediaHTTP{Route: observation, Status: status, RoundTripNanoseconds: duration, ObservedAt: time.Now()})
	}
	observeResponse(response, err)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProviderResult{}, &ProviderError{
				Code: "provider_timeout", OutcomeUnknown: true,
			}
		}
		if route.canonical == nil {
			_ = p.router.MarkChannelFail(ctx, route.legacyChannelID)
		}
		return ProviderResult{}, &ProviderError{
			Code: "provider_transport_outcome_unknown", OutcomeUnknown: true,
		}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxProviderResponseBytes {
		if route.canonical == nil {
			_ = p.router.MarkChannelFail(ctx, route.legacyChannelID)
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return ProviderResult{}, &ProviderError{
				Code: "provider_success_response_unusable", OutcomeUnknown: true,
			}
		}
		failureKind, retryable := profile.ClassifyHTTPStatus(response.StatusCode)
		return ProviderResult{}, &ProviderError{
			Code: fmt.Sprintf(
				"provider_http_%d_%s_body_unusable",
				response.StatusCode,
				failureKind,
			),
			Retryable: retryable,
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failureKind, retryable := profile.ClassifyHTTPStatus(response.StatusCode)
		switch failureKind {
		case "auth":
			if route.canonical == nil {
				_ = p.router.MarkKeyAuthFailure(ctx, route.legacyKeyID)
			}
		case "rate_limit":
			if route.canonical == nil {
				_ = p.router.MarkKeyRateLimited(ctx, route.legacyKeyID)
			}
		case "channel_failure":
			if route.canonical == nil {
				_ = p.router.MarkChannelFail(ctx, route.legacyChannelID)
			}
		}
		return ProviderResult{}, &ProviderError{
			Code:      fmt.Sprintf("provider_http_%d_%s", response.StatusCode, failureKind),
			Retryable: retryable,
		}
	}
	outcome, err := profile.ParseResponse(ctx, binding, response.StatusCode, body)
	if err != nil {
		if route.canonical == nil {
			_ = p.router.MarkChannelFail(ctx, route.legacyChannelID)
		}
		return ProviderResult{}, &ProviderError{
			Code: protocolErrorCode(err), OutcomeUnknown: true,
		}
	}
	if taskID := outcome.MediaTaskReference(); taskID != "" && route.canonical != nil {
		return ProviderResult{TaskID: taskID}, nil
	}
	if outcome.ProviderState == endpointprotocol.MediaTaskFailed {
		return ProviderResult{}, &ProviderError{Code: "provider_task_failed"}
	}
	if remote, ok := profile.(endpointprotocol.RemoteMediaProfile); ok && route.canonical != nil {
		outcome.Media, err = endpointprotocol.DownloadRemoteMedia(ctx, p.httpClient, remote, outcome.ResultURL)
		outcome.ResultURL = ""
		if err != nil {
			return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err), OutcomeUnknown: true}
		}
	}
	if outcome.Media == nil || outcome.Media.MediaType == "" {
		return ProviderResult{}, &ProviderError{Code: "provider_media_missing", OutcomeUnknown: true}
	}
	media := outcome.Media
	outcome.Media = nil
	return ProviderResult{
		MappedModel: route.mappedModel,
		Bytes:       media.Bytes, MediaType: media.MediaType, Outcome: outcome,
	}, nil
}

func protocolErrorCode(err error) string {
	var protocolErr *endpointprotocol.ProtocolError
	if errors.As(err, &protocolErr) && protocolErr.Code != "" {
		return "endpoint_" + sanitizeErrorCode(protocolErr.Code)
	}
	return "endpoint_contract_failed"
}

func clearString(value *string) {
	if value == nil || *value == "" {
		return
	}
	*value = strings.Repeat("\x00", len(*value))
	*value = ""
}
