package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	DashScopeQwenImage30        = "qwen-image-3.0"
	DashScopeQwenImage30Pro     = "qwen-image-3.0-pro"
	DashScopeQwenMaxPromptBytes = 4096
	DashScopeQwenImageEndpoint  = "/compatible-mode/v1/images/generations"
)

var dashScopeBeijingWorkspaceHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.cn-beijing\.maas\.aliyuncs\.com$`)

func IsDashScopeBeijingMediaOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !dashScopeBeijingWorkspaceHost.MatchString(u.Host) ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.User != nil || u.ForceQuery || u.RawQuery != "" ||
		u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	workspace, _, _ := strings.Cut(u.Host, ".")
	return workspace != "trial" && workspace != "token-plan"
}

func IsDashScopeQwenImageModel(model string) bool {
	return model == DashScopeQwenImage30 || model == DashScopeQwenImage30Pro
}

type dashScopeQwenImageProfile struct{}

func (dashScopeQwenImageProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileDashScopeQwenImage, ProfileVersion: "1.0.0", Provider: "dashscope",
		EndpointKind: EndpointImageGeneration, DownstreamEndpoint: "/v1/images/generations", UpstreamEndpoint: DashScopeQwenImageEndpoint,
		Authentication: "bearer", RuntimeState: RuntimeRepositoryImageV1, ProviderFinality: "synchronous_single_image_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly, SupportedModalities: []string{"text_to_image"},
		ObservedUnitClasses:  []UnitClass{UnitInputImages, UnitOutputImages},
		UnsupportedSemantics: []string{"image_edit_or_variation", "partial_images", "provider_hosted_url", "streaming", "multiple_images"},
	}
}

func (p dashScopeQwenImageProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if !IsDashScopeBeijingMediaOrigin(input.BaseURL) || !IsDashScopeQwenImageModel(input.MappedModel) {
		return nil, nil, protocolError(ErrInvalidConfiguration, "qwen_image_binding_invalid")
	}
	var downstream openAIImageRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel ||
		strings.TrimSpace(downstream.Prompt) == "" || len(downstream.Prompt) > DashScopeQwenMaxPromptBytes {
		return nil, nil, protocolError(ErrInvalidRequest, "qwen_image_request_invalid")
	}
	if downstream.N != nil && *downstream.N != 1 {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_multiple_images")
	}
	if downstream.Stream != nil && *downstream.Stream {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_streaming")
	}
	if !SupportsSquarePNGImageControls(downstream.Size, downstream.Quality, downstream.Background, downstream.OutputFormat) {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "qwen_image_controls_unsupported")
	}
	wire, err := json.Marshal(struct {
		Model          string `json:"model"`
		Prompt         string `json:"prompt"`
		N              int    `json:"n"`
		Size           string `json:"size"`
		ResponseFormat string `json:"response_format"`
		PromptExtend   bool   `json:"prompt_extend"`
	}{input.MappedModel, downstream.Prompt, 1, "1024x1024", "b64_json", false})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, baseURL, DashScopeQwenImageEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	descriptor := p.Descriptor()
	return req, &Binding{profileID: descriptor.ProfileID, profileVersion: descriptor.ProfileVersion,
		requestedModel: input.RequestedModel, mappedModel: input.MappedModel, outputCount: 1}, nil
}

func SupportsSquarePNGImageControls(size, quality, background, outputFormat string) bool {
	return oneOfOrEmpty(size, "1024x1024") && quality == "" && background == "" && oneOfOrEmpty(outputFormat, "png")
}

func (p dashScopeQwenImageProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var response struct {
		Model   string            `json:"model"`
		Created json.Number       `json:"created"`
		Data    []openAIImageData `json:"data"`
		Usage   *struct {
			InputImages  json.Number `json:"input_image_count"`
			OutputImages json.Number `json:"output_image_count"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &response, MaxOpenAIImageResponseBytes); err != nil {
		return Outcome{}, err
	}
	if response.Model != "" && response.Model != binding.mappedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "qwen_image_model_mismatch")
	}
	if _, err := parseNonnegativeInt64(response.Created, "created"); err != nil {
		return Outcome{}, err
	}
	if len(response.Data) != binding.outputCount {
		return Outcome{}, protocolError(ErrInvalidResponse, "image_count_invalid")
	}
	data := response.Data[0]
	if data.URL != nil {
		return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_provider_hosted_url")
	}
	media, err := decodeSquarePNG(data.B64JSON)
	if err != nil {
		return Outcome{}, err
	}
	if response.Usage == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	input, inputErr := parseNonnegativeInt64(response.Usage.InputImages, string(UnitInputImages))
	output, outputErr := parseNonnegativeInt64(response.Usage.OutputImages, string(UnitOutputImages))
	if inputErr != nil || outputErr != nil || input != 0 || output != int64(binding.outputCount) {
		return Outcome{}, protocolError(ErrInvalidResponse, "qwen_image_counts_invalid")
	}
	return Outcome{Media: media, ContractVersion: ContractVersion, ProfileID: binding.profileID, ProviderState: "completed", MappedModel: binding.mappedModel,
		OutputCount: binding.outputCount, Units: []UnitObservation{observedUnit(UnitInputImages, response.Usage.InputImages.String()),
			observedUnit(UnitOutputImages, response.Usage.OutputImages.String())}}, nil
}

func (dashScopeQwenImageProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
