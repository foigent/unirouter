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
	ProfileGLMCogVideo      = "unirouter.glm.video.cogvideox-3.v1"
	GLMCogVideo             = "cogvideox-3"
	GLMCogVideoEndpoint     = "/api/paas/v4/videos/generations"
	GLMCogVideoStatusPrefix = "/api/paas/v4/async-result/"
	GLMCogVideoPromptBytes  = 512
	GLMChinaMediaOrigin     = "https://open.bigmodel.cn"
)

type glmCogVideoProfile struct{}

func (glmCogVideoProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion: ContractVersion, ProfileID: ProfileGLMCogVideo, ProfileVersion: "1.0.0",
		Provider: "glm", EndpointKind: EndpointVideoGeneration, DownstreamEndpoint: "/v1/media/jobs",
		UpstreamEndpoint: GLMCogVideoEndpoint, Authentication: "bearer", RuntimeState: RuntimeRepositoryAsyncMediaV1,
		ProviderFinality: "accepted_task_then_exact_status", CustomerChargeState: ChargeObservedOnly,
		SupportedModalities:  []string{"text_to_video"},
		UnsupportedSemantics: []string{"input_video", "image_to_video", "audio_track", "multiple_videos", "provider_metering"},
	}
}

func (glmCogVideoProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != GLMChinaMediaOrigin || input.MappedModel != GLMCogVideo {
		return nil, nil, protocolError(ErrInvalidConfiguration, "cog_video_binding_invalid")
	}
	prompt, err := decodeMediaVideoPrompt(input, GLMCogVideoPromptBytes)
	if err != nil {
		return nil, nil, err
	}
	wire, err := json.Marshal(map[string]any{"model": input.MappedModel, "prompt": prompt,
		"size": "1280x720", "duration": 5, "fps": 30, "quality": "speed", "with_audio": false})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, baseURL, GLMCogVideoEndpoint, input.APIKey, input.RequestID, wire)
	if err != nil {
		return nil, nil, err
	}
	return req, &Binding{profileID: ProfileGLMCogVideo, profileVersion: "1.0.0", mappedModel: input.MappedModel, requestedModel: input.RequestedModel, outputCount: 1}, nil
}

func (p glmCogVideoProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(binding, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	return p.ParseStatusResponse(binding.mappedModel, "", status, body)
}

func (glmCogVideoProfile) BuildStatusRequest(ctx context.Context, input BuildInput, taskID string) (*http.Request, error) {
	if strings.TrimSuffix(input.BaseURL, "/") != GLMChinaMediaOrigin || input.MappedModel != GLMCogVideo {
		return nil, protocolError(ErrInvalidConfiguration, "cog_video_status_binding_invalid")
	}
	return buildMediaStatusGET(ctx, input, GLMCogVideoStatusPrefix, taskID)
}

func (p glmCogVideoProfile) ParseStatusResponse(expectedModel, taskID string, status int, body []byte) (Outcome, error) {
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		TaskStatus string `json:"task_status"`
		Videos     []struct {
			URL string `json:"url"`
		} `json:"video_result"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if expectedModel != GLMCogVideo || wire.Model != expectedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "cog_video_task_identity_mismatch")
	}
	if taskID == "" {
		taskID = wire.ID
		if wire.TaskStatus == "SUCCESS" || wire.TaskStatus == "FAIL" {
			// A terminal create acknowledgement still needs a read of the
			// original task's authenticated status endpoint.
			wire.TaskStatus = "PROCESSING"
			wire.Videos = nil
		}
	}
	// The status API can omit id. Its authenticated request URL binds the
	// task; request_id in the result identifies an API request instead.
	if !ValidMediaTaskID(taskID) || (wire.ID != "" && wire.ID != taskID) {
		return Outcome{}, protocolError(ErrInvalidResponse, "cog_video_task_identity_mismatch")
	}
	outcome := Outcome{ContractVersion: ContractVersion, ProfileID: ProfileGLMCogVideo, MappedModel: expectedModel, ProviderObject: taskID}
	switch wire.TaskStatus {
	case "PROCESSING":
		if len(wire.Videos) != 0 {
			return Outcome{}, protocolError(ErrInvalidResponse, "cog_video_pending_has_result")
		}
		outcome.ProviderState = MediaTaskPending
		return outcome, nil
	case "FAIL":
		outcome.ProviderState = MediaTaskFailed
		return outcome, nil
	case "SUCCESS":
		if len(wire.Videos) != 1 {
			return Outcome{}, protocolError(ErrInvalidResponse, "cog_video_terminal_invalid")
		}
	default:
		return Outcome{}, protocolError(ErrNonFinalResponse, "cog_video_task_status_unknown")
	}
	u, err := url.Parse(wire.Videos[0].URL)
	if err != nil || !p.AcceptsResultURL(u) {
		return Outcome{}, protocolError(ErrInvalidResponse, "cog_video_result_origin_invalid")
	}
	outcome.ProviderState, outcome.OutputCount, outcome.ResultURL = MediaTaskCompleted, 1, u.String()
	outcome.Units = []UnitObservation{}
	return outcome, nil
}

func (glmCogVideoProfile) AcceptsResultURL(u *url.URL) bool {
	return validMediaAssetURL(u) && u.Hostname() == "sfile.chatglm.cn"
}

func (glmCogVideoProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}

func (glmCogVideoProfile) VideoRequirements() MP4Requirements {
	return MP4Requirements{Width: 1280, Height: 720, Duration: 5 * time.Second}
}
