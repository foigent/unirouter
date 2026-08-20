// Package accessscope defines the small, shared runtime contract for an
// enterprise access grant's model and endpoint envelope.
package accessscope

import (
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/modeloperation"
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
	_, ok := modeloperation.Lookup(strings.TrimSpace(endpoint))
	return ok
}

// CanonicalExecutionEndpoint maps an exact runtime path to the stable endpoint
// family used by Grant, Agent capability and model-authorization checks. It
// deliberately validates the full media job identity and the only two control
// suffixes; unknown paths are returned unchanged so they cannot inherit media
// authority through a broad prefix match.
func CanonicalExecutionEndpoint(path string) string {
	return modeloperation.CanonicalPath(path)
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
	return modeloperation.ProjectDefaultPaths()
}

func IsProjectDefaultEndpoint(endpoint string) bool {
	definition, ok := modeloperation.Lookup(strings.TrimSpace(endpoint))
	return ok && definition.ProjectDefault
}

// RequiresExplicitScope identifies operations which may only be admitted from
// an explicit Grant envelope. It does not independently decide whether an
// established operation retains legacy-unbound compatibility.
func RequiresExplicitScope(endpoint string) bool {
	definition, ok := modeloperation.Lookup(strings.TrimSpace(endpoint))
	return ok && definition.RequiresExplicitScope
}

// ExecutionEndpointAllowed applies the effective endpoint boundary shared by
// root API-key admission, Agent capability issuance, and offline trust review.
// Unknown paths fail closed. Legacy-unbound authority is limited to operations
// that explicitly retain that compatibility contract. A bound Grant must name
// the exact operation, and newly introduced operations additionally require an
// explicit Grant rather than project-default inheritance.
func ExecutionEndpointAllowed(
	grantBound bool,
	scopeMode string,
	endpointDenied bool,
	allowedEndpoints []string,
	path string,
) bool {
	endpoint := modeloperation.CanonicalPath(path)
	definition, known := modeloperation.Lookup(endpoint)
	if !known {
		return false
	}
	if !grantBound {
		return definition.LegacyUnboundAllowed
	}
	if endpointDenied || (definition.RequiresExplicitScope && scopeMode != ModeExplicit) {
		return false
	}
	for _, allowed := range allowedEndpoints {
		if strings.TrimSpace(allowed) == endpoint {
			return true
		}
	}
	return false
}

// IsCapabilityManagementEndpoint identifies the separate control surface that
// can attenuate an already-authorized workload identity. These endpoints are
// deliberately excluded from the execution endpoint envelope.
func IsCapabilityManagementEndpoint(endpoint string) bool {
	return slices.Contains(capabilityManagementEndpoints, strings.TrimSpace(endpoint))
}
