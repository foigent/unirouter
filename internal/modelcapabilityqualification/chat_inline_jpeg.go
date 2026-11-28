package modelcapabilityqualification

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	ChatInlineJPEGProbeID          = "unirouter.capability-probe.chat-inline-jpeg.v1"
	ChatInlineJPEGProbeVersion     = "1.0.0"
	chatInlineJPEGProbeOutputLimit = 24
	InlineJPEGChallengeQuestion    = "This image contains a 2-row by 3-column grid of colored blocks. Reading top row left to right then bottom row left to right, give exactly six digits and nothing else. Red=1, green=2, blue=3, yellow=4."
)

type chatInlineJPEGWire struct {
	outputField                string
	outputLimit                int
	thinking                   string
	effort                     string
	requireReportedImageTokens bool
	disableQwenThinking        bool
}

func chatInlineJPEGComposition(b Binding, p modelprofile.Profile) bool {
	_, ok := chatInlineJPEGRequestWire(b, p)
	return ok
}

func chatInlineJPEGRequestWire(b Binding, p modelprofile.Profile) (chatInlineJPEGWire, bool) {
	if b.CapabilityID != modelprofile.CapabilityVision ||
		!slices.Equal(p.UpstreamModelIDs, []string{b.ExpectedUpstreamModel}) {
		return chatInlineJPEGWire{}, false
	}
	var provider, endpoint, reasoning, cache string
	var wire chatInlineJPEGWire
	switch p.ProtocolProfile {
	case modelprofile.DeepSeekInlineJPEGProtocol:
		provider, endpoint, reasoning, cache = "deepseek", modelprofile.EndpointClassDeepSeekOfficialOpenAI,
			"reasoning.deepseek-v4-flash-thinking-toggle.v5", "cache-usage.deepseek-v4.v1"
		wire = chatInlineJPEGWire{outputField: "max_tokens", outputLimit: chatInlineJPEGProbeOutputLimit, thinking: "disabled"}
	case modelprofile.KimiK3InlineJPEGProtocol, modelprofile.KimiK3InlineVideoProtocol:
		provider, endpoint, reasoning, cache = "kimi", modelprofile.EndpointClassKimiPlatformOpenAIV1,
			"reasoning.kimi-k3-effort.v2", modelprofile.KimiChatReadWriteCacheUsageProfile
		wire = chatInlineJPEGWire{outputField: "max_completion_tokens", outputLimit: 512, effort: "low"}
	case modelprofile.MiniMaxM31InlineJPEGProtocol, modelprofile.MiniMaxM31InlineVideoProtocol:
		provider, endpoint, reasoning, cache = "minimax", modelprofile.EndpointClassMiniMaxOfficialCNOpenAIV1,
			"reasoning.minimax-m31-required-split.v1", "cache-usage.minimax-m31-openai.v1"
		wire = chatInlineJPEGWire{outputField: "max_completion_tokens", outputLimit: 512, thinking: "adaptive", effort: "low"}
	case modelprofile.GLM53InlineJPEGProtocol, modelprofile.GLM53InlineFileProtocol, modelprofile.GLM53PublicVideoProtocol:
		provider, endpoint, reasoning, cache = "glm", modelprofile.EndpointClassGLMOfficialOpenAIV4,
			"reasoning.glm-5.3-required.v1", "cache-usage.glm-openai.v1"
		wire = chatInlineJPEGWire{outputField: "max_tokens", outputLimit: 512, thinking: "enabled"}
	case modelprofile.QwenHostedVisionProtocol:
		if !modelprofile.QwenHostedSplitProfile(p) {
			return chatInlineJPEGWire{}, false
		}
		return chatInlineJPEGWire{outputField: "max_tokens", outputLimit: 48, disableQwenThinking: true}, true
	case modelprofile.QwenOmniProtocol:
		provider, endpoint, reasoning, cache = "dashscope", modelprofile.EndpointClassDashScopeBeijingWorkspacePayGo,
			modelprofile.QwenOmniReasoningProfile, modelprofile.QwenOmniUsageProfile
		wire = chatInlineJPEGWire{outputField: "max_tokens", outputLimit: chatInlineJPEGProbeOutputLimit, effort: "none", requireReportedImageTokens: true}
	case modelprofile.QwenHybridInlineJPEGProtocol, modelprofile.QwenHybridInlineVideoProtocol:
		return chatInlineJPEGWire{outputField: "max_completion_tokens", outputLimit: 512}, qwenHybridMediaProfile(p)
	case modelprofile.MiMo26InlineJPEGProtocol, modelprofile.MiMo26InlineAudioProtocol:
		provider, endpoint, reasoning, cache = "mimo", modelprofile.EndpointClassMiMoChinaPayGoOpenAIV1,
			"reasoning.mimo-v2.5-thinking-toggle.v1", "cache-usage.mimo-openai.v1"
		wire = chatInlineJPEGWire{outputField: "max_completion_tokens", outputLimit: 512, requireReportedImageTokens: true}
	default:
		return chatInlineJPEGWire{}, false
	}
	return wire, p.Provider == provider && p.ReasoningProfile == reasoning && p.CacheUsageProfile == cache &&
		slices.Equal(p.EndpointClasses, []string{endpoint})
}

func qwenHybridMediaProfile(p modelprofile.Profile) bool {
	return p.Provider == "dashscope" && p.CacheUsageProfile == "cache-usage.qwen-context-cache.v1" &&
		slices.Contains([]string{"reasoning.qwen4-max-hybrid.v1", "reasoning.qwen3.8-flash-hybrid.v1"}, p.ReasoningProfile) &&
		slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassDashScopeBeijingSharedPayGo, modelprofile.EndpointClassDashScopeBeijingWorkspacePayGo})
}

// The expected text is process-local and is never projected into request or
// response evidence. The same challenge validates the reply to its exact image.
type ChatInlineJPEGChallenge struct {
	Body         []byte
	model        string
	cacheProfile string
	answer       string
	wire         chatInlineJPEGWire
}

func NewChatInlineJPEGChallenge(b Binding) (ChatInlineJPEGChallenge, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != ChatInlineJPEGProbeID || b.ProbeContractVersion != ChatInlineJPEGProbeVersion {
		return ChatInlineJPEGChallenge{}, ErrUnsupported
	}
	wire, ok := chatInlineJPEGRequestWire(b, p)
	if !ok {
		return ChatInlineJPEGChallenge{}, ErrUnsupported
	}
	var seed [6]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return ChatInlineJPEGChallenge{}, err
	}
	return buildChatInlineJPEGChallenge(b.ExpectedUpstreamModel, p.CacheUsageProfile, seed, wire)
}

func buildChatInlineJPEGChallenge(model, cacheProfile string, seed [6]byte, wire chatInlineJPEGWire) (ChatInlineJPEGChallenge, error) {
	media, answer, err := renderInlineJPEGChallenge(seed)
	if err != nil {
		return ChatInlineJPEGChallenge{}, err
	}
	payload := map[string]any{
		"model": model, "stream": false, wire.outputField: wire.outputLimit,
		"messages": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "text", "text": InlineJPEGChallengeQuestion},
			{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + media}},
		}}},
	}
	if wire.thinking != "" {
		payload["thinking"] = map[string]string{"type": wire.thinking}
	}
	if wire.disableQwenThinking {
		payload["enable_thinking"] = false
	}
	if wire.effort != "" {
		payload["reasoning_effort"] = wire.effort
	}
	body, err := json.Marshal(payload)
	return ChatInlineJPEGChallenge{Body: body, model: model, cacheProfile: cacheProfile, answer: answer, wire: wire}, err
}

func RandomInlineJPEGChallenge() (image, answer string, err error) {
	var seed [6]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return "", "", err
	}
	return renderInlineJPEGChallenge(seed)
}

func renderInlineJPEGChallenge(seed [6]byte) (string, string, error) {
	const blockSize = 48
	palette := [...]color.RGBA{{R: 255, A: 255}, {G: 200, A: 255}, {B: 255, A: 255}, {R: 255, G: 230, A: 255}}
	picture := image.NewRGBA(image.Rect(0, 0, blockSize*3, blockSize*2))
	answer := make([]byte, len(seed))
	for index, value := range seed {
		choice := value & 3
		answer[index] = '1' + choice
		x, y := index%3*blockSize, index/3*blockSize
		draw.Draw(picture, image.Rect(x, y, x+blockSize, y+blockSize), image.NewUniform(palette[choice]), image.Point{}, draw.Src)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(encoded.Bytes()), string(answer), nil
}

func (c ChatInlineJPEGChallenge) Observe(contentType string, reader io.Reader) (adapters.Usage, int, error) {
	response, usage, size, err := readExactChatProbeResponse(contentType, c.model, c.cacheProfile, c.wire.outputLimit, reader)
	if err != nil {
		return adapters.Usage{}, size, err
	}
	message := response.Choices[0].Message
	if c.answer == "" || strings.TrimSpace(message.Content) != c.answer || (c.wire.thinking == "disabled" && message.Reasoning != "") ||
		((c.wire.effort == "none" || c.wire.disableQwenThinking) && !observedChatProbeWithoutThinking(response, usage)) {
		return adapters.Usage{}, size, fmt.Errorf("Chat vision response did not match the image challenge")
	}
	if c.wire.requireReportedImageTokens && !validChatProbeMediaTokens(response.Usage.PromptDetails.ImageTokens, usage.PromptTokens) {
		return adapters.Usage{}, size, fmt.Errorf("Chat vision response lacks valid provider image tokens")
	}
	return usage, size, nil
}
