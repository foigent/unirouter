package adapters

import (
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

// The Definition is an immutable server-owned value resolved by Release. A
// profile ID string or client-supplied digest alone cannot select mechanics.
func validateDataMechanicsIdentity(d modelprofile.Definition, id, version, provider, upstream, operation, protocol, protocolVersion, cache string) error {
	p := d.Profile()
	if d.Digest() == "" || p.ID != id || p.Version != version || p.Provider != provider || !slices.Contains(p.UpstreamModelIDs, upstream) ||
		operation != modelprofile.OperationOpenAIChat || p.ProtocolProfile != protocol || protocolVersion != modelprofile.ChatOperationProtocolBindingVersionV1 || p.CacheUsageProfile != cache {
		return fmt.Errorf("immutable data Profile response identity mismatch")
	}
	return nil
}

func dataChatNonStreamMechanics(in ResponseInput) (ChatNonStreamMechanics, bool, error) {
	p := in.ProfileDefinition.Profile()
	if in.Endpoint != EndpointChatCompletions || in.ProtocolProfile != p.ProtocolProfile {
		return ChatNonStreamMechanics{}, false, fmt.Errorf("data Profile response operation mismatch")
	}
	if err := validateDataMechanicsIdentity(in.ProfileDefinition, in.ExecutionProfileID, in.ExecutionProfileVersion, in.Channel.Provider, in.UpstreamModel, in.ModelOperationID, in.OperationProtocolProfileID, in.OperationProtocolProfileVersion, in.CacheUsageProfile); err != nil {
		return ChatNonStreamMechanics{}, false, err
	}
	// These are existing typed implementations selected by their exact protocol;
	// adding data cannot install or broaden one. Legacy address rules stay below.
	switch p.ProtocolProfile {
	case "protocol.glm-openai-chat.v5", "protocol.glm-openai-chat.v6", "protocol.glm-openai-chat.v7", "protocol.glm-openai-chat.glm5v.v5", "protocol.glm-openai-chat.glm5v.v6":
		if p.Provider != "glm" {
			return ChatNonStreamMechanics{}, false, fmt.Errorf("GLM normalizer provider mismatch")
		}
		return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeRequestID: true}, true, nil
	case "protocol.kimi-openai-chat.v5", "protocol.kimi-openai-chat.k3.v5", "protocol.kimi-openai-chat.v6", "protocol.kimi-openai-chat.k3.v6":
		if p.Provider != "kimi" {
			return ChatNonStreamMechanics{}, false, fmt.Errorf("Kimi normalizer provider mismatch")
		}
		return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true, dataToolCallIndex: true}, true, nil
	case "protocol.minimax-m2-openai-chat.v7", "protocol.minimax-m2-openai-chat.v8":
		if p.Provider != "minimax" {
			return ChatNonStreamMechanics{}, false, fmt.Errorf("MiniMax normalizer provider mismatch")
		}
		return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true, dataToolCallIndex: true}, true, nil
	default:
		return ChatNonStreamMechanics{}, false, nil
	}
}
