package modelcapabilityqualification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const OpenAIChatFunctionToolsMaxResponseBytes = 1 << 20

var ErrInvalidOpenAIChatFunctionToolsCapability = errors.New("invalid OpenAI-compatible Chat function-tools capability probe response")

type openAIChatFunctionToolsValidationError struct {
	code   string
	cause  error
	stage  string
	member string
}

func (e *openAIChatFunctionToolsValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrInvalidOpenAIChatFunctionToolsCapability, e.code, e.cause)
	}
	return fmt.Sprintf("%s: %s", ErrInvalidOpenAIChatFunctionToolsCapability, e.code)
}

func (e *openAIChatFunctionToolsValidationError) Unwrap() error {
	return ErrInvalidOpenAIChatFunctionToolsCapability
}

func invalidOpenAIChatFunctionTools(code string, cause ...error) error {
	err := &openAIChatFunctionToolsValidationError{code: code}
	if len(cause) != 0 {
		err.cause = cause[0]
	}
	return err
}

func OpenAIChatFunctionToolsErrorCode(err error) string {
	var validationErr *openAIChatFunctionToolsValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

// OpenAIChatFunctionToolsDiagnostic contains only fixed classifications. The
// underlying error may include provider-controlled text and must not be used
// in a persisted Probe summary or public response.
type OpenAIChatFunctionToolsDiagnostic struct {
	Stage  string
	Member string
}

func DescribeOpenAIChatFunctionToolsDiagnostic(err error) OpenAIChatFunctionToolsDiagnostic {
	var failure *openAIChatFunctionToolsValidationError
	if !errors.As(err, &failure) {
		return OpenAIChatFunctionToolsDiagnostic{}
	}
	return CanonicalizeOpenAIChatFunctionToolsDiagnostic(failure.stage, failure.member)
}

// CanonicalizeOpenAIChatFunctionToolsDiagnostic is also used when projecting
// stored Probe summaries, which must be treated as untrusted historical data.
func CanonicalizeOpenAIChatFunctionToolsDiagnostic(stage, member string) OpenAIChatFunctionToolsDiagnostic {
	// An explicit allowlist keeps future caller mistakes from exposing a raw
	// SSE field, model name, tool argument, or adapter error message.
	switch stage + "/" + member {
	case "sse/unsupported_field", "sse/event_too_large", "sse/invalid_event",
		"frame_json/invalid_json", "normalization/identity", "normalization/json",
		"normalization/usage", "normalization/tool_normalization",
		"normalization/builtin_metering", "normalization/provider_envelope",
		"normalization/cache_projection", "normalization/unknown",
		"model_identity/mismatch", "envelope/invalid_shape", "usage/invalid_usage",
		"frame_shape/invalid_shape", "stream_state/invalid_transition",
		"terminal/invalid_state", "terminal/missing_done", "read/read_error",
		"read/response_too_large":
		return OpenAIChatFunctionToolsDiagnostic{Stage: stage, Member: member}
	}
	return OpenAIChatFunctionToolsDiagnostic{}
}

func invalidOpenAIChatFunctionToolsDiagnostic(code, stage, member string, cause error) error {
	return &openAIChatFunctionToolsValidationError{code: code, stage: stage, member: member, cause: cause}
}

type OpenAIChatFunctionToolsObservation struct {
	ReportedModel            string
	FinishReason             string
	PromptTokens             int64
	CompletionTokens         int64
	TotalTokens              int64
	CacheReadCounterObserved bool
	CachedTokens             int64
	ReasoningObserved        bool
	ToolCallObserved         bool
	ResponseBytes            int
	// DataEventCount counts fully assembled data events, including [DONE],
	// before frame validation. DeltaCount counts parsed choice/delta frames.
	DataEventCount int
	DeltaCount     int
}

type openAIChatFunctionToolsBindingIdentity struct {
	profileID, profileVersion     string
	protocolID, protocolVersion   string
	provider, upstreamModel, path string
}

var openAIChatFunctionToolsBindingIdentities = map[string][]openAIChatFunctionToolsBindingIdentity{
	MiniMaxChatFunctionToolsProbeContractID: {{
		profileID: "minimax.m3.token-plan", profileVersion: "1.7.0",
		protocolID: "protocol.minimax-m3-openai-chat.v9", protocolVersion: "1.0.0",
		provider: "minimax", upstreamModel: "MiniMax-M3", path: "/v1/chat/completions",
	}},
	MiniMaxPAYGM3ChatFunctionToolsProbeContractID: {{
		profileID: "minimax.m3.payg", profileVersion: "1.0.0",
		protocolID: "protocol.minimax-m3-openai-chat.v9", protocolVersion: "1.0.0",
		provider: "minimax", upstreamModel: "MiniMax-M3", path: "/v1/chat/completions",
	}, {
		profileID: "minimax.m3.payg", profileVersion: "1.1.0",
		protocolID: "protocol.minimax-m3-openai-chat.v9", protocolVersion: "1.0.0",
		provider: "minimax", upstreamModel: "MiniMax-M3", path: "/v1/chat/completions",
	}, {
		profileID: "minimax.m3.payg", profileVersion: "1.2.0",
		protocolID: "protocol.minimax-m3-openai-chat.v9", protocolVersion: "1.0.0",
		provider: "minimax", upstreamModel: "MiniMax-M3", path: "/v1/chat/completions",
	}},
	MiniMaxPAYGM2ChatFunctionToolsProbeContractID: {
		{
			profileID: "minimax.m2.7.payg", profileVersion: "1.3.0",
			protocolID: "protocol.minimax-m27-openai-chat.v1", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7.payg", profileVersion: "1.0.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7-highspeed.payg", profileVersion: "1.0.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7-highspeed", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7.payg", profileVersion: "1.1.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7-highspeed.payg", profileVersion: "1.1.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7-highspeed", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7.payg", profileVersion: "1.2.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7", path: "/v1/chat/completions",
		},
		{
			profileID: "minimax.m2.7-highspeed.payg", profileVersion: "1.2.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", protocolVersion: "1.0.0",
			provider: "minimax", upstreamModel: "MiniMax-M2.7-highspeed", path: "/v1/chat/completions",
		},
	},
	GLMChatFunctionToolsProbeContractID: {{
		profileID: "glm.5.3.api", profileVersion: "1.8.0",
		protocolID: "protocol.glm-openai-chat.v5", protocolVersion: "1.0.0",
		provider: "glm", upstreamModel: "glm-5.3", path: "/v1/chat/completions",
	}},
	GLMToggleChatFunctionToolsProbeContractID: {
		{
			profileID: "glm.5.2.api", profileVersion: "1.8.0",
			protocolID: "protocol.glm-openai-chat.v6", protocolVersion: "1.0.0",
			provider: "glm", upstreamModel: "glm-5.2", path: "/v1/chat/completions",
		},
		{
			profileID: "glm.5.1.api", profileVersion: "1.0.0",
			protocolID: "protocol.glm-openai-chat.v6", protocolVersion: "1.0.0",
			provider: "glm", upstreamModel: "glm-5.1", path: "/v1/chat/completions",
		},
		{
			profileID: "glm.5-turbo.api", profileVersion: "1.0.0",
			protocolID: "protocol.glm-openai-chat.v6", protocolVersion: "1.0.0",
			provider: "glm", upstreamModel: "glm-5-turbo", path: "/v1/chat/completions",
		},
		{
			profileID: "glm.5.api", profileVersion: "1.0.0",
			protocolID: "protocol.glm-openai-chat.v6", protocolVersion: "1.0.0",
			provider: "glm", upstreamModel: "glm-5", path: "/v1/chat/completions",
		},
		{
			profileID: "glm.5v-turbo.api", profileVersion: "1.11.0",
			protocolID: "protocol.glm-openai-chat.glm5v.v5", protocolVersion: "1.0.0",
			provider: "glm", upstreamModel: "glm-5v-turbo", path: "/v1/chat/completions",
		},
	},
}

func openAIChatFunctionToolsIdentity(binding Binding) (openAIChatFunctionToolsBindingIdentity, bool) {
	if binding.definition != nil && binding.validateDataDefinition() == nil {
		return openAIChatFunctionToolsBindingIdentity{profileID: binding.ExecutionProfileID, profileVersion: binding.ExecutionProfileVersion,
			protocolID: binding.ProtocolProfileID, protocolVersion: binding.ProtocolProfileVersion,
			provider: binding.ExpectedProvider, upstreamModel: binding.ExpectedUpstreamModel, path: binding.PublicPath}, true
	}
	for _, candidate := range openAIChatFunctionToolsBindingIdentities[binding.ProbeContractID] {
		if binding.ExecutionProfileID == candidate.profileID && binding.ExecutionProfileVersion == candidate.profileVersion &&
			binding.ProtocolProfileID == candidate.protocolID && binding.ProtocolProfileVersion == candidate.protocolVersion &&
			binding.ExpectedProvider == candidate.provider && binding.ExpectedUpstreamModel == candidate.upstreamModel &&
			binding.PublicPath == candidate.path {
			return candidate, true
		}
	}
	return openAIChatFunctionToolsBindingIdentity{}, false
}

func isMiniMaxFunctionToolsProbe(contractID string) bool {
	switch contractID {
	case MiniMaxChatFunctionToolsProbeContractID,
		MiniMaxPAYGM3ChatFunctionToolsProbeContractID,
		MiniMaxPAYGM2ChatFunctionToolsProbeContractID:
		return true
	default:
		return false
	}
}

func openAIChatRuntimeCapabilityAllowed(binding Binding) bool {
	if binding.definition != nil {
		return binding.validateDataDefinition() == nil && (binding.ProbeContractID == MiniMaxPAYGM2ChatFunctionToolsProbeContractID || binding.ProbeContractID == MiniMaxPAYGM3ChatFunctionToolsProbeContractID)
	}
	var capabilities []string
	switch binding.ProbeContractID {
	case MiniMaxPAYGM3ChatFunctionToolsProbeContractID, MiniMaxPAYGM2ChatFunctionToolsProbeContractID:
		capabilities = []string{
			modelprofile.CapabilityCacheReadUsage,
			modelprofile.CapabilityFunctionTools,
			modelprofile.CapabilityPromptCache,
			modelprofile.CapabilityReasoning,
			modelprofile.CapabilityToolChoiceAuto,
			modelprofile.CapabilityTools,
		}
		if binding.ExecutionProfileVersion != "1.2.0" && !(binding.ExecutionProfileID == "minimax.m2.7.payg" && binding.ExecutionProfileVersion == "1.3.0" && (binding.CapabilityID == modelprofile.CapabilityPromptCache || binding.CapabilityID == modelprofile.CapabilityCacheReadUsage)) {
			return false
		}
	case GLMChatFunctionToolsProbeContractID, GLMToggleChatFunctionToolsProbeContractID:
		capabilities = []string{
			modelprofile.CapabilityFunctionTools,
			modelprofile.CapabilityReasoning,
			modelprofile.CapabilityToolChoiceAuto,
			modelprofile.CapabilityTools,
		}
	default:
		return false
	}
	return slices.Contains(capabilities, binding.CapabilityID)
}

// The new cache Probe is data-only and requires its exact immutable composition.
// Keep all legacy v2 semantics, including nonstream cache-hit qualification.
func usesOpenAIChatRuntimeProbeSemantics(b Binding) bool {
	return b.ProbeContractVersion == OpenAIChatRuntimeFunctionToolsProbeContractV2 ||
		(b.ProbeContractID == MiniMaxPAYGM2ChatFunctionToolsProbeContractID && b.ProbeContractVersion == "3.0.0" &&
			b.definition != nil && b.validateDataDefinition() == nil)
}

func openAIChatRuntimeProbePrompt() string {
	// The provider-reported prompt token count, not this local construction,
	// is the qualification authority. A deterministic long prefix makes the
	// request eligible for MiniMax's documented 512-token automatic-cache
	// threshold while remaining well inside the bounded response contract.
	return strings.Repeat("unirouter-cache-eligible-prefix ", 700) +
		"Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."
}

func openAIChatProbeTools() []map[string]interface{} {
	return []map[string]interface{}{{"type": "function", "function": map[string]interface{}{
		"name": "unirouter_probe_echo", "description": "Return the supplied probe value through a function call.",
		"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]string{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false},
	}}}
}

// BuildOpenAIChatFunctionToolsProbeRequest derives the exact fixed request
// from the complete immutable Binding and the selected Profile contracts. The
// two provider contracts deliberately keep separate identities because their
// reasoning and tool-choice request semantics differ; only the mechanics are
// shared.
func BuildOpenAIChatFunctionToolsProbeRequest(binding Binding) ([]byte, error) {
	if binding.ProbeContractID == QwenStreamFunctionToolsProbeContractID || binding.ProbeContractID == QwenProviderCompletionProbeContractID {
		return BuildQwenStreamFunctionToolsProbeRequest(binding)
	}
	if existingFamilyStreamProbe(binding.ProbeContractID) {
		return BuildExistingFamilyStreamFunctionToolsProbeRequest(binding)
	}
	if binding.ProbeContractID == GLMStreamFunctionToolsProbeContractID {
		return BuildGLMStreamFunctionToolsProbeRequest(binding)
	}
	if binding.ProbeContractID == MiniMaxM27StreamFunctionToolsProbeContractID {
		return BuildMiniMaxM27StreamFunctionToolsProbeRequest(binding)
	}
	profile, protocol, reasoning, err := resolveOpenAIChatFunctionToolsBinding(binding)
	if err != nil {
		return nil, err
	}
	prompt := "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."
	if usesOpenAIChatRuntimeProbeSemantics(binding) && isMiniMaxFunctionToolsProbe(binding.ProbeContractID) {
		prompt = openAIChatRuntimeProbePrompt()
	}
	payload := map[string]interface{}{
		"model":    binding.ExpectedUpstreamModel,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"stream":   false,
		"tools":    openAIChatProbeTools(),
	}
	if !protocol.OutputLimitModeled || protocol.OutputLimitField == "" || protocol.OutputLimitMax < 256 {
		return nil, fmt.Errorf("%w: exact output limit contract", ErrUnsupported)
	}
	payload[protocol.OutputLimitField] = 256
	if reasoning.Mode == modelprofile.ReasoningModeRequired {
		control := reasoning.ToggleControl
		switch {
		case control.Field == "":
			// MiniMax M2 reasoning is an immutable provider-required default;
			// there is deliberately no request toggle to synthesize.
		case control.Field == "thinking" && control.ProviderDefault == "enabled" && slices.Contains(control.AllowedValues, "enabled"):
			payload[control.Field] = map[string]string{"type": "enabled"}
		default:
			return nil, fmt.Errorf("%w: exact required-thinking contract", ErrUnsupported)
		}
	}
	switch {
	case protocol.FunctionToolProviderDefaultChoice == "auto":
		// Omission is the exact MiniMax M3 provider-default contract.
	case slices.Contains(protocol.FunctionToolChoices, "auto"):
		payload["tool_choice"] = "auto"
	default:
		return nil, fmt.Errorf("%w: exact automatic tool-choice contract", ErrUnsupported)
	}
	if protocol.FunctionToolsRequireNonStream {
		payload["stream"] = false
	}
	_ = profile
	return json.Marshal(payload)
}

func resolveOpenAIChatFunctionToolsBinding(binding Binding) (modelprofile.Profile, modelprofile.ChatProtocolContract, modelprofile.ReasoningContract, error) {
	want, ok := openAIChatFunctionToolsIdentity(binding)
	versionAllowed := binding.ProbeContractVersion == OpenAIChatFunctionToolsProbeContractVersion &&
		binding.CapabilityID == modelprofile.CapabilityFunctionTools
	if usesOpenAIChatRuntimeProbeSemantics(binding) {
		versionAllowed = openAIChatRuntimeCapabilityAllowed(binding)
	}
	if !ok || !versionAllowed ||
		binding.OperationID != modelprofile.OperationOpenAIChat ||
		binding.ExecutionProfileID != want.profileID || binding.ExecutionProfileVersion != want.profileVersion {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: exact function-tools binding", ErrUnsupported)
	}
	profile, profileErr := binding.ExecutionProfile()
	if profileErr != nil || profile.Provider != binding.ExpectedProvider || profile.ProtocolProfile != binding.ProtocolProfileID ||
		!slices.Contains(profile.UpstreamModelIDs, binding.ExpectedUpstreamModel) ||
		!slices.Contains(profile.Capabilities, modelprofile.CapabilityFunctionTools) ||
		!slices.Contains(profile.Capabilities, binding.CapabilityID) {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: exact execution Profile", ErrUnsupported)
	}
	identity, found, err := modelprofile.ResolveOperationProtocolIdentity(profile.ID, profile.Version, binding.OperationID)
	if binding.definition != nil {
		identity, err = binding.definition.OperationProtocolIdentity(binding.OperationID)
		found = err == nil
	}
	if err != nil || !found || identity.ProtocolProfileID != binding.ProtocolProfileID || identity.ProtocolProfileVersion != binding.ProtocolProfileVersion {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: typed operation identity", ErrUnsupported)
	}
	protocol, ok := modelprofile.ResolveChatProtocolContract(profile.ProtocolProfile)
	if !ok || !protocol.FunctionToolsModeled || !protocol.FunctionTools || !protocol.FunctionToolParametersRequired {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: exact function-tools protocol", ErrUnsupported)
	}
	if isMiniMaxFunctionToolsProbe(binding.ProbeContractID) &&
		(!protocol.FunctionToolInvocationCapabilitiesModeled || !protocol.ExactFunctionToolResponseModeled) {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: exact MiniMax function-tools invocation protocol", ErrUnsupported)
	}
	reasoning, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile)
	if !ok {
		return modelprofile.Profile{}, modelprofile.ChatProtocolContract{}, modelprofile.ReasoningContract{}, fmt.Errorf("%w: exact reasoning contract", ErrUnsupported)
	}
	return profile, protocol, reasoning, nil
}

// ValidateNormalizedOpenAIChatFunctionToolsForBinding validates the canonical
// adapter-normalized response. Runtime delivery remains independently owned by
// the Gateway guardrail; successful Probe semantics do not create a Runtime
// response identity for GLM.
func ValidateNormalizedOpenAIChatFunctionToolsForBinding(
	binding Binding, contentType string, reader io.Reader,
) (OpenAIChatFunctionToolsObservation, error) {
	if _, _, _, err := resolveOpenAIChatFunctionToolsBinding(binding); err != nil {
		return OpenAIChatFunctionToolsObservation{}, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return OpenAIChatFunctionToolsObservation{}, invalidOpenAIChatFunctionTools("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: OpenAIChatFunctionToolsMaxResponseBytes + 1}
	body, err := io.ReadAll(limited)
	observation := OpenAIChatFunctionToolsObservation{ResponseBytes: len(body)}
	if err != nil {
		return observation, invalidOpenAIChatFunctionTools("read", err)
	}
	if limited.N <= 0 {
		return observation, invalidOpenAIChatFunctionTools("response_too_large")
	}
	if err := strictjson.Validate(body); err != nil {
		return observation, invalidOpenAIChatFunctionTools("json", err)
	}
	var normalizedEnvelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &normalizedEnvelope); err != nil || normalizedEnvelope == nil {
		return observation, invalidOpenAIChatFunctionTools("json")
	}
	if binding.ExpectedProvider == "glm" {
		// The exact adapter preparation removes documented string metadata.
		// A retained value is malformed or was not normalized by that exact
		// authority, so it cannot qualify merely because struct decoding skips it.
		if _, present := normalizedEnvelope["request_id"]; present {
			return observation, invalidOpenAIChatFunctionTools("delivery_shape")
		}
	}
	for _, providerEnvelopeField := range []string{
		"base_resp", "input_sensitive", "output_sensitive", "input_sensitive_type", "output_sensitive_type", "output_sensitive_int",
	} {
		if _, exists := normalizedEnvelope[providerEnvelopeField]; exists {
			return observation, invalidOpenAIChatFunctionTools("delivery_shape")
		}
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			Index        json.Number `json:"index"`
			FinishReason string      `json:"finish_reason"`
			Message      struct {
				Role         string          `json:"role"`
				Content      json.RawMessage `json:"content"`
				Reasoning    json.RawMessage `json:"reasoning_content"`
				Name         json.RawMessage `json:"name"`
				AudioContent json.RawMessage `json:"audio_content"`
				ToolCalls    []struct {
					Index    json.RawMessage `json:"index"`
					ID       string          `json:"id"`
					Type     string          `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return observation, invalidOpenAIChatFunctionTools("json")
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != binding.ExpectedUpstreamModel {
		return observation, invalidOpenAIChatFunctionTools("reported_model")
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Index != "0" {
		return observation, invalidOpenAIChatFunctionTools("choices")
	}
	choice := payload.Choices[0]
	observation.FinishReason = choice.FinishReason
	if choice.FinishReason != "tool_calls" || choice.Message.Role != "assistant" ||
		len(choice.Message.Name) != 0 || len(choice.Message.AudioContent) != 0 || len(choice.Message.ToolCalls) != 1 {
		return observation, invalidOpenAIChatFunctionTools("tool_calls")
	}
	if isMiniMaxFunctionToolsProbe(binding.ProbeContractID) {
		var replayContent string
		if len(choice.Message.Content) == 0 || json.Unmarshal(choice.Message.Content, &replayContent) != nil || strings.TrimSpace(replayContent) == "" {
			return observation, invalidOpenAIChatFunctionTools("tool_calls")
		}
		if usesOpenAIChatRuntimeProbeSemantics(binding) {
			if !hasExactEmbeddedThinking(replayContent) {
				return observation, invalidOpenAIChatFunctionTools("reasoning")
			}
			observation.ReasoningObserved = true
		}
	} else if usesOpenAIChatRuntimeProbeSemantics(binding) {
		var reasoning string
		if len(choice.Message.Reasoning) == 0 || json.Unmarshal(choice.Message.Reasoning, &reasoning) != nil || strings.TrimSpace(reasoning) == "" {
			return observation, invalidOpenAIChatFunctionTools("reasoning")
		}
		observation.ReasoningObserved = true
	}
	call := choice.Message.ToolCalls[0]
	if binding.ExecutionProfileID == "minimax.m2.7-highspeed.payg" && binding.ExecutionProfileVersion == "1.2.0" && len(call.Index) != 0 {
		// The exact adapter removes only valid non-semantic child indexes.
		// Never qualify a residual value merely because typed decoding ignores it.
		// Binding validation above already establishes the complete exact tuple.
		return observation, invalidOpenAIChatFunctionTools("delivery_shape")
	}
	if strings.TrimSpace(call.ID) == "" || call.Type != "function" || call.Function.Name != "unirouter_probe_echo" {
		return observation, invalidOpenAIChatFunctionTools("tool_call_identity")
	}
	arguments := []byte(call.Function.Arguments)
	if err := strictjson.Validate(arguments); err != nil {
		return observation, invalidOpenAIChatFunctionTools("tool_arguments", err)
	}
	var decodedArguments map[string]interface{}
	if err := decodeOneJSON(arguments, &decodedArguments); err != nil || len(decodedArguments) != 1 || decodedArguments["value"] != "UNIROUTER_TOOL_PROBE_OK" {
		return observation, invalidOpenAIChatFunctionTools("tool_arguments")
	}
	prompt, completion, total, cacheObserved, cached, err := parseOpenAIChatFunctionToolsUsage(payload.Usage)
	if err != nil {
		return observation, invalidOpenAIChatFunctionTools("usage", err)
	}
	observation.PromptTokens = prompt
	observation.CompletionTokens = completion
	observation.TotalTokens = total
	observation.CacheReadCounterObserved = cacheObserved
	observation.CachedTokens = cached
	if usesOpenAIChatRuntimeProbeSemantics(binding) && isMiniMaxFunctionToolsProbe(binding.ProbeContractID) {
		if prompt < 512 || !cacheObserved || cached < 0 || cached > prompt {
			return observation, invalidOpenAIChatFunctionTools("cache_usage")
		}
		if binding.CapabilityID == modelprofile.CapabilityPromptCache && cached == 0 {
			return observation, invalidOpenAIChatFunctionTools("cache_miss")
		}
	}
	observation.ToolCallObserved = true
	return observation, nil
}

func hasExactEmbeddedThinking(content string) bool {
	const openTag, closeTag = "<think>", "</think>"
	if strings.Count(content, openTag) != 1 || strings.Count(content, closeTag) != 1 {
		return false
	}
	start := strings.Index(content, openTag)
	if start < 0 {
		return false
	}
	remainder := content[start+len(openTag):]
	end := strings.Index(remainder, closeTag)
	if end < 0 || strings.TrimSpace(remainder[:end]) == "" {
		return false
	}
	return true
}

func parseOpenAIChatFunctionToolsUsage(raw json.RawMessage) (int64, int64, int64, bool, int64, error) {
	var usage struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		TotalTokens      json.Number `json:"total_tokens"`
		PromptDetails    *struct {
			CachedTokens json.RawMessage `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&usage) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, 0, 0, false, 0, errors.New("invalid usage object")
	}
	values := []json.Number{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens}
	parsed := make([]int64, len(values))
	for index, value := range values {
		if value == "" || strings.ContainsAny(string(value), ".eE+") {
			return 0, 0, 0, false, 0, errors.New("usage counters must be decimal integers")
		}
		integer, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || integer < 0 {
			return 0, 0, 0, false, 0, errors.New("usage counters must be non-negative")
		}
		parsed[index] = integer
	}
	if parsed[0] <= 0 || parsed[1] <= 0 || parsed[0]+parsed[1] != parsed[2] {
		return 0, 0, 0, false, 0, errors.New("usage total mismatch")
	}
	var cachedRaw json.RawMessage
	if usage.PromptDetails != nil {
		cachedRaw = bytes.TrimSpace(usage.PromptDetails.CachedTokens)
	}
	cacheObserved := len(cachedRaw) > 0 && !bytes.Equal(cachedRaw, []byte("null"))
	var cached int64
	if cacheObserved {
		for _, value := range cachedRaw {
			if value < '0' || value > '9' {
				return 0, 0, 0, false, 0, errors.New("cached_tokens must be a decimal integer")
			}
		}
		if len(cachedRaw) == 0 {
			return 0, 0, 0, false, 0, errors.New("cached_tokens must be a decimal integer")
		}
		var err error
		cached, err = strconv.ParseInt(string(cachedRaw), 10, 64)
		if err != nil || cached < 0 || cached > parsed[0] {
			return 0, 0, 0, false, 0, errors.New("cached_tokens must be within prompt_tokens")
		}
	}
	return parsed[0], parsed[1], parsed[2], cacheObserved, cached, nil
}
