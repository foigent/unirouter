package workloadapp

import (
	"slices"
	"strings"
)

const ReadinessContractVersion = "unirouter.workload-readiness.v1"

const (
	AxisReady           = "ready"
	AxisBlocked         = "blocked"
	AxisContextRequired = "context_required"
	AxisNotApplicable   = "not_applicable"

	OverallReady      = "ready"
	OverallBlocked    = "blocked"
	OverallNeedsInput = "needs_input"
)

type Axis struct {
	Key         string   `json:"key"`
	State       string   `json:"state"`
	ReasonCode  string   `json:"reason_code"`
	Description string   `json:"description"`
	FixRoute    string   `json:"fix_route,omitempty"`
	Evidence    []string `json:"evidence"`
}

type ReadinessInput struct {
	ApplicationStatus              string
	ClientProfile                  ClientProfile
	ClientProfileOptional          bool
	ClientProfileSelectionRequired bool
	Endpoint                       string
	Model                          string
	ProjectActive                  bool
	ActiveCredentialCount          int
	CredentialAllowsModel          bool
	AuthorizationState             string
	LimitState                     string
	ModelGovernanceState           string
	RoutingCandidateCount          int
	RoutingPreviewAvailable        bool
}

type ReadinessResult struct {
	ContractVersion string `json:"contract_version"`
	Overall         string `json:"overall"`
	Endpoint        string `json:"endpoint,omitempty"`
	Model           string `json:"model,omitempty"`
	Axes            []Axis `json:"axes"`
	RuntimeMutation string `json:"runtime_mutation"`
}

// ResolveReadiness composes already-authoritative product facts. It never
// performs a mutation and does not infer a route when the model is absent.
func ResolveReadiness(input ReadinessInput) ReadinessResult {
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	input.Model = strings.TrimSpace(input.Model)
	result := ReadinessResult{
		ContractVersion: ReadinessContractVersion,
		Overall:         OverallReady,
		Endpoint:        input.Endpoint,
		Model:           input.Model,
		RuntimeMutation: "none",
		Axes:            make([]Axis, 0, 9),
	}
	result.Axes = append(result.Axes, applicationAxis(input.ApplicationStatus))
	result.Axes = append(result.Axes, projectAxis(input.ProjectActive))
	result.Axes = append(result.Axes, profileAxis(input.ClientProfile, input.Endpoint, input.ClientProfileOptional, input.ClientProfileSelectionRequired))
	result.Axes = append(result.Axes, credentialAxis(input.ActiveCredentialCount, input.Model, input.CredentialAllowsModel))
	result.Axes = append(result.Axes, stateAxis("model_authorization", input.AuthorizationState, input.Model,
		"模型调用授权允许该请求。", "模型调用授权阻止该请求。", "/agent-trust"))
	result.Axes = append(result.Axes, stateAxis("limit_policy", input.LimitState, input.Model,
		"共享限额状态可用。", "共享限额状态不可用。", "/limit-policies"))
	result.Axes = append(result.Axes, stateAxis("model_governance", input.ModelGovernanceState, input.Model,
		"模型治理允许该模型。", "模型治理阻止该模型。", "/model-governance"))
	result.Axes = append(result.Axes, routingAxis(input))

	for _, axis := range result.Axes {
		switch axis.State {
		case AxisBlocked:
			result.Overall = OverallBlocked
		case AxisContextRequired:
			if result.Overall == OverallReady {
				result.Overall = OverallNeedsInput
			}
		}
	}
	return result
}

func applicationAxis(status string) Axis {
	if status == StatusActive {
		return axis("application", AxisReady, "application_active", "应用处于启用状态。", "", "workload_applications.status")
	}
	return axis("application", AxisBlocked, "application_inactive", "应用未启用，不能发起新的模型请求。", "/applications", "workload_applications.status")
}

func projectAxis(active bool) Axis {
	if active {
		return axis("project", AxisReady, "project_active", "所属项目处于启用状态。", "", "projects.status")
	}
	return axis("project", AxisBlocked, "project_inactive", "所属项目不存在或未启用。", "/organization-members?section=projects", "projects.status")
}

func profileAxis(profile ClientProfile, endpoint string, optional, selectionRequired bool) Axis {
	if profile.ProfileID == "" {
		if optional && selectionRequired {
			return axis("client_profile", AxisContextRequired, "client_profile_selection_optional",
				"已配置多个可选客户端 Profile；需要选择上下文后才能检查兼容性。", "", "workload_application_client_profiles")
		}
		if optional {
			return axis("client_profile", AxisNotApplicable, "client_profile_not_configured_optional",
				"客户端 Profile 是可选兼容性信息，未配置不会扩大权限或阻断运行。", "", "workload_applications.identity_contract_version")
		}
		return axis("client_profile", AxisBlocked, "client_profile_unknown", "客户端 Profile 不存在。", "/docs", "client_profile.registry")
	}
	if !slices.Contains(profile.SupportedEndpoints, endpoint) {
		return axis("client_profile", AxisBlocked, "endpoint_not_supported_by_client", "客户端 Profile 不支持所选模型端点。", "/docs", profile.ProfileID)
	}
	switch profile.RuntimeState {
	case RuntimeSupported, RuntimeProtocolOnly:
		return axis("client_profile", AxisReady, profile.RuntimeState, "客户端 Profile 与当前端点兼容。", "", profile.ProfileID)
	case RuntimeContractOnly:
		return axis("client_profile", AxisBlocked, "endpoint_contract_only", "端点目前只有合同和离线证据，没有运行时实现。", "/docs", profile.ProfileID)
	default:
		return axis("client_profile", AxisBlocked, "isolated_adapter_required", "该客户端需要独立 Adapter 和目标环境验证。", "/integrations", profile.ProfileID)
	}
}

func credentialAxis(count int, model string, allowsModel bool) Axis {
	if count <= 0 {
		return axis("application_credential", AxisBlocked, "no_active_application_credential", "应用没有启用的绑定凭证。", "/applications", "api_keys.status", "api_keys.workload_application_id")
	}
	if model == "" {
		return axis("application_credential", AxisContextRequired, "model_required_for_credential_scope", "选择模型后才能验证凭证的模型范围。", "", "api_keys.allowed_models")
	}
	if !allowsModel {
		return axis("application_credential", AxisBlocked, "credential_model_not_allowed", "绑定凭证不允许所选模型。", "/applications", "api_keys.allowed_models")
	}
	return axis("application_credential", AxisReady, "active_credential_allows_model", "至少一个启用凭证允许所选模型。", "", "api_keys.status", "api_keys.allowed_models")
}

func stateAxis(key, state, model, readyDescription, blockedDescription, fixRoute string) Axis {
	if model == "" {
		return axis(key, AxisContextRequired, "model_required", "选择模型后才能计算该控制轴。", "", key)
	}
	switch state {
	case "ready", "legacy_authority", "builtin":
		return axis(key, AxisReady, state, readyDescription, "", key)
	case "not_applicable":
		return axis(key, AxisNotApplicable, state, readyDescription, "", key)
	default:
		if state == "" {
			state = key + "_unavailable"
		}
		return axis(key, AxisBlocked, state, blockedDescription, fixRoute, key)
	}
}

func routingAxis(input ReadinessInput) Axis {
	if input.Model == "" {
		return axis("upstream_route", AxisContextRequired, "model_required_for_routing", "选择模型后才能解析上游通道和供应商凭证。", "", "routing-policy.v1")
	}
	if !input.RoutingPreviewAvailable {
		return axis("upstream_route", AxisBlocked, "routing_preview_unavailable", "上游路由状态暂时不可用，系统按 fail-closed 处理。", "/test", "routing-policy.v1")
	}
	if input.RoutingCandidateCount <= 0 {
		return axis("upstream_route", AxisBlocked, "no_eligible_upstream_route", "没有同时满足通道、熔断、模型和供应商凭证条件的活跃路由。", "/test", "routing-policy.v1")
	}
	return axis("upstream_route", AxisReady, "eligible_upstream_route", "存在可用上游路由和供应商凭证。", "", "routing-policy.v1")
}

func axis(key, state, reason, description, fixRoute string, evidence ...string) Axis {
	return Axis{Key: key, State: state, ReasonCode: reason, Description: description, FixRoute: fixRoute, Evidence: evidence}
}
