// Package modelcapabilityqualification owns the compiled half of typed
// provider capability qualification. Mutable probes, approvals and current
// qualification are database-owned; this package never turns a Profile claim
// into live routing evidence.
package modelcapabilityqualification

import (
	"errors"
	"fmt"
	"sort"

	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
)

const (
	DeepSeekChatSSEProbeContractID                = "unirouter.capability-probe.deepseek-chat-sse.v1"
	DeepSeekChatSSEProbeContractVersionV1         = "1.0.0"
	DeepSeekChatSSEProbeContractCurrentVersion    = "2.0.0"
	DeepSeekChatReasoningProbeContractID          = "unirouter.capability-probe.deepseek-chat-reasoning.v1"
	DeepSeekChatReasoningEffortProbeContractID    = "unirouter.capability-probe.deepseek-chat-reasoning-effort.v1"
	DeepSeekChatReasoningProbeContractVersion     = "1.0.0"
	DeepSeekChatFunctionToolsProbeContractID      = "unirouter.capability-probe.deepseek-chat-function-tools.v1"
	DeepSeekChatFunctionToolsProbeContractV2      = "2.0.0"
	DeepSeekChatJSONObjectProbeContractID         = "unirouter.capability-probe.deepseek-chat-json-object.v1"
	DeepSeekChatCoreProbeContractVersion          = "1.0.0"
	KimiChatSSEProbeContractID                    = "unirouter.capability-probe.kimi-chat-sse.v1"
	KimiChatSSEProbeContractVersionV2             = "2.0.0"
	KimiChatSSEProbeContractVersionV3             = "3.0.0"
	KimiChatSSEProbeContractVersionV4             = "4.0.0"
	KimiChatReasoningProbeContractID              = "unirouter.capability-probe.kimi-chat-reasoning.v1"
	KimiChatReasoningEffortProbeContractID        = "unirouter.capability-probe.kimi-chat-reasoning-effort.v1"
	KimiChatReasoningEffortProbeContractVersion   = "1.0.0"
	KimiChatFunctionToolsProbeContractID          = "unirouter.capability-probe.kimi-chat-function-tools.v1"
	KimiChatJSONObjectProbeContractID             = "unirouter.capability-probe.kimi-chat-json-object.v1"
	KimiChatCoreProbeContractVersion              = "1.0.0"
	KimiChatCoreProbeContractVersionV2            = "2.0.0"
	KimiChatFunctionToolsProbeContractVersionV3   = "3.0.0"
	KimiChatFunctionToolsProbeContractVersionV4   = "4.0.0"
	MiniMaxChatFunctionToolsProbeContractID       = "unirouter.capability-probe.minimax-chat-function-tools.v1"
	MiniMaxPAYGM3ChatFunctionToolsProbeContractID = "unirouter.capability-probe.minimax-payg-m3-chat-function-tools.v1"
	MiniMaxPAYGM2ChatFunctionToolsProbeContractID = "unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1"
	GLMChatFunctionToolsProbeContractID           = "unirouter.capability-probe.glm-chat-function-tools.v1"
	OpenAIChatFunctionToolsProbeContractVersion   = "1.0.0"
	ProbeSourceKind                               = "admin.model_capability_probe.v1"
	ApprovalSourceKind                            = "admin.model_capability_routing_approval.v1"
)

var ErrUnsupported = errors.New("model capability qualification contract is unsupported")

// Binding is one immutable provider/product/surface capability probe contract.
// It must match M88's immutable database support row exactly.
type Binding struct {
	OperationID             string
	CapabilityID            string
	ExecutionProfileID      string
	ExecutionProfileVersion string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	ProbeContractID         string
	ProbeContractVersion    string
	ExpectedProvider        string
	ExpectedUpstreamModel   string
	PublicPath              string
}

func allBindings() ([]Binding, error) {
	compiled, err := publication.CurrentBindings()
	if err != nil {
		return nil, fmt.Errorf("load compiled capability publication: %w", err)
	}
	bindings := make([]Binding, 0, len(compiled))
	for _, candidate := range compiled {
		bindings = append(bindings, Binding{
			OperationID: candidate.OperationID, CapabilityID: candidate.CapabilityID,
			ExecutionProfileID: candidate.ExecutionProfileID, ExecutionProfileVersion: candidate.ExecutionProfileVersion,
			ProtocolProfileID: candidate.ProtocolProfileID, ProtocolProfileVersion: candidate.ProtocolProfileVersion,
			ProbeContractID: candidate.ProbeContractID, ProbeContractVersion: candidate.ProbeContractVersion,
			ExpectedProvider: candidate.ExpectedProvider, ExpectedUpstreamModel: candidate.ExpectedUpstreamModel,
			PublicPath: candidate.PublicPath,
		})
	}
	return bindings, nil
}

func Resolve(
	operationID, capabilityID,
	executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) (Binding, error) {
	bindings, err := allBindings()
	if err != nil {
		return Binding{}, err
	}
	for _, candidate := range bindings {
		if candidate.OperationID == operationID && candidate.CapabilityID == capabilityID &&
			candidate.ExecutionProfileID == executionProfileID && candidate.ExecutionProfileVersion == executionProfileVersion &&
			candidate.ProtocolProfileID == protocolProfileID && candidate.ProtocolProfileVersion == protocolProfileVersion {
			return candidate, nil
		}
	}
	return Binding{}, fmt.Errorf("%w: %s/%s %s@%s %s@%s", ErrUnsupported,
		operationID, capabilityID, executionProfileID, executionProfileVersion,
		protocolProfileID, protocolProfileVersion)
}

// ResolveAll returns every compiled capability binding for one exact
// deployment operation tuple. Callers must still match each returned binding
// against the migration-owned current-contract row before materializing a
// declared capability.
func ResolveAll(
	operationID, executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) ([]Binding, error) {
	bindings := make([]Binding, 0, 9)
	compiled, err := allBindings()
	if err != nil {
		return nil, err
	}
	for _, candidate := range compiled {
		if candidate.OperationID == operationID &&
			candidate.ExecutionProfileID == executionProfileID && candidate.ExecutionProfileVersion == executionProfileVersion &&
			candidate.ProtocolProfileID == protocolProfileID && candidate.ProtocolProfileVersion == protocolProfileVersion {
			bindings = append(bindings, candidate)
		}
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("%w: %s %s@%s %s@%s", ErrUnsupported,
			operationID, executionProfileID, executionProfileVersion,
			protocolProfileID, protocolProfileVersion)
	}
	return bindings, nil
}

func DeepSeekFlashStreamingBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "streaming", "deepseek.v4-flash.api", "1.4.0", "protocol.deepseek-openai-chat.v4", "1.0.0")
}

func DeepSeekFlashLegacyStreamingBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "streaming", "deepseek.v4-flash.api", "1.4.0", "openai.chat_completions", "1.0.0")
}

func DeepSeekFlashReasoningBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "reasoning", "deepseek.v4-flash.api", "1.4.0", "protocol.deepseek-openai-chat.v4", "1.0.0")
}

func DeepSeekFlashLegacyReasoningBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "reasoning", "deepseek.v4-flash.api", "1.4.0", "openai.chat_completions", "1.0.0")
}

func DeepSeekFlashReasoningEffortBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "reasoning_effort", "deepseek.v4-flash.api", "1.4.0", "protocol.deepseek-openai-chat.v4", "1.0.0")
}

func DeepSeekFlashLegacyReasoningEffortBinding() Binding {
	return mustResolve("openai.chat_completions.v1", "reasoning_effort", "deepseek.v4-flash.api", "1.4.0", "openai.chat_completions", "1.0.0")
}

func DeepSeekFlashCoreBindings() []Binding {
	all := mustResolveAll("openai.chat_completions.v1", "deepseek.v4-flash.api", "1.4.0", "")
	bindings := make([]Binding, 0, 12)
	for _, binding := range all {
		if binding.CapabilityID != "streaming" && binding.CapabilityID != "reasoning" && binding.CapabilityID != "reasoning_effort" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func DeepSeekFlashCurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "deepseek.v4-flash.api", "1.5.0", "")
}

func DeepSeekProCurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "deepseek.v4-pro.api", "1.5.0", "")
}

func KimiK26CurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "kimi.k2.6.api", "1.7.0", "")
}

func KimiK27CurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "kimi.k2.7-code.api", "1.3.0", "")
}

func KimiK3CurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "kimi.k3.api", "1.9.0", "")
}

func MiniMaxM3CurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "minimax.m3.token-plan", "1.7.0", "")
}

func MiniMaxPAYGCurrentBindings() []Binding {
	bindings := make([]Binding, 0, 3)
	for _, profile := range []struct{ id, version string }{
		{"minimax.m3.payg", "1.0.0"},
		{"minimax.m2.7.payg", "1.0.0"},
		{"minimax.m2.7-highspeed.payg", "1.0.0"},
	} {
		bindings = append(bindings, mustResolveAll("openai.chat_completions.v1", profile.id, profile.version, "")...)
	}
	return bindings
}

func GLM53CurrentBindings() []Binding {
	return mustResolveAll("openai.chat_completions.v1", "glm.5.3.api", "1.8.0", "")
}

func mustResolve(operationID, capabilityID, profileID, profileVersion, protocolID, protocolVersion string) Binding {
	binding, err := Resolve(operationID, capabilityID, profileID, profileVersion, protocolID, protocolVersion)
	if err != nil {
		panic(err)
	}
	return binding
}

func mustResolveAll(operationID, profileID, profileVersion, protocolID string) []Binding {
	all, err := allBindings()
	if err != nil {
		panic(err)
	}
	bindings := make([]Binding, 0, 12)
	for _, binding := range all {
		if binding.OperationID == operationID && binding.ExecutionProfileID == profileID &&
			binding.ExecutionProfileVersion == profileVersion && (protocolID == "" || binding.ProtocolProfileID == protocolID) {
			bindings = append(bindings, binding)
		}
	}
	if len(bindings) == 0 {
		panic(fmt.Errorf("%w: no compiled bindings for %s %s@%s", ErrUnsupported, operationID, profileID, profileVersion))
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		left, right := capabilityOrder(bindings[i].CapabilityID), capabilityOrder(bindings[j].CapabilityID)
		if left != right {
			return left < right
		}
		leftTyped := bindings[i].ProtocolProfileID != "openai.chat_completions"
		rightTyped := bindings[j].ProtocolProfileID != "openai.chat_completions"
		if leftTyped != rightTyped {
			return leftTyped
		}
		return bindings[i].ProtocolProfileID < bindings[j].ProtocolProfileID
	})
	return bindings
}

func capabilityOrder(capabilityID string) int {
	order := map[string]int{
		"streaming": 0, "reasoning": 1, "reasoning_effort": 2,
		"tools": 3, "function_tools": 4, "tool_choice_auto": 5,
		"tool_choice_named": 6, "tool_choice_required": 7, "json_object": 8,
	}
	if index, exists := order[capabilityID]; exists {
		return index
	}
	return len(order)
}
