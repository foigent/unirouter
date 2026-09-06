package modelcapabilityqualification

import (
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

// These are typed Probe implementations, not model allowlists. Adding data
// cannot install an implementation or loosen its accepted protocol semantics.
// Legacy contracts keep their exact historical tuple validation.
var dataProbeImplementations = map[string]func(Binding, modelprofile.Profile) bool{
	DeepSeekStreamFunctionToolsProbeContractID + "@1.0.0": validExistingFamilyStreamComposition,
	KimiStreamFunctionToolsProbeContractID + "@1.0.0":     validExistingFamilyStreamComposition,
	KimiStreamFunctionToolsProbeContractID + "@" + KimiStreamReasoningEffortProbeVersion: func(b Binding, p modelprofile.Profile) bool {
		return b.CapabilityID == modelprofile.CapabilityReasoningEffort &&
			p.Provider == "kimi" && p.ProtocolProfile == "protocol.kimi-openai-chat.k3.v6" &&
			p.ReasoningProfile == "reasoning.kimi-k3-effort.v2" && p.CacheUsageProfile == "cache-usage.kimi-platform.v1"
	},
	MiniMaxM3StreamFunctionToolsProbeContractID + "@1.0.0": validExistingFamilyStreamComposition,
	MiniMaxPAYGM3ChatFunctionToolsProbeContractID + "@2.0.0": func(b Binding, p modelprofile.Profile) bool {
		return compatibleMiniMaxM3StreamImplementation(p) &&
			(b.CapabilityID == modelprofile.CapabilityCacheReadUsage || b.CapabilityID == modelprofile.CapabilityPromptCache)
	},
	GLMStreamFunctionToolsProbeContractID + "@1.0.0": func(b Binding, p modelprofile.Profile) bool {
		return p.Provider == "glm" && slices.Contains([]string{"protocol.glm-openai-chat.v7", "protocol.glm-openai-chat.glm5v.v6"}, p.ProtocolProfile) && streamToolCapability(b.CapabilityID)
	},
	MiniMaxM27StreamFunctionToolsProbeContractID + "@1.0.0": func(b Binding, p modelprofile.Profile) bool {
		return p.Provider == "minimax" && compatibleMiniMaxM2StreamImplementation(p) && streamToolCapability(b.CapabilityID)
	},
	MiniMaxPAYGM2ChatFunctionToolsProbeContractID + "@2.0.0": func(b Binding, p modelprofile.Profile) bool {
		// The streamed M2 implementation retains its existing nonstream cache
		// Probe. Neither a cache counter nor this binding claims a cache hit.
		return p.Provider == "minimax" && compatibleMiniMaxM2StreamImplementation(p) &&
			(b.CapabilityID == modelprofile.CapabilityCacheReadUsage || b.CapabilityID == modelprofile.CapabilityPromptCache)
	},
}

func compatibleMiniMaxM2StreamImplementation(p modelprofile.Profile) bool {
	// M2 v8 reuses tool assembly while retaining its existing nonstream child
	// index normalizer and required eligible cache counter. Content replay is
	// selected by the exact reasoning revision, never guessed from the model.
	return p.ProtocolProfile == "protocol.minimax-m27-openai-chat.v1" ||
		(p.ProtocolProfile == "protocol.minimax-m2-openai-chat.v8" &&
			slices.Contains([]string{"reasoning.minimax-m2-required-embedded.v1", "reasoning.minimax-m2-required-embedded.v2"}, p.ReasoningProfile) && p.CacheUsageProfile == "cache-usage.minimax-openai.v2")
}

func compatibleMiniMaxM3StreamImplementation(p modelprofile.Profile) bool {
	if p.Provider != "minimax" || p.CacheUsageProfile != "cache-usage.minimax-openai.v2" {
		return false
	}
	return (slices.Contains([]string{"protocol.minimax-m3-openai-chat.v10", "protocol.minimax-m3-openai-chat.v11"}, p.ProtocolProfile) && p.ReasoningProfile == "reasoning.minimax-m3-adaptive-embedded.v2") ||
		(p.ProtocolProfile == "protocol.minimax-m3-openai-chat.v11" && p.ReasoningProfile == "reasoning.minimax-m3-adaptive-embedded.v3")
}

func streamToolCapability(capability string) bool {
	return slices.Contains([]string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning,
		modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}, capability)
}

func ProbeImplementations() []modelprofile.ImplementationReference {
	refs := make([]modelprofile.ImplementationReference, 0, len(dataProbeImplementations))
	for id := range dataProbeImplementations {
		refs = append(refs, modelprofile.ImplementationReference{Kind: "capability_probe", ID: id})
	}
	modelprofile.SortImplementationReferences(refs)
	return refs
}

// BindDefinition validates an exact database/publication binding against an
// immutable data definition. It grants no observation, approval or execution.
func BindDefinition(b Binding, d modelprofile.Definition) (Binding, error) {
	b.definition = &d
	if err := b.validateDataDefinition(); err != nil {
		return Binding{}, err
	}
	return b, nil
}

func (b Binding) validateDataDefinition() error {
	if b.definition == nil || b.definition.Digest() == "" {
		return fmt.Errorf("%w: data definition absent", ErrUnsupported)
	}
	p := b.definition.Profile()
	identity, err := b.definition.OperationProtocolIdentity(b.OperationID)
	implementation := dataProbeImplementations[b.ProbeContractID+"@"+b.ProbeContractVersion]
	if err != nil || identity.ProtocolProfileID != b.ProtocolProfileID || identity.ProtocolProfileVersion != b.ProtocolProfileVersion ||
		p.ID != b.ExecutionProfileID || p.Version != b.ExecutionProfileVersion || p.Provider != b.ExpectedProvider ||
		b.PublicPath != "/v1/chat/completions" || !slices.Contains(p.UpstreamModelIDs, b.ExpectedUpstreamModel) ||
		!slices.Contains(p.Capabilities, b.CapabilityID) || implementation == nil || !implementation(b, p) {
		return fmt.Errorf("%w: exact data Profile and typed Probe composition", ErrUnsupported)
	}
	return nil
}

func (b Binding) ExecutionProfile() (modelprofile.Profile, error) {
	if b.definition != nil {
		if err := b.validateDataDefinition(); err != nil {
			return modelprofile.Profile{}, err
		}
		return b.definition.Profile(), nil
	}
	p, ok := modelprofile.Lookup(b.ExecutionProfileID, b.ExecutionProfileVersion)
	if !ok {
		return modelprofile.Profile{}, fmt.Errorf("%w: compiled Profile unavailable", ErrUnsupported)
	}
	return p, nil
}
