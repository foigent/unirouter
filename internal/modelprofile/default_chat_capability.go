package modelprofile

import (
	"slices"
	"strings"
)

// DefaultChatReasoningCapability is the capability an omitted Chat reasoning
// control requires when the exact Profile enables reasoning by default.
func DefaultChatReasoningCapability(profile Profile) string {
	// Legacy Chat Profiles retain their historical capability authority.
	if profile.LegacyProtocolAuthority {
		return ""
	}
	contract, ok := ResolveReasoningContract(profile.ReasoningProfile)
	if !ok {
		return ""
	}
	active := contract.Mode == ReasoningModeAlwaysOn || contract.Mode == ReasoningModeRequired
	if !active {
		control := contract.EffortControl
		if contract.ToggleControl.Field != "" {
			control = contract.ToggleControl
		}
		active = ReasoningControlProviderDefaultActive(control)
	}
	if !active {
		return ""
	}
	if slices.Contains(profile.Capabilities, CapabilityReasoningEffort) {
		return CapabilityReasoningEffort
	}
	return CapabilityReasoning
}

func ReasoningControlProviderDefaultActive(contract ReasoningControlContract) bool {
	providerDefault := strings.TrimSpace(contract.ProviderDefault)
	if providerDefault == "" || providerDefault == "disabled" || slices.Contains(contract.DisabledValues, providerDefault) {
		return false
	}
	if effective, ok := contract.CompatibilityValues[providerDefault]; ok {
		return effective != "disabled" && !slices.Contains(contract.DisabledValues, effective)
	}
	return true
}
