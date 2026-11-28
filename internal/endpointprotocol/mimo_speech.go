package endpointprotocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	ProfileMiMoASR     = "unirouter.mimo.audio.asr.v1"
	ProfileMiMoTTS     = "unirouter.mimo.audio.tts.v1"
	MiMoASR            = "mimo-v2.5-asr"
	MiMoTTS            = "mimo-v2.5-tts"
	MiMoSpeechOrigin   = "https://api.xiaomimimo.com"
	MiMoSpeechEndpoint = "/v1/chat/completions"
)

type miMoSpeechProfile struct{ synthesize bool }

func (p miMoSpeechProfile) Descriptor() Descriptor {
	d := Descriptor{
		ProfileID: ProfileMiMoASR, Provider: "mimo", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: MiMoSpeechEndpoint, ProviderFinality: "completed_transcript_and_reported_duration",
		SupportedModalities: []string{"speech_to_text"}, ObservedUnitClasses: []UnitClass{UnitInputAudioDurationMS},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "asr_language_override", "provider_token_pricing"},
	}
	if p.synthesize {
		d.ProfileID, d.EndpointKind = ProfileMiMoTTS, EndpointSpeechSynthesis
		d.ProviderFinality = "complete_bounded_pcm_wave_with_optional_usage"
		d.SupportedModalities = []string{"text_to_speech"}
		d.ObservedUnitClasses = []UnitClass{UnitInputTokens, UnitOutputTokens}
		d.UnsupportedSemantics = []string{"streaming", "voice_cloning", "voice_design", "raw_pcm", "voice_selection"}
	}
	return mediaToolJobDescriptor(d)
}

func (p miMoSpeechProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	d := p.Descriptor()
	model := MiMoASR
	if p.synthesize {
		model = MiMoTTS
	}
	if strings.TrimSuffix(input.BaseURL, "/") != MiMoSpeechOrigin || input.MappedModel != model {
		return nil, nil, protocolError(ErrInvalidConfiguration, "mimo_audio_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, d.SupportedModalities[0])
	if err != nil {
		return nil, nil, err
	}
	b := &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion, mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}
	payload := map[string]any{"model": model, "stream": false}
	if p.synthesize {
		if r.InputMedia != "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > MediaTTSMaxTextBytes {
			return nil, nil, protocolError(ErrInvalidRequest, "mimo_tts_text_invalid")
		}
		payload["messages"] = []map[string]string{{"role": "assistant", "content": r.Prompt}}
		payload["audio"] = map[string]string{"voice": "mimo_default", "format": "wav"}
	} else {
		if r.Prompt != "" {
			return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
		}
		_, info, err := DecodeASRInputWAV(r.InputMedia)
		if err != nil {
			return nil, nil, err
		}
		b.inputDurationMS = info.DurationMS
		payload["messages"] = []map[string]any{{"role": "user", "content": inlineSpeechParts(r.InputMedia)}}
	}
	wire, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	req, err := buildPOSTRequest(ctx, origin, d.UpstreamEndpoint, input.APIKey, input.RequestID, wire)
	return req, b, err
}

func (p miMoSpeechProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	d := p.Descriptor()
	if err := requireBinding(b, d); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Index   *int   `json:"index"`
			Finish  string `json:"finish_reason"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
				Audio   *struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Seconds    json.Number `json:"seconds"`
			Prompt     *int64      `json:"prompt_tokens"`
			Completion *int64      `json:"completion_tokens"`
			Total      *int64      `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 6<<20); err != nil {
		return Outcome{}, err
	}
	if !ValidMediaTaskID(wire.ID) || wire.Created <= 0 || wire.Object != "chat.completion" ||
		wire.Model != b.mappedModel || len(wire.Choices) != 1 {
		return Outcome{}, protocolError(ErrInvalidResponse, "mimo_speech_result_invalid")
	}
	choice := wire.Choices[0]
	if choice.Finish != "stop" || choice.Index == nil || *choice.Index != 0 || choice.Message.Role != "assistant" {
		return Outcome{}, protocolError(ErrInvalidResponse, "mimo_speech_result_incomplete")
	}
	out := Outcome{ContractVersion: ContractVersion, ProfileID: d.ProfileID, MappedModel: b.mappedModel,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: []UnitObservation{}}
	if p.synthesize {
		if choice.Message.Audio == nil {
			return Outcome{}, protocolError(ErrInvalidResponse, "mimo_tts_wave_invalid")
		}
		wave, err := base64.StdEncoding.Strict().DecodeString(choice.Message.Audio.Data)
		info, ok := ValidatePCM16WAV(wave)
		if err != nil || !ok || !info.HasSignal || info.DurationMS > MediaTTSMaxOutputMS || info.Channels != 1 {
			return Outcome{}, protocolError(ErrInvalidResponse, "mimo_tts_wave_invalid")
		}
		if wire.Usage != nil {
			out.Units, err = reportedMediaTokenPair(wire.Usage.Prompt, wire.Usage.Completion, wire.Usage.Total)
			if err != nil || wire.Usage.Seconds != "" {
				return Outcome{}, protocolError(ErrInvalidResponse, "mimo_tts_usage_invalid")
			}
		}
		out.Media = &MediaArtifact{Bytes: wave, MediaType: "audio/wav"}
		return out, nil
	}
	if !ValidMediaTranscript([]byte(choice.Message.Content)) || choice.Message.Audio != nil || wire.Usage == nil {
		return Outcome{}, protocolError(ErrInvalidResponse, "mimo_asr_result_invalid")
	}
	raw, err := mediaMilliseconds(wire.Usage.Seconds.String())
	ms, parseErr := strconv.ParseInt(raw, 10, 64)
	if err != nil || parseErr != nil || ms <= 0 || ms > 4000 ||
		ms < b.inputDurationMS-1000 || ms > b.inputDurationMS+1000 {
		return Outcome{}, protocolError(ErrInvalidResponse, "mimo_asr_usage_invalid")
	}
	out.Units = []UnitObservation{observedUnit(UnitInputAudioDurationMS, raw)}
	out.Media = &MediaArtifact{Bytes: []byte(choice.Message.Content), MediaType: "text/plain; charset=utf-8"}
	return out, nil
}

func (miMoSpeechProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
