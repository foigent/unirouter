package modelcapabilityqualification

import (
	"encoding/json"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// A scenario is a private immutable value, resolved through the existing typed
// Probe binding. Request construction and the response oracle share its owner;
// it is not another registry or an independently publishable contract.
type streamToolProbeScenario struct {
	prompt          string
	functionName    string
	description     string
	expectedValue   string
	strictArguments bool
}

func legacyStreamToolProbeScenario() streamToolProbeScenario {
	return streamToolProbeScenario{
		prompt:        "Think briefly, then call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly.",
		functionName:  "unirouter_probe_echo",
		description:   "Return the supplied probe value through a function call.",
		expectedValue: "UNIROUTER_TOOL_PROBE_OK",
	}
}

// Callers retain their existing exact binding validation before resolving the
// legacy scenario. The new tuple independently requires its immutable data
// Definition, so another provider or an unbound tuple cannot select it.
func streamToolProbeScenarioForBinding(b Binding) (streamToolProbeScenario, error) {
	if b.ProbeContractID == MiniMaxM27StreamFunctionToolsProbeContractID {
		scenario := legacyStreamToolProbeScenario()
		scenario.prompt = "Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."
		return scenario, nil
	}
	if b.ProbeContractID == DeepSeekProviderCompletionProbeContractID && b.ProbeContractVersion == DeepSeekMeaningfulStreamProbeVersion {
		if err := b.validateDataDefinition(); err != nil {
			return streamToolProbeScenario{}, err
		}
		scenario := legacyStreamToolProbeScenario()
		scenario.prompt = "Find the smallest four-digit integer n such that n modulo 7 is 3, n modulo 11 is 5, n modulo 13 is 7, and the sum of its decimal digits is 21. If unirouter_probe_echo is available, call it once with value equal to the decimal representation of n; otherwise return the decimal representation of n as the final answer."
		scenario.expectedValue = "6429"
		scenario.strictArguments = true
		return scenario, nil
	}
	return legacyStreamToolProbeScenario(), nil
}

func (s streamToolProbeScenario) tools() []map[string]interface{} {
	return []map[string]interface{}{{"type": "function", "function": map[string]interface{}{
		"name": s.functionName, "description": s.description,
		"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]string{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false},
	}}}
}

// The task oracle consumes only assembled arguments. Provider framing, usage,
// identity, terminal-state checks and exact binding authority remain with their
// existing owners, so a new task does not install a new transport or parser.
func (s streamToolProbeScenario) matchesArguments(raw string) bool {
	if s.strictArguments && strictjson.Validate([]byte(raw)) != nil {
		return false
	}
	var args map[string]interface{}
	return json.Unmarshal([]byte(raw), &args) == nil && len(args) == 1 && args["value"] == s.expectedValue
}
