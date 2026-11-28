package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	ProfileOpenAITranscribe         = "unirouter.openai.audio.transcribe.v1"
	OpenAITranscribe                = "gpt-transcribe"
	OpenAITranscribeEndpoint        = "/v1/audio/transcriptions"
	openAITranscribeDurationLimitMS = 4000
)

type openAITranscribeProfile struct{}

func (openAITranscribeProfile) Descriptor() Descriptor {
	return mediaToolJobDescriptor(Descriptor{
		ProfileID: ProfileOpenAITranscribe, Provider: "openai", EndpointKind: EndpointTranscription,
		UpstreamEndpoint: OpenAITranscribeEndpoint, ProviderFinality: "completed_transcript_with_optional_final_usage",
		SupportedModalities:  []string{"speech_to_text"},
		ObservedUnitClasses:  []UnitClass{UnitInputAudioDurationMS, UnitInputTokens, UnitInputTextTokens, UnitInputAudioTokens, UnitOutputTokens},
		UnsupportedSemantics: []string{"remote_input_uri", "streaming", "realtime", "diarization", "transcription_prompt"},
	})
}

func (p openAITranscribeProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	origin, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSuffix(input.BaseURL, "/") != "https://api.openai.com" || input.MappedModel != OpenAITranscribe {
		return nil, nil, protocolError(ErrInvalidConfiguration, "openai_transcribe_binding_invalid")
	}
	r, err := decodeMediaToolInput(input, "speech_to_text")
	if err != nil {
		return nil, nil, err
	}
	if r.Prompt != "" {
		return nil, nil, protocolError(ErrInvalidRequest, "speech_request_invalid")
	}
	wave, info, err := DecodeASRInputWAV(r.InputMedia)
	if err != nil {
		return nil, nil, err
	}
	d := p.Descriptor()
	b := &Binding{profileID: d.ProfileID, profileVersion: d.ProfileVersion, mappedModel: input.MappedModel,
		requestedModel: input.RequestedModel, outputCount: 1, inputDurationMS: info.DurationMS}
	req, err := buildMediaMultipart(ctx, origin, d.UpstreamEndpoint, input, "speech.wav", wave,
		mediaFormField{Name: "model", Value: OpenAITranscribe},
		mediaFormField{Name: "response_format", Value: "json"},
		mediaFormField{Name: "stream", Value: "false"})
	return req, b, err
}

type openAITranscribeUsage struct {
	Type    string      `json:"type"`
	Seconds json.Number `json:"seconds"`
	Input   *int64      `json:"input_tokens"`
	Output  *int64      `json:"output_tokens"`
	Total   *int64      `json:"total_tokens"`
	Details *struct {
		Audio *int64 `json:"audio_tokens"`
		Text  *int64 `json:"text_tokens"`
	} `json:"input_token_details"`
}

func (u *openAITranscribeUsage) units(inputDurationMS int64) ([]UnitObservation, error) {
	invalid := protocolError(ErrInvalidResponse, "openai_transcribe_usage_invalid")
	if u == nil {
		return []UnitObservation{}, nil
	}
	switch u.Type {
	case "duration":
		raw, err := mediaMilliseconds(u.Seconds.String())
		ms, parseErr := strconv.ParseInt(raw, 10, 64)
		// The API may report whole seconds for a fractional recording.
		if err != nil || parseErr != nil || ms <= 0 || ms > openAITranscribeDurationLimitMS ||
			ms < inputDurationMS-1000 || ms > inputDurationMS+1000 ||
			u.Input != nil || u.Output != nil || u.Total != nil || u.Details != nil {
			return nil, invalid
		}
		return []UnitObservation{observedUnit(UnitInputAudioDurationMS, raw)}, nil
	case "tokens":
		if u.Seconds != "" || u.Input == nil || u.Output == nil || u.Total == nil ||
			*u.Input <= 0 || *u.Output <= 0 || *u.Total < *u.Input || *u.Total-*u.Input != *u.Output {
			return nil, invalid
		}
		units := []UnitObservation{observedUnit(UnitInputTokens, strconv.FormatInt(*u.Input, 10))}
		if u.Details != nil {
			if u.Details.Text != nil {
				units = append(units, observedUnit(UnitInputTextTokens, strconv.FormatInt(*u.Details.Text, 10)))
			}
			if u.Details.Audio != nil {
				units = append(units, observedUnit(UnitInputAudioTokens, strconv.FormatInt(*u.Details.Audio, 10)))
			}
		}
		units = append(units, observedUnit(UnitOutputTokens, strconv.FormatInt(*u.Output, 10)))
		if !validTranscriptTokenUnits(units) {
			return nil, invalid
		}
		return units, nil
	default:
		return nil, invalid
	}
}

func (p openAITranscribeProfile) ParseResponse(_ context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	d := p.Descriptor()
	if err := requireBinding(b, d); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(status); err != nil {
		return Outcome{}, err
	}
	var wire struct {
		Text  string                 `json:"text"`
		Usage *openAITranscribeUsage `json:"usage"`
	}
	if err := decodeResponseWithLimit(body, &wire, 128<<10); err != nil {
		return Outcome{}, err
	}
	if !ValidMediaTranscript([]byte(wire.Text)) {
		return Outcome{}, protocolError(ErrInvalidResponse, "openai_transcribe_result_invalid")
	}
	units, err := wire.Usage.units(b.inputDurationMS)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ContractVersion: ContractVersion, ProfileID: d.ProfileID, MappedModel: b.mappedModel,
		ProviderState: MediaTaskCompleted, OutputCount: 1, Units: units,
		Media: &MediaArtifact{Bytes: []byte(wire.Text), MediaType: "text/plain; charset=utf-8"},
	}, nil
}

func (openAITranscribeProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return classifyHTTPStatus(status)
}
