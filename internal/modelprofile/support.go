package modelprofile

import (
	"slices"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/tokenprofile"
)

// ImplementationReference identifies typed behavior, never a model catalog
// entry. IDs already include their immutable revision; tokenizer revisions
// additionally retain the existing exact @version address.
type ImplementationReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Implementations derives support from the actual component registries used
// by request validation, URL composition and token counting. Profile identity
// and documented capabilities cannot add entries to this set.
func Implementations() []ImplementationReference {
	refs := []ImplementationReference{
		{"profile_definition", DefinitionSchemaV1},
		{"tokenizer", TokenizerByteUpperID + "@" + TokenizerByteUpperVersion},
	}
	for _, endpoint := range endpointprotocol.NewRegistry().Descriptors() {
		refs = append(refs, ImplementationReference{"endpoint_protocol", endpoint.ProfileID + "@" + endpoint.ProfileVersion})
	}
	refs = append(refs, ImplementationReference{"operation_policy", AnthropicMessages55ToolsPolicy})
	for id := range chatProtocolContracts {
		refs = append(refs, ImplementationReference{"chat", id})
	}
	for id := range reasoningContracts {
		refs = append(refs, ImplementationReference{"reasoning", id})
	}
	for id := range cacheUsageContracts {
		refs = append(refs, ImplementationReference{"cache_usage", id})
	}
	for id := range processingTierPolicies {
		refs = append(refs, ImplementationReference{"processing_tier", id})
	}
	for id := range endpointPathModes {
		refs = append(refs, ImplementationReference{"endpoint", id})
	}
	for _, id := range tokenprofile.Implementations() {
		refs = append(refs, ImplementationReference{"tokenizer", id})
	}
	SortImplementationReferences(refs)
	return refs
}

// Requirements is available only after the complete composition was accepted
// by DecodeDefinition. Set inclusion alone does not validate compositions.
func (d Definition) Requirements() []ImplementationReference {
	if d.digest == "" {
		return nil
	}
	p := d.profile
	if MediaJobProfile(p) {
		refs := []ImplementationReference{
			{"endpoint_protocol", p.ProtocolProfile + "@" + p.ProtocolProfileVersion},
			{"profile_definition", DefinitionSchemaV1},
		}
		for _, endpoint := range p.EndpointClasses {
			refs = append(refs, ImplementationReference{"endpoint", endpoint})
		}
		SortImplementationReferences(refs)
		return refs
	}
	refs := []ImplementationReference{
		{"profile_definition", DefinitionSchemaV1},
		{"reasoning", p.ReasoningProfile},
		{"tokenizer", p.TokenizerProfileID + "@" + p.TokenizerProfileVersion},
	}
	if NativeClientToolsProfile(p) || NativeInlineJPEGProfile(p) {
		refs = append(refs, ImplementationReference{"endpoint_protocol", p.ProtocolProfile + "@" + p.ProtocolProfileVersion})
		if p.OperationPolicyProfile != "" {
			refs = append(refs, ImplementationReference{"operation_policy", p.OperationPolicyProfile})
		}
	} else {
		refs = append(refs, ImplementationReference{"chat", p.ProtocolProfile}, ImplementationReference{"cache_usage", p.CacheUsageProfile})
	}
	if p.ProcessingTierPolicy != "" {
		refs = append(refs, ImplementationReference{"processing_tier", p.ProcessingTierPolicy})
	}
	for _, endpoint := range p.EndpointClasses {
		refs = append(refs, ImplementationReference{"endpoint", endpoint})
	}
	SortImplementationReferences(refs)
	return refs
}

func SortImplementationReferences(refs []ImplementationReference) {
	slices.SortFunc(refs, func(a, b ImplementationReference) int {
		if a.Kind < b.Kind {
			return -1
		}
		if a.Kind > b.Kind {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}

// MaximumImplementedRequestTimeoutSeconds covers every installed behavior,
// including components not yet referenced by a published data definition.
// Publishing a new model cannot silently exceed a process's drain contract.
func MaximumImplementedRequestTimeoutSeconds() int {
	seconds := 300
	for _, contract := range reasoningContracts {
		seconds = max(seconds, contract.ActiveNonStreamTimeoutSeconds)
	}
	for _, policy := range processingTierPolicies {
		seconds = max(seconds, policy.OmittedTimeoutSeconds)
		for _, value := range policy.RequestValues {
			seconds = max(seconds, value.RequestTimeoutSeconds)
		}
	}
	return seconds
}
