package admin

import (
	"encoding/json"
	"slices"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const exactTextOperationProbeOutputLimit = 256

func canonicalOperationProbeSpecForSnapshot(s canonicalOperationProbeSnapshot) (canonicalOperationProbeSpec, error) {
	if s.ProfileDefinitionDigest == "" {
		return canonicalOperationProbeSpecForExactProfile(modeloperation.ID(s.OperationID), s.ExecutionProfileID, s.ExecutionProfileVersion, s.ProfileID, s.ProfileVersion)
	}
	identity, err := s.Definition.OperationProtocolIdentity(s.OperationID)
	if err != nil || s.ProfileDefinitionDigest != s.Definition.Digest() || identity.ProtocolProfileID != s.ProfileID || identity.ProtocolProfileVersion != s.ProfileVersion {
		return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
	}
	if exactTextOperationProbe(s) || nativeProbeProfile(s.Definition.Profile()) || modelprofile.MediaJobProfile(s.Definition.Profile()) {
		profile := s.Definition.Profile()
		pathMode, err := adapters.EffectiveEndpointPathMode(s.AdapterType, s.AdapterConfig)
		if err != nil || s.ExecutionProfileID != profile.ID || s.ExecutionProfileVersion != profile.Version ||
			modelprofile.ValidateProductBinding(profile, modelprofile.ProductBinding{
				Provider: s.Provider, BaseURL: s.BaseURL, AdapterType: s.AdapterType, EndpointPathMode: pathMode,
				OperationID: s.OperationID, ProtocolProfileID: s.ProfileID, ProtocolProfileVersion: s.ProfileVersion, UpstreamModel: s.UpstreamModel,
			}) != nil {
			return canonicalOperationProbeSpec{}, errCanonicalOperationProbeUnsupported
		}
	}
	if nativeProbeProfile(s.Definition.Profile()) || modelprofile.MediaJobProfile(s.Definition.Profile()) {
		return canonicalOperationProbeSpecForProfile(modeloperation.ID(s.OperationID), s.ProfileID, s.ProfileVersion)
	}
	return canonicalOperationProbeSpec{Endpoint: adapters.EndpointChatCompletions, ProfileID: s.ProfileID, ProfileVersion: s.ProfileVersion, SourceKind: canonicalOperationProbeSourceKind}, nil
}

// The data path selects bounded connectivity controls from installed typed
// contracts. It does not infer behavior from model names or grant capabilities.
func canonicalOperationProbePayloadForSnapshot(s canonicalOperationProbeSnapshot, text string) ([]byte, error) {
	if s.OperationID == string(modeloperation.IDMediaJobs) {
		p := s.Definition.Profile()
		if !modelprofile.MediaJobProfile(p) || !slices.Contains(p.UpstreamModelIDs, s.UpstreamModel) || text != "" {
			return nil, errCanonicalOperationProbeUnsupported
		}
		operation := endpointprotocol.MediaJobOperation(p.ProtocolProfile)
		switch operation {
		case "text_to_video":
			return json.Marshal(map[string]string{"model": s.UpstreamModel, "operation": operation, "prompt": "A blue square on a white background."})
		case "text_to_speech":
			return json.Marshal(map[string]string{"model": s.UpstreamModel, "operation": operation, "prompt": "Hello from UniRouter."})
		case "speech_to_text", "image_to_text":
			// The exact bound handler supplies the challenge media on send.
			return json.Marshal(map[string]string{"model": s.UpstreamModel, "operation": operation})
		}
		if p.ProtocolProfile == endpointprotocol.ProfileGLMImage {
			return json.Marshal(map[string]any{"model": s.UpstreamModel, "prompt": "A simple solid blue square on a white background."})
		}
		payload := map[string]any{"model": s.UpstreamModel, "prompt": "A simple solid blue square on a white background.",
			"n": 1, "size": "1024x1024", "output_format": "png", "stream": false}
		if p.ProtocolProfile == endpointprotocol.ProfileOpenAIImageGPT {
			payload["quality"], payload["background"] = "low", "opaque"
		}
		return json.Marshal(payload)
	}
	if p := s.Definition.Profile(); s.ProfileDefinitionDigest != "" && nativeProbeProfile(p) {
		if p.APISurface == modelprofile.APISurfaceOpenAIResponses {
			return json.Marshal(map[string]any{
				"model": s.UpstreamModel, "input": "Reply with the word Ready.", "max_output_tokens": nativeToolProbeOutputLimit, "store": false, "stream": false,
			})
		}
		return json.Marshal(map[string]any{
			"model": s.UpstreamModel, "messages": []map[string]string{{"role": "user", "content": "Reply with the word Ready."}},
			"max_tokens": nativeToolProbeOutputLimit, "stream": false,
		})
	}
	if s.ProfileDefinitionDigest == "" {
		return canonicalOperationProbePayload(modeloperation.ID(s.OperationID), s.UpstreamModel, text, s.ExecutionProfileID, s.ExecutionProfileVersion)
	}
	if _, err := canonicalOperationProbeSpecForSnapshot(s); err != nil {
		return nil, err
	}
	p := s.Definition.Profile()
	protocol, ok := modelprofile.ResolveChatProtocolContract(p.ProtocolProfile)
	if !ok || !slices.Contains(p.UpstreamModelIDs, s.UpstreamModel) || !protocol.OutputLimitModeled || protocol.OutputLimitField == "" {
		return nil, errCanonicalOperationProbeUnsupported
	}
	if text == "" {
		text = "UniRouter connectivity probe"
	}
	limit := 64
	if protocol.ExactTextOperationProbe {
		limit = exactTextOperationProbeOutputLimit
	}
	if int64(limit) < protocol.OutputLimitMin || int64(limit) > protocol.OutputLimitMax ||
		(p.ProviderMaxOutput > 0 && p.ProviderMaxOutput < limit) || (p.ExecutableMaxOutput > 0 && p.ExecutableMaxOutput < limit) {
		return nil, errCanonicalOperationProbeUnsupported
	}
	payload := map[string]any{"model": s.UpstreamModel, "stream": false, protocol.OutputLimitField: limit, "messages": []map[string]string{{"role": "user", "content": text}}}
	if protocol.ExactTextOperationProbe {
		exactPayload, err := modelcapabilityqualification.PlainChatProbePayload(p, s.UpstreamModel, text, limit)
		if err != nil {
			return nil, errCanonicalOperationProbeUnsupported
		}
		payload = exactPayload
	}
	reasoning, ok := modelprofile.ResolveReasoningContract(p.ReasoningProfile)
	if !ok {
		return nil, errCanonicalOperationProbeUnsupported
	}
	if protocol.RequiredDisabledReasoningEffort != "" {
		return json.Marshal(payload)
	}
	if modelprofile.QwenHostedSplitProfile(p) {
		payload["max_tokens"] = limit - modelprofile.QwenHostedAnswerTolerance
		if reasoning.ToggleControl.Field == "enable_thinking" {
			payload["enable_thinking"] = false
		} else {
			payload["thinking_budget"] = modelprofile.QwenHostedProbeThinking
		}
		return json.Marshal(payload)
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
		if value != "enabled" && value != "disabled" && value != "adaptive" {
			return nil, errCanonicalOperationProbeUnsupported
		}
		thinking := map[string]string{"type": value}
		if value == "enabled" && reasoning.ThinkingKeepProviderDefault != "" {
			thinking["keep"] = reasoning.ThinkingKeepProviderDefault
		}
		payload["thinking"] = thinking
	} else if protocol.ExactTextOperationProbe && reasoning.EffortControl.Field == "reasoning_effort" &&
		slices.Contains(reasoning.EffortControl.AllowedValues, "low") {
		payload[reasoning.EffortControl.Field] = "low"
	}
	return json.Marshal(payload)
}

func exactTextOperationProbe(s canonicalOperationProbeSnapshot) bool {
	if s.ProfileDefinitionDigest == "" {
		return false
	}
	contract, ok := modelprofile.ResolveChatProtocolContract(s.Definition.Profile().ProtocolProfile)
	return ok && contract.ExactTextOperationProbe
}
