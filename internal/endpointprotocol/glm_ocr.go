package endpointprotocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	ProfileGLMOCR  = "unirouter.glm.ocr.layout.v1"
	GLMOCR         = "glm-ocr"
	GLMOCREndpoint = "/api/paas/v4/layout_parsing"
)

type glmOCRProfile struct{}

func (glmOCRProfile) Descriptor() Descriptor {
	return mediaToolJobDescriptor(Descriptor{
		ProfileID: ProfileGLMOCR, Provider: "glm", EndpointKind: EndpointOCR,
		UpstreamEndpoint: GLMOCREndpoint, ProviderFinality: "completed_page_and_reported_tokens",
		SupportedModalities: []string{"image_to_text"}, ObservedUnitClasses: []UnitClass{UnitInputTokens, UnitOutputTokens},
		UnsupportedSemantics: []string{"multiple_pages", "remote_input_uri", "pdf", "cropped_image_download"},
	})
}

func (p glmOCRProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != GLMChinaMediaOrigin || input.MappedModel != GLMOCR {
		return nil, nil, protocolError(ErrInvalidConfiguration, "glm_ocr_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, "image_to_text")
	if err != nil {
		return nil, nil, err
	}
	if r.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "ocr_request_invalid")
	}
	png, err := DecodeOCRInputPNG(r.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	wire, err := json.Marshal(map[string]any{
		"model": GLMOCR, "file": base64.StdEncoding.EncodeToString(png), "return_crop_images": false, "need_layout_visualization": false,
	})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, origin, p.Descriptor().UpstreamEndpoint, input.APIKey, input.RequestID, wire)
	return req, &Binding{profileID: ProfileGLMOCR, profileVersion: "1.0.0", mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}, err
}

func (p glmOCRProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(b, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		ID        string `json:"id"`
		Created   int64  `json:"created"`
		Model     string `json:"model"`
		RequestID string `json:"request_id"`
		Text      string `json:"md_results"`
		DataInfo  struct {
			Pages int `json:"num_pages"`
		} `json:"data_info"`
		Usage struct {
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
			Total      *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 512<<10); err != nil {
		return Outcome{}, err
	}
	// The request ID is lowercase. The published response example uses the
	// uppercase display name for exactly this model.
	if !ValidMediaTaskID(wire.ID) || !ValidMediaTaskID(wire.RequestID) || wire.Created <= 0 ||
		(wire.Model != GLMOCR && wire.Model != "GLM-OCR") || wire.DataInfo.Pages != 1 ||
		!ValidMediaTranscript([]byte(wire.Text)) {
		return Outcome{}, protocolError(ErrInvalidResponse, "glm_ocr_result_invalid")
	}
	units, err := reportedMediaTokenPair(wire.Usage.Prompt, wire.Usage.Completion, wire.Usage.Total)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ContractVersion: ContractVersion, ProfileID: ProfileGLMOCR, MappedModel: b.mappedModel, VerifiedProviderModel: wire.Model,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: units,
		Media: &MediaArtifact{Bytes: []byte(wire.Text), MediaType: "text/plain; charset=utf-8"},
	}, nil
}

func (glmOCRProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
