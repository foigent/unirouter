package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ProfileDeepSeekOCR            = "unirouter.deepseek.tools.ocr.v1"
	DeepSeekOCR                   = "deepseek-ocr"
	DeepSeekOCROrigin             = "https://api.deepseek.com"
	DeepSeekOCREndpoint           = "/v1/tools/ocr"
	MaxOCRPNGBytes                = 32 << 10
	MaxOCRTextBytes               = 32 << 10
	UnitRecognizedPages UnitClass = "recognized_pages"
)

type deepSeekOCRProfile struct{}

func (deepSeekOCRProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileDeepSeekOCR, ProfileVersion: "1.0.0",
		Provider: "deepseek", EndpointKind: EndpointOCR, DownstreamEndpoint: "/v1/media/jobs", UpstreamEndpoint: DeepSeekOCREndpoint,
		Authentication: "bearer", RuntimeState: RuntimeRepositoryAsyncMediaV1, ProviderFinality: "completed_page_and_final_report",
		CustomerChargeState: ChargeObservedOnly, SupportedModalities: []string{"image_to_text"},
		ObservedUnitClasses:  []UnitClass{UnitRecognizedPages},
		UnsupportedSemantics: []string{"multiple_pages", "remote_input_uri", "rich_layout_recognition", "provider_token_billing"},
	}
}

func DecodeOCRInputPNG(input string) ([]byte, error) {
	const prefix = "data:image/png;base64,"
	encoded, ok := strings.CutPrefix(input, prefix)
	if !ok || len(encoded) > base64.StdEncoding.EncodedLen(MaxOCRPNGBytes) || strings.ContainsAny(encoded, "\r\n") {
		return nil, protocolError(ErrInvalidRequest, "ocr_png_input_invalid")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) > MaxOCRPNGBytes {
		return nil, protocolError(ErrInvalidRequest, "ocr_png_input_invalid")
	}
	if err := validateImageDelivery(raw, imageDeliveryConstraints{format: "png"}); err != nil {
		return nil, protocolError(ErrInvalidRequest, "ocr_png_input_invalid")
	}
	return raw, nil
}

func (deepSeekOCRProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != DeepSeekOCROrigin || input.MappedModel != DeepSeekOCR {
		return nil, nil, protocolError(ErrInvalidConfiguration, "ocr_binding_invalid")
	}
	var request struct {
		Model      string `json:"model"`
		Operation  string `json:"operation"`
		InputMedia string `json:"input_media"`
		Prompt     string `json:"prompt,omitempty"`
		N          int    `json:"n,omitempty"`
		Stream     bool   `json:"stream,omitempty"`
	}
	if err := decodeStrictRequest(input.Body, &request); err != nil {
		return nil, nil, err
	}
	if request.Model != input.RequestedModel || request.Operation != "image_to_text" ||
		request.Prompt != "" || (request.N != 0 && request.N != 1) || request.Stream {
		return nil, nil, protocolError(ErrInvalidRequest, "ocr_request_invalid")
	}
	png, err := DecodeOCRInputPNG(request.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	req, err := buildMediaMultipart(ctx, baseURL, DeepSeekOCREndpoint, input, "page.png", png,
		mediaFormField{Name: "purpose", Value: "text_recognition"})
	if err != nil {
		return nil, nil, err
	}
	return req, &Binding{profileID: ProfileDeepSeekOCR, profileVersion: "1.0.0", mappedModel: input.MappedModel, requestedModel: input.RequestedModel, outputCount: 1}, nil
}

func (p deepSeekOCRProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		ID        string `json:"id"`
		Object    string `json:"object"`
		Created   int64  `json:"created"`
		Purpose   string `json:"purpose"`
		PageCount int    `json:"page_count"`
		Pages     []struct {
			Index        *int   `json:"index"`
			Text         string `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"pages"`
		Usage struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
			Total  *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 256<<10); err != nil {
		return Outcome{}, err
	}
	id, hasPrefix := strings.CutPrefix(wire.ID, "ocr-")
	if !hasPrefix || !ValidMediaTaskID(id) || wire.Created <= 0 || wire.Object != "ocr.result" ||
		wire.Purpose != "text_recognition" || wire.PageCount != 1 || len(wire.Pages) != 1 ||
		wire.Pages[0].Index == nil || *wire.Pages[0].Index != 0 || wire.Pages[0].FinishReason != "stop" ||
		!ValidMediaTranscript([]byte(wire.Pages[0].Text)) {
		return Outcome{}, protocolError(ErrInvalidResponse, "ocr_result_invalid")
	}
	if u := wire.Usage; u.Input == nil || u.Output == nil || u.Total == nil || *u.Input < 0 || *u.Output < 0 ||
		*u.Total < *u.Input || *u.Total-*u.Input != *u.Output {
		return Outcome{}, protocolError(ErrInvalidResponse, "ocr_usage_invalid")
	}
	return Outcome{ContractVersion: ContractVersion, ProfileID: ProfileDeepSeekOCR, ProviderState: MediaTaskCompleted,
		MappedModel: DeepSeekOCR, OutputCount: 1, Units: []UnitObservation{observedUnit(UnitRecognizedPages, "1")},
		Media: &MediaArtifact{Bytes: []byte(wire.Pages[0].Text), MediaType: "text/plain; charset=utf-8"}}, nil
}

func (deepSeekOCRProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}

func ValidMediaTranscript(body []byte) bool {
	return len(body) > 0 && len(body) <= MaxOCRTextBytes && utf8.Valid(body) && len(bytes.TrimSpace(body)) > 0 &&
		!bytes.ContainsRune(body, '\x00') && strings.IndexFunc(string(body), func(r rune) bool {
		return unicode.IsControl(r) && r != '\r' && r != '\n' && r != '\t'
	}) == -1
}

// DeepSeekOCRProbeBody uses the same public media command shape as customers.
func DeepSeekOCRProbeBody(model string, png []byte) ([]byte, error) {
	return json.Marshal(map[string]string{"model": model, "operation": "image_to_text", "input_media": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}
