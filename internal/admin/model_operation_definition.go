package admin

import (
	"encoding/json"
	"slices"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func canonicalOperationProbeSpecForSnapshot(s canonicalOperationProbeSnapshot) (canonicalOperationProbeSpec, error) {
	if s.ProfileDefinitionDigest == "" {
		return canonicalOperationProbeSpecForExactProfile(modeloperation.ID(s.OperationID), s.ExecutionProfileID, s.ExecutionProfileVersion, s.ProfileID, s.ProfileVersion)
	}
	identity, err := s.Definition.OperationProtocolIdentity(s.OperationID)
	if err != nil || s.ProfileDefinitionDigest != s.Definition.Digest() || identity.ProtocolProfileID != s.ProfileID || identity.ProtocolProfileVersion != s.ProfileVersion {
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
	return canonicalOperationProbeSpec{Endpoint: adapters.EndpointChatCompletions, ProfileID: s.ProfileID, ProfileVersion: s.ProfileVersion, SourceKind: canonicalOperationProbeSourceKind}, nil
}

// The data path selects bounded connectivity controls from installed typed
// contracts. It does not infer behavior from model names or grant capabilities.
func canonicalOperationProbePayloadForSnapshot(s canonicalOperationProbeSnapshot, text string) ([]byte, error) {
	if s.ProfileDefinitionDigest == "" {
		return canonicalOperationProbePayload(modeloperation.ID(s.OperationID), s.UpstreamModel, text, s.ExecutionProfileID, s.ExecutionProfileVersion)
	}
	if _, err := canonicalOperationProbeSpecForSnapshot(s); err != nil {
		return nil, err
	}
	p := s.Definition.Profile()
	protocol, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	if !ok || !slices.Contains(p.UpstreamModelIDs, s.UpstreamModel) || !protocol.OutputLimitModeled || protocol.OutputLimitField == "" || protocol.OutputLimitMin > 64 || protocol.OutputLimitMax < 64 {
		return nil, errCanonicalOperationProbeUnsupported
	}
	if text == "" {
		text = "UniRouter connectivity probe"
	}
	payload := map[string]any{"model": s.UpstreamModel, "stream": false, protocol.OutputLimitField: 64, "messages": []map[string]string{{"role": "user", "content": text}}}
	reasoning, ok := modelprofile.ResolveReasoningContract(p.ReasoningProfile)
	if !ok {
		return nil, errCanonicalOperationProbeUnsupported
	}
	if reasoning.ToggleControl.Field != "" {
		if reasoning.ToggleControl.WireType == modelprofile.ReasoningControlWireBoolean {
			// Only the installed top-level boolean contract can take this path.
			// Disable reasoning explicitly for this bounded connectivity request.
			if reasoning.ToggleControl.Field != "enable_thinking" || reasoning.ToggleRequestObjectField() != "" || !slices.Contains(reasoning.ToggleControl.AllowedValues, "disabled") {
				return nil, errCanonicalOperationProbeUnsupported
			}
			payload["enable_thinking"] = false
			return json.Marshal(payload)
		}
		// Current supported Chat contracts encode this control as thinking.type.
		// A new wire shape needs a typed implementation, never a JSON policy.
		if reasoning.ToggleControl.Field != "thinking" || reasoning.ToggleRequestObjectField() != "" {
			return nil, errCanonicalOperationProbeUnsupported
		}
		value := reasoning.ToggleControl.ProviderDefault
		if slices.Contains(reasoning.ToggleControl.AllowedValues, "disabled") {
			value = "disabled"
		}
		if value != "enabled" && value != "disabled" {
			return nil, errCanonicalOperationProbeUnsupported
		}
		thinking := map[string]string{"type": value}
		if value == "enabled" && reasoning.ThinkingKeepProviderDefault != "" {
			thinking["keep"] = reasoning.ThinkingKeepProviderDefault
		}
		payload["thinking"] = thinking
	}
	return json.Marshal(payload)
}
