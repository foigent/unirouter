package modelcapabilityqualification

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

func validateMiniMaxM27StreamBinding(b Binding) error {
	if b.OperationID != modelprofile.OperationOpenAIChat || b.ExecutionProfileID != "minimax.m2.7.payg" || b.ExecutionProfileVersion != "1.3.0" || b.ProtocolProfileID != "protocol.minimax-m27-openai-chat.v1" || b.ProtocolProfileVersion != "1.0.0" || b.ProbeContractID != MiniMaxM27StreamFunctionToolsProbeContractID || b.ProbeContractVersion != "1.0.0" || b.ExpectedProvider != "minimax" || b.ExpectedUpstreamModel != "MiniMax-M2.7" || b.PublicPath != "/v1/chat/completions" || !slices.Contains([]string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}, b.CapabilityID) {
		return fmt.Errorf("%w: exact M2.7 streamed tools binding", ErrUnsupported)
	}
	return nil
}

func BuildMiniMaxM27StreamFunctionToolsProbeRequest(b Binding) ([]byte, error) {
	if err := validateMiniMaxM27StreamBinding(b); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", "max_completion_tokens": 1024, "tools": openAIChatProbeTools(),
		"messages": []map[string]string{{"role": "user", "content": "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}},
	})
}

// ValidateMiniMaxM27StreamFunctionToolsForBinding preserves M111 assertions.
func ValidateMiniMaxM27StreamFunctionToolsForBinding(b Binding, contentType string, reader io.Reader) (OpenAIChatFunctionToolsObservation, error) {
	if err := validateMiniMaxM27StreamBinding(b); err != nil {
		return OpenAIChatFunctionToolsObservation{}, invalidOpenAIChatFunctionTools("probe_contract", err)
	}
	return validateChatStreamFunctionTools(b, contentType, reader, true)
}
