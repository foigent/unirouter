// Package modelreadiness defines the read-only contract that explains model
// registration/routing truth separately from one project's current caller
// authority. It contains no database or network side effects.
package modelreadiness

import "time"

const ContractVersion = "unirouter.model-readiness.v2"

const (
	ModeCaller     = "caller"
	ModeConnection = "connection"
)

type State string

const (
	StateReady         State = "ready"
	StateBlocked       State = "blocked"
	StateWarning       State = "warning"
	StateNotApplicable State = "not_applicable"
	StateUnavailable   State = "unavailable"
)

type Axis struct {
	State        State          `json:"state"`
	ReasonCode   string         `json:"reason_code"`
	SafeEvidence map[string]any `json:"safe_evidence"`
	FixRoute     string         `json:"fix_route,omitempty"`
	FixOwner     string         `json:"fix_owner,omitempty"`
}

type Fact struct {
	Value      *bool  `json:"value,omitempty"`
	State      State  `json:"state"`
	ReasonCode string `json:"reason_code"`
}

type Scope struct {
	Mode             string `json:"mode"`
	OrgID            int64  `json:"org_id"`
	ProjectID        int64  `json:"project_id"`
	DeploymentID     int64  `json:"deployment_id,omitempty"`
	RequestedModel   string `json:"requested_model"`
	ResolvedModel    string `json:"resolved_model"`
	Endpoint         string `json:"endpoint"`
	ModelOperationID string `json:"model_operation_id"`
	Caller           string `json:"caller"`
	ApplicationRef   string `json:"application_ref,omitempty"`
}

type Summary struct {
	Registered                   Fact `json:"registered"`
	CatalogVisible               Fact `json:"catalog_visible"`
	Routable                     Fact `json:"routable"`
	CurrentProjectCallerCallable Fact `json:"current_project_caller_callable"`
}

type PlatformModel struct {
	GatewayLicense     Axis `json:"gateway_license"`
	Registration       Axis `json:"registration"`
	Lifecycle          Axis `json:"lifecycle"`
	Catalog            Axis `json:"catalog"`
	PriceAuthority     Axis `json:"price_authority"`
	Deployment         Axis `json:"deployment"`
	ProviderCredential Axis `json:"provider_credential"`
	Operation          Axis `json:"operation"`
	BaseRoute          Axis `json:"base_route"`
	RuntimeSlot        Axis `json:"runtime_slot"`
}

type ProjectCaller struct {
	Project            Axis `json:"project"`
	Alias              Axis `json:"alias"`
	ModelSet           Axis `json:"model_set"`
	Wallet             Axis `json:"wallet"`
	ProjectBudget      Axis `json:"project_budget"`
	Application        Axis `json:"application"`
	Grant              Axis `json:"grant"`
	Credential         Axis `json:"credential"`
	ModelAuthorization Axis `json:"model_authorization"`
	LimitPolicy        Axis `json:"limit_policy"`
}

type Response struct {
	ContractVersion  string        `json:"contract_version"`
	Scope            Scope         `json:"scope"`
	Summary          Summary       `json:"summary"`
	PlatformModel    PlatformModel `json:"platform_model"`
	ProjectCaller    ProjectCaller `json:"project_caller"`
	RuntimeMutation  string        `json:"runtime_mutation"`
	ExternalRequests string        `json:"external_requests"`
	AsOf             time.Time     `json:"as_of"`
}

func Evidence(values ...any) map[string]any {
	result := make(map[string]any, len(values)/2)
	for i := 0; i+1 < len(values); i += 2 {
		key, ok := values[i].(string)
		if ok {
			result[key] = values[i+1]
		}
	}
	return result
}

func Ready(reason string, evidence map[string]any) Axis {
	return Axis{State: StateReady, ReasonCode: reason, SafeEvidence: nonNil(evidence)}
}

func Blocked(reason string, evidence map[string]any, fixRoute, fixOwner string) Axis {
	return Axis{State: StateBlocked, ReasonCode: reason, SafeEvidence: nonNil(evidence), FixRoute: fixRoute, FixOwner: fixOwner}
}

func Warning(reason string, evidence map[string]any, fixRoute, fixOwner string) Axis {
	return Axis{State: StateWarning, ReasonCode: reason, SafeEvidence: nonNil(evidence), FixRoute: fixRoute, FixOwner: fixOwner}
}

func Optional(reason string, evidence map[string]any) Axis {
	return Axis{State: StateNotApplicable, ReasonCode: reason, SafeEvidence: nonNil(evidence)}
}

func Unavailable(reason string) Axis {
	return Axis{State: StateUnavailable, ReasonCode: reason, SafeEvidence: map[string]any{}}
}

func Summarize(platform PlatformModel, project ProjectCaller) Summary {
	return Summary{
		Registered:                   factFromAxis(platform.Registration),
		CatalogVisible:               factFromAxis(platform.Catalog),
		Routable:                     factFromAxis(platform.BaseRoute),
		CurrentProjectCallerCallable: callableFact(platform, project),
	}
}

func factFromAxis(axis Axis) Fact {
	if axis.State == StateUnavailable {
		return Fact{State: StateUnavailable, ReasonCode: axis.ReasonCode}
	}
	if axis.State == StateNotApplicable {
		return Fact{State: StateNotApplicable, ReasonCode: axis.ReasonCode}
	}
	value := axis.State == StateReady
	state := StateBlocked
	if value {
		state = StateReady
	}
	return Fact{Value: &value, State: state, ReasonCode: axis.ReasonCode}
}

func callableFact(platform PlatformModel, project ProjectCaller) Fact {
	required := []Axis{
		platform.GatewayLicense, platform.Lifecycle, platform.PriceAuthority,
		platform.Deployment, platform.ProviderCredential, platform.Operation,
		platform.BaseRoute, platform.RuntimeSlot,
		project.Project, project.Alias, project.ModelSet, project.Wallet, project.Grant,
		project.Credential, project.ModelAuthorization, project.LimitPolicy,
	}
	if project.Application.State != StateNotApplicable {
		required = append(required, project.Application)
	}
	for _, axis := range required {
		if axis.State == StateUnavailable {
			return Fact{State: StateUnavailable, ReasonCode: "caller_evidence_unavailable"}
		}
	}
	for _, axis := range required {
		if axis.State == StateBlocked {
			value := false
			return Fact{Value: &value, State: StateBlocked, ReasonCode: axis.ReasonCode}
		}
	}
	value := true
	return Fact{Value: &value, State: StateReady, ReasonCode: "current_project_caller_ready"}
}

func nonNil(evidence map[string]any) map[string]any {
	if evidence == nil {
		return map[string]any{}
	}
	return evidence
}
