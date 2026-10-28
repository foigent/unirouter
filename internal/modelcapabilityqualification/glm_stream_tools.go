package modelcapabilityqualification

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

func validateGLMStreamBinding(b Binding) error {
	if b.definition != nil {
		if b.ProbeContractID != GLMStreamFunctionToolsProbeContractID {
			return ErrUnsupported
		}
		return b.validateDataDefinition()
	}
	// The existing compiled declaration owns the exact tuple set; do not
	// maintain another provider/model list beside Publication and Profile.
	compiled, err := Resolve(b.OperationID, b.CapabilityID, b.ExecutionProfileID, b.ExecutionProfileVersion, b.ProtocolProfileID, b.ProtocolProfileVersion)
	if err != nil || compiled != b || b.ProbeContractID != GLMStreamFunctionToolsProbeContractID || b.ProbeContractVersion != "1.0.0" || b.ExpectedProvider != "glm" || b.OperationID != modelprofile.OperationOpenAIChat || !slices.Contains([]string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}, b.CapabilityID) {
		return fmt.Errorf("%w: exact GLM streamed tools binding", ErrUnsupported)
	}
	return nil
}

func BuildGLMStreamFunctionToolsProbeRequest(b Binding) ([]byte, error) {
	if err := validateGLMStreamBinding(b); err != nil {
		return nil, err
	}
	profile, err := b.ExecutionProfile()
	if err != nil {
		return nil, ErrUnsupported
	}
	protocol, ok := modelprofile.ResolveChatProtocolContract(b.ProtocolProfileID)
	if !ok || !protocol.StreamContract.FunctionToolDeltas || protocol.OutputLimitField != "max_tokens" || protocol.OutputLimitMax < 1024 {
		return nil, ErrUnsupported
	}
	reasoning, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile)
	if !ok || reasoning.ToggleControl.Field != "thinking" || !slices.Contains(reasoning.ToggleControl.AllowedValues, "enabled") {
		return nil, ErrUnsupported
	}
	scenario, err := streamToolProbeScenarioForBinding(b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", "max_tokens": 1024, "thinking": map[string]string{"type": "enabled"}, "tools": scenario.tools(),
		"messages": []map[string]string{{"role": "user", "content": scenario.prompt}},
	})
}

func ValidateOpenAIChatStreamFunctionToolsForBinding(b Binding, contentType string, reader io.Reader) (OpenAIChatFunctionToolsObservation, error) {
	if existingFamilyStreamProbe(b.ProbeContractID) || b.ProbeContractID == QwenStreamFunctionToolsProbeContractID || b.ProbeContractID == QwenProviderCompletionProbeContractID {
		if err := b.validateDataDefinition(); err != nil {
			return OpenAIChatFunctionToolsObservation{}, invalidOpenAIChatFunctionTools("probe_contract", err)
		}
		observation, err := validateChatStreamFunctionTools(b, contentType, reader, b.ProbeContractID == MiniMaxM3StreamFunctionToolsProbeContractID)
		if err == nil && (b.ProbeContractID == QwenProviderCompletionProbeContractID || b.ProbeContractID == DeepSeekProviderCompletionProbeContractID) && observation.CompletionTokens > 2048 {
			return observation, invalidOpenAIChatFunctionTools("usage", fmt.Errorf("provider completion exceeds probe contract"))
		}
		return observation, err
	}
	if b.ProbeContractID == MiniMaxM27StreamFunctionToolsProbeContractID {
		return ValidateMiniMaxM27StreamFunctionToolsForBinding(b, contentType, reader)
	}
	if err := validateGLMStreamBinding(b); err != nil {
		return OpenAIChatFunctionToolsObservation{}, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	return validateChatStreamFunctionTools(b, contentType, reader, false)
}
