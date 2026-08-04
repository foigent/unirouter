package invocationopt

const (
	ReadinessContractVersion = "unirouter.invocation-optimization-readiness.v1"

	ReadinessReady         = "ready"
	ReadinessBlocked       = "blocked"
	ReadinessNotApplicable = "not_applicable"

	DriverReadinessNotTargeted   = "not_targeted"
	DriverReadinessPendingShadow = "pending_shadow"
	DriverReadinessBlocked       = "blocked"
	DriverReadinessReady         = "ready"
	DriverReadinessCanaryActive  = "canary_active"
	DriverReadinessRolledBack    = "rolled_back"
)

// ShadowEvidence contains only content-free admission counters for one driver
// and one exact policy revision. Failures has the same meaning as the Canary
// transition gate: assigned/mutated Shadow rows or eligible safety/quality
// failures all fail closed.
type ShadowEvidence struct {
	Samples  int `json:"samples"`
	Eligible int `json:"eligible"`
	Failures int `json:"failures"`
}

type ReadinessBlocker struct {
	Code        string `json:"code"`
	Driver      string `json:"driver,omitempty"`
	Description string `json:"description"`
}

type DriverReadiness struct {
	Driver               string             `json:"driver"`
	Target               string             `json:"target"`
	State                string             `json:"state"`
	RuntimeRequired      bool               `json:"runtime_required"`
	RuntimeReady         bool               `json:"runtime_ready"`
	MinimumShadowSamples int                `json:"minimum_shadow_samples"`
	Evidence             ShadowEvidence     `json:"evidence"`
	Blockers             []ReadinessBlocker `json:"blockers"`
}

type CanaryReadinessInput struct {
	Policy                    Policy
	ResponseCacheRuntimeReady bool
	Evidence                  map[string]ShadowEvidence
}

// CanaryReadinessResult is a read-only technical admission view. It never
// grants activation or traffic authority; those decisions remain separate
// audited operator actions even when every technical gate is ready.
type CanaryReadinessResult struct {
	ContractVersion           string             `json:"contract_version"`
	Purpose                   string             `json:"purpose"`
	Overall                   string             `json:"overall"`
	CanaryAdmissionReady      bool               `json:"canary_admission_ready"`
	PolicyRef                 string             `json:"policy_ref"`
	PolicyRevision            int64              `json:"policy_revision"`
	PolicyStage               string             `json:"policy_stage"`
	PolicyStatus              string             `json:"policy_status"`
	Drivers                   []DriverReadiness  `json:"drivers"`
	Blockers                  []ReadinessBlocker `json:"blockers"`
	RuntimeMutation           string             `json:"runtime_mutation"`
	GrantsActivationAuthority bool               `json:"grants_activation_authority"`
	GrantsTrafficAuthority    bool               `json:"grants_traffic_authority"`
}

// ResolveCanaryReadiness is the single product-local interpretation used by
// the read-only API and the server-side Canary transition. The fixed driver
// and blocker order makes the response deterministic and reviewable.
func ResolveCanaryReadiness(input CanaryReadinessInput) CanaryReadinessResult {
	policy := input.Policy
	result := CanaryReadinessResult{
		ContractVersion: ReadinessContractVersion,
		Purpose:         "canary_admission",
		Overall:         ReadinessBlocked,
		PolicyRef:       policy.PolicyRef,
		PolicyRevision:  policy.Revision,
		PolicyStage:     policy.Stage,
		PolicyStatus:    policy.Status,
		Drivers:         make([]DriverReadiness, 0, 2),
		Blockers:        make([]ReadinessBlocker, 0, 7),
		RuntimeMutation: "none",
	}

	targetedDrivers := 0
	for _, driver := range []string{DriverResponseCache, DriverCompression} {
		target := policy.ResponseCacheTarget
		runtimeRequired := driver == DriverResponseCache
		runtimeReady := input.ResponseCacheRuntimeReady
		if driver == DriverCompression {
			target = policy.CompressionTarget
			runtimeReady = true
		}
		item := DriverReadiness{
			Driver: driver, Target: target, State: DriverReadinessNotTargeted,
			RuntimeRequired: runtimeRequired, RuntimeReady: runtimeReady,
			MinimumShadowSamples: policy.MinimumShadowSamples,
			Evidence:             input.Evidence[driver],
			Blockers:             make([]ReadinessBlocker, 0, 3),
		}
		if target == TargetCanary {
			targetedDrivers++
			item.State = driverStateForPolicy(policy.Stage)
		}
		result.Drivers = append(result.Drivers, item)
	}

	if policy.Status != "active" {
		result.Blockers = append(result.Blockers, blocker("policy_inactive", "", "Policy status is not active."))
	}
	if targetedDrivers == 0 {
		result.Blockers = append(result.Blockers, blocker("no_driver_targeted", "", "At least one optimization driver must target Canary."))
	}

	switch policy.Stage {
	case StageCanary:
		result.Overall = ReadinessNotApplicable
		return result
	case StageShadow:
		// Continue below and evaluate the exact current-revision evidence.
	default:
		result.Blockers = append(result.Blockers, blocker("policy_stage_not_shadow", "", "Policy must be in Shadow before Canary admission."))
		return result
	}

	for index := range result.Drivers {
		item := &result.Drivers[index]
		if item.Target != TargetCanary {
			continue
		}
		if item.RuntimeRequired && !item.RuntimeReady {
			item.Blockers = append(item.Blockers, blocker("response_cache_runtime_unavailable", item.Driver, "Exact Response Cache key backend is unavailable."))
		}
		if item.Evidence.Samples < item.MinimumShadowSamples {
			item.Blockers = append(item.Blockers, blocker("shadow_samples_insufficient", item.Driver, "Current-revision Shadow samples are below the policy minimum."))
		}
		if item.Evidence.Eligible <= 0 {
			item.Blockers = append(item.Blockers, blocker("shadow_no_eligible_samples", item.Driver, "Current-revision Shadow evidence has no eligible sample."))
		}
		if item.Evidence.Failures > 0 {
			item.Blockers = append(item.Blockers, blocker("shadow_failures_present", item.Driver, "Current-revision Shadow evidence contains admission failures."))
		}
		if len(item.Blockers) == 0 {
			item.State = DriverReadinessReady
		} else {
			item.State = DriverReadinessBlocked
			result.Blockers = append(result.Blockers, item.Blockers...)
		}
	}

	if len(result.Blockers) == 0 {
		result.Overall = ReadinessReady
		result.CanaryAdmissionReady = true
	}
	return result
}

func driverStateForPolicy(stage string) string {
	switch stage {
	case StageShadow:
		return DriverReadinessBlocked
	case StageCanary:
		return DriverReadinessCanaryActive
	case StageRolledBack:
		return DriverReadinessRolledBack
	default:
		return DriverReadinessPendingShadow
	}
}

func blocker(code, driver, description string) ReadinessBlocker {
	return ReadinessBlocker{Code: code, Driver: driver, Description: description}
}
