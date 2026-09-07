package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

type clientControlProjection struct {
	Model               string            `json:"model"`
	Mode                string            `json:"mode"`
	Native              []string          `json:"native_effort_values,omitempty"`
	Client              []string          `json:"client_levels,omitempty"`
	Mapping             map[string]string `json:"client_level_mapping,omitempty"`
	Unsupported         []string          `json:"unsupported_controls,omitempty"`
	Notes               []string          `json:"notes,omitempty"`
	ProviderDefaults    map[string]any    `json:"provider_defaults,omitempty"`
	ClientDefaultStatus string            `json:"client_default_status,omitempty"`
}

var configuredThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

func hasExplicitReasoningProjection(model map[string]any) bool {
	if _, exists := model["thinkingLevelMap"]; exists {
		return true
	}
	compat, _ := model["compat"].(map[string]any)
	for _, key := range []string{"thinkingFormat", "reasoningEffortMap"} {
		if _, exists := compat[key]; exists {
			return true
		}
	}
	return false
}

// This projection supports the normal OpenClaw host and selected completions
// transport. Nullable menu entries cannot validate arbitrary direct SDK inputs.
func projectReasoningParameters(provider, id string, model map[string]any, profile catalogExecutionProfile) (clientControlProjection, error) {
	report := clientControlProjection{Model: id, Mode: "legacy_metadata"}
	fail := func(reason string) (clientControlProjection, error) {
		return clientControlProjection{}, fmt.Errorf("model %q: %s", id, reason)
	}
	p := profile.ParameterControls
	if p == nil {
		if hasExplicitReasoningProjection(model) {
			return fail("server omitted exact controls; existing thinking settings cannot be verified")
		}
		return report, nil
	}
	if p.Version != modelprofile.ParameterControlsVersion {
		return fail("unsupported parameter-controls version")
	}
	compat := objectField(model, "compat")
	menu := map[string]any{}
	for _, level := range configuredThinkingLevels {
		menu[level] = nil
	}
	mapping := map[string]any{}
	format := ""
	clientEfforts := []string{}
	report.Mode = "provider_default_only"
	report.ClientDefaultStatus = "host_selected_not_measured_by_sync"
	report.Mapping = map[string]string{}
	report.Notes = append(report.Notes, "Provider defaults describe omitted wire fields. The OpenClaw host chooses its own default level; these defaults are not interchangeable.")
	r := p.Reasoning
	nativeWrapper := usesOpenClawDeepSeekV4Wrapper(id)
	if nativeWrapper && (r == nil || strings.HasPrefix(strings.ToLower(provider), "microsoft-foundry")) {
		return fail("the installed client's DeepSeek V4 wrapper cannot preserve this contract in the selected provider namespace")
	}
	if r != nil {
		if err := validateReasoningDescription(r); err != nil {
			return fail(err.Error())
		}
		report.ProviderDefaults = map[string]any{}
		for _, control := range []*modelprofile.ParameterControl{r.Toggle, r.Effort} {
			if control != nil && control.ProviderDefault != "" {
				path := control.Field
				if control.Object != "" {
					path = control.Object + "." + path
				}
				report.ProviderDefaults[path] = control.ProviderDefault
			}
		}
		if r.Budget != nil {
			report.ProviderDefaults[r.Budget.Field] = r.Budget.ProviderDefault
			report.Unsupported = append(report.Unsupported, r.Budget.Field)
			report.Notes = append(report.Notes, "Thinking budget is a separate provider control; this projection does not configure it. Raw body overrides require separate validation.")
		}
		if r.Effort != nil {
			report.Native = slices.Clone(r.Effort.Values)
		}
		t := r.Toggle
		qwenToggle := t != nil && t.Object == "" && t.Field == "enable_thinking" && t.WireType == "boolean" &&
			slices.Contains(t.Values, "enabled") && r.Effort == nil
		rootEffort := r.Effort != nil && r.Effort.Object == "" && r.Effort.Field == "reasoning_effort" && r.Effort.WireType == "string"
		if nativeWrapper && !(t != nil && t.Object == "thinking" && t.Field == "type" && t.WireType == "string" &&
			slices.Contains(t.Values, "enabled") && rootEffort && slices.Contains(r.Effort.Values, "high") && !r.ToggleAndEffortMutuallyExclusive) {
			return fail("the installed client's DeepSeek V4 wrapper does not match the exact typed controls")
		}
		switch {
		case nativeWrapper:
			format, report.Mode = "deepseek", "effort"
			report.Notes = append(report.Notes, "The normal host's DeepSeek V4 wrapper is limited to verified off/high here; other levels are withheld because the wrapper rewrites them. This is a client constraint, not a model capability list.")
		case qwenToggle:
			format, report.Mode = "qwen", "toggle"
		case rootEffort && (t == nil || t.ProviderDefault == "enabled" && !r.ToggleAndEffortMutuallyExclusive):
			// The selected host transport sends root effort but does not implement
			// the standalone SDK's deepseek/zai toggle. Leave an enabled provider
			// toggle omitted; only the independently valid effort control is sent.
			format, report.Mode = "openai", "effort"
			if t != nil {
				report.Unsupported = append(report.Unsupported, "toggle:"+t.Field)
				report.Notes = append(report.Notes, "The provider's enabled toggle default is retained; this transport projects only the independent root effort control.")
			}
		default:
			if t != nil {
				report.Unsupported = append(report.Unsupported, t.Field)
			}
			if r.Effort != nil {
				report.Unsupported = append(report.Unsupported, r.Effort.Field)
			}
		}
		if report.Mode != "provider_default_only" {
			if (qwenToggle || nativeWrapper) && slices.Contains(t.Values, "disabled") || rootEffort && slices.Contains(r.Effort.DisabledValues, "none") {
				menu["off"], mapping["off"], report.Mapping["off"] = "none", "none", "disabled"
				if rootEffort {
					clientEfforts = append(clientEfforts, "none")
				}
			}
			if r.Effort == nil {
				menu["low"], mapping["low"], report.Mapping["low"] = "enabled", "enabled", "enabled"
				report.Notes = append(report.Notes, "The single low client choice means thinking enabled, not a native low effort or token budget.")
			} else {
				report.Mode = "effort"
				for _, level := range configuredThinkingLevels[1:6] {
					if nativeWrapper && level != "high" {
						report.Unsupported = append(report.Unsupported, "client_level:"+level)
						continue
					}
					wire := level
					if alias, ok := r.Effort.Aliases[level]; ok {
						wire = alias
					}
					if !slices.Contains(r.Effort.Values, wire) {
						continue
					}
					if wire == "max" {
						report.Unsupported = append(report.Unsupported, "client_level:"+level)
						continue
					}
					menu[level], mapping[level], report.Mapping[level] = wire, wire, wire
					if !slices.Contains(clientEfforts, wire) && wire != "max" {
						clientEfforts = append(clientEfforts, wire)
					}
				}
				if slices.Contains(r.Effort.Values, "max") {
					report.Unsupported = append(report.Unsupported, "client_level:max", "client_level:ultra")
					report.Notes = append(report.Notes, "The installed host adds ultra when max is advertised; that menu is withheld until its actual transport semantics are verified.")
				}
			}
		}
	}
	for _, level := range configuredThinkingLevels {
		if menu[level] != nil {
			report.Client = append(report.Client, level)
		}
	}
	if len(report.Client) == 0 {
		report.Mode = "provider_default_only"
		report.Notes = append(report.Notes, "No client thinking menu is offered. The installed host may label its fallback off; no disabling field is sent, so required/provider-default thinking remains in force.")
	}
	// Never guess whether an existing field was user-authored or a previous sync.
	if previous, exists := model["thinkingLevelMap"]; exists && !sameProjection(previous, menu) {
		return fail("existing thinkingLevelMap conflicts with the exact supported client projection")
	}
	for _, field := range []struct {
		key   string
		value any
	}{{"thinkingFormat", format}, {"reasoningEffortMap", mapping}} {
		if previous, exists := compat[field.key]; exists && !sameProjection(previous, field.value) {
			return fail("existing " + field.key + " conflicts with the exact supported client projection")
		}
	}
	if params, ok := model["params"].(map[string]any); ok {
		for _, key := range []string{"thinking_budget", "enable_thinking", "thinking", "reasoning", "reasoning_effort", "reasoningEffort"} {
			if _, exists := params[key]; exists {
				return fail("explicit params." + key + " is not a verified client-control path; no configuration was changed")
			}
		}
		for _, field := range []string{"extra_body", "extraBody"} {
			if value, exists := params[field]; exists && value != nil {
				body, ok := value.(map[string]any)
				if !ok {
					return fail("params." + field + " is not a verifiable object; no configuration was changed")
				}
				for _, key := range []string{"thinking_budget", "enable_thinking", "thinking", "reasoning", "reasoning_effort", "reasoningEffort", "max_tokens", "max_completion_tokens", "maxTokens"} {
					if _, exists := body[key]; exists {
						return fail("params." + field + "." + key + " overrides the verified client-control path; no configuration was changed")
					}
				}
			}
		}
	}
	model["thinkingLevelMap"] = menu
	compat["supportsReasoningEffort"] = report.Mode == "effort" && len(report.Client) > 0
	if format != "" {
		compat["thinkingFormat"] = format
	} else {
		delete(compat, "thinkingFormat")
	}
	if len(mapping) > 0 {
		compat["reasoningEffortMap"] = mapping
	} else {
		delete(compat, "reasoningEffortMap")
	}
	if len(clientEfforts) > 0 {
		compat["supportedReasoningEfforts"] = clientEfforts
	} else {
		delete(compat, "supportedReasoningEfforts")
	}
	model["compat"] = compat
	return report, nil
}

// This reproduces a version-verified CLIENT wrapper trigger, never a provider,
// execution capability or routing inference. A stable Alias need not trigger it;
// typed controls still decide what may be sent in either case.
func usesOpenClawDeepSeekV4Wrapper(id string) bool {
	id = strings.SplitN(strings.ToLower(strings.TrimSpace(id)), ":", 2)[0]
	parts := strings.Split(id, "/")
	id = parts[len(parts)-1]
	return id == "deepseek-v4-flash" || id == "deepseek-v4-pro"
}

func sameProjection(a, b any) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

func validateReasoningDescription(r *modelprofile.ReasoningControls) error {
	if r.ProfileID == "" || (r.Mode != "toggle" && r.Mode != "required" && r.Mode != "always_on") {
		return fmt.Errorf("invalid reasoning control identity or mode")
	}
	for _, c := range []*modelprofile.ParameterControl{r.Toggle, r.Effort} {
		if c == nil {
			continue
		}
		if c.Field == "" || (c.WireType != "string" && c.WireType != "boolean") || len(c.Values) == 0 {
			return fmt.Errorf("invalid reasoning control shape")
		}
		for _, target := range c.Aliases {
			if !slices.Contains(c.Values, target) {
				return fmt.Errorf("invalid reasoning alias target")
			}
		}
	}
	if b := r.Budget; b != nil && (b.Field == "" || b.Minimum < 0 || b.Maximum < b.Minimum || b.ProviderDefault < b.Minimum || b.ProviderDefault > b.Maximum) {
		return fmt.Errorf("invalid thinking budget bounds")
	}
	return nil
}
