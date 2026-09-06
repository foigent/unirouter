package modelcapabilityqualification

import (
	"encoding/json"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	DeepSeekStreamFunctionToolsProbeContractID  = "unirouter.capability-probe.deepseek-chat-stream-tools.v1"
	KimiStreamFunctionToolsProbeContractID      = "unirouter.capability-probe.kimi-chat-stream-tools.v1"
	MiniMaxM3StreamFunctionToolsProbeContractID = "unirouter.capability-probe.minimax-m3-chat-stream-tools.v1"
)

func existingFamilyStreamProbe(id string) bool {
	return id == DeepSeekStreamFunctionToolsProbeContractID || id == KimiStreamFunctionToolsProbeContractID || id == MiniMaxM3StreamFunctionToolsProbeContractID
}

func validExistingFamilyStreamComposition(b Binding, p modelprofile.Profile) bool {
	if !streamToolCapability(b.CapabilityID) {
		return false
	}
	switch b.ProbeContractID {
	case DeepSeekStreamFunctionToolsProbeContractID:
		return p.Provider == "deepseek" && p.ProtocolProfile == "protocol.deepseek-openai-chat.v5" && p.CacheUsageProfile == "cache-usage.deepseek-v4.v1" &&
			slices.Contains([]string{"reasoning.deepseek-v4-flash-thinking-toggle.v5", "reasoning.deepseek-v4-pro-thinking-toggle.v5"}, p.ReasoningProfile)
	case KimiStreamFunctionToolsProbeContractID:
		if p.Provider != "kimi" || p.CacheUsageProfile != "cache-usage.kimi-platform.v1" {
			return false
		}
		return (p.ProtocolProfile == "protocol.kimi-openai-chat.k3.v6" && p.ReasoningProfile == "reasoning.kimi-k3-effort.v2") ||
			(p.ProtocolProfile == "protocol.kimi-openai-chat.v6" && slices.Contains([]string{"reasoning.kimi-k2.6-thinking-toggle.v2", "reasoning.kimi-k2.7-thinking-required.v2"}, p.ReasoningProfile))
	case MiniMaxM3StreamFunctionToolsProbeContractID:
		return compatibleMiniMaxM3StreamImplementation(p)
	}
	return false
}

func BuildExistingFamilyStreamFunctionToolsProbeRequest(b Binding) ([]byte, error) {
	if !existingFamilyStreamProbe(b.ProbeContractID) || b.validateDataDefinition() != nil {
		return nil, ErrUnsupported
	}
	p := b.definition.Profile()
	protocol, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	if !ok || !protocol.StreamContract.FunctionToolDeltas || protocol.OutputLimitField == "" || protocol.OutputLimitMax < 1024 {
		return nil, ErrUnsupported
	}
	reasoning, ok := modelprofile.ResolveReasoningContract(p.ReasoningProfile)
	if !ok {
		return nil, ErrUnsupported
	}
	payload := map[string]interface{}{
		"model": b.ExpectedUpstreamModel, "stream": true, "stream_options": map[string]bool{"include_usage": true},
		"tool_choice": "auto", protocol.OutputLimitField: 1024, "tools": openAIChatProbeTools(),
		"messages": []map[string]string{{"role": "user", "content": "Think briefly, then call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}},
	}
	if control := reasoning.ToggleControl; control.Field != "" {
		if control.Field == "thinking" {
			// The existing Chat contract carries this control in thinking.type;
			// WireString describes that value, not a scalar thinking envelope.
			payload[control.Field] = map[string]string{"type": control.ProviderDefault}
		} else if control.WireType == modelprofile.ReasoningControlWireString {
			payload[control.Field] = control.ProviderDefault
		} else if slices.Contains(control.AllowedValues, "enabled") {
			payload[control.Field] = map[string]string{"type": "enabled"}
		} else {
			return nil, ErrUnsupported
		}
	}
	if slices.Contains(reasoning.UnsupportedRequestFieldsWhileActive, "tool_choice") {
		// Existing DeepSeek authority normalizes redundant auto to its exact
		// omitted provider default. The Probe uses that same admissible wire.
		if protocol.FunctionToolProviderDefaultChoice != "auto" {
			return nil, ErrUnsupported
		}
		delete(payload, "tool_choice")
	}
	return json.Marshal(payload)
}
