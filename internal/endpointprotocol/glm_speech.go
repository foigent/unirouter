package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	ProfileGLMASR  = "unirouter.glm.speech.asr-2512.v1"
	ProfileGLMTTS  = "unirouter.glm.speech.tts.v1"
	GLMASR         = "glm-asr-2512"
	GLMTTS         = "glm-tts"
	GLMASREndpoint = "/api/paas/v4/audio/transcriptions"
	GLMTTSEndpoint = "/api/paas/v4/audio/speech"
)

type glmSpeechProfile struct{ synthesize bool }

func (p glmSpeechProfile) Descriptor() Descriptor {
	d := Descriptor{
		ProfileID: ProfileGLMASR, Provider: "glm", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: GLMASREndpoint, ProviderFinality: "completed_transcript",
		SupportedModalities:  []string{"speech_to_text"},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "hotwords", "provider_metering"},
	}
	if p.synthesize {
		d.ProfileID, d.EndpointKind, d.UpstreamEndpoint = ProfileGLMTTS, EndpointSpeechSynthesis, GLMTTSEndpoint
		d.ProviderFinality = "complete_bounded_pcm_wave"
		d.SupportedModalities = []string{"text_to_speech"}
		d.UnsupportedSemantics = []string{"streaming", "raw_pcm", "voice_selection", "provider_metering"}
	}
	return mediaToolJobDescriptor(d)
}

func (p glmSpeechProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	d := p.Descriptor()
	model := GLMASR
	if p.synthesize {
		model = GLMTTS
	}
	if strings.TrimSuffix(input.BaseURL, "/") != GLMChinaMediaOrigin || input.MappedModel != model {
		return nil, nil, protocolError(ErrInvalidConfiguration, "glm_speech_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, d.SupportedModalities[0])
	if err != nil {
		return nil, nil, err
	}
	b := &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion, mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}
	if p.synthesize {
		if r.InputMedia != "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > MediaTTSMaxTextBytes {
			return nil, nil, protocolError(ErrInvalidRequest, "glm_tts_text_invalid")
		}
		wire, err := json.Marshal(map[string]any{
			"model": model, "input": r.Prompt, "voice": "tongtong", "response_format": "wav", "stream": false,
		})
		if err != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
		}
		req, err := buildPOSTRequest(ctx, origin, d.UpstreamEndpoint, input.APIKey, input.RequestID, wire)
		return req, b, err
	}
	if r.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	wave, _, err := DecodeASRInputWAV(r.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	req, err := buildMediaMultipart(ctx, origin, d.UpstreamEndpoint, input, "speech.wav", wave,
		mediaFormField{Name: "model", Value: model}, mediaFormField{Name: "stream", Value: "false"})
	return req, b, err
}

func (p glmSpeechProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	d := p.Descriptor()
	if err := requireBinding(b, d); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	out := Outcome{
		ContractVersion: ContractVersion, ProfileID: d.ProfileID, MappedModel: b.mappedModel,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: []UnitObservation{},
	}
	if p.synthesize {
		info, ok := ValidatePCM16WAV(body)
		if !ok || !info.HasSignal || info.DurationMS > MediaTTSMaxOutputMS {
			return Outcome{}, protocolError(ErrInvalidResponse, "glm_tts_wave_incomplete")
		}
		out.Media = &MediaArtifact{Bytes: body, MediaType: "audio/wav"}
		return out, nil
	}
	var wire struct {
		ID        string `json:"id"`
		Created   int64  `json:"created"`
		RequestID string `json:"request_id"`
		Model     string `json:"model"`
		Text      string `json:"text"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if !ValidMediaTaskID(wire.ID) || !ValidMediaTaskID(wire.RequestID) || wire.Created <= 0 ||
		wire.Model != b.mappedModel || !ValidMediaTranscript([]byte(wire.Text)) {
		return Outcome{}, protocolError(ErrInvalidResponse, "glm_asr_result_invalid")
	}
	out.Media = &MediaArtifact{Bytes: []byte(wire.Text), MediaType: "text/plain; charset=utf-8"}
	return out, nil
}

func (glmSpeechProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
