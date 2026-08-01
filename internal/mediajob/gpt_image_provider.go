package mediajob

import (
	"context"
	"encoding/base64"
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
	"github.com/uniroute/unirouter/internal/gateway"
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

type GPTImageProvider struct {
	router     ImageRouter
	profile    endpointprotocol.Profile
	httpClient *http.Client
	encKey     []byte
	timeout    time.Duration
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
	if options.Timeout == 0 {
		options.Timeout = 3 * time.Minute
	}
	if options.Timeout < time.Second || options.Timeout > 15*time.Minute {
		return nil, domainError(ErrInvalid, "provider_timeout_invalid")
	}
	return &GPTImageProvider{
		router: router, profile: profile, httpClient: &httpClient,
		encKey: append([]byte(nil), encKey...), timeout: options.Timeout,
	}, nil
}

func (p *GPTImageProvider) Generate(
	ctx context.Context,
	job Job,
	request NormalizedRequest,
) (ProviderResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	requestBody, err := json.Marshal(request)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: "request_encoding_failed"}
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
	for _, route := range routes {
		if adapters.NormalizeType(route.Channel.AdapterType) != adapters.TypeOpenAICompatible {
			lastErr = &ProviderError{Code: "endpoint_adapter_unsupported"}
			continue
		}
		result, generateErr := p.generateRoute(callCtx, job, requestBody, route)
		if generateErr == nil {
			_ = p.router.MarkChannelSuccess(ctx, route.Channel.ID)
			_ = p.router.MarkKeySuccess(ctx, route.ChannelKey.ID)
			return result, nil
		}
		lastErr = generateErr
		var providerErr *ProviderError
		if !errors.As(generateErr, &providerErr) || !providerErr.Retryable {
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
	route gateway.RouteResult,
) (ProviderResult, error) {
	apiKey, err := auth.DecryptKey(route.ChannelKey.KeyEnc, p.encKey)
	if err != nil {
		_ = p.router.MarkKeyFail(ctx, route.ChannelKey.ID)
		return ProviderResult{}, &ProviderError{Code: "provider_key_unavailable"}
	}
	defer clearString(&apiKey)
	upstreamRequest, binding, err := p.profile.BuildRequest(
		ctx,
		endpointprotocol.BuildInput{
			BaseURL: route.Channel.BaseURL, APIKey: apiKey,
			RequestID: job.JobID, RequestedModel: job.RequestedModel,
			MappedModel: route.MappedModel, Body: requestBody,
		},
	)
	if err != nil {
		return ProviderResult{}, &ProviderError{Code: protocolErrorCode(err)}
	}
	response, err := p.httpClient.Do(upstreamRequest)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProviderResult{}, &ProviderError{
				Code: "provider_timeout", OutcomeUnknown: true,
			}
		}
		_ = p.router.MarkChannelFail(ctx, route.Channel.ID)
		return ProviderResult{}, &ProviderError{
			Code: "provider_transport_outcome_unknown", OutcomeUnknown: true,
		}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxProviderResponseBytes {
		_ = p.router.MarkChannelFail(ctx, route.Channel.ID)
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return ProviderResult{}, &ProviderError{
				Code: "provider_success_response_unusable", OutcomeUnknown: true,
			}
		}
		failureKind, retryable := p.profile.ClassifyHTTPStatus(response.StatusCode)
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
		failureKind, retryable := p.profile.ClassifyHTTPStatus(response.StatusCode)
		switch failureKind {
		case "auth":
			_ = p.router.MarkKeyAuthFailure(ctx, route.ChannelKey.ID)
		case "rate_limit":
			_ = p.router.MarkKeyRateLimited(ctx, route.ChannelKey.ID)
		case "channel_failure":
			_ = p.router.MarkChannelFail(ctx, route.Channel.ID)
		}
		return ProviderResult{}, &ProviderError{
			Code:      fmt.Sprintf("provider_http_%d_%s", response.StatusCode, failureKind),
			Retryable: retryable,
		}
	}
	outcome, err := p.profile.ParseResponse(ctx, binding, response.StatusCode, body)
	if err != nil {
		_ = p.router.MarkChannelFail(ctx, route.Channel.ID)
		return ProviderResult{}, &ProviderError{
			Code: protocolErrorCode(err), OutcomeUnknown: true,
		}
	}
	imageBytes, mediaType, err := extractSingleImage(body)
	if err != nil {
		_ = p.router.MarkChannelFail(ctx, route.Channel.ID)
		var providerErr *ProviderError
		if errors.As(err, &providerErr) {
			providerErr.OutcomeUnknown = true
		}
		return ProviderResult{}, err
	}
	return ProviderResult{
		MappedModel: route.MappedModel,
		Bytes:       imageBytes, MediaType: mediaType, Outcome: outcome,
	}, nil
}

func extractSingleImage(body []byte) ([]byte, string, error) {
	var response struct {
		Data []struct {
			B64JSON *string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil ||
		len(response.Data) != 1 ||
		response.Data[0].B64JSON == nil {
		return nil, "", &ProviderError{Code: "image_response_invalid"}
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(*response.Data[0].B64JSON)
	if err != nil || len(decoded) == 0 || len(decoded) > MaxOutputBytes {
		return nil, "", &ProviderError{Code: "image_bytes_invalid"}
	}
	mediaType := detectImageMediaType(decoded)
	if mediaType == "" {
		return nil, "", &ProviderError{Code: "image_media_type_invalid"}
	}
	return decoded, mediaType, nil
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
