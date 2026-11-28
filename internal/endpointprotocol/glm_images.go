package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const (
	ProfileGLMImage     = "unirouter.glm.images.glm-image.v1"
	GLMImage            = "glm-image"
	GLMImageEndpoint    = "/api/paas/v4/images/generations"
	GLMImagePromptBytes = 1000
)

type glmImageProfile struct{}

func (glmImageProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileGLMImage, ProfileVersion: "1.0.0",
		Provider: "glm", EndpointKind: EndpointImageGeneration, DownstreamEndpoint: "/v1/media/jobs",
		UpstreamEndpoint: GLMImageEndpoint, Authentication: "bearer", RuntimeState: RuntimeRepositoryImageV1,
		ProviderFinality: "completed_image_with_validated_output", CustomerChargeState: ChargeObservedOnly,
		SupportedModalities:  []string{"text_to_image"},
		UnsupportedSemantics: []string{"image_edit", "image_variation", "multiple_images", "provider_metering", "custom_size", "custom_output_format"},
	}
}

func (glmImageProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != GLMChinaMediaOrigin || input.MappedModel != GLMImage {
		return nil, nil, protocolError(ErrInvalidConfiguration, "glm_image_binding_invalid")
	}
	var request struct {
		Model        string `json:"model"`
		Prompt       string `json:"prompt"`
		Operation    string `json:"operation,omitempty"`
		Size         string `json:"size,omitempty"`
		Quality      string `json:"quality,omitempty"`
		Background   string `json:"background,omitempty"`
		OutputFormat string `json:"output_format,omitempty"`
		N            int    `json:"n,omitempty"`
		Stream       bool   `json:"stream,omitempty"`
	}
	if err := decodeStrictRequest(input.Body, &request); err != nil {
		return nil, nil, err
	}
	if request.Model != input.RequestedModel || (request.Operation != "" && request.Operation != "text_to_image") ||
		(request.N != 0 && request.N != 1) || request.Stream ||
		strings.TrimSpace(request.Prompt) == "" || len(request.Prompt) > GLMImagePromptBytes ||
		!SupportsGLMImageControls(request.Size, request.Quality, request.Background, request.OutputFormat) {
		return nil, nil, protocolError(ErrInvalidRequest, "glm_image_request_invalid")
	}
	wire, err := json.Marshal(map[string]string{"model": input.MappedModel, "prompt": request.Prompt, "size": "1280x1280"})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, baseURL, GLMImageEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	return req, &Binding{profileID: ProfileGLMImage, profileVersion: "1.0.0", mappedModel: input.MappedModel, requestedModel: input.RequestedModel, outputCount: 1}, nil
}

func SupportsGLMImageControls(size, quality, background, format string) bool {
	return size == "" && quality == "" && background == "" && format == ""
}

func (p glmImageProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if wire.Created <= 0 || len(wire.Data) != 1 || (wire.Model != "" && wire.Model != binding.mappedModel) {
		return Outcome{}, protocolError(ErrInvalidResponse, "glm_image_terminal_invalid")
	}
	u, err := url.Parse(wire.Data[0].URL)
	if err != nil || !p.AcceptsResultURL(u) {
		return Outcome{}, protocolError(ErrInvalidResponse, "glm_image_result_origin_invalid")
	}
	return Outcome{ContractVersion: ContractVersion, ProfileID: ProfileGLMImage, ProviderState: MediaTaskCompleted,
		MappedModel: binding.mappedModel, OutputCount: 1, ResultURL: u.String(), Units: []UnitObservation{}}, nil
}

func (glmImageProfile) AcceptsResultURL(u *url.URL) bool {
	return validMediaAssetURL(u) && u.Hostname() == "sfile.chatglm.cn"
}

func (glmImageProfile) ValidateRemoteMedia(body []byte) (*MediaArtifact, error) {
	if err := validateImageDelivery(body, imageDeliveryConstraints{width: 1280, height: 1280}); err != nil {
		return nil, err
	}
	return &MediaArtifact{Bytes: body, MediaType: DetectImageMediaType(body)}, nil
}

func (glmImageProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
