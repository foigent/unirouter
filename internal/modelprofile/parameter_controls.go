package modelprofile

import "slices"

// ParameterControls is a read-only description of referenced implementations.
// It is deliberately outside Profile and Definition canonical bytes. It grants
// neither request-combination qualification nor execution authority.
type ParameterControls struct {
	Version   string             `json:"version"`
	Output    *OutputControl     `json:"output,omitempty"`
	Reasoning *ReasoningControls `json:"reasoning,omitempty"`
}

const ParameterControlsVersion = "unirouter.parameter-controls.v1"

type OutputControl struct {
	Field             string           `json:"field"`
	AcceptedFields    []string         `json:"accepted_fields"`
	Minimum           int64            `json:"minimum"`
	Maximum           int64            `json:"maximum"`
	FieldMaximums     map[string]int64 `json:"field_maximums,omitempty"`
	MutuallyExclusive bool             `json:"mutually_exclusive"`
}

type ParameterControl struct {
	Object          string            `json:"object,omitempty"`
	Field           string            `json:"field"`
	WireType        string            `json:"wire_type"`
	Values          []string          `json:"values"`
	ProviderDefault string            `json:"provider_default,omitempty"`
	DisabledValues  []string          `json:"disabled_values,omitempty"`
	Aliases         map[string]string `json:"aliases,omitempty"`
}

type ParameterBudget struct {
	Field                       string `json:"field"`
	Minimum                     int64  `json:"minimum"`
	Maximum                     int64  `json:"maximum"`
	ProviderDefault             int64  `json:"provider_default"`
	RequiresActiveReasoning     bool   `json:"requires_active_reasoning"`
	MutuallyExclusiveWithEffort bool   `json:"mutually_exclusive_with_effort"`
}

type ReasoningControls struct {
	ProfileID                        string            `json:"profile_id"`
	Mode                             string            `json:"mode"`
	DefaultAuthority                 string            `json:"default_authority"`
	Toggle                           *ParameterControl `json:"toggle,omitempty"`
	Effort                           *ParameterControl `json:"effort,omitempty"`
	Budget                           *ParameterBudget  `json:"budget,omitempty"`
	ActiveOutputFieldMaximums        map[string]int64  `json:"active_output_field_maximums,omitempty"`
	ThinkingClearSupported           bool              `json:"thinking_clear_supported"`
	ToggleAndEffortMutuallyExclusive bool              `json:"toggle_and_effort_mutually_exclusive"`
}

func DescribeParameterControls(p Profile) ParameterControls {
	d := ParameterControls{Version: ParameterControlsVersion}
	if protocol, ok := ResolveChatProtocolContract(p.ProtocolProfile); ok && protocol.OutputLimitModeled {
		d.Output = &OutputControl{
			Field: protocol.OutputLimitField, AcceptedFields: slices.Clone(protocol.OutputLimitAcceptedFields),
			Minimum: protocol.OutputLimitMin, Maximum: protocol.OutputLimitMax,
			FieldMaximums:     cloneParameterNumbers(protocol.OutputLimitFieldMaximums),
			MutuallyExclusive: protocol.OutputLimitFieldsMutuallyExclusive,
		}
		if len(d.Output.AcceptedFields) == 0 {
			d.Output.AcceptedFields = []string{protocol.OutputLimitField}
		}
	}
	if c, ok := ResolveReasoningContract(p.ReasoningProfile); ok {
		r := &ReasoningControls{ProfileID: c.ProfileID, Mode: c.Mode, DefaultAuthority: c.DefaultAuthority,
			ActiveOutputFieldMaximums: cloneParameterNumbers(c.ActiveOutputFieldMaximums), ThinkingClearSupported: c.ThinkingClearSupported,
			ToggleAndEffortMutuallyExclusive: c.ToggleAndEffortMutuallyExclusive}
		r.Toggle = describeParameterControl(c.ToggleControl, c.ToggleObjectField, c.RequestObjectField)
		r.Effort = describeParameterControl(c.EffortControl, c.EffortObjectField, c.RequestObjectField)
		if b := c.BudgetControl; b.Field != "" {
			r.Budget = &ParameterBudget{b.Field, b.Minimum, b.Maximum, b.ProviderDefault, b.RequiresActiveReasoning, b.MutuallyExclusiveWithEffort}
		}
		d.Reasoning = r
	}
	return d
}

func describeParameterControl(c ReasoningControlContract, object, fallback string) *ParameterControl {
	if c.Field == "" {
		return nil
	}
	if object == "" {
		object = fallback
	}
	// Legacy Chat contracts name the whole thinking object; the existing Chat
	// validator reads its type member. An omitted wire type means string there.
	if object == "" && c.Field == "thinking" && c.WireType != ReasoningControlWireBoolean {
		object, c.Field = "thinking", "type"
	}
	if c.WireType == "" {
		c.WireType = ReasoningControlWireString
	}
	return &ParameterControl{Object: object, Field: c.Field, WireType: c.WireType,
		Values: slices.Clone(c.AllowedValues), ProviderDefault: c.ProviderDefault,
		DisabledValues: slices.Clone(c.DisabledValues), Aliases: cloneStringMap(c.CompatibilityValues)}
}

func cloneParameterNumbers(source map[string]int64) map[string]int64 {
	if source == nil {
		return nil
	}
	result := make(map[string]int64, len(source))
	for k, v := range source {
		result[k] = v
	}
	return result
}
