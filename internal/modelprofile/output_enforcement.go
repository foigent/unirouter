package modelprofile

// Output enforcement is typed implementation metadata, not a configurable
// tokenizer or policy language. A new protocol revision opts in explicitly.
const (
	OutputEnforcementByteUpperBound       = "utf8_byte_upper_bound"
	OutputEnforcementProviderCompletionV1 = "provider_completion_final_v1"
)

type OutputEnforcement struct {
	Accounting              string `json:"accounting"`
	Scope                   string `json:"scope,omitempty"`
	ProviderDeviationTokens int    `json:"provider_deviation_tokens,omitempty"`
	MaxDecodedOutputBytes   int64  `json:"max_decoded_output_bytes,omitempty"`
	MaxUpstreamWireBytes    int64  `json:"max_upstream_wire_bytes,omitempty"`
}

func ResolveOutputEnforcement(protocolID string) (OutputEnforcement, bool) {
	contract, ok := ResolveChatProtocolContract(protocolID)
	if !ok {
		return OutputEnforcement{}, false
	}
	switch contract.OutputEnforcement {
	case "":
		return OutputEnforcement{Accounting: OutputEnforcementByteUpperBound}, true
	case OutputEnforcementProviderCompletionV1:
		return OutputEnforcement{
			Accounting: OutputEnforcementProviderCompletionV1, Scope: "reasoning_and_answer",
			ProviderDeviationTokens: 10, MaxDecodedOutputBytes: 16 << 20, MaxUpstreamWireBytes: 128 << 20,
		}, true
	default:
		return OutputEnforcement{}, false
	}
}

func qwenProviderCompletionContract(id string) ChatProtocolContract {
	c := qwenFlashStreamFunctionToolsContract(id)
	c.OutputLimitField = "max_completion_tokens"
	c.OutputLimitAcceptedFields = []string{"max_completion_tokens"}
	c.OutputLimitFieldMaximums = nil
	c.OutputLimitMin = 11
	c.OutputEnforcement = OutputEnforcementProviderCompletionV1
	return c
}
