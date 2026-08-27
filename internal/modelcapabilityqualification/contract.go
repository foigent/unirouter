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

func Resolve(
	operationID, capabilityID,
	executionProfileID, executionProfileVersion,
	protocolProfileID, protocolProfileVersion string,
) (Binding, error) {
	for _, candidate := range [...]Binding{deepSeekFlashStreaming, deepSeekFlashLegacyStreaming} {
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
