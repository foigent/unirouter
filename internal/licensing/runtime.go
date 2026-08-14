package licensing

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/uniroute/unirouter/internal/errdocs"
)

const (
	FeatureAll               = "*"
	FeatureGateway           = "gateway"
	FeatureAgentCapabilities = "agent_capabilities"
	FeatureOIDC              = "oidc"
	FeatureGuardrails        = "guardrails"
	FeatureNotifications     = "notifications"
	FeatureModelGovernance   = "model_governance"
	FeatureDataExchange      = "data_exchange"

	LimitActiveAPIKeys  = "active_api_keys"
	LimitActiveUsers    = "active_users"
	LimitActiveProjects = "active_projects"
	LimitActiveChannels = "active_channels"
)

var supportedFeatures = map[string]bool{
	FeatureAll: true, FeatureGateway: true, FeatureAgentCapabilities: true,
	FeatureOIDC: true, FeatureGuardrails: true, FeatureNotifications: true,
	FeatureModelGovernance: true, FeatureDataExchange: true,
}

var concreteFeatures = []string{
	FeatureGateway,
	FeatureAgentCapabilities,
	FeatureOIDC,
	FeatureGuardrails,
	FeatureNotifications,
	FeatureModelGovernance,
	FeatureDataExchange,
}

// Core management features are part of every enforceable commercial License.
// They remain named capabilities so clients can fail closed when the License
// itself is unavailable, but they are not independently sold feature flags.
var coreManagementFeatures = map[string]bool{
	FeatureGuardrails:      true,
	FeatureNotifications:   true,
	FeatureModelGovernance: true,
}

var supportedLimits = map[string]bool{
	LimitActiveAPIKeys: true, LimitActiveUsers: true,
	LimitActiveProjects: true, LimitActiveChannels: true,
}

type Decision struct {
	Allowed bool
	Code    string
	Message string
	Status  Status
}

// CapabilityStatus is the non-sensitive authorization projection exposed to
// authenticated clients. It deliberately excludes the signed payload,
// customer identity, key ID, signature and resource limits.
type CapabilityStatus struct {
	Status      string          `json:"status"`
	Enforceable bool            `json:"enforceable"`
	Features    map[string]bool `json:"features"`
}

// Runtime holds the latest stored license bytes while evaluating signature,
// deployment binding and wall-clock expiry on every admission. Replace is
// safe for concurrent multi-handler use.
type Runtime struct {
	verifier *Verifier
	raw      atomic.Value // []byte
}

func NewRuntime(verifier *Verifier) *Runtime {
	if verifier == nil {
		verifier = NewCommercialVerifier(nil, nil, 30, "")
	}
	runtime := &Runtime{verifier: verifier}
	runtime.raw.Store([]byte{})
	return runtime
}

func (r *Runtime) Replace(raw []byte) Status {
	copyRaw := append([]byte(nil), raw...)
	r.raw.Store(copyRaw)
	return r.verifier.VerifyBytes(copyRaw)
}

func (r *Runtime) Evaluate(raw []byte) Status {
	return r.verifier.VerifyBytes(raw)
}

func (r *Runtime) Status() Status {
	if r == nil || r.verifier == nil {
		return Status{Status: StatusInvalid, Error: "commercial license runtime is not configured"}
	}
	raw, _ := r.raw.Load().([]byte)
	return r.verifier.VerifyBytes(raw)
}

// Capabilities expands wildcard and partial licenses into one stable boolean
// result per supported concrete feature. Every enforceable commercial License
// includes the core management features. Invalid, missing, expired, not-yet-
// valid and deployment-mismatched licenses deny every feature.
func (r *Runtime) Capabilities() CapabilityStatus {
	status := r.Status()
	capabilities := CapabilityStatus{
		Status:      status.Status,
		Enforceable: status.Enforceable && status.Valid,
		Features:    make(map[string]bool, len(concreteFeatures)),
	}
	for _, feature := range concreteFeatures {
		capabilities.Features[feature] = authorizeStatus(status, feature).Allowed
	}
	return capabilities
}

func (r *Runtime) Authorize(feature string) Decision {
	status := r.Status()
	return authorizeStatus(status, feature)
}

func authorizeStatus(status Status, feature string) Decision {
	decision := Decision{Status: status}
	if !status.Enforceable || !status.Valid {
		decision.Code, decision.Message = denialForStatus(status)
		return decision
	}
	if feature != "" && !coreManagementFeatures[feature] && !hasFeature(status.License, feature) {
		decision.Code = "license_error.feature_not_licensed"
		decision.Message = "The commercial license does not authorize this feature"
		return decision
	}
	decision.Allowed = true
	return decision
}

func (r *Runtime) AuthorizeAll(features ...string) Decision {
	status := r.Status()
	if len(features) == 0 {
		return authorizeStatus(status, "")
	}
	for _, feature := range features {
		decision := authorizeStatus(status, feature)
		if !decision.Allowed {
			return decision
		}
	}
	return Decision{Allowed: true, Status: status}
}

func (r *Runtime) Limit(name string) (int64, bool) {
	status := r.Status()
	if !status.Enforceable || !status.Valid || status.License == nil {
		return 0, false
	}
	limit, ok := status.License.Limits[strings.TrimSpace(name)]
	return limit, ok
}

func (r *Runtime) Gate(feature string) func(http.Handler) http.Handler {
	return r.GateAll(feature)
}

func (r *Runtime) GateAll(features ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			decision := r.AuthorizeAll(features...)
			if decision.Allowed {
				next.ServeHTTP(w, req)
				return
			}
			writeLicenseError(w, decision)
		})
	}
}

func hasFeature(payload *Payload, feature string) bool {
	if payload == nil {
		return false
	}
	for _, allowed := range payload.Features {
		if allowed == FeatureAll || allowed == feature {
			return true
		}
	}
	return false
}

func denialForStatus(status Status) (string, string) {
	switch status.Status {
	case StatusMissing:
		return "license_error.missing", "A commercial license is required"
	case StatusExpired:
		return "license_error.expired", "The commercial license has expired"
	case StatusNotYetValid:
		return "license_error.not_yet_valid", "The commercial license is not valid yet"
	case StatusDeploymentMismatch:
		return "license_error.deployment_mismatch", "The commercial license is bound to a different deployment"
	case StatusActive, StatusExpiring:
		return "license_error.commercial_required", "An enforceable commercial license is required"
	case StatusInvalid:
		return "license_error.invalid", "The commercial license cannot be verified"
	default:
		return "license_error.invalid", "The commercial license is not valid"
	}
}

func writeLicenseError(w http.ResponseWriter, decision Decision) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-UniRouter-License-Status", decision.Status.Status)
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "permission_error",
			"code":    decision.Code,
			"message": decision.Message,
			"doc_url": errdocs.URLForCode(decision.Code),
			"param":   nil,
		},
	})
}
