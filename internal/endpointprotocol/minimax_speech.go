package endpointprotocol

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	ProfileMiniMaxASR                = "unirouter.minimax.speech.asr-1.v1"
	ProfileMiniMaxTTS                = "unirouter.minimax.speech.2-8-turbo.v1"
	MiniMaxASR                       = "asr-1.0"
	MiniMaxTTS                       = "speech-2.8-turbo"
	MiniMaxASREndpoint               = "/v1/speech_to_text"
	MiniMaxTTSEndpoint               = "/v1/t2a_v2"
	UnitInputBillableChars UnitClass = "input_billable_characters"
)

type miniMaxSpeechProfile struct{ synthesize bool }

func (p miniMaxSpeechProfile) Descriptor() Descriptor {
	d := Descriptor{
		ProfileID: ProfileMiniMaxASR, Provider: "minimax", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: MiniMaxASREndpoint, ProviderFinality: "completed_transcript_and_reported_duration",
		SupportedModalities: []string{"speech_to_text"}, ObservedUnitClasses: []UnitClass{UnitInputAudioDurationMS},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "word_timestamps", "stereo"},
	}
	if p.synthesize {
		d.ProfileID, d.EndpointKind, d.UpstreamEndpoint = ProfileMiniMaxTTS, EndpointSpeechSynthesis, MiniMaxTTSEndpoint
		d.SupportedModalities = []string{"text_to_speech"}
		d.ProviderFinality = "complete_bounded_pcm_wave_with_optional_reported_characters"
		d.ObservedUnitClasses = []UnitClass{UnitInputBillableChars}
		d.UnsupportedSemantics = []string{"websocket", "streaming", "raw_pcm", "voice_selection", "subtitle"}
	}
	return mediaToolJobDescriptor(d)
}

func (p miniMaxSpeechProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	base, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	d := p.Descriptor()
	want := MiniMaxASR
	if p.synthesize {
		want = MiniMaxTTS
	}
	if strings.TrimSuffix(input.BaseURL, "/") != MiniMaxChinaImageOrigin || input.MappedModel != want {
		return nil, nil, protocolError(ErrInvalidConfiguration, "minimax_speech_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, d.SupportedModalities[0])
	if err != nil {
		return nil, nil, err
	}
	b := &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion, mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1}
	if p.synthesize {
		if r.InputMedia != "" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > MediaTTSMaxTextBytes ||
			strings.Contains(r.Prompt, "<#") {
			return nil, nil, protocolError(ErrInvalidRequest, "minimax_tts_text_invalid")
		}
		wire, err := json.Marshal(map[string]any{
			"model": want, "text": r.Prompt, "stream": false, "output_format": "hex",
			"voice_setting": map[string]string{"voice_id": "male-qn-qingse"},
			"audio_setting": map[string]any{"format": "wav", "sample_rate": 32000, "channel": 1},
		})
		if err != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
		}
		req, err := buildPOSTRequest(ctx, base, d.UpstreamEndpoint, input.APIKey, input.RequestID, wire)
		return req, b, err
	}
	if r.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	audio, info, err := DecodeASRInputWAV(r.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	b.inputDurationMS = info.DurationMS
	req, err := buildMediaMultipart(ctx, base, d.UpstreamEndpoint, input, "speech.wav", audio,
		mediaFormField{Name: "model", Value: want},
		mediaFormField{Name: "response_format", Value: "json"},
		mediaFormField{Name: "stream", Value: "false"})
	return req, b, err
}

func (p miniMaxSpeechProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	d := p.Descriptor()
	if err := requireBinding(b, d); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	out := Outcome{ContractVersion: ContractVersion, ProfileID: d.ProfileID, MappedModel: b.mappedModel, ProviderState: MediaTaskCompleted, OutputCount: 1}
	if p.synthesize {
		var wire struct {
			Data struct {
				Audio  string `json:"audio"`
				Status int    `json:"status"`
			} `json:"data"`
			Extra struct {
				Characters *int    `json:"usage_characters"`
				Rate       *uint32 `json:"audio_sample_rate"`
				Format     string  `json:"audio_format"`
				Size       *int    `json:"audio_size"`
				Channels   *uint16 `json:"audio_channel"`
			} `json:"extra_info"`
			Base struct {
				Code *int `json:"status_code"`
			} `json:"base_resp"`
		}
		if err := decodeResponseWithLimit(body, &wire, 6<<20); err != nil {
			return Outcome{}, err
		}
		if wire.Base.Code == nil || *wire.Base.Code != 0 || wire.Data.Status != 2 ||
			wire.Extra.Characters != nil && (*wire.Extra.Characters < 0 || *wire.Extra.Characters > 2*MediaTTSMaxTextBytes) {
			return Outcome{}, protocolError(ErrInvalidResponse, "minimax_tts_result_invalid")
		}
		// The response schema omits WAV from extra_info.audio_format even
		// though the requested data.audio encoding supports a PCM WAV.
		format := wire.Extra.Format
		wave, err := hex.DecodeString(wire.Data.Audio)
		info, ok := ValidatePCM16WAV(wave)
		if err != nil || !ok || !info.HasSignal || info.DurationMS > MediaTTSMaxOutputMS ||
			info.SampleRate != 32000 || info.Channels != 1 ||
			(format != "" && format != "wav" && format != "pcm") ||
			(wire.Extra.Rate != nil && *wire.Extra.Rate != info.SampleRate) ||
			(wire.Extra.Channels != nil && *wire.Extra.Channels != info.Channels) ||
			(wire.Extra.Size != nil && *wire.Extra.Size != len(wave)) {
			return Outcome{}, protocolError(ErrInvalidResponse, "minimax_tts_wave_invalid")
		}
		out.Media = &MediaArtifact{Bytes: wave, MediaType: "audio/wav"}
		out.Units = []UnitObservation{}
		if wire.Extra.Characters != nil {
			out.Units = append(out.Units, observedUnit(UnitInputBillableChars, strconv.Itoa(*wire.Extra.Characters)))
		}
		return out, nil
	}
	var wire struct {
		TraceID  string      `json:"trace_id"`
		Text     string      `json:"text"`
		Duration json.Number `json:"duration"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	raw, err := mediaMilliseconds(wire.Duration.String())
	ms, convertErr := strconv.ParseInt(raw, 10, 64)
	if err != nil || convertErr != nil || !ValidMediaTaskID(wire.TraceID) ||
		!ValidMediaTranscript([]byte(wire.Text)) || ms <= 0 || b.inputDurationMS <= 0 ||
		ms > b.inputDurationMS+10 || ms < b.inputDurationMS-10 {
		return Outcome{}, protocolError(ErrInvalidResponse, "minimax_asr_result_invalid")
	}
	out.Media = &MediaArtifact{Bytes: []byte(wire.Text), MediaType: "text/plain; charset=utf-8"}
	out.Units = []UnitObservation{observedUnit(UnitInputAudioDurationMS, raw)}
	return out, nil
}

func (miniMaxSpeechProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
