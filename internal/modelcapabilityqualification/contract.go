// Package modelcapabilityqualification owns the compiled half of typed
// provider capability qualification. Mutable probes, approvals and current
// qualification are database-owned; this package never turns a Profile claim
// into live routing evidence.
package modelcapabilityqualification

import (
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	DeepSeekChatSSEProbeContractID             = "unirouter.capability-probe.deepseek-chat-sse.v1"
	DeepSeekChatSSEProbeContractVersionV1      = "1.0.0"
	DeepSeekChatSSEProbeContractCurrentVersion = "2.0.0"
	DeepSeekChatReasoningProbeContractID       = "unirouter.capability-probe.deepseek-chat-reasoning.v1"
	DeepSeekChatReasoningEffortProbeContractID = "unirouter.capability-probe.deepseek-chat-reasoning-effort.v1"
	DeepSeekChatReasoningProbeContractVersion  = "1.0.0"
	DeepSeekChatFunctionToolsProbeContractID   = "unirouter.capability-probe.deepseek-chat-function-tools.v1"
	DeepSeekChatJSONObjectProbeContractID      = "unirouter.capability-probe.deepseek-chat-json-object.v1"
	DeepSeekChatCoreProbeContractVersion       = "1.0.0"
	ProbeSourceKind                            = "admin.model_capability_probe.v1"
	ApprovalSourceKind                         = "admin.model_capability_routing_approval.v1"
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

var deepSeekFlashStreaming = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityStreaming,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "protocol.deepseek-openai-chat.v4",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatSSEProbeContractID,
	ProbeContractVersion:    DeepSeekChatSSEProbeContractCurrentVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

// deepSeekFlashLegacyStreaming preserves the exact Chat surface already
// sealed by pre-M88 DeepSeek Flash Releases. The execution Profile, provider,
// upstream model and probe contract remain exact; only the historical
// operation protocol identity differs from new attachments.
var deepSeekFlashLegacyStreaming = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityStreaming,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "openai.chat_completions",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatSSEProbeContractID,
	ProbeContractVersion:    DeepSeekChatSSEProbeContractCurrentVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

var deepSeekFlashReasoning = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityReasoning,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "protocol.deepseek-openai-chat.v4",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatReasoningProbeContractID,
	ProbeContractVersion:    DeepSeekChatReasoningProbeContractVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

var deepSeekFlashLegacyReasoning = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityReasoning,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "openai.chat_completions",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatReasoningProbeContractID,
	ProbeContractVersion:    DeepSeekChatReasoningProbeContractVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

var deepSeekFlashReasoningEffort = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityReasoningEffort,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "protocol.deepseek-openai-chat.v4",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatReasoningEffortProbeContractID,
	ProbeContractVersion:    DeepSeekChatReasoningProbeContractVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

var deepSeekFlashLegacyReasoningEffort = Binding{
	OperationID:             string(modeloperation.IDChatCompletions),
	CapabilityID:            modelprofile.CapabilityReasoningEffort,
	ExecutionProfileID:      "deepseek.v4-flash.api",
	ExecutionProfileVersion: "1.4.0",
	ProtocolProfileID:       "openai.chat_completions",
	ProtocolProfileVersion:  "1.0.0",
	ProbeContractID:         DeepSeekChatReasoningEffortProbeContractID,
	ProbeContractVersion:    DeepSeekChatReasoningProbeContractVersion,
	ExpectedProvider:        "deepseek",
	ExpectedUpstreamModel:   "deepseek-v4-flash",
	PublicPath:              modeloperation.PathChatCompletions,
}

var deepSeekFlashCoreBindings = func() []Binding {
	capabilities := []struct {
		CapabilityID    string
		ProbeContractID string
	}{
		{modelprofile.CapabilityTools, DeepSeekChatFunctionToolsProbeContractID},
		{modelprofile.CapabilityFunctionTools, DeepSeekChatFunctionToolsProbeContractID},
		{modelprofile.CapabilityToolChoiceAuto, DeepSeekChatFunctionToolsProbeContractID},
		{modelprofile.CapabilityToolChoiceNamed, DeepSeekChatFunctionToolsProbeContractID},
		{modelprofile.CapabilityToolChoiceReq, DeepSeekChatFunctionToolsProbeContractID},
		{modelprofile.CapabilityJSONObject, DeepSeekChatJSONObjectProbeContractID},
	}
	protocols := []string{"protocol.deepseek-openai-chat.v4", "openai.chat_completions"}
	bindings := make([]Binding, 0, len(capabilities)*len(protocols))
	for _, capability := range capabilities {
		for _, protocolProfileID := range protocols {
			bindings = append(bindings, Binding{
				OperationID: string(modeloperation.IDChatCompletions), CapabilityID: capability.CapabilityID,
				ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.4.0",
				ProtocolProfileID: protocolProfileID, ProtocolProfileVersion: "1.0.0",
				ProbeContractID: capability.ProbeContractID, ProbeContractVersion: DeepSeekChatCoreProbeContractVersion,
				ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-flash",
				PublicPath: modeloperation.PathChatCompletions,
			})
		}
	}
	return bindings
}()

func Resolve(
	operationID, capabilityID,
	executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) (Binding, error) {
	for _, candidate := range [...]Binding{
		deepSeekFlashStreaming, deepSeekFlashLegacyStreaming,
		deepSeekFlashReasoning, deepSeekFlashLegacyReasoning,
		deepSeekFlashReasoningEffort, deepSeekFlashLegacyReasoningEffort,
	} {
		if candidate.OperationID == operationID && candidate.CapabilityID == capabilityID &&
			candidate.ExecutionProfileID == executionProfileID && candidate.ExecutionProfileVersion == executionProfileVersion &&
			candidate.ProtocolProfileID == protocolProfileID && candidate.ProtocolProfileVersion == protocolProfileVersion {
			return candidate, nil
		}
	}
	for _, candidate := range deepSeekFlashCoreBindings {
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

func DeepSeekFlashStreamingBinding() Binding { return deepSeekFlashStreaming }

func DeepSeekFlashLegacyStreamingBinding() Binding { return deepSeekFlashLegacyStreaming }

func DeepSeekFlashReasoningBinding() Binding { return deepSeekFlashReasoning }

func DeepSeekFlashLegacyReasoningBinding() Binding { return deepSeekFlashLegacyReasoning }

func DeepSeekFlashReasoningEffortBinding() Binding { return deepSeekFlashReasoningEffort }

func DeepSeekFlashLegacyReasoningEffortBinding() Binding { return deepSeekFlashLegacyReasoningEffort }

func DeepSeekFlashCoreBindings() []Binding {
	return append([]Binding(nil), deepSeekFlashCoreBindings...)
}
