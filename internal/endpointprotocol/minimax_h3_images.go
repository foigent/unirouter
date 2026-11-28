package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
)

const (
	MiniMaxH3Image          = "MiniMax-H3-Image"
	MiniMaxChinaImageOrigin = "https://api.minimax.cn"
	MiniMaxImagePromptBytes = 4096
)

type miniMaxH3ImageProfile struct{}

func (miniMaxH3ImageProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileMiniMaxH3Image, ProfileVersion: "1.0.0", Provider: "minimax",
		EndpointKind: EndpointImageGeneration, DownstreamEndpoint: "/v1/images/generations", UpstreamEndpoint: "/v1/images/generations",
		Authentication: "bearer", RuntimeState: RuntimeRepositoryImageV1, ProviderFinality: "synchronous_single_image_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly, SupportedModalities: []string{"text_to_image"},
		ObservedUnitClasses:  []UnitClass{UnitInputTokens, UnitInputTextTokens, UnitInputImageTokens, UnitOutputImageTokens, UnitOutputImages},
		UnsupportedSemantics: []string{"image_edit_or_variation", "partial_images", "provider_hosted_url", "streaming", "multiple_images"},
	}
}

func (p miniMaxH3ImageProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != MiniMaxChinaImageOrigin || input.MappedModel != MiniMaxH3Image {
		return nil, nil, protocolError(ErrInvalidConfiguration, "minimax_image_binding_invalid")
	}
	var downstream openAIImageRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel ||
		strings.TrimSpace(downstream.Prompt) == "" || len(downstream.Prompt) > MiniMaxImagePromptBytes {
		return nil, nil, protocolError(ErrInvalidRequest, "minimax_image_request_invalid")
	}
	if downstream.N != nil && *downstream.N != 1 {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_multiple_images")
	}
	if downstream.Stream != nil && *downstream.Stream {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_streaming")
	}
	if !SupportsSquarePNGImageControls(downstream.Size, downstream.Quality, downstream.Background, downstream.OutputFormat) {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "minimax_image_controls_unsupported")
	}
	wire, err := json.Marshal(struct {
		Model                 string `json:"model"`
		Prompt                string `json:"prompt"`
		N                     int    `json:"n"`
		Size                  string `json:"size"`
		ResponseFormat        string `json:"response_format"`
		OutputFormat          string `json:"output_format"`
		DisablePromptEnhanced bool   `json:"disable_prompt_enhanced"`
		Stream                bool   `json:"stream"`
	}{input.MappedModel, downstream.Prompt, 1, "1024x1024", "b64_json", "png", true, false})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	d := p.Descriptor()
	req, err := buildPOSTRequest(ctx, baseURL, d.UpstreamEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	return req, &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion,
		requestedModel: input.RequestedModel, mappedModel: input.MappedModel, outputCount: 1}, nil
}

func (p miniMaxH3ImageProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var response struct {
		Model        string      `json:"model"`
		Created      json.Number `json:"created"`
		OutputFormat string      `json:"output_format"`
		Data         []struct {
			B64JSON *string         `json:"b64_json"`
			URL     *string         `json:"url"`
			Size    string          `json:"size"`
			Error   json.RawMessage `json:"error"`
		} `json:"data"`
		Usage *struct {
			GeneratedImages json.Number `json:"generated_images"`
			openAIImageUsage
			OutputTokensDetails *struct {
				ImageTokens json.Number `json:"image_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &response, MaxOpenAIImageResponseBytes); err != nil {
		return Outcome{}, err
	}
	if _, err := parseNonnegativeInt64(response.Created, "created"); err != nil {
		return Outcome{}, err
	}
	if response.Model != "" && response.Model != binding.mappedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "minimax_image_model_mismatch")
	}
	if len(response.Data) != binding.outputCount {
		return Outcome{}, protocolError(ErrInvalidResponse, "image_count_invalid")
	}
	image := response.Data[0]
	if image.URL != nil {
		return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_provider_hosted_url")
	}
	if len(image.Error) != 0 || response.OutputFormat != "png" || image.Size != "1024x1024" {
		return Outcome{}, protocolError(ErrInvalidResponse, "minimax_image_delivery_mismatch")
	}
	media, err := decodeSquarePNG(image.B64JSON)
	if err != nil {
		return Outcome{}, err
	}
	u := response.Usage
	if u == nil || u.InputTokenDetails == nil || u.OutputTokensDetails == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	input, inputErr := parseNonnegativeInt64(u.InputTokens, string(UnitInputTokens))
	text, textErr := parseNonnegativeInt64(u.InputTokenDetails.TextTokens, string(UnitInputTextTokens))
	inputImage, inputImageErr := parseNonnegativeInt64(u.InputTokenDetails.ImageTokens, string(UnitInputImageTokens))
	output, outputErr := parseNonnegativeInt64(u.OutputTokens, "output_tokens")
	outputImage, outputImageErr := parseNonnegativeInt64(u.OutputTokensDetails.ImageTokens, string(UnitOutputImageTokens))
	total, totalErr := parseNonnegativeInt64(u.TotalTokens, "total_tokens")
	images, imagesErr := parseNonnegativeInt64(u.GeneratedImages, string(UnitOutputImages))
	if inputErr != nil || textErr != nil || inputImageErr != nil || outputErr != nil || outputImageErr != nil || totalErr != nil || imagesErr != nil ||
		inputImage != 0 || input != text || outputImage <= 0 || output != outputImage ||
		input > math.MaxInt64-output || input+output != total || images != int64(binding.outputCount) {
		return Outcome{}, protocolError(ErrInvalidResponse, "minimax_image_usage_mismatch")
	}
	return Outcome{Media: media, ContractVersion: ContractVersion, ProfileID: binding.profileID, ProviderState: "completed", MappedModel: binding.mappedModel,
		OutputCount: binding.outputCount, Units: []UnitObservation{
			observedUnit(UnitInputTokens, u.InputTokens.String()),
			observedUnit(UnitInputTextTokens, u.InputTokenDetails.TextTokens.String()),
			observedUnit(UnitInputImageTokens, u.InputTokenDetails.ImageTokens.String()),
			observedUnit(UnitOutputImageTokens, u.OutputTokensDetails.ImageTokens.String()),
			observedUnit(UnitOutputImages, u.GeneratedImages.String()),
		}}, nil
}
func (miniMaxH3ImageProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
