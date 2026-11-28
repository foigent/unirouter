package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

const (
	ProfileMiniMaxH3Video      = "unirouter.minimax.video.h3.v1"
	MiniMaxH3Video             = "MiniMax-H3"
	MiniMaxH3VideoEndpoint     = "/v2/video_generation"
	MiniMaxH3VideoStatusPrefix = "/v2/query/video_generation/"
	MiniMaxH3VideoPromptBytes  = 4096
)

type miniMaxH3VideoProfile struct{}

func (miniMaxH3VideoProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileMiniMaxH3Video, ProfileVersion: "1.0.0",
		Provider: "minimax", EndpointKind: EndpointVideoGeneration, DownstreamEndpoint: "/v1/media/jobs", UpstreamEndpoint: MiniMaxH3VideoEndpoint,
		Authentication: "bearer", RuntimeState: RuntimeRepositoryAsyncMediaV1, ProviderFinality: "accepted_task_then_exact_status",
		CustomerChargeState: ChargeObservedOnly, SupportedModalities: []string{"text_to_video"},
		ObservedUnitClasses:  []UnitClass{UnitInputVideoDurationMS, UnitOutputVideoDurationMS, UnitInputImages},
		UnsupportedSemantics: []string{"input_video", "image_to_video", "audio_track", "multiple_videos"},
	}
}

func (miniMaxH3VideoProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if input.BaseURL != MiniMaxChinaImageOrigin || input.MappedModel != MiniMaxH3Video {
		return nil, nil, protocolError(ErrInvalidConfiguration, "h3_video_binding_invalid")
	}
	prompt, err := decodeMediaVideoPrompt(input, MiniMaxH3VideoPromptBytes)
	if err != nil {
		return nil, nil, err
	}
	wire, err := json.Marshal(map[string]any{
		"model": input.MappedModel, "content": []map[string]string{{"type": "text", "text": prompt}},
		"resolution": "768P", "duration": 4, "ratio": "16:9", "generate_audio": false, "response_format": "task",
	})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, baseURL, MiniMaxH3VideoEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	return req, &Binding{profileID: ProfileMiniMaxH3Video, profileVersion: "1.0.0", mappedModel: input.MappedModel, requestedModel: input.RequestedModel, outputCount: 1}, nil
}

func (p miniMaxH3VideoProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if !ValidMediaTaskID(wire.TaskID) {
		return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_task_identity_mismatch")
	}
	return Outcome{ContractVersion: ContractVersion, ProfileID: ProfileMiniMaxH3Video, MappedModel: binding.mappedModel,
		ProviderObject: wire.TaskID, ProviderState: MediaTaskPending}, nil
}

func (miniMaxH3VideoProfile) BuildStatusRequest(ctx context.Context, input BuildInput, taskID string) (*http.Request, error) {
	if input.BaseURL != MiniMaxChinaImageOrigin || input.MappedModel != MiniMaxH3Video {
		return nil, protocolError(ErrInvalidConfiguration, "h3_video_status_binding_invalid")
	}
	return buildMediaStatusGET(ctx, input, MiniMaxH3VideoStatusPrefix, taskID)
}

func (p miniMaxH3VideoProfile) ParseStatusResponse(expectedModel, taskID string, status int, body []byte) (Outcome, error) {
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		Task struct {
			ID         string `json:"id"`
			Model      string `json:"model"`
			Status     string `json:"status"`
			Resolution string `json:"resolution"`
			Duration   int    `json:"duration"`
			Modality   string `json:"modality"`
			TaskType   string `json:"task_type"`
			Content    *struct {
				URL string `json:"url"`
			} `json:"content"`
			Usage *struct {
				TotalSeconds      json.Number `json:"total_seconds"`
				InputSeconds      json.Number `json:"input_seconds"`
				OutputSeconds     json.Number `json:"output_seconds"`
				InputImages       json.Number `json:"input_image_count"`
				InputAudioSeconds json.Number `json:"input_audio_seconds"`
			} `json:"usage"`
		} `json:"task"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	task := wire.Task
	if expectedModel != MiniMaxH3Video || !ValidMediaTaskID(taskID) || taskID != task.ID ||
		task.Model != expectedModel || (task.Modality != "" && task.Modality != "video") || (task.TaskType != "" && task.TaskType != "generation") {
		return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_task_identity_mismatch")
	}
	outcome := Outcome{ContractVersion: ContractVersion, ProfileID: ProfileMiniMaxH3Video, MappedModel: expectedModel, ProviderObject: taskID}
	switch task.Status {
	case "queued", "running":
		if task.Content != nil && task.Content.URL != "" {
			return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_pending_has_result")
		}
		outcome.ProviderState = MediaTaskPending
		return outcome, nil
	case "failed", "cancelled":
		outcome.ProviderState = MediaTaskFailed
		return outcome, nil
	case "succeeded":
		if task.Content == nil || task.Usage == nil || task.Duration != 4 || task.Resolution != "768P" {
			return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_terminal_invalid")
		}
	default:
		return Outcome{}, protocolError(ErrNonFinalResponse, "h3_video_task_status_unknown")
	}
	u, err := url.Parse(task.Content.URL)
	if err != nil || !p.AcceptsResultURL(u) {
		return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_result_origin_invalid")
	}
	total, totalErr := mediaMilliseconds(task.Usage.TotalSeconds.String())
	output, outputErr := mediaMilliseconds(task.Usage.OutputSeconds.String())
	if totalErr != nil || outputErr != nil || total != output || output != "4000" {
		return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_usage_mismatch")
	}
	if raw := task.Usage.InputSeconds.String(); raw != "" {
		input, err := mediaMilliseconds(raw)
		if err != nil || input != "0" {
			return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_usage_mismatch")
		}
		outcome.Units = append(outcome.Units, observedUnit(UnitInputVideoDurationMS, input))
	}
	outcome.Units = append(outcome.Units, observedUnit(UnitOutputVideoDurationMS, output))
	if n := task.Usage.InputImages.String(); n != "" {
		if n != "0" {
			return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_usage_mismatch")
		}
		outcome.Units = append(outcome.Units, observedUnit(UnitInputImages, n))
	}
	if raw := task.Usage.InputAudioSeconds.String(); raw != "" {
		input, err := mediaMilliseconds(raw)
		if err != nil || input != "0" {
			return Outcome{}, protocolError(ErrInvalidResponse, "h3_video_usage_mismatch")
		}
	}
	outcome.ProviderState, outcome.OutputCount, outcome.ResultURL = MediaTaskCompleted, 1, u.String()
	return outcome, nil
}

func (miniMaxH3VideoProfile) AcceptsResultURL(u *url.URL) bool {
	return validMediaAssetURL(u) && u.Hostname() == "cdn.hailuoai.com"
}

func (miniMaxH3VideoProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}

func (miniMaxH3VideoProfile) VideoRequirements() MP4Requirements {
	return MP4Requirements{DisplayRatioWidth: 16, DisplayRatioHeight: 9, Height: 768, Duration: 4 * time.Second}
}
