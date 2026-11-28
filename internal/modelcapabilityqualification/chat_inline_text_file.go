package modelcapabilityqualification

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	ChatInlineTextFileProbeID          = "unirouter.capability-probe.chat-inline-text-file.v1"
	ChatInlineTextFileProbeVersion     = "1.0.0"
	chatInlineTextFileProbeOutputLimit = 512
)

func chatInlineTextFileComposition(b Binding, p modelprofile.Profile) bool {
	return b.CapabilityID == modelprofile.CapabilityFiles && (p.ProtocolProfile == modelprofile.GLM53InlineFileProtocol || p.ProtocolProfile == modelprofile.GLM53PublicVideoProtocol) &&
		p.Provider == b.ExpectedProvider && p.Provider == "glm" &&
		p.ReasoningProfile == "reasoning.glm-5.3-required.v1" && p.CacheUsageProfile == "cache-usage.glm-openai.v1" &&
		slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassGLMOfficialOpenAIV4}) &&
		slices.Equal(p.UpstreamModelIDs, []string{b.ExpectedUpstreamModel})
}

type ChatInlineTextFileChallenge struct {
	Body                        []byte
	model, cacheProfile, answer string
}

func NewChatInlineTextFileChallenge(b Binding) (ChatInlineTextFileChallenge, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != ChatInlineTextFileProbeID || b.ProbeContractVersion != ChatInlineTextFileProbeVersion ||
		!chatInlineTextFileComposition(b, p) {
		return ChatInlineTextFileChallenge{}, ErrUnsupported
	}
	var seed [6]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return ChatInlineTextFileChallenge{}, err
	}
	answer := hex.EncodeToString(seed[:])
	file := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("Verification code: "+answer+"\n"))
	payload, err := boundedChatProbePayload(p, b.ExpectedUpstreamModel, "", chatInlineTextFileProbeOutputLimit)
	if err != nil {
		return ChatInlineTextFileChallenge{}, err
	}
	payload["thinking"] = map[string]string{"type": "enabled"}
	payload["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{
		{"type": "text", "text": "Read the attached text file. Reply with exactly the verification code, without any other text."},
		{"type": "file", "file": map[string]string{"file_data": file, "filename": "verification.txt"}},
	}}}
	body, err := json.Marshal(payload)
	return ChatInlineTextFileChallenge{Body: body, model: b.ExpectedUpstreamModel, cacheProfile: p.CacheUsageProfile, answer: answer}, err
}

func (c ChatInlineTextFileChallenge) Observe(contentType string, reader io.Reader) (adapters.Usage, int, error) {
	response, usage, size, err := readExactChatProbeResponse(contentType, c.model, c.cacheProfile, chatInlineTextFileProbeOutputLimit, reader)
	if err != nil {
		return adapters.Usage{}, size, err
	}
	if c.answer == "" || strings.TrimSpace(response.Choices[0].Message.Content) != c.answer {
		return adapters.Usage{}, size, fmt.Errorf("inline file Probe response did not match the file challenge")
	}
	return usage, size, nil
}
