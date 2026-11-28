package modelcapabilityqualification

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const (
	PlainChatReasoningProbeID      = "unirouter.capability-probe.plain-chat-reasoning.v1"
	PlainChatReasoningProbeVersion = "1.0.0"
	PlainChatReasoningOutputLimit  = 1024
)

func plainChatReasoningComposition(b Binding, p modelprofile.Profile) bool {
	if b.CapabilityID != modelprofile.CapabilityReasoning || len(p.UpstreamModelIDs) != 1 ||
		modelprofile.DefaultChatReasoningCapability(p) != modelprofile.CapabilityReasoning {
		return false
	}
	switch p.Provider {
	case "hy3":
		return p.Region == "cn" && slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassHunyuanTokenHubOpenAIV1}) &&
			p.ProtocolProfile == modelprofile.PlainTextChatMaxTokensProtocol &&
			p.ReasoningProfile == "reasoning.hy4-chat.v1" && p.CacheUsageProfile == "cache-usage.hy3-openai.v1"
	case "doubao":
		return p.Region == "cn-beijing" && slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassDoubaoArkBeijingOpenAIV3}) &&
			p.ProtocolProfile == modelprofile.PlainTextChatCompletionTokensProtocol &&
			p.ReasoningProfile == "reasoning.doubao-seed-2.1-pro-toggle.v1" && p.CacheUsageProfile == "cache-usage.doubao-ark-chat.v1"
	case "xai":
		return p.Region == "global" && slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassXAIOfficialOpenAIV1}) &&
			p.ProtocolProfile == modelprofile.PlainTextChatMaxTokensProtocol &&
			p.ReasoningProfile == "reasoning.grok-4.7-chat.v1" && p.CacheUsageProfile == modelprofile.XAIChatAdditiveReasoningCacheUsageProfile
	case "openai":
		return p.Region == "global" && slices.Equal(p.EndpointClasses, []string{modelprofile.EndpointClassOpenAIOfficialV1}) &&
			p.ProtocolProfile == modelprofile.PlainTextChatCompletionTokensProtocol &&
			p.ReasoningProfile == "reasoning.openai-gpt-6.2-chat.v1" && p.CacheUsageProfile == "cache-usage.openai-gpt-5.6.v1"
	default:
		return false
	}
}

func PlainChatProbePayload(p modelprofile.Profile, model, text string, limit int) (map[string]any, error) {
	protocol, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	if !ok || !protocol.ExactTextOperationProbe {
		return nil, ErrUnsupported
	}
	return boundedChatProbePayload(p, model, text, limit)
}

func boundedChatProbePayload(p modelprofile.Profile, model, text string, limit int) (map[string]any, error) {
	protocol, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	if !ok || !protocol.OutputLimitModeled || protocol.OutputLimitField == "" || !slices.Equal(p.UpstreamModelIDs, []string{model}) ||
		int64(limit) < protocol.OutputLimitMin || int64(limit) > protocol.OutputLimitMax ||
		(p.ProviderMaxOutput > 0 && p.ProviderMaxOutput < limit) || (p.ExecutableMaxOutput > 0 && p.ExecutableMaxOutput < limit) {
		return nil, ErrUnsupported
	}
	payload := map[string]any{
		"model": model, "stream": false, protocol.OutputLimitField: limit,
		"messages": []map[string]string{{"role": "user", "content": text}},
	}
	if protocol.RequiredDisabledReasoningEffort != "" {
		payload["reasoning_effort"] = protocol.RequiredDisabledReasoningEffort
	}
	return payload, nil
}

func BuildPlainChatReasoningProbeRequest(b Binding) ([]byte, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != PlainChatReasoningProbeID || b.ProbeContractVersion != PlainChatReasoningProbeVersion ||
		!plainChatReasoningComposition(b, p) {
		return nil, ErrUnsupported
	}
	// Qualify the model's own default. The connectivity Probe can select lower
	// effort or disable reasoning, which would not prove this capability.
	payload, err := PlainChatProbePayload(p, b.ExpectedUpstreamModel,
		"Solve this problem and give only the number: 37 times 28 minus 149.", PlainChatReasoningOutputLimit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

type plainChatProbeResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Finish  string `json:"finish_reason"`
		Message struct {
			Role      string            `json:"role"`
			Content   string            `json:"content"`
			Reasoning string            `json:"reasoning_content"`
			Tools     []json.RawMessage `json:"tool_calls"`
			Function  json.RawMessage   `json:"function_call"`
			Refusal   string            `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
		PromptDetails    struct {
			ImageTokens json.RawMessage `json:"image_tokens"`
			AudioTokens json.RawMessage `json:"audio_tokens"`
			VideoTokens json.RawMessage `json:"video_tokens"`
		} `json:"prompt_tokens_details"`
		Details struct {
			ReasoningTokens json.RawMessage `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

func IsExactPlainChatResponse(contentType string, body []byte, model string, usage adapters.Usage, limit int, cacheProfile string) bool {
	_, err := parsePlainChatProbeResponse(contentType, body, model, usage, limit, cacheProfile)
	return err == nil
}

func readExactChatProbeResponse(contentType, model, cacheProfile string, limit int, reader io.Reader) (plainChatProbeResponse, adapters.Usage, int, error) {
	const maxBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return plainChatProbeResponse{}, adapters.Usage{}, len(body), fmt.Errorf("cannot read bounded Chat response")
	}
	usage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(body, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, cacheProfile)
	if err != nil || !present {
		return plainChatProbeResponse{}, adapters.Usage{}, len(body), fmt.Errorf("Chat Probe response has no valid provider usage")
	}
	response, err := parsePlainChatProbeResponse(contentType, body, model, usage, limit, cacheProfile)
	return response, usage, len(body), err
}

func validChatProbeMediaTokens(raw json.RawMessage, prompt int) bool {
	var reported int
	return json.Unmarshal(raw, &reported) == nil && reported > 0 && reported <= prompt
}

func observedChatProbeWithoutThinking(response plainChatProbeResponse, usage adapters.Usage) bool {
	if strings.TrimSpace(response.Choices[0].Message.Reasoning) != "" ||
		(usage.ProviderReasoningTokens != nil && *usage.ProviderReasoningTokens != 0) {
		return false
	}
	raw := response.Usage.Details.ReasoningTokens
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var reasoning int
	return json.Unmarshal(raw, &reasoning) == nil && reasoning == 0
}

func parsePlainChatProbeResponse(contentType string, body []byte, model string, usage adapters.Usage, limit int, cacheProfile string) (plainChatProbeResponse, error) {
	var response plainChatProbeResponse
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" || usage.Source != metering.UsageSourceUpstream ||
		usage.PromptTokens <= 0 || usage.CompletionTokens <= 0 || usage.CompletionTokens > limit || usage.ValidateContract() != nil ||
		strictjson.Validate(body) != nil || json.Unmarshal(body, &response) != nil || response.Model != model || len(response.Choices) != 1 {
		return response, fmt.Errorf("invalid exact Chat response")
	}
	if response.Usage.PromptTokens == nil || response.Usage.CompletionTokens == nil || response.Usage.TotalTokens == nil ||
		*response.Usage.PromptTokens != usage.PromptTokens ||
		*response.Usage.TotalTokens != usage.TotalTokens || usage.TotalTokens-usage.PromptTokens != usage.CompletionTokens {
		return response, fmt.Errorf("exact Chat response lacks reconciled provider totals")
	}
	visibleOrInclusive := *response.Usage.CompletionTokens
	if cacheProfile == modelprofile.XAIChatAdditiveReasoningCacheUsageProfile {
		// The adapter has already reconciled the exact additive wire. Require
		// some visible output here because this Probe proves a complete answer.
		if visibleOrInclusive <= 0 || visibleOrInclusive > usage.CompletionTokens {
			return response, fmt.Errorf("exact Chat response lacks visible provider output")
		}
	} else if visibleOrInclusive != usage.CompletionTokens {
		return response, fmt.Errorf("exact Chat completion count differs from provider usage")
	}
	choice := response.Choices[0]
	if choice.Finish != "stop" || choice.Message.Role != "assistant" || len(choice.Message.Tools) != 0 ||
		(len(choice.Message.Function) != 0 && string(choice.Message.Function) != "null") ||
		strings.TrimSpace(choice.Message.Refusal) != "" || strings.TrimSpace(choice.Message.Content) == "" {
		return response, fmt.Errorf("exact Chat response lacks completed assistant text")
	}
	return response, nil
}

func ObservePlainChatReasoning(b Binding, contentType string, reader io.Reader) (adapters.Usage, int, error) {
	p, err := b.ExecutionProfile()
	if err != nil || b.ProbeContractID != PlainChatReasoningProbeID || b.ProbeContractVersion != PlainChatReasoningProbeVersion ||
		!plainChatReasoningComposition(b, p) {
		return adapters.Usage{}, 0, ErrUnsupported
	}
	const maxBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return adapters.Usage{}, len(body), fmt.Errorf("cannot read bounded Chat response")
	}
	usage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(body,
		adapters.EndpointChatCompletions, metering.UsageSourceUpstream, p.CacheUsageProfile)
	if err != nil || !present {
		return adapters.Usage{}, len(body), fmt.Errorf("Chat response has no valid provider usage")
	}
	response, err := parsePlainChatProbeResponse(contentType, body, b.ExpectedUpstreamModel, usage, PlainChatReasoningOutputLimit, p.CacheUsageProfile)
	if err != nil {
		return adapters.Usage{}, len(body), err
	}
	raw := response.Usage.Details.ReasoningTokens
	if len(raw) != 0 {
		var tokens int
		if json.Unmarshal(raw, &tokens) != nil || tokens <= 0 || tokens >= usage.CompletionTokens {
			return adapters.Usage{}, len(body), fmt.Errorf("invalid or empty reasoning token partition")
		}
	} else if p.Provider != "hy3" || strings.TrimSpace(response.Choices[0].Message.Reasoning) == "" {
		// TokenHub also documents a meaningful native reasoning_content
		// field. Ark summaries and GPT assistant text grant no such evidence.
		return adapters.Usage{}, len(body), fmt.Errorf("Chat response has no documented reasoning evidence")
	}
	return usage, len(body), nil
}
