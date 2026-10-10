package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelreadiness"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

const (
	maxReadinessModelLength          = 160
	maxReadinessApplicationRefLength = 64
)

func (h *Handler) GetModelReadiness(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = modelreadiness.ModeCaller
	}
	requestedModel := strings.TrimSpace(r.URL.Query().Get("model"))
	rawEndpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	endpoint := accessscope.CanonicalExecutionEndpoint(rawEndpoint)
	operation, operationKnown := modeloperation.Lookup(endpoint)
	caller := strings.TrimSpace(r.URL.Query().Get("caller"))
	applicationRef := strings.TrimSpace(r.URL.Query().Get("application_ref"))
	if requestedModel == "" || len(requestedModel) > maxReadinessModelLength ||
		strings.ContainsAny(requestedModel, "\x00\r\n") || endpoint != rawEndpoint || !accessscope.IsSupportedEndpoint(endpoint) || !operationKnown ||
		!slices.Contains([]string{modelreadiness.ModeCaller, modelreadiness.ModeConnection}, mode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid model readiness request", "reason": "invalid_request"})
		return
	}
	if mode == modelreadiness.ModeConnection {
		if strings.TrimSpace(r.URL.Query().Get("project_id")) != "" || caller != "" || applicationRef != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid model readiness request", "reason": "invalid_request"})
			return
		}
		if !auth.IsOrgAdmin(actorOrganizationRole(r.Context())) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Connection readiness requires organization operations access", "reason": "forbidden"})
			return
		}
		deploymentID, deploymentErr := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("deployment_id")), 10, 64)
		if deploymentErr != nil || deploymentID <= 0 || strings.TrimSpace(r.URL.Query().Get("channel_id")) != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid model readiness request", "reason": "invalid_request"})
			return
		}
		orgID, resolveErr := resolveActorOrganization(r.Context(), 0)
		if resolveErr != nil || orgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Customer organization unavailable", "reason": "customer_context_unavailable"})
			return
		}
		response := modelreadiness.Response{
			ContractVersion: modelreadiness.ContractVersion,
			Scope: modelreadiness.Scope{Mode: mode, OrgID: orgID, DeploymentID: deploymentID, RequestedModel: requestedModel,
				ResolvedModel: requestedModel, Endpoint: endpoint, ModelOperationID: string(operation.ID)},
			RuntimeMutation: "none", ExternalRequests: "none", AsOf: time.Now().UTC(),
		}
		connectionOnly := modelreadiness.Optional("connection_mode_not_applicable", nil)
		response.ProjectCaller = modelreadiness.ProjectCaller{
			Project: connectionOnly, Alias: connectionOnly, ModelSet: connectionOnly,
			Wallet: connectionOnly, ProjectBudget: connectionOnly, Application: connectionOnly,
			Grant: connectionOnly, Credential: connectionOnly,
			ModelAuthorization: connectionOnly, LimitPolicy: connectionOnly,
		}
		applications, projectionErr := h.loadReadinessCanonicalApplications(r.Context(), orgID, deploymentID, response.Scope.ResolvedModel, response.Scope.ModelOperationID)
		if projectionErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Model readiness unavailable", "reason": "canonical_projection_unavailable"})
			return
		}
		billingState, billingErr := h.inspectBillingReadiness(r.Context(), orgID, response.Scope.ResolvedModel)
		h.readinessPlatformAxes(r, orgID, &response, applications, billingState, billingErr)
		response.Summary = modelreadiness.Summarize(response.PlatformModel, response.ProjectCaller)
		response.Summary.CurrentProjectCallerCallable = modelreadiness.Fact{
			State: modelreadiness.StateNotApplicable, ReasonCode: "connection_mode_has_no_caller",
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	projectID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("project_id")), 10, 64)
	if err != nil || projectID <= 0 || !slices.Contains([]string{"self", "workload_application"}, caller) ||
		(caller == "self" && applicationRef != "") ||
		(caller == "workload_application" && (applicationRef == "" || len(applicationRef) > maxReadinessApplicationRefLength ||
			strings.ContainsAny(applicationRef, "\x00\r\n"))) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid model readiness request", "reason": "invalid_request"})
		return
	}
	if !h.canReadProject(r.Context(), projectID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found", "reason": "not_found"})
		return
	}

	project, err := modelreadiness.ReadProject(r.Context(), h.db, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found", "reason": "not_found"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Model readiness unavailable", "reason": "service_unavailable"})
		return
	}
	orgID := project.OrgID
	if actorOrganizationRole(r.Context()) != auth.RolePlatformAdmin && orgID != middleware.GetOrgIDFromContext(r.Context()) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found", "reason": "not_found"})
		return
	}

	response := modelreadiness.Response{
		ContractVersion: modelreadiness.ContractVersion,
		Scope: modelreadiness.Scope{Mode: mode, OrgID: orgID, ProjectID: projectID, RequestedModel: requestedModel,
			ResolvedModel: requestedModel, Endpoint: endpoint, ModelOperationID: string(operation.ID), Caller: caller, ApplicationRef: applicationRef},
		RuntimeMutation: "none", ExternalRequests: "none", AsOf: time.Now().UTC(),
	}
	projectRoute := "/organization-members?section=projects&project_id=" + strconv.FormatInt(projectID, 10)
	response.ProjectCaller.Project = readinessProjectAxis(project.Status, h.readinessFixRoute(r, projectRoute, h.canWriteProject(r.Context(), projectID)))
	response.ProjectCaller.ProjectBudget = readinessProjectBudgetAxis(h.effectiveBillingMode(), project.BudgetConfigured, project.BudgetPeriod,
		h.readinessFixRoute(r, projectRoute+"&tab=budget", h.canWriteProject(r.Context(), projectID)))

	governance := modelreadiness.InspectGovernance(r.Context(), h.db, orgID, projectID, requestedModel)
	response.Scope.ResolvedModel = governance.Access.ResolvedModel
	if response.Scope.ResolvedModel == "" {
		response.Scope.ResolvedModel = requestedModel
	}
	response.ProjectCaller.Alias, response.ProjectCaller.ModelSet = h.readinessGovernanceAxes(r, orgID, projectID, governance)
	applications, projectionErr := h.loadReadinessCanonicalApplications(r.Context(), orgID, 0, response.Scope.ResolvedModel, response.Scope.ModelOperationID)
	if projectionErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Model readiness unavailable", "reason": "canonical_projection_unavailable"})
		return
	}
	billingState, billingErr := h.inspectBillingReadiness(r.Context(), orgID, response.Scope.ResolvedModel)
	h.readinessPlatformAxes(r, orgID, &response, applications, billingState, billingErr)
	h.readinessWalletAxis(&response, billingState, billingErr)
	h.readinessCallerAxes(r, orgID, projectID, caller, applicationRef, &response)
	response.Summary = modelreadiness.Summarize(response.PlatformModel, response.ProjectCaller)
	writeJSON(w, http.StatusOK, response)
}

func readinessProjectAxis(status, fixRoute string) modelreadiness.Axis {
	if status == "active" {
		return modelreadiness.Ready("project_active", modelreadiness.Evidence("status", status))
	}
	return modelreadiness.Blocked("project_inactive", modelreadiness.Evidence("status", status), fixRoute, "project_admin")
}

func readinessProjectBudgetAxis(mode billing.BillingMode, configured bool, period, fixRoute string) modelreadiness.Axis {
	if mode == billing.BillingModeCostObservation {
		evidence := modelreadiness.Evidence(
			"configured", configured,
			"billing_mode", string(mode),
			"project_budget_admission_required", false,
			"project_budget_enforced", false,
			"calculated_cost_budget_alerts_available", false,
		)
		if configured {
			evidence["period"] = period
			return modelreadiness.Warning("project_budget_configured_only_not_enforced_under_cost_observation", evidence, fixRoute, "project_admin")
		}
		return modelreadiness.Optional("project_budget_not_configured_or_enforced_under_cost_observation", evidence)
	}
	evidence := modelreadiness.Evidence(
		"configured", configured,
		"billing_mode", string(mode),
		"project_budget_admission_required", false,
	)
	if !configured {
		return modelreadiness.Optional("project_budget_not_configured_monitoring_only", evidence)
	}
	evidence["period"] = period
	return modelreadiness.Warning("project_budget_monitoring_only", evidence, fixRoute, "project_admin")
}

func (h *Handler) readinessGovernanceAxes(r *http.Request, orgID, projectID int64, snapshot modelreadiness.GovernanceSnapshot) (modelreadiness.Axis, modelreadiness.Axis) {
	aliasRoute := h.readinessFixRoute(r, "/model-governance?tab=aliases&org_id="+strconv.FormatInt(orgID, 10),
		actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin)
	aliasAxis := modelreadiness.Optional("direct_model_optional", modelreadiness.Evidence("alias_applied", false))
	if snapshot.Access.Alias != "" {
		aliasAxis = modelreadiness.Ready("active_alias_resolved", modelreadiness.Evidence("alias_applied", true))
	} else if snapshot.AliasConfigured {
		aliasAxis = modelreadiness.Blocked("alias_inactive", modelreadiness.Evidence("alias_applied", false, "status", snapshot.AliasStatus), aliasRoute, "organization_admin")
	} else if snapshot.AliasEvidenceError != nil {
		aliasAxis = modelreadiness.Unavailable("alias_evidence_unavailable")
	}
	if snapshot.ModelSetEvidenceError != nil {
		return aliasAxis, modelreadiness.Unavailable("model_set_evidence_unavailable")
	}
	if snapshot.AccessError != nil && !errors.Is(snapshot.AccessError, modelgov.ErrModelDenied) && !errors.Is(snapshot.AccessError, modelgov.ErrModelRetired) {
		return modelreadiness.Unavailable("alias_evidence_unavailable"), modelreadiness.Unavailable("model_set_evidence_unavailable")
	}
	if snapshot.AssignedModelSetCount == 0 {
		return aliasAxis, modelreadiness.Optional("unrestricted_optional", modelreadiness.Evidence("assigned_model_set_count", 0))
	}
	if errors.Is(snapshot.AccessError, modelgov.ErrModelDenied) {
		return aliasAxis, modelreadiness.Blocked("project_model_set_denied", modelreadiness.Evidence("assigned_model_set_count", snapshot.AssignedModelSetCount),
			h.readinessFixRoute(r, "/model-governance?tab=model-sets&project_id="+strconv.FormatInt(projectID, 10),
				actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin), "organization_admin")
	}
	return aliasAxis, modelreadiness.Ready("project_model_set_allowed", modelreadiness.Evidence("assigned_model_set_count", snapshot.AssignedModelSetCount))
}

func (h *Handler) readinessPlatformAxes(
	r *http.Request,
	orgID int64,
	response *modelreadiness.Response,
	applications []canonicalModelApplication,
	billingState billing.AdmissionReadiness,
	billingErr error,
) {
	model := response.Scope.ResolvedModel
	response.PlatformModel.GatewayLicense = modelreadiness.Optional("commercial_license_not_required", nil)
	registry, err := modelreadiness.ReadRegistry(r.Context(), h.db, orgID, model)
	registryFix := h.readinessFixRoute(r, "/model-governance?tab=registry&model="+url.QueryEscape(model),
		actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin)
	switch {
	case err != nil:
		response.PlatformModel.Registration = modelreadiness.Unavailable("model_registry_unavailable")
		response.PlatformModel.Lifecycle = modelreadiness.Unavailable("model_lifecycle_unavailable")
		response.PlatformModel.Catalog = modelreadiness.Unavailable("model_catalog_unavailable")
	case !registry.Exists:
		response.PlatformModel.Registration = modelreadiness.Blocked("model_unregistered", modelreadiness.Evidence("registered", false), registryFix, "organization_admin")
		response.PlatformModel.Catalog = modelreadiness.Blocked("catalog_not_published", modelreadiness.Evidence("visible", false), registryFix, "organization_admin")
		response.PlatformModel.Lifecycle = modelreadiness.Optional("unregistered_lifecycle_not_applicable", nil)
	default:
		response.PlatformModel.Registration = modelreadiness.Ready("model_registered", modelreadiness.Evidence("registered", true))
		if registry.Lifecycle == modelgov.LifecycleRetired {
			response.PlatformModel.Lifecycle = modelreadiness.Blocked("model_retired", modelreadiness.Evidence("lifecycle", registry.Lifecycle), registryFix, "organization_admin")
		} else {
			response.PlatformModel.Lifecycle = modelreadiness.Ready("model_callable_lifecycle", modelreadiness.Evidence("lifecycle", registry.Lifecycle))
		}
		if registry.ApprovedCatalog && modelgov.IsCatalogLifecycle(registry.Lifecycle) {
			response.PlatformModel.Catalog = modelreadiness.Ready("catalog_published", modelreadiness.Evidence("visible", true))

		} else {
			response.PlatformModel.Catalog = modelreadiness.Blocked("catalog_not_published", modelreadiness.Evidence("visible", false), registryFix, "organization_admin")
		}
	}

	response.PlatformModel.PriceAuthority = h.readinessPriceAuthorityAxis(r, model, billingState, billingErr)
	h.readinessCanonicalRouteAxes(r, response, applications)
}

func (h *Handler) loadReadinessCanonicalApplications(
	ctx context.Context,
	orgID, deploymentID int64,
	model, operationID string,
) ([]canonicalModelApplication, error) {
	projection, err := h.loadCanonicalModelApplications(ctx, orgID, deploymentID, operationID)
	if errors.Is(err, errCanonicalApplicationNotFound) {
		return []canonicalModelApplication{}, nil
	}
	if err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	applications := make([]canonicalModelApplication, 0, len(projection.Applications))
	for _, application := range projection.Applications {
		if application.PublicModel.ModelName == model && (deploymentID == 0 || application.Deployment.ID == deploymentID) {
			applications = append(applications, application)
		}
	}
	return applications, nil
}

func (h *Handler) readinessCanonicalRouteAxes(
	r *http.Request,
	response *modelreadiness.Response,
	applications []canonicalModelApplication,
) {
	fixTarget := "/model-endpoints?model=" + url.QueryEscape(response.Scope.ResolvedModel) +
		"&operation_id=" + url.QueryEscape(response.Scope.ModelOperationID)
	if response.Scope.DeploymentID > 0 {
		fixTarget += "&deployment_id=" + strconv.FormatInt(response.Scope.DeploymentID, 10)
	}
	fixRoute := h.readinessFixRoute(r, fixTarget, auth.IsOrgAdmin(actorOrganizationRole(r.Context())))

	if len(applications) == 0 {
		missingEvidence := modelreadiness.Evidence(
			"deployment_id", response.Scope.DeploymentID,
			"model_operation_id", response.Scope.ModelOperationID,
			"deployment_count", 0,
		)
		response.PlatformModel.Deployment = modelreadiness.Blocked("canonical_deployment_missing", missingEvidence, fixRoute, "organization_admin")
		response.PlatformModel.ProviderCredential = modelreadiness.Blocked("canonical_provider_credential_unavailable", missingEvidence, fixRoute, "organization_admin")
		response.PlatformModel.Operation = modelreadiness.Blocked("canonical_model_operation_missing", missingEvidence, fixRoute, "organization_admin")
		response.PlatformModel.RuntimeSlot = modelreadiness.Blocked("canonical_runtime_membership_missing", missingEvidence, fixRoute, "organization_admin")
		response.PlatformModel.BaseRoute = modelreadiness.Blocked("canonical_route_unavailable", missingEvidence, fixRoute, "organization_admin")
		return
	}

	eligibleCredentials := 0
	operationCount := 0
	operationReadyCount := 0
	runtimeMembershipCount := 0
	connectionReadyCount := 0
	for _, application := range applications {
		eligibleCredentials += application.Credential.EligibleCount
		if application.Connection.AdminState == "enabled" && application.Deployment.AdminState == "enabled" &&
			application.Deployment.RetiredAt == nil && application.Connection.RetiredAt == nil {
			connectionReadyCount++
		}
		for _, operation := range application.Operations {
			if operation.OperationID != response.Scope.ModelOperationID {
				continue
			}
			operationCount++
			if canonicalReadinessOperationVerified(operation) {
				operationReadyCount++
			}
			if operation.Runtime.State == "stable" || operation.Runtime.State == "canary" {
				runtimeMembershipCount++
			}
		}
	}
	response.PlatformModel.Deployment = modelreadiness.Ready("canonical_deployment_present", modelreadiness.Evidence(
		"deployment_id", response.Scope.DeploymentID,
		"deployment_count", len(applications),
		"enabled_count", connectionReadyCount,
	))
	if eligibleCredentials > 0 {
		response.PlatformModel.ProviderCredential = modelreadiness.Ready("canonical_provider_credential_available", modelreadiness.Evidence(
			"deployment_id", response.Scope.DeploymentID,
			"eligible_credential_count", eligibleCredentials,
		))
	} else {
		response.PlatformModel.ProviderCredential = modelreadiness.Blocked("canonical_provider_credential_unavailable", modelreadiness.Evidence(
			"deployment_id", response.Scope.DeploymentID,
			"eligible_credential_count", 0,
		), fixRoute, "organization_admin")
	}
	if operationCount == 0 {
		response.PlatformModel.Operation = modelreadiness.Blocked("canonical_model_operation_missing", modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
		), fixRoute, "organization_admin")
	} else if operationReadyCount == 0 {
		response.PlatformModel.Operation = modelreadiness.Blocked("canonical_model_operation_unverified", modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
			"operation_count", operationCount,
			"verified_count", 0,
		), fixRoute, "organization_admin")
	} else {
		response.PlatformModel.Operation = modelreadiness.Ready("canonical_model_operation_verified", modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
			"operation_count", operationCount,
			"verified_count", operationReadyCount,
		))
	}
	if runtimeMembershipCount == 0 {
		response.PlatformModel.RuntimeSlot = modelreadiness.Blocked("canonical_runtime_membership_missing", modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
			"membership_count", 0,
		), fixRoute, "organization_admin")
	} else {
		response.PlatformModel.RuntimeSlot = modelreadiness.Ready("canonical_runtime_membership_present", modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
			"membership_count", runtimeMembershipCount,
		))
	}

	if response.Scope.Mode == modelreadiness.ModeConnection {
		callable := connectionReadyCount > 0 && eligibleCredentials > 0 && operationReadyCount > 0 && runtimeMembershipCount > 0
		if callable {
			response.PlatformModel.BaseRoute = modelreadiness.Ready("canonical_deployment_operation_ready", modelreadiness.Evidence(
				"deployment_id", response.Scope.DeploymentID,
				"model_operation_id", response.Scope.ModelOperationID,
			))
		} else {
			response.PlatformModel.BaseRoute = modelreadiness.Blocked("canonical_deployment_operation_unavailable", modelreadiness.Evidence(
				"deployment_id", response.Scope.DeploymentID,
				"model_operation_id", response.Scope.ModelOperationID,
			), fixRoute, "organization_admin")
		}
		return
	}

	if h == nil || h.modelRoutePlanner == nil {
		response.PlatformModel.BaseRoute = modelreadiness.Unavailable("canonical_route_planner_unavailable")
		return
	}
	plan, err := h.modelRoutePlanner.Plan(r.Context(), modelrouting.Request{
		Mode:             modelrouting.ModeCanonical,
		CustomerOrgID:    response.Scope.OrgID,
		PublicModel:      response.Scope.ResolvedModel,
		ModelOperationID: response.Scope.ModelOperationID,
		SelectionKey: "admin-readiness:" + strconv.FormatInt(response.Scope.OrgID, 10) + ":" +
			response.Scope.ResolvedModel + ":" + response.Scope.ModelOperationID,
	})
	if err != nil {
		if errors.Is(err, modelrouting.ErrUnavailable) {
			response.PlatformModel.BaseRoute = modelreadiness.Unavailable("canonical_route_snapshot_unavailable")
			return
		}
		response.PlatformModel.BaseRoute = modelreadiness.Blocked(canonicalReadinessRouteReason(err), modelreadiness.Evidence(
			"model_operation_id", response.Scope.ModelOperationID,
			"exclusion_count", len(plan.Exclusions),
		), fixRoute, "organization_admin")
		return
	}
	response.PlatformModel.BaseRoute = modelreadiness.Ready("canonical_route_plan_available", modelreadiness.Evidence(
		"authority_mode", string(plan.AuthorityMode),
		"model_operation_id", plan.ModelOperationID,
		"route_count", len(plan.Routes),
		"runtime_release_ref", plan.RuntimeReleaseRef,
		"runtime_stage", plan.RuntimeStage,
		"slot_version", plan.SlotVersion,
	))
}

func canonicalReadinessOperationVerified(operation canonicalOperationAxis) bool {
	if operation.SupportState != "enabled" || operation.VerifiedAt == nil || operation.ProbeState != "ready" {
		return false
	}
	switch modeloperation.ID(operation.OperationID) {
	case modeloperation.IDResponses, modeloperation.IDMessages:
		return operation.EvidenceState == "production_approved" &&
			operation.ProductionQualification.Qualified &&
			operation.ProductionQualification.State == "production_qualified"
	default:
		return slices.Contains([]string{"live_probed", "conformance_verified", "production_approved"}, operation.EvidenceState)
	}
}

func canonicalReadinessRouteReason(err error) string {
	switch {
	case errors.Is(err, modelrouting.ErrManifestStale):
		return "canonical_runtime_manifest_stale"
	case errors.Is(err, modelrouting.ErrManifestUnsealed):
		return "canonical_runtime_manifest_unsealed"
	case errors.Is(err, modelrouting.ErrAuthorityNotReady):
		return "canonical_route_authority_not_ready"
	case errors.Is(err, modelrouting.ErrNoRoute):
		return "canonical_route_unavailable"
	default:
		return "canonical_route_plan_rejected"
	}
}

func (h *Handler) inspectBillingReadiness(ctx context.Context, orgID int64, model string) (billing.AdmissionReadiness, error) {
	if h == nil || h.billingReadiness == nil {
		return billing.AdmissionReadiness{}, errors.New("billing readiness unavailable")
	}
	return h.billingReadiness.InspectAdmissionReadiness(ctx, orgID, model)
}

func (h *Handler) readinessPriceAuthorityAxis(r *http.Request, model string, state billing.AdmissionReadiness, err error) modelreadiness.Axis {
	if err != nil {
		return modelreadiness.Unavailable("request_pricing_authority_unavailable")
	}
	mode := string(state.PricingAuthority)
	if state.PriceConfigured {
		evidence := modelreadiness.Evidence(
			"configured", true,
			"request_pricing_mode", mode,
			"pricing_version", state.PricingVersion,
		)
		if state.PricingAuthority == billing.RequestPricingAuthorityRateCard {
			evidence["selection_key"] = state.SelectionKey
			evidence["rate_card_ref"] = state.RateCardRef
			evidence["rate_card_version"] = state.RateCardVersion
			evidence["layer"] = state.RateCardLayer
			evidence["provider_ref"] = state.ProviderRef
			return modelreadiness.Ready("rate_card_authority_resolved", evidence)
		}
		return modelreadiness.Ready("model_price_configured", evidence)
	}
	if state.PricingAuthority == billing.RequestPricingAuthorityRateCard {
		return modelreadiness.Blocked("rate_card_authority_not_ready", modelreadiness.Evidence(
			"configured", false, "request_pricing_mode", mode, "selection_key", state.SelectionKey,
		), h.readinessFixRoute(r, "/enterprise-finance?section=rate-cards&model="+url.QueryEscape(model),
			actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleBillingAdmin), "billing_admin")
	}
	if state.BillingMode == billing.BillingModeCostObservation {
		return modelreadiness.Optional("model_price_unconfigured_cost_observation", modelreadiness.Evidence(
			"configured", false, "request_pricing_mode", mode,
			"billing_mode", string(state.BillingMode), "traffic_admission_required", false,
			"calculated_cost_available", false, "pricing_version", state.PricingVersion,
		))
	}
	return modelreadiness.Blocked("model_price_missing", modelreadiness.Evidence(
		"configured", false, "request_pricing_mode", mode,
	), h.readinessFixRoute(r, "/model-prices?model="+url.QueryEscape(model), actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin), "platform_admin")
}

func (h *Handler) readinessWalletAxis(response *modelreadiness.Response, state billing.AdmissionReadiness, err error) {
	if err != nil {
		response.ProjectCaller.Wallet = modelreadiness.Unavailable("wallet_unavailable")
		return
	}
	mode := state.BillingMode
	if !state.WalletAdmissionRequired {
		response.ProjectCaller.Wallet = modelreadiness.Optional(
			"wallet_not_used_for_cost_observation_admission",
			modelreadiness.Evidence(
				"billing_mode", string(mode),
				"wallet_admission_required", false,
			),
		)
		return
	}
	evidence := func(values ...any) map[string]any {
		result := modelreadiness.Evidence(
			"billing_mode", string(mode),
			"wallet_admission_required", true,
		)
		for i := 0; i+1 < len(values); i += 2 {
			if key, ok := values[i].(string); ok {
				result[key] = values[i+1]
			}
		}
		return result
	}
	if !state.WalletExists {
		response.ProjectCaller.Wallet = modelreadiness.Blocked("wallet_missing", evidence("exists", false), "", "billing_admin")
	} else if state.SpendableBalanceMicros > 0 {
		response.ProjectCaller.Wallet = modelreadiness.Ready("wallet_has_spendable_balance", evidence(
			"exists", true, "has_spendable_balance", true, "spendable_balance_micros", state.SpendableBalanceMicros))
	} else {
		response.ProjectCaller.Wallet = modelreadiness.Blocked("wallet_no_spendable_balance", evidence(
			"exists", true, "has_spendable_balance", false, "spendable_balance_micros", state.SpendableBalanceMicros), "", "billing_admin")
	}
}

func (h *Handler) readinessCallerAxes(r *http.Request, orgID, projectID int64, caller, applicationRef string, response *modelreadiness.Response) {
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	accessFixRoute := "/access?project_id=" + strconv.FormatInt(projectID, 10)
	accessFixAllowed := caller == "self"
	state := modelreadiness.InspectCaller(r.Context(), h.db, modelreadiness.CallerInput{
		OrgID: orgID, ProjectID: projectID, Caller: caller, ActorUserID: actorUserID,
		ApplicationRef: applicationRef, RequestedModel: response.Scope.RequestedModel,
		ResolvedModel: response.Scope.ResolvedModel, Endpoint: response.Scope.Endpoint,
	})
	if caller == "self" {
		response.ProjectCaller.Application = modelreadiness.Optional("self_application_not_applicable", nil)
	} else {
		accessFixRoute = "/applications?project_id=" + strconv.FormatInt(projectID, 10) + "&application_ref=" + url.QueryEscape(applicationRef)
		accessFixAllowed = h.canWriteProject(r.Context(), projectID)
		if state.Workload.EvidenceError != nil {
			response.ProjectCaller.Application = modelreadiness.Unavailable("workload_application_unavailable")
		} else if !state.Workload.Exists {
			response.ProjectCaller.Application = modelreadiness.Blocked("workload_application_not_found", modelreadiness.Evidence("present", false),
				h.readinessFixRoute(r, "/applications?project_id="+strconv.FormatInt(projectID, 10), h.canWriteProject(r.Context(), projectID)), "project_admin")
		} else if state.Workload.Status != "active" || state.Workload.IdentityContractVersion != "unirouter.workload-identity.v2" {
			response.ProjectCaller.Application = modelreadiness.Blocked("workload_application_inactive", modelreadiness.Evidence("present", true, "status", state.Workload.Status, "identity_v2", state.Workload.IdentityContractVersion == "unirouter.workload-identity.v2"),
				h.readinessFixRoute(r, "/applications?project_id="+strconv.FormatInt(projectID, 10)+"&application_ref="+url.QueryEscape(applicationRef), h.canWriteProject(r.Context(), projectID)), "project_admin")
		} else {
			response.ProjectCaller.Application = modelreadiness.Ready("workload_application_active_v2", modelreadiness.Evidence("present", true, "environment", state.Workload.Environment, "production_use", state.Workload.ProductionUse))
		}
	}

	grant := state.Grant
	if state.GrantEvidenceError != nil {
		response.ProjectCaller.Grant = modelreadiness.Unavailable("access_grant_unavailable")
		response.ProjectCaller.Credential = modelreadiness.Unavailable("access_credential_unavailable")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if grant.ID == 0 {
		response.ProjectCaller.Grant = modelreadiness.Blocked("access_grant_missing", modelreadiness.Evidence("present", false),
			h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "project_admin")
		response.ProjectCaller.Credential = modelreadiness.Blocked("access_credential_missing", modelreadiness.Evidence("present", false),
			h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	now := time.Now().UTC()
	grantScopeAllowed := grant.AllowsExecution(response.Scope.RequestedModel, response.Scope.ResolvedModel, response.Scope.Endpoint)
	if grant.Status != "active" {
		response.ProjectCaller.Grant = modelreadiness.Blocked("access_grant_"+grant.Status, modelreadiness.Evidence("status", grant.Status), "", "project_admin")
	} else if grant.ValidFrom.After(now) {
		response.ProjectCaller.Grant = modelreadiness.Blocked("access_grant_not_yet_valid", modelreadiness.Evidence("status", grant.Status), "", "project_admin")
	} else if grant.ValidUntil != nil && !grant.ValidUntil.After(now) {
		response.ProjectCaller.Grant = modelreadiness.Blocked("access_grant_expired", modelreadiness.Evidence("status", grant.Status), "", "project_admin")
	} else if !grantScopeAllowed {
		response.ProjectCaller.Grant = modelreadiness.Blocked("access_grant_scope_excludes_request", modelreadiness.Evidence("scope_mode", grant.ScopeMode),
			h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "project_admin")
	} else {
		response.ProjectCaller.Grant = modelreadiness.Ready("access_grant_active", modelreadiness.Evidence("status", grant.Status, "scope_mode", grant.ScopeMode))
	}
	if state.CredentialError != nil {
		response.ProjectCaller.Credential = modelreadiness.Unavailable("runtime_caller_boundary_unavailable")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if !state.CredentialCandidates {
		response.ProjectCaller.Credential = modelreadiness.Blocked("access_credential_missing", modelreadiness.Evidence("present", false),
			h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if !state.LiveBoundary {
		response.ProjectCaller.Credential = modelreadiness.Blocked("runtime_caller_boundary_inactive", modelreadiness.Evidence("present", true), "", "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if len(state.Credentials) == 0 {
		response.ProjectCaller.Credential = modelreadiness.Blocked("access_credential_scope_excludes_request",
			modelreadiness.Evidence("present", true), h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	snapshot := state.Credentials[0]
	response.ProjectCaller.Credential = modelreadiness.Ready("runtime_caller_boundary_active", modelreadiness.Evidence("present", true, "grant_revision", snapshot.GrantRevision))
	h.readinessPolicyAxes(r, response, state.Credentials, actorUserID)
}

func (h *Handler) readinessPolicyAxes(r *http.Request, response *modelreadiness.Response, snapshots []accessgrantboundary.Snapshot, actorUserID int64) {
	if len(snapshots) == 0 {
		snapshots = []accessgrantboundary.Snapshot{{}}
	}
	allowedSnapshots := make([]accessgrantboundary.Snapshot, 0, len(snapshots))
	deniedReason := "model_authorization_denied"
	deniedPolicyCount := 0
	if h.modelAuthorizationEvaluator == nil {
		response.ProjectCaller.ModelAuthorization = modelreadiness.Unavailable("model_authorization_unavailable")
	} else {
		inputs := make([]modelauthz.Input, len(snapshots))
		for index, snapshot := range snapshots {
			inputs[index] = readinessModelAuthorizationInput(response, snapshot, actorUserID)
		}
		decisions, err := h.modelAuthorizationEvaluator.EvaluateAlternatives(r.Context(), inputs)
		if err != nil {
			response.ProjectCaller.ModelAuthorization = modelreadiness.Unavailable("model_authorization_unavailable")
		} else {
			for index, decision := range decisions {
				if decision.Allowed {
					allowedSnapshots = append(allowedSnapshots, snapshots[index])
					if len(allowedSnapshots) == 1 {
						response.ProjectCaller.ModelAuthorization = modelreadiness.Ready(decision.Reason,
							modelreadiness.Evidence("allowed", true, "matched_policy_count", len(decision.PolicyRefs)))
					}
					continue
				}
				deniedReason = decision.Reason
				deniedPolicyCount = len(decision.PolicyRefs)
			}
			if len(allowedSnapshots) == 0 {
				response.ProjectCaller.ModelAuthorization = modelreadiness.Blocked(deniedReason,
					modelreadiness.Evidence("allowed", false, "matched_policy_count", deniedPolicyCount), "", "organization_admin")
			}
		}
	}

	limitSnapshots := snapshots
	if len(allowedSnapshots) > 0 {
		limitSnapshots = allowedSnapshots
	}
	provider, ok := h.limitPolicyReloader.(interface {
		Readiness(context.Context, limitpolicy.Subject) (limitpolicy.Readiness, error)
	})
	if !ok {
		response.ProjectCaller.LimitPolicy = modelreadiness.Unavailable("limit_policy_unavailable")
		return
	}
	var blocked *limitpolicy.Readiness
	unavailableReason := ""
	for _, snapshot := range limitSnapshots {
		subject := readinessLimitPolicySubject(response, snapshot, actorUserID)
		projection, err := provider.Readiness(r.Context(), subject)
		if err != nil {
			unavailableReason = "limit_policy_unavailable"
			continue
		}
		switch projection.State {
		case limitpolicy.ReadinessReady:
			response.ProjectCaller.LimitPolicy = modelreadiness.Ready("limit_policy_evaluated", modelreadiness.Evidence(
				"matching_policy_count", projection.MatchingPolicyCount, "enforced_policy_count", projection.EnforcedPolicyCount))
			return
		case limitpolicy.ReadinessBlocked:
			if blocked == nil {
				copy := projection
				blocked = &copy
			}
		case limitpolicy.ReadinessUnavailable:
			unavailableReason = projection.Reason
		default:
			unavailableReason = "limit_policy_unavailable"
		}
	}
	if unavailableReason != "" {
		response.ProjectCaller.LimitPolicy = modelreadiness.Unavailable(unavailableReason)
		return
	}
	if blocked != nil {
		response.ProjectCaller.LimitPolicy = modelreadiness.Blocked(blocked.Reason, modelreadiness.Evidence(
			"matching_policy_count", blocked.MatchingPolicyCount, "enforced_policy_count", blocked.EnforcedPolicyCount), "", "project_admin")
		return
	}
	response.ProjectCaller.LimitPolicy = modelreadiness.Unavailable("limit_policy_unavailable")
}

func readinessModelAuthorizationInput(response *modelreadiness.Response, snapshot accessgrantboundary.Snapshot, actorUserID int64) modelauthz.Input {
	input := modelauthz.Input{OrgID: response.Scope.OrgID, ProjectID: response.Scope.ProjectID,
		Endpoint: response.Scope.Endpoint, RequestedModel: response.Scope.RequestedModel, ResolvedModel: response.Scope.ResolvedModel}
	if response.Scope.Caller == "self" {
		input.OwnerUserID = actorUserID
	}
	if snapshot.APIKey.ID > 0 {
		input.APIKeyID = snapshot.APIKey.ID
		if snapshot.APIKey.AccessCredentialID != nil {
			input.AccessCredentialID = *snapshot.APIKey.AccessCredentialID
		}
		if snapshot.APIKey.OwnerUserID != nil {
			input.OwnerUserID = *snapshot.APIKey.OwnerUserID
		}
	}
	return input
}

func readinessLimitPolicySubject(response *modelreadiness.Response, snapshot accessgrantboundary.Snapshot, actorUserID int64) limitpolicy.Subject {
	input := readinessModelAuthorizationInput(response, snapshot, actorUserID)
	subject := limitpolicy.Subject{OrgID: response.Scope.OrgID, ProjectID: response.Scope.ProjectID,
		APIKeyID: input.APIKeyID, AccessCredentialID: input.AccessCredentialID, Model: response.Scope.ResolvedModel}
	if snapshot.APIKey.AccessGrantID != nil {
		subject.AccessGrantID = *snapshot.APIKey.AccessGrantID
	}
	if snapshot.APIKey.AccessCredentialID != nil {
		subject.CredentialRef = snapshot.APIKey.CredentialRef
	}
	subject.BaseRPMLimit = int64(snapshot.APIKey.RPMLimit)
	subject.BaseRPSLimit = int64(snapshot.APIKey.QPSLimit)
	subject.CredentialConcurrencyLimit = readinessInt64Pointer(snapshot.APIKey.CredentialConcurrencyLimit)
	subject.GrantRPMLimit = readinessInt64Pointer(snapshot.APIKey.GrantRPMLimit)
	subject.GrantRPSLimit = readinessInt64Pointer(snapshot.APIKey.GrantRPSLimit)
	subject.GrantConcurrencyLimit = readinessInt64Pointer(snapshot.APIKey.GrantConcurrencyLimit)
	return subject
}

func readinessInt64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	result := int64(*value)
	return &result
}

func (h *Handler) readinessFixRoute(r *http.Request, route string, allowed bool) string {
	if !allowed {
		return ""
	}
	return route
}
