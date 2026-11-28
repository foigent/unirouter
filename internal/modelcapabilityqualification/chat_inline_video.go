package modelcapabilityqualification

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	ChatInlineVideoProbeID      = "unirouter.capability-probe.chat-inline-video.v1"
	ChatInlineVideoProbeVersion = "1.0.0"
)

// Each one-second segment contains four independent H.264 frames with fixed
// sample lengths. Copying a frame preserves the original MP4 sample table.
//
//go:embed testdata/inline-video-colors.mp4
var videoColorTemplate []byte

type chatInlineVideoWire struct {
	thinking, effort              string
	outputLimit                   int
	reportedVideoTokens, omitMIME bool
	disableQwenThinking           bool
	releaseVideoSample            bool
}

func chatInlineVideoSettings(b Binding, p modelprofile.Profile) (chatInlineVideoWire, bool) {
	if b.CapabilityID != modelprofile.CapabilityVideo || p.Provider != b.ExpectedProvider ||
		!slices.Equal(p.UpstreamModelIDs, []string{b.ExpectedUpstreamModel}) {
		return chatInlineVideoWire{}, false
	}
	switch p.ProtocolProfile {
	case modelprofile.GLM53PublicVideoProtocol:
		return chatInlineVideoWire{thinking: "enabled", outputLimit: 512, releaseVideoSample: true},
			p.Provider == "glm" && p.ReasoningProfile == "reasoning.glm-5.3-required.v1" && p.CacheUsageProfile == "cache-usage.glm-openai.v1" &&
				slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassGLMOfficialOpenAIV4})
	case modelprofile.QwenHybridInlineVideoProtocol:
		return chatInlineVideoWire{outputLimit: 48, disableQwenThinking: true}, qwenHybridMediaProfile(p)
	case modelprofile.QwenHostedVisionProtocol:
		return chatInlineVideoWire{outputLimit: 48, disableQwenThinking: true}, modelprofile.QwenHostedSplitProfile(p)
	case modelprofile.KimiK3InlineVideoProtocol:
		return chatInlineVideoWire{outputLimit: 512, effort: "low"}, p.Provider == "kimi" && p.ReasoningProfile == "reasoning.kimi-k3-effort.v2" &&
			p.CacheUsageProfile == modelprofile.KimiChatReadWriteCacheUsageProfile && slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassKimiPlatformOpenAIV1})
	case modelprofile.QwenOmniProtocol:
		return chatInlineVideoWire{effort: "none", outputLimit: 48, reportedVideoTokens: true, omitMIME: true}, modelprofile.QwenOmniChatProfile(p)
	case modelprofile.MiMo26InlineAudioProtocol:
		return chatInlineVideoWire{thinking: "disabled", outputLimit: 48, reportedVideoTokens: true},
			p.Provider == "mimo" && p.ReasoningProfile == "reasoning.mimo-v2.5-thinking-toggle.v1" && p.CacheUsageProfile == "cache-usage.mimo-openai.v1" &&
				slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassMiMoChinaPayGoOpenAIV1})
	case modelprofile.MiniMaxM31InlineVideoProtocol:
		return chatInlineVideoWire{thinking: "adaptive", effort: "low", outputLimit: 512},
			p.Provider == "minimax" && p.ReasoningProfile == "reasoning.minimax-m31-required-split.v1" && p.CacheUsageProfile == "cache-usage.minimax-m31-openai.v1" &&
				slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassMiniMaxOfficialCNOpenAIV1})
	default:
		return chatInlineVideoWire{}, false
	}
}

func chatInlineVideoComposition(b Binding, p modelprofile.Profile) bool {
	_, ok := chatInlineVideoSettings(b, p)
	return ok
}

type ChatInlineVideoChallenge struct {
	Body                        []byte
	model, cacheProfile, answer string
	settings                    chatInlineVideoWire
}

func inlineVideoColorSequence(seed [6]byte) ([]byte, string) {
	const dataOffset, stride, framesPerColor = 48, 128, 4
	clip := bytes.Clone(videoColorTemplate)
	code := make([]byte, len(seed))
	for second, value := range seed {
		color := int(value & 3)
		code[second] = '1' + byte(color)
		source := dataOffset + (color*framesPerColor+1)*stride
		for frame := range framesPerColor {
			offset := dataOffset + (second*framesPerColor+frame)*stride
			copy(clip[offset:offset+stride], videoColorTemplate[source:source+stride])
		}
	}
	return clip, string(code)
}

func NewChatInlineVideoChallenge(b Binding, releaseCommit string) (ChatInlineVideoChallenge, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != ChatInlineVideoProbeID || b.ProbeContractVersion != ChatInlineVideoProbeVersion {
		return ChatInlineVideoChallenge{}, ErrUnsupported
	}
	wire, ok := chatInlineVideoSettings(b, p)
	if !ok {
		return ChatInlineVideoChallenge{}, ErrUnsupported
	}
	var seed [6]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return ChatInlineVideoChallenge{}, err
	}
	prompt := "Read the background color at 0.5, 1.5, 2.5, 3.5, 4.5 and 5.5 seconds. Return exactly six digits in order. Red=1, green=2, blue=3, yellow=4."
	var media, answer string
	if wire.releaseVideoSample {
		var ok bool
		media, ok = modelprofile.ReleaseVideoSampleURL(releaseCommit)
		if !ok {
			return ChatInlineVideoChallenge{}, ErrUnsupported
		}
		answer, prompt = releaseSampleVideoQuestion(seed)
	} else {
		clip, code := inlineVideoColorSequence(seed)
		answer = code
		prefix := "data:video/mp4;base64,"
		if wire.omitMIME {
			prefix = "data:;base64,"
		}
		media = prefix + base64.StdEncoding.EncodeToString(clip)
	}
	payload, err := boundedChatProbePayload(p, b.ExpectedUpstreamModel, "", wire.outputLimit)
	if err != nil {
		return ChatInlineVideoChallenge{}, err
	}
	if wire.thinking != "" {
		payload["thinking"] = map[string]string{"type": wire.thinking}
	}
	if wire.effort != "" {
		payload["reasoning_effort"] = wire.effort
	}
	if wire.disableQwenThinking {
		payload["enable_thinking"] = false
	}
	if wire.omitMIME {
		payload["modalities"] = []string{"text"}
	}
	payload["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "video_url", "video_url": map[string]string{"url": media}},
	}}}
	body, err := json.Marshal(payload)
	return ChatInlineVideoChallenge{Body: body, model: b.ExpectedUpstreamModel, cacheProfile: p.CacheUsageProfile, answer: answer, settings: wire}, err
}

// The published immutable sample has red, green, blue and yellow in the
// first four seconds. The question changes the timestamp order on each Probe.
func releaseSampleVideoQuestion(seed [6]byte) (string, string) {
	times := make([]string, len(seed))
	answer := make([]byte, len(seed))
	for i, value := range seed {
		second := int(value & 3)
		times[i] = fmt.Sprintf("%d.5", second)
		answer[i] = '1' + byte(second)
	}
	return string(answer), "Read the background colors at these timestamps in the listed order (" + strings.Join(times, ", ") + "). Return exactly six digits. Red=1, green=2, blue=3, yellow=4."
}

func (c ChatInlineVideoChallenge) Observe(contentType string, reader io.Reader) (adapters.Usage, int, error) {
	response, usage, size, err := readExactChatProbeResponse(contentType, c.model, c.cacheProfile, c.settings.outputLimit, reader)
	if err != nil {
		return adapters.Usage{}, size, err
	}
	if c.answer == "" || strings.TrimSpace(response.Choices[0].Message.Content) != c.answer ||
		((c.settings.thinking == "disabled" || c.settings.effort == "none" || c.settings.disableQwenThinking) && !observedChatProbeWithoutThinking(response, usage)) {
		return adapters.Usage{}, size, fmt.Errorf("inline video Probe response did not match the temporal challenge")
	}
	if c.settings.reportedVideoTokens && !validChatProbeMediaTokens(response.Usage.PromptDetails.VideoTokens, usage.PromptTokens) {
		return adapters.Usage{}, size, fmt.Errorf("inline video Probe response has no matching provider video usage")
	}
	return usage, size, nil
}
