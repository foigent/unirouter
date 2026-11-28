package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ProfileDashScopeWanVideo   = "unirouter.dashscope.video.wan-3.v1"
	DashScopeWanVideo          = "wan3.0-video"
	DashScopeWanVideoEndpoint  = "/api/v1/services/aigc/video-generation/video-synthesis"
	DashScopeWanMaxPromptBytes = 4096
)

type dashScopeWanVideoProfile struct{}

func (dashScopeWanVideoProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileDashScopeWanVideo, ProfileVersion: "1.0.0",
		Provider: "dashscope", EndpointKind: EndpointVideoGeneration, DownstreamEndpoint: "/v1/media/jobs", UpstreamEndpoint: DashScopeWanVideoEndpoint,
		Authentication: "bearer", RuntimeState: RuntimeRepositoryAsyncMediaV1, ProviderFinality: "accepted_task_then_exact_status",
		CustomerChargeState: ChargeObservedOnly, SupportedModalities: []string{"text_to_video"},
		ObservedUnitClasses:  []UnitClass{UnitInputVideoDurationMS, UnitOutputVideoDurationMS},
		UnsupportedSemantics: []string{"input_video", "image_to_video", "audio_track", "prime_fallback", "multiple_videos"},
	}
}

func (p dashScopeWanVideoProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if !IsDashScopeBeijingMediaOrigin(input.BaseURL) || input.MappedModel != DashScopeWanVideo {
		return nil, nil, protocolError(ErrInvalidConfiguration, "wan_video_binding_invalid")
	}
	var downstream struct {
		Model     string `json:"model"`
		Prompt    string `json:"prompt"`
		Operation string `json:"operation"`
		N         int    `json:"n,omitempty"`
		Stream    bool   `json:"stream,omitempty"`
	}
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model != input.RequestedModel || downstream.Operation != "text_to_video" || (downstream.N != 0 && downstream.N != 1) ||
		downstream.Stream || strings.TrimSpace(downstream.Prompt) == "" || len(downstream.Prompt) > DashScopeWanMaxPromptBytes {
		return nil, nil, protocolError(ErrInvalidRequest, "wan_video_request_invalid")
	}
	wire, err := json.Marshal(map[string]any{
		"model": input.MappedModel, "input": map[string]string{"prompt": downstream.Prompt},
		"parameters": map[string]any{"resolution": "480P", "duration": 2, "ratio": "16:9", "prompt_extend": false, "audio": false},
	})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, baseURL, DashScopeWanVideoEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-DashScope-Async", "enable")
	return req, &Binding{profileID: ProfileDashScopeWanVideo, profileVersion: "1.0.0", mappedModel: input.MappedModel, requestedModel: input.RequestedModel, outputCount: 1}, nil
}

func (p dashScopeWanVideoProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	return p.ParseStatusResponse(binding.mappedModel, "", status, body)
}

func (p dashScopeWanVideoProfile) BuildStatusRequest(ctx context.Context, input BuildInput, taskID string) (*http.Request, error) {
	if !IsDashScopeBeijingMediaOrigin(input.BaseURL) || input.MappedModel != DashScopeWanVideo || !ValidMediaTaskID(taskID) || strings.TrimSpace(input.APIKey) == "" {
		return nil, protocolError(ErrInvalidConfiguration, "wan_video_status_binding_invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(input.BaseURL, "/")+"/api/v1/tasks/"+taskID, nil)
	if err != nil {
		return nil, protocolError(ErrInvalidConfiguration, "wan_video_status_binding_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+input.APIKey)
	return req, nil
}

func (p dashScopeWanVideoProfile) ParseStatusResponse(expectedModel, taskID string, status int, body []byte) (Outcome, error) {
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
			VideoURL   string `json:"video_url"`
		} `json:"output"`
		Usage *struct {
			Count          json.Number `json:"video_count"`
			Duration       json.Number `json:"duration"`
			InputDuration  json.Number `json:"input_video_duration"`
			OutputDuration json.Number `json:"output_video_duration"`
			Resolution     int         `json:"SR"`
			ServedModel    string      `json:"served_model"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if expectedModel != DashScopeWanVideo || !ValidMediaTaskID(wire.Output.TaskID) ||
		(taskID != "" && wire.Output.TaskID != taskID) {
		return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_task_identity_mismatch")
	}
	outcome := Outcome{ContractVersion: ContractVersion, ProfileID: ProfileDashScopeWanVideo, MappedModel: expectedModel, ProviderObject: wire.Output.TaskID}
	switch wire.Output.TaskStatus {
	case "PENDING", "RUNNING":
		if wire.Output.VideoURL != "" {
			return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_pending_has_result")
		}
		outcome.ProviderState = MediaTaskPending
		return outcome, nil
	case "UNKNOWN":
		if taskID == "" || wire.Output.VideoURL != "" {
			return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_task_identity_mismatch")
		}
		outcome.ProviderState = MediaTaskUnavailable
		return outcome, nil
	case "FAILED", "CANCELED":
		outcome.ProviderState = MediaTaskFailed
		return outcome, nil
	case "SUCCEEDED":
		if taskID == "" || wire.Usage == nil || wire.Usage.Count.String() != "1" ||
			wire.Usage.Resolution != 480 || (wire.Usage.ServedModel != "" && wire.Usage.ServedModel != expectedModel) {
			return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_terminal_invalid")
		}
	default:
		return Outcome{}, protocolError(ErrNonFinalResponse, "wan_video_task_status_unknown")
	}
	u, err := url.Parse(wire.Output.VideoURL)
	if err != nil || !p.AcceptsResultURL(u) {
		return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_result_origin_invalid")
	}
	total, totalErr := mediaMilliseconds(wire.Usage.Duration.String())
	output, outputErr := mediaMilliseconds(wire.Usage.OutputDuration.String())
	if totalErr != nil || outputErr != nil || total != output || output != "2000" {
		return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_usage_mismatch")
	}
	if raw := wire.Usage.InputDuration.String(); raw != "" {
		value, err := mediaMilliseconds(raw)
		if err != nil || value != "0" {
			return Outcome{}, protocolError(ErrInvalidResponse, "wan_video_usage_mismatch")
		}
		outcome.Units = append(outcome.Units, observedUnit(UnitInputVideoDurationMS, value))
	}
	outcome.Units = append(outcome.Units, observedUnit(UnitOutputVideoDurationMS, output))
	outcome.ProviderState, outcome.OutputCount, outcome.ResultURL = MediaTaskCompleted, 1, u.String()
	return outcome, nil
}

func (dashScopeWanVideoProfile) AcceptsResultURL(u *url.URL) bool {
	return validMediaAssetURL(u) && u.Hostname() == "dashscope-result-bj.oss-cn-beijing.aliyuncs.com"
}

func (dashScopeWanVideoProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}

func validateWanOutcome(model string, quantities map[UnitClass]int64) error {
	if model != DashScopeWanVideo || quantities[UnitOutputVideoDurationMS] != 2000 ||
		quantities[UnitInputVideoDurationMS] != 0 {
		return protocolError(ErrInvalidResponse, "wan_video_usage_mismatch")
	}
	return nil
}

func (dashScopeWanVideoProfile) VideoRequirements() MP4Requirements {
	return MP4Requirements{DisplayRatioWidth: 16, DisplayRatioHeight: 9, Height: 480, Duration: 2 * time.Second}
}
