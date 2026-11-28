package modelcapabilityqualification

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	ChatInlineWAVProbeID          = "unirouter.capability-probe.chat-inline-wav.v1"
	ChatInlineWAVProbeVersion     = "1.0.0"
	ChatInlineWAVProbeOutputLimit = 48
)

func chatInlineWAVComposition(b Binding, p modelprofile.Profile) bool {
	contract, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	return ok && contract.BoundedAudioWAV &&
		b.CapabilityID == modelprofile.CapabilityAudio && len(p.UpstreamModelIDs) == 1 &&
		p.UpstreamModelIDs[0] == b.ExpectedUpstreamModel && p.Provider == b.ExpectedProvider
}

func BuildChatInlineWAVProbeRequest(b Binding, samples []byte) ([]byte, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != ChatInlineWAVProbeID || b.ProbeContractVersion != ChatInlineWAVProbeVersion ||
		!chatInlineWAVComposition(b, p) {
		return nil, ErrUnsupported
	}
	media := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(samples)
	if _, _, err := endpointprotocol.DecodeASRInputWAV(media); err != nil {
		return nil, err
	}
	contract, _ := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)

	payload, err := boundedChatProbePayload(p, b.ExpectedUpstreamModel, "", ChatInlineWAVProbeOutputLimit)
	if err != nil {
		return nil, err
	}
	audio := map[string]string{"data": media}
	if contract.InlineAudioOmitMIME {
		audio["format"] = "wav"
		payload["modalities"] = []string{"text"}
	}
	if p.ProtocolProfile == modelprofile.MiMo26InlineAudioProtocol {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}
	payload["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{
		{"type": "text", "text": "Listen to the audio. Reply with exactly the five English digits spoken, in their original order, and nothing else."},
		{"type": "input_audio", "input_audio": audio},
	}}}
	return json.Marshal(payload)
}

func ObserveChatInlineWAV(b Binding, contentType string, reader io.Reader) (string, adapters.Usage, int, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != ChatInlineWAVProbeID || b.ProbeContractVersion != ChatInlineWAVProbeVersion ||
		!chatInlineWAVComposition(b, p) {
		return "", adapters.Usage{}, 0, ErrUnsupported
	}
	response, usage, size, err := readExactChatProbeResponse(contentType, b.ExpectedUpstreamModel, p.CacheUsageProfile, ChatInlineWAVProbeOutputLimit, reader)
	if err != nil {
		return "", adapters.Usage{}, size, err
	}
	if !validChatProbeMediaTokens(response.Usage.PromptDetails.AudioTokens, usage.PromptTokens) || !observedChatProbeWithoutThinking(response, usage) {
		return "", adapters.Usage{}, size, fmt.Errorf("audio Probe response has no matching non-thinking audio evidence")
	}
	return response.Choices[0].Message.Content, usage, size, nil
}
