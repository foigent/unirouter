// Package accessscope defines the small, shared runtime contract for an
// enterprise access grant's model and endpoint envelope.
package accessscope

import (
	"regexp"
	"slices"
	"strings"
)

const (
	ModeProjectDefault = "project_default"
	ModeExplicit       = "explicit"

	// ProjectDefaultRPMLimit and ProjectDefaultRPSLimit are the canonical
	// model-execution ceilings when a Grant omits an explicit rate override.
	// Credential issuance and deterministic approval policy must use the same
	// values so a credential cannot bypass review by relying on runtime fallbacks.
	ProjectDefaultRPMLimit = 60
	ProjectDefaultRPSLimit = 10
)

var projectDefaultEndpoints = []string{
	"/v1/chat/completions",
	"/v1/embeddings",
	"/v1/models",
}

var supportedExecutionEndpoints = []string{
	"/v1/chat/completions",
	"/v1/embeddings",
	"/v1/models",
	"/v1/media/jobs",
}

var mediaJobIDPattern = regexp.MustCompile(`^mediajob_[0-9a-fA-F]{32}$`)

var capabilityManagementEndpoints = []string{
	"/v1/agent-capabilities/exchange",
	"/v1/agent-capabilities/delegate",
	"/v1/agent-capabilities/refresh",
	"/v1/agent-capabilities/revoke",
}

// NormalizeMode preserves compatibility for clients written before scope_mode
// was explicit: two empty lists mean project_default; otherwise they mean an
// explicit scope and must subsequently pass Validate.
func NormalizeMode(raw string, models, endpoints []string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode != "" {
		return mode
	}
	if len(models) == 0 && len(endpoints) == 0 {
		return ModeProjectDefault
	}
	return ModeExplicit
}

func Validate(mode string, models, endpoints []string) bool {
	switch mode {
	case ModeProjectDefault:
		return len(models) == 0 && len(endpoints) == 0
	case ModeExplicit:
		return len(models) > 0 && len(endpoints) > 0 && allSupportedEndpoints(endpoints)
	default:
		return false
	}
}

// IsSupportedEndpoint is the complete canonical v1 access-grant execution
// vocabulary. Dynamic media control paths are represented by the one
// /v1/media/jobs family and never persisted as per-job grant entries. Agent
// capability control endpoints remain identity-management operations outside
// this execution envelope.
func IsSupportedEndpoint(endpoint string) bool {
	return slices.Contains(supportedExecutionEndpoints, strings.TrimSpace(endpoint))
}

// CanonicalExecutionEndpoint maps an exact runtime path to the stable endpoint
// family used by Grant, Agent capability and model-authorization checks. It
// deliberately validates the full media job identity and the only two control
// suffixes; unknown paths are returned unchanged so they cannot inherit media
// authority through a broad prefix match.
func CanonicalExecutionEndpoint(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[0] != "" || parts[1] != "v1" || parts[2] != "media" || parts[3] != "jobs" ||
		!mediaJobIDPattern.MatchString(parts[4]) {
		return path
	}
	if len(parts) == 5 || (len(parts) == 6 && (parts[5] == "cancel" || parts[5] == "content")) {
		return "/v1/media/jobs"
	}
	return path
}

func allSupportedEndpoints(endpoints []string) bool {
	for _, endpoint := range endpoints {
		if !IsSupportedEndpoint(endpoint) {
			return false
		}
	}
	return true
}

func ProjectDefaultEndpoints() []string {
	return slices.Clone(projectDefaultEndpoints)
}

func IsProjectDefaultEndpoint(endpoint string) bool {
	return slices.Contains(projectDefaultEndpoints, strings.TrimSpace(endpoint))
}

// IsCapabilityManagementEndpoint identifies the separate control surface that
// can attenuate an already-authorized workload identity. These endpoints are
// deliberately excluded from the execution endpoint envelope.
func IsCapabilityManagementEndpoint(endpoint string) bool {
	return slices.Contains(capabilityManagementEndpoints, strings.TrimSpace(endpoint))
}
