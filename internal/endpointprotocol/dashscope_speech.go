package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	ProfileDashScopeASR       = "unirouter.dashscope.speech.asr-3-1.v1"
	ProfileDashScopeTTS       = "unirouter.dashscope.speech.tts-3-1.v1"
	DashScopeASR              = "qwen-audio-3.1-asr-flash"
	DashScopeTTS              = "qwen-audio-3.1-tts-flash"
	DashScopeASREndpoint      = "/api/v1/services/aigc/multimodal-generation/generation"
	DashScopeTTSEndpoint      = "/api/v1/services/audio/tts/SpeechSynthesizer"
	dashScopeSpeechObjectHost = "dashscope-result-bj.oss-cn-beijing.aliyuncs.com"
	dashScopeSpeechSampleRate = 24000
)

type dashScopeSpeechProfile struct{}

func (dashScopeSpeechProfile) Descriptor() Descriptor {
	return mediaToolJobDescriptor(Descriptor{
		ProfileID: ProfileDashScopeASR, Provider: "dashscope", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: DashScopeASREndpoint, ProviderFinality: "completed_transcript_with_optional_reported_tokens",
		SupportedModalities:  []string{"speech_to_text"},
		ObservedUnitClasses:  []UnitClass{UnitInputTokens, UnitOutputTokens, UnitInputAudioDurationMS},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "speaker_diarization", "hotwords"},
	})
}

func (p dashScopeSpeechProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if !IsDashScopeBeijingMediaOrigin(input.BaseURL) || input.MappedModel != DashScopeASR {
		return nil, nil, protocolError(ErrInvalidConfiguration, "dashscope_speech_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, "speech_to_text")
	if err != nil {
		return nil, nil, err
	}
	if r.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	_, info, err := DecodeASRInputWAV(r.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"model": DashScopeASR,
		"input": map[string]any{"messages": []map[string]any{{"role": "user", "content": inlineSpeechParts(r.InputMedia)}}},
	})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, origin, DashScopeASREndpoint, input.APIKey, input.RequestID, payload)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-DashScope-SSE", "disable")
	return req, &Binding{profileID: ProfileDashScopeASR, profileVersion: "1.0.0", mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, inputDurationMS: info.DurationMS, outputCount: 1}, nil
}

type dashScopeSpeechUsage struct {
	Input    *int64      `json:"input_tokens"`
	Output   *int64      `json:"output_tokens"`
	Total    *int64      `json:"total_tokens"`
	Duration json.Number `json:"duration"`
}

func (p dashScopeSpeechProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(b, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
		Output    struct {
			Text string `json:"text"`
		} `json:"output"`
		Usage dashScopeSpeechUsage `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 256<<10); err != nil {
		return Outcome{}, err
	}
	if wire.Code != "" || !ValidMediaTaskID(wire.RequestID) || !ValidMediaTranscript([]byte(wire.Output.Text)) {
		return Outcome{}, protocolError(ErrInvalidResponse, "dashscope_asr_result_invalid")
	}
	units, err := reportedMediaTokenDirections(wire.Usage.Input, wire.Usage.Output, wire.Usage.Total)
	if err != nil {
		return Outcome{}, err
	}
	if wire.Usage.Duration != "" {
		raw, err := mediaMilliseconds(wire.Usage.Duration.String())
		ms, parseErr := strconv.ParseInt(raw, 10, 64)
		if err != nil || parseErr != nil || ms < b.inputDurationMS-1000 || ms > b.inputDurationMS+1000 || ms <= 0 || ms > 4000 {
			return Outcome{}, protocolError(ErrInvalidResponse, "dashscope_asr_usage_duration_invalid")
		}
		units = append(units, observedUnit(UnitInputAudioDurationMS, raw))
	}
	return Outcome{
		ContractVersion: ContractVersion, ProfileID: ProfileDashScopeASR, MappedModel: b.mappedModel,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: units,
		Media: &MediaArtifact{Bytes: []byte(wire.Output.Text), MediaType: "text/plain; charset=utf-8"},
	}, nil
}

func (dashScopeSpeechProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}

type dashScopeSpeechSynthesisProfile struct{}

func (dashScopeSpeechSynthesisProfile) Descriptor() Descriptor {
	return mediaToolJobDescriptor(Descriptor{
		ProfileID: ProfileDashScopeTTS, Provider: "dashscope", EndpointKind: EndpointSpeechSynthesis,
		UpstreamEndpoint: DashScopeTTSEndpoint, ProviderFinality: "complete_bounded_pcm_wave_with_optional_reported_tokens",
		SupportedModalities:  []string{"text_to_speech"},
		ObservedUnitClasses:  []UnitClass{UnitInputTokens, UnitOutputTokens},
		UnsupportedSemantics: []string{"streaming", "voice_cloning", "voice_design", "voice_selection", "ssml"},
	})
}

func (p dashScopeSpeechSynthesisProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if !IsDashScopeBeijingMediaOrigin(input.BaseURL) || input.MappedModel != DashScopeTTS {
		return nil, nil, protocolError(ErrInvalidConfiguration, "dashscope_speech_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, "text_to_speech")
	if err != nil {
		return nil, nil, err
	}
	if r.InputMedia != "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > MediaTTSMaxTextBytes {
		return nil, nil, protocolError(ErrInvalidRequest, "dashscope_tts_text_invalid")
	}
	payload, err := json.Marshal(map[string]any{
		"model": DashScopeTTS, "input": map[string]any{"text": r.Prompt, "voice": "longanhuan_v3.1",
			"format": "wav", "sample_rate": dashScopeSpeechSampleRate, "enable_ssml": false},
	})
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, origin, DashScopeTTSEndpoint, input.APIKey, input.RequestID, payload)
	return req, &Binding{profileID: ProfileDashScopeTTS, profileVersion: "1.0.0", mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}, err
}

func (p dashScopeSpeechSynthesisProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(b, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
		Output    struct {
			Finish string `json:"finish_reason"`
			Audio  struct {
				Data string `json:"data"`
				URL  string `json:"url"`
			} `json:"audio"`
		} `json:"output"`
		Usage dashScopeSpeechUsage `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if wire.Code != "" || !ValidMediaTaskID(wire.RequestID) || wire.Output.Finish != "stop" ||
		wire.Output.Audio.Data != "" || wire.Usage.Duration != "" {
		return Outcome{}, protocolError(ErrInvalidResponse, "dashscope_tts_result_invalid")
	}
	asset, parseErr := url.Parse(wire.Output.Audio.URL)
	if parseErr != nil {
		return Outcome{}, protocolError(ErrInvalidResponse, "dashscope_tts_asset_origin_invalid")
	}
	// The provider publishes HTTP spellings for its Beijing OSS links.
	// The stored query and object path are preserved; transport is TLS-only.
	if asset.Scheme == "http" && asset.Host == dashScopeSpeechObjectHost {
		asset.Scheme = "https"
	}
	if !p.AcceptsResultURL(asset) {
		return Outcome{}, protocolError(ErrInvalidResponse, "dashscope_tts_asset_origin_invalid")
	}
	units, err := reportedMediaTokenDirections(wire.Usage.Input, wire.Usage.Output, wire.Usage.Total)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{ContractVersion: ContractVersion, ProfileID: ProfileDashScopeTTS, MappedModel: b.mappedModel,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: units, ResultURL: asset.String()}, nil
}

func (dashScopeSpeechSynthesisProfile) AcceptsResultURL(u *url.URL) bool {
	return validMediaAssetURL(u) && u.Hostname() == dashScopeSpeechObjectHost && strings.HasSuffix(u.Path, ".wav")
}

func (dashScopeSpeechSynthesisProfile) ValidateRemoteMedia(body []byte) (*MediaArtifact, error) {
	info, ok := ValidatePCM16WAV(body)
	if !ok || !info.HasSignal || info.DurationMS > MediaTTSMaxOutputMS || info.SampleRate != dashScopeSpeechSampleRate || info.Channels != 1 {
		return nil, protocolError(ErrInvalidResponse, "dashscope_tts_wave_invalid")
	}
	return &MediaArtifact{Bytes: body, MediaType: "audio/wav"}, nil
}

func (dashScopeSpeechSynthesisProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
