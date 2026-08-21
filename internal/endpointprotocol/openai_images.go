package endpointprotocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strings"
)

const (
	// MaxOpenAIImageBinaryBytes is the repository Image V1 output ceiling.
	// Base64 expands by roughly 4/3; one additional MiB bounds the JSON usage
	// and data envelope without silently lowering the binary limit.
	MaxOpenAIImageBinaryBytes   = 32 << 20
	MaxOpenAIImageResponseBytes = ((MaxOpenAIImageBinaryBytes + 2) / 3 * 4) + (1 << 20)
)

type openAIImageProfile struct{}

type openAIImageRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	N            *int64 `json:"n,omitempty"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Stream       *bool  `json:"stream,omitempty"`
}

type openAIImageWireRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	N            int64  `json:"n"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Stream       bool   `json:"stream"`
}

type openAIImageResponse struct {
	Created json.Number       `json:"created"`
	Data    []openAIImageData `json:"data"`
	Usage   *openAIImageUsage `json:"usage"`
}

type openAIImageData struct {
	B64JSON *string `json:"b64_json"`
	URL     *string `json:"url"`
}

type openAIImageUsage struct {
	InputTokens       json.Number              `json:"input_tokens"`
	OutputTokens      json.Number              `json:"output_tokens"`
	TotalTokens       json.Number              `json:"total_tokens"`
	InputTokenDetails *openAIImageInputDetails `json:"input_tokens_details"`
}

type openAIImageInputDetails struct {
	TextTokens  json.Number `json:"text_tokens"`
	ImageTokens json.Number `json:"image_tokens"`
}

func (openAIImageProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion:     ContractVersion,
		ProfileID:           ProfileOpenAIImageGPT,
		ProfileVersion:      "1.0.0",
		Provider:            "openai",
		EndpointKind:        EndpointImageGeneration,
		DownstreamEndpoint:  "/v1/images/generations",
		UpstreamEndpoint:    "/v1/images/generations",
		Authentication:      "bearer",
		RuntimeState:        RuntimeRepositoryImageV1,
		ProviderFinality:    "synchronous_single_image_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly,
		SupportedModalities: []string{"text_to_image"},
		ObservedUnitClasses: []UnitClass{
			UnitInputTokens,
			UnitInputTextTokens,
			UnitInputImageTokens,
			UnitOutputImageTokens,
		},
		UnsupportedSemantics: []string{
			"image_edit_or_variation",
			"partial_images",
			"provider_hosted_url",
			"streaming",
			"multiple_images",
		},
	}
}

func (profile openAIImageProfile) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectKnownUnsupported(
		input.Body,
		"moderation",
		"output_compression",
		"partial_images",
		"response_format",
		"user",
	); err != nil {
		return nil, nil, err
	}
	var downstream openAIImageRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel {
		return nil, nil, protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	if !strings.HasPrefix(input.MappedModel, "gpt-image-") {
		return nil, nil, protocolError(ErrInvalidConfiguration, "mapped_model_not_gpt_image")
	}
	if strings.TrimSpace(downstream.Prompt) == "" || len(downstream.Prompt) > 32_000 {
		return nil, nil, protocolError(ErrInvalidRequest, "image_prompt_invalid")
	}
	if downstream.N != nil && *downstream.N != 1 {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_multiple_images")
	}
	if downstream.Stream != nil && *downstream.Stream {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_streaming")
	}
	if !oneOfOrEmpty(downstream.Size, "auto", "1024x1024", "1536x1024", "1024x1536") {
		return nil, nil, protocolError(ErrInvalidRequest, "image_size_invalid")
	}
	if !oneOfOrEmpty(downstream.Quality, "auto", "low", "medium", "high") {
		return nil, nil, protocolError(ErrInvalidRequest, "image_quality_invalid")
	}
	if !oneOfOrEmpty(downstream.Background, "auto", "transparent", "opaque") {
		return nil, nil, protocolError(ErrInvalidRequest, "image_background_invalid")
	}
	if !oneOfOrEmpty(downstream.OutputFormat, "png", "jpeg", "webp") {
		return nil, nil, protocolError(ErrInvalidRequest, "image_output_format_invalid")
	}
	wireBody, marshalErr := json.Marshal(openAIImageWireRequest{
		Model:        input.MappedModel,
		Prompt:       downstream.Prompt,
		N:            1,
		Size:         downstream.Size,
		Quality:      downstream.Quality,
		Background:   downstream.Background,
		OutputFormat: downstream.OutputFormat,
		Stream:       false,
	})
	if marshalErr != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, err := buildPOSTRequest(
		ctx,
		baseURL,
		"/v1/images/generations",
		input.APIKey,
		input.RequestID,
		wireBody,
	)
	if err != nil {
		return nil, nil, err
	}
	return request, &Binding{
		profileID:      profile.Descriptor().ProfileID,
		profileVersion: profile.Descriptor().ProfileVersion,
		requestedModel: input.RequestedModel,
		mappedModel:    input.MappedModel,
		outputCount:    1,
	}, nil
}

func (profile openAIImageProfile) ParseResponse(
	_ context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	if err := requireBinding(binding, profile.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(statusCode); err != nil {
		return Outcome{}, err
	}
	var response openAIImageResponse
	if err := decodeResponseWithLimit(
		body,
		&response,
		MaxOpenAIImageResponseBytes,
	); err != nil {
		return Outcome{}, err
	}
	if _, err := parseNonnegativeInt64(response.Created, "created"); err != nil {
		return Outcome{}, err
	}
	if len(response.Data) != binding.outputCount {
		return Outcome{}, protocolError(ErrInvalidResponse, "image_count_invalid")
	}
	for _, data := range response.Data {
		if data.URL != nil {
			return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_provider_hosted_url")
		}
		if data.B64JSON == nil || *data.B64JSON == "" {
			return Outcome{}, protocolError(ErrInvalidResponse, "image_base64_missing")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(*data.B64JSON)
		if err != nil || len(decoded) == 0 ||
			len(decoded) > MaxOpenAIImageBinaryBytes {
			return Outcome{}, protocolError(ErrInvalidResponse, "image_base64_invalid")
		}
	}
	if response.Usage == nil || response.Usage.InputTokenDetails == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	inputTokens, err := parseNonnegativeInt64(response.Usage.InputTokens, string(UnitInputTokens))
	if err != nil {
		return Outcome{}, err
	}
	outputTokens, err := parseNonnegativeInt64(response.Usage.OutputTokens, string(UnitOutputImageTokens))
	if err != nil {
		return Outcome{}, err
	}
	totalTokens, err := parseNonnegativeInt64(response.Usage.TotalTokens, "total_tokens")
	if err != nil || inputTokens > math.MaxInt64-outputTokens || inputTokens+outputTokens != totalTokens {
		return Outcome{}, protocolError(ErrInvalidResponse, "usage_total_mismatch")
	}
	textTokens, err := parseNonnegativeInt64(
		response.Usage.InputTokenDetails.TextTokens,
		string(UnitInputTextTokens),
	)
	if err != nil {
		return Outcome{}, err
	}
	imageTokens, err := parseNonnegativeInt64(
		response.Usage.InputTokenDetails.ImageTokens,
		string(UnitInputImageTokens),
	)
	if err != nil || textTokens > math.MaxInt64-imageTokens || textTokens+imageTokens != inputTokens {
		return Outcome{}, protocolError(ErrInvalidResponse, "image_input_partition_mismatch")
	}
	return Outcome{
		ContractVersion: ContractVersion,
		ProfileID:       profile.Descriptor().ProfileID,
		ProviderState:   "completed",
		MappedModel:     binding.mappedModel,
		OutputCount:     len(response.Data),
		Units: []UnitObservation{
			observedUnit(UnitInputTokens, response.Usage.InputTokens.String()),
			observedUnit(UnitInputTextTokens, response.Usage.InputTokenDetails.TextTokens.String()),
			observedUnit(UnitInputImageTokens, response.Usage.InputTokenDetails.ImageTokens.String()),
			observedUnit(UnitOutputImageTokens, response.Usage.OutputTokens.String()),
		},
	}, nil
}

func (openAIImageProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return classifyHTTPStatus(statusCode)
}

func oneOfOrEmpty(value string, options ...string) bool {
	if value == "" {
		return true
	}
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
