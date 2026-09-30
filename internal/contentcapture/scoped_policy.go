package contentcapture

// This file resolves already-applied policy configurations. It performs no
// authentication, database reads, writes, or policy activation. Callers must
// obtain the principal from the authenticated API key and recheck the selected
// scope and versions in the reservation transaction before storing content.

const (
	PolicyScopeDeployment     = "deployment"
	PolicyScopeEnterprise     = "enterprise"
	PolicyScopeProject        = "project"
	PolicyScopeUser           = "user"
	ExecutionStorageTemporary = "temporary_allowed"
	ExecutionStorageStrict    = "strict_no_persist"
)

// UserPolicyException is a complete user exception, without a private quota.
// Full-content exceptions consume the project's one shared quota. An absent
// exception inherits the most specific enterprise/project configuration.
type UserPolicyException struct {
	UserID               int64
	ExecutionStorageMode string
	Mode                 string
	SupportedProtocols   []string
	ObservationPoints    []string
	RetentionSeconds     *int64
	MaxSavedBytes        *int64
	AllowedPurposes      []string
	OnLimit              string
}

type ScopedPolicyInput struct {
	Deployment                     PolicyConfig
	DeploymentExecutionStorageMode string
	Enterprise                     *PolicyConfig
	EnterpriseExecutionStorageMode string
	Project                        *PolicyConfig
	ProjectExecutionStorageMode    string
	User                           *UserPolicyException
	KeyType                        string
	OwnerUserID                    int64
	ProjectQuotaBytes              int64
	FullContentEnabled             bool
	StorageReady                   bool
	IgnoreDeploymentGate           bool // Management preview only; never use for capture/admission.
	SupportedProtocols             []string
	Limits                         PolicyLimits
}

type ScopedPolicyResult struct {
	Scope                string
	Config               PolicyConfig
	ExecutionStorageMode string
}

func normalizeExecutionStorageMode(mode string, config PolicyConfig) (string, error) {
	if mode == "" { // Legacy policies predate the execution-storage constraint.
		mode = ExecutionStorageTemporary
	}
	if mode != ExecutionStorageTemporary && mode != ExecutionStorageStrict {
		return "", invalidPolicy("execution storage mode is unsupported")
	}
	if mode == ExecutionStorageStrict && config.Mode == ModeFullContent {
		return "", invalidPolicy("full review content conflicts with strict no-persist")
	}
	return mode, nil
}

// ResolveScopedEffective chooses the most specific explicit effective policy.
// Deployment hard-stop and physical capabilities remain mandatory, while an
// explicit project or personal exception may override an enterprise default.
// This function cannot establish that a key owner is currently a member; its
// inputs must come from the caller's authenticated and current authority read.
func ResolveScopedEffective(input ScopedPolicyInput) (ScopedPolicyResult, error) {
	switch input.KeyType {
	case "personal":
		if input.OwnerUserID <= 0 {
			return ScopedPolicyResult{}, invalidPolicy("personal key has no authenticated owner")
		}
	case "shared", "service":
	default:
		return ScopedPolicyResult{}, invalidPolicy("key type is unsupported")
	}
	if err := input.Deployment.ValidateSupport(input.SupportedProtocols, input.Limits); err != nil {
		return ScopedPolicyResult{}, err
	}
	selected := clonePolicyConfig(input.Deployment)
	executionStorageMode := input.DeploymentExecutionStorageMode
	scope := PolicyScopeDeployment
	if input.Enterprise != nil {
		selected = clonePolicyConfig(*input.Enterprise)
		executionStorageMode = input.EnterpriseExecutionStorageMode
		scope = PolicyScopeEnterprise
	}
	if input.Project != nil {
		selected = clonePolicyConfig(*input.Project)
		executionStorageMode = input.ProjectExecutionStorageMode
		scope = PolicyScopeProject
	}
	if input.User != nil && input.KeyType == "personal" {
		if input.OwnerUserID <= 0 || input.User.UserID <= 0 || input.OwnerUserID != input.User.UserID {
			return ScopedPolicyResult{}, invalidPolicy("personal exception does not match authenticated owner")
		}
		selected = PolicyConfig{
			Mode:               input.User.Mode,
			SupportedProtocols: append([]string{}, input.User.SupportedProtocols...),
			ObservationPoints:  append([]string{}, input.User.ObservationPoints...),
			RetentionSeconds:   input.User.RetentionSeconds,
			MaxSavedBytes:      input.User.MaxSavedBytes,
			AllowedPurposes:    append([]string{}, input.User.AllowedPurposes...),
			OnLimit:            input.User.OnLimit,
		}
		if selected.Mode == ModeFullContent {
			quota := input.ProjectQuotaBytes
			selected.QuotaBytes = &quota
		}
		selected = clonePolicyConfig(selected)
		executionStorageMode = input.User.ExecutionStorageMode
		scope = PolicyScopeUser
	}
	// The project-wide counter limit is selected independently from review
	// mode. A full enterprise or project policy still consumes that one limit.
	if selected.Mode == ModeFullContent && input.ProjectQuotaBytes > 0 && scope != PolicyScopeDeployment {
		quota := input.ProjectQuotaBytes
		selected.QuotaBytes = &quota
	}
	var err error
	executionStorageMode, err = normalizeExecutionStorageMode(executionStorageMode, selected)
	if err != nil {
		return ScopedPolicyResult{}, err
	}
	if err := selected.ValidateSupport(input.SupportedProtocols, input.Limits); err != nil {
		return ScopedPolicyResult{}, err
	}
	if !input.IgnoreDeploymentGate && input.Deployment.Mode == ModeDisabled && selected.Mode != ModeDisabled {
		return ScopedPolicyResult{}, ErrPolicyInactive
	}
	if !input.IgnoreDeploymentGate && selected.Mode == ModeFullContent && (!input.FullContentEnabled || !input.StorageReady) {
		return ScopedPolicyResult{}, ErrPolicyInactive
	}
	return ScopedPolicyResult{Scope: scope, Config: selected, ExecutionStorageMode: executionStorageMode}, nil
}
