package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	ProfileDeepSeekASR                 = "unirouter.deepseek.tools.asr.v1"
	ProfileDeepSeekTTS                 = "unirouter.deepseek.tools.tts.v1"
	DeepSeekASR                        = "deepseek-asr"
	DeepSeekTTS                        = "deepseek-tts"
	DeepSeekASREndpoint                = "/v1/tools/asr"
	DeepSeekTTSEndpoint                = "/v1/tools/tts"
	MediaTTSMaxTextBytes               = 256
	MediaTTSMaxOutputMS                = 30_000
	UnitInputAudioDurationMS UnitClass = "input_audio_milliseconds"
)

type deepSeekSpeechProfile struct {
	synthesize bool
}

func (p deepSeekSpeechProfile) Descriptor() Descriptor {
	d := Descriptor{
		ProfileID: ProfileDeepSeekASR, Provider: "deepseek", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: DeepSeekASREndpoint, ProviderFinality: "completed_transcript_and_reported_duration",
		SupportedModalities: []string{"speech_to_text"}, ObservedUnitClasses: []UnitClass{UnitInputAudioDurationMS},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "ogg", "stereo"},
	}
	if p.synthesize {
		d.ProfileID, d.EndpointKind, d.UpstreamEndpoint = ProfileDeepSeekTTS, EndpointSpeechSynthesis, DeepSeekTTSEndpoint
		d.ProviderFinality = "complete_bounded_pcm_wave"
		d.SupportedModalities, d.ObservedUnitClasses = []string{"text_to_speech"}, nil
		d.UnsupportedSemantics = []string{"websocket", "raw_pcm", "voice_selection", "provider_metering"}
	}
	return mediaToolJobDescriptor(d)
}

func (p deepSeekSpeechProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	base, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	d := p.Descriptor()
	wantModel := DeepSeekASR
	if p.synthesize {
		wantModel = DeepSeekTTS
	}
	if strings.TrimSuffix(input.BaseURL, "/") != DeepSeekOCROrigin || input.MappedModel != wantModel {
		return nil, nil, protocolError(ErrInvalidConfiguration, "speech_binding_invalid")
	}
	request, err := decodeMediaToolInput(input, d.SupportedModalities[0])
	if err != nil {
		return nil, nil, err
	}
	b := &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion, mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}
	if p.synthesize {
		if request.InputMedia != "" || strings.TrimSpace(request.Prompt) == "" || len(request.Prompt) > MediaTTSMaxTextBytes {
			return nil, nil, protocolError(ErrInvalidRequest, "tts_text_invalid")
		}
		wire, err := json.Marshal(map[string]any{"text": request.Prompt, "voice_id": "stella", "format": "wav", "filter_markdown": false})
		if err != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
		}
		req, err := buildPOSTRequest(ctx, base, d.UpstreamEndpoint, input.APIKey, input.RequestID, wire)
		return req, b, err
	}
	if request.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	audio, info, err := DecodeASRInputWAV(request.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	b.inputDurationMS = info.DurationMS
	req, err := buildMediaMultipart(ctx, base, d.UpstreamEndpoint, input, "speech.wav", audio)
	return req, b, err
}

func (p deepSeekSpeechProfile) ParseResponse(_ context.Context, binding *Binding, status int, body []byte) (Outcome, error) {
	d := p.Descriptor()
	if err := requireBinding(binding, d); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	out := Outcome{ContractVersion: ContractVersion, ProfileID: d.ProfileID, ProviderState: MediaTaskCompleted,
		MappedModel: binding.mappedModel, OutputCount: 1, Units: []UnitObservation{}}
	if p.synthesize {
		info, ok := ValidatePCM16WAV(body)
		if !ok || !info.HasSignal || info.DurationMS > MediaTTSMaxOutputMS {
			return Outcome{}, protocolError(ErrInvalidResponse, "tts_wave_incomplete")
		}
		out.Media = &MediaArtifact{Bytes: body, MediaType: "audio/wav"}
		return out, nil
	}
	var wire struct {
		RequestID  string `json:"request_id"`
		Text       string `json:"text"`
		DurationMS int64  `json:"duration_ms"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if !ValidMediaTaskID(wire.RequestID) || !ValidMediaTranscript([]byte(wire.Text)) || wire.DurationMS <= 0 ||
		binding.inputDurationMS <= 0 || wire.DurationMS > binding.inputDurationMS+10 || wire.DurationMS < binding.inputDurationMS-10 {
		return Outcome{}, protocolError(ErrInvalidResponse, "asr_result_invalid")
	}
	out.Units = append(out.Units, observedUnit(UnitInputAudioDurationMS, strconv.FormatInt(wire.DurationMS, 10)))
	out.Media = &MediaArtifact{Bytes: []byte(wire.Text), MediaType: "text/plain; charset=utf-8"}
	return out, nil
}

func (deepSeekSpeechProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
