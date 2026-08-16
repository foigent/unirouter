package admin

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modelreadiness"
)

const (
	maxReadinessModelLength          = 160
	maxReadinessApplicationRefLength = 64
)

type readinessGrantEvidence struct {
	ID               int64
	Status           string
	ScopeMode        string
	AllowedModels    []string
	AllowedEndpoints []string
	ValidFrom        time.Time
	ValidUntil       *time.Time
}

func (h *Handler) GetModelReadiness(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = modelreadiness.ModeCaller
	}
	requestedModel := strings.TrimSpace(r.URL.Query().Get("model"))
	rawEndpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	endpoint := accessscope.CanonicalExecutionEndpoint(rawEndpoint)
	caller := strings.TrimSpace(r.URL.Query().Get("caller"))
	applicationRef := strings.TrimSpace(r.URL.Query().Get("application_ref"))
	if requestedModel == "" || len(requestedModel) > maxReadinessModelLength ||
		strings.ContainsAny(requestedModel, "\x00\r\n") || endpoint != rawEndpoint || !accessscope.IsSupportedEndpoint(endpoint) ||
		!slices.Contains([]string{modelreadiness.ModeCaller, modelreadiness.ModeConnection}, mode) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid model readiness request", "reason": "invalid_request"})
		return
	}
	if mode == modelreadiness.ModeConnection {
		if strings.TrimSpace(r.URL.Query().Get("project_id")) != "" || caller != "" || applicationRef != "" {
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
			Scope: modelreadiness.Scope{Mode: mode, OrgID: orgID, RequestedModel: requestedModel,
				ResolvedModel: requestedModel, Endpoint: endpoint},
			RuntimeMutation: "none", ExternalRequests: "none", AsOf: time.Now().UTC(),
		}
		connectionOnly := modelreadiness.Optional("connection_mode_not_applicable", nil)
		response.ProjectCaller = modelreadiness.ProjectCaller{
			Project: connectionOnly, Alias: connectionOnly, ModelSet: connectionOnly,
			Wallet: connectionOnly, ProjectBudget: connectionOnly, Application: connectionOnly,
			Grant: connectionOnly, Credential: connectionOnly,
			ModelAuthorization: connectionOnly, LimitPolicy: connectionOnly,
		}
		h.readinessPlatformAxes(r, orgID, &response)
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

	var orgID int64
	var projectStatus, budgetPeriod string
	var budgetCents sql.NullInt64
	if err := h.db.QueryRow(r.Context(), `
		SELECT org_id,status,budget_cents,COALESCE(budget_period,'')
		FROM projects WHERE id=$1`, projectID).Scan(&orgID, &projectStatus, &budgetCents, &budgetPeriod); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found", "reason": "not_found"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Model readiness unavailable", "reason": "service_unavailable"})
		return
	}
	if actorOrganizationRole(r.Context()) != auth.RolePlatformAdmin && orgID != middleware.GetOrgIDFromContext(r.Context()) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Project not found", "reason": "not_found"})
		return
	}

	response := modelreadiness.Response{
		ContractVersion: modelreadiness.ContractVersion,
		Scope: modelreadiness.Scope{Mode: mode, OrgID: orgID, ProjectID: projectID, RequestedModel: requestedModel,
			ResolvedModel: requestedModel, Endpoint: endpoint, Caller: caller, ApplicationRef: applicationRef},
		RuntimeMutation: "none", ExternalRequests: "none", AsOf: time.Now().UTC(),
	}
	projectRoute := "/organization-members?section=projects&project_id=" + strconv.FormatInt(projectID, 10)
	response.ProjectCaller.Project = readinessProjectAxis(projectStatus, h.readinessFixRoute(r, projectRoute, h.canWriteProject(r.Context(), projectID)))
	response.ProjectCaller.ProjectBudget = readinessProjectBudgetAxis(h.effectiveBillingMode(), budgetCents, budgetPeriod,
		h.readinessFixRoute(r, projectRoute+"&tab=budget", h.canWriteProject(r.Context(), projectID)))

	access, accessErr := modelgov.CheckAccess(r.Context(), h.db, orgID, projectID, requestedModel)
	response.Scope.ResolvedModel = access.ResolvedModel
	if response.Scope.ResolvedModel == "" {
		response.Scope.ResolvedModel = requestedModel
	}
	response.ProjectCaller.Alias, response.ProjectCaller.ModelSet = h.readinessGovernanceAxes(r, orgID, projectID, access, accessErr)
	h.readinessPlatformAxes(r, orgID, &response)
	h.readinessWalletAxis(r, orgID, &response)
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

func readinessProjectBudgetAxis(mode billing.BillingMode, budget sql.NullInt64, period, fixRoute string) modelreadiness.Axis {
	if mode == billing.BillingModeCostObservation {
		evidence := modelreadiness.Evidence(
			"configured", budget.Valid,
			"billing_mode", string(mode),
			"project_budget_admission_required", false,
			"project_budget_enforced", false,
			"calculated_cost_budget_alerts_available", false,
		)
		if budget.Valid {
			evidence["period"] = period
			return modelreadiness.Warning("project_budget_configured_only_not_enforced_under_cost_observation", evidence, fixRoute, "project_admin")
		}
		return modelreadiness.Optional("project_budget_not_configured_or_enforced_under_cost_observation", evidence)
	}
	evidence := modelreadiness.Evidence(
		"configured", budget.Valid,
		"billing_mode", string(mode),
		"project_budget_admission_required", false,
	)
	if !budget.Valid {
		return modelreadiness.Optional("project_budget_not_configured_monitoring_only", evidence)
	}
	evidence["period"] = period
	return modelreadiness.Warning("project_budget_monitoring_only", evidence, fixRoute, "project_admin")
}

func (h *Handler) readinessGovernanceAxes(r *http.Request, orgID, projectID int64, access modelgov.AccessResult, accessErr error) (modelreadiness.Axis, modelreadiness.Axis) {
	aliasRoute := h.readinessFixRoute(r, "/model-governance?tab=aliases&org_id="+strconv.FormatInt(orgID, 10),
		actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin)
	aliasAxis := modelreadiness.Optional("direct_model_optional", modelreadiness.Evidence("alias_applied", false))
	if access.Alias != "" {
		aliasAxis = modelreadiness.Ready("active_alias_resolved", modelreadiness.Evidence("alias_applied", true))
	} else {
		var aliasStatus string
		if err := h.db.QueryRow(r.Context(), `SELECT status FROM model_aliases WHERE org_id=$1 AND alias=$2`, orgID, access.RequestedModel).Scan(&aliasStatus); err == nil {
			aliasAxis = modelreadiness.Blocked("alias_inactive", modelreadiness.Evidence("alias_applied", false, "status", aliasStatus), aliasRoute, "organization_admin")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			aliasAxis = modelreadiness.Unavailable("alias_evidence_unavailable")
		}
	}
	var modelSetCount int
	setErr := h.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM project_model_sets pms
		JOIN projects p ON p.id=pms.project_id
		JOIN model_sets ms ON ms.id=pms.model_set_id AND ms.org_id=p.org_id
		WHERE pms.project_id=$1 AND p.org_id=$2`, projectID, orgID).Scan(&modelSetCount)
	if setErr != nil {
		return aliasAxis, modelreadiness.Unavailable("model_set_evidence_unavailable")
	}
	if accessErr != nil && !errors.Is(accessErr, modelgov.ErrModelDenied) && !errors.Is(accessErr, modelgov.ErrModelRetired) {
		return modelreadiness.Unavailable("alias_evidence_unavailable"), modelreadiness.Unavailable("model_set_evidence_unavailable")
	}
	if modelSetCount == 0 {
		return aliasAxis, modelreadiness.Optional("unrestricted_optional", modelreadiness.Evidence("assigned_model_set_count", 0))
	}
	if errors.Is(accessErr, modelgov.ErrModelDenied) {
		return aliasAxis, modelreadiness.Blocked("project_model_set_denied", modelreadiness.Evidence("assigned_model_set_count", modelSetCount),
			h.readinessFixRoute(r, "/model-governance?tab=model-sets&project_id="+strconv.FormatInt(projectID, 10),
				actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin), "organization_admin")
	}
	return aliasAxis, modelreadiness.Ready("project_model_set_allowed", modelreadiness.Evidence("assigned_model_set_count", modelSetCount))
}

func (h *Handler) readinessPlatformAxes(r *http.Request, orgID int64, response *modelreadiness.Response) {
	model := response.Scope.ResolvedModel
	if h.licenseRuntime == nil {
		response.PlatformModel.GatewayLicense = modelreadiness.Optional(
			"gateway_license_evidence_not_configured", modelreadiness.Evidence("runtime_configured", false))
	} else if decision := h.licenseRuntime.Authorize(licensing.FeatureGateway); decision.Allowed {
		response.PlatformModel.GatewayLicense = modelreadiness.Ready(
			"gateway_license_authorized", modelreadiness.Evidence("status", decision.Status.Status, "enforceable", decision.Status.Enforceable))
	} else {
		response.PlatformModel.GatewayLicense = modelreadiness.Blocked(
			decision.Code,
			modelreadiness.Evidence("status", decision.Status.Status, "enforceable", decision.Status.Enforceable),
			h.readinessFixRoute(r, "/license", actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin),
			"platform_admin",
		)
	}
	modelGovernanceLicensed := h.licenseRuntime == nil || h.licenseRuntime.Authorize(licensing.FeatureModelGovernance).Allowed
	var lifecycle string
	var approvedCatalog bool
	err := h.db.QueryRow(r.Context(), `SELECT lifecycle,approved_catalog FROM model_registry WHERE org_id=$1 AND model_name=$2`, orgID, model).Scan(&lifecycle, &approvedCatalog)
	registryFix := h.readinessFixRoute(r, "/model-governance?tab=registry&model="+url.QueryEscape(model),
		actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleOrgAdmin)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if modelGovernanceLicensed {
			response.PlatformModel.Registration = modelreadiness.Blocked("model_unregistered", modelreadiness.Evidence("registered", false), registryFix, "organization_admin")
			response.PlatformModel.Catalog = modelreadiness.Blocked("catalog_not_published", modelreadiness.Evidence("visible", false), registryFix, "organization_admin")
		} else {
			response.PlatformModel.Registration = modelreadiness.Optional("model_governance_not_licensed_optional", modelreadiness.Evidence("registered", false, "feature_licensed", false))
			response.PlatformModel.Catalog = modelreadiness.Optional("model_governance_not_licensed_optional", modelreadiness.Evidence("visible", false, "feature_licensed", false))
		}
		response.PlatformModel.Lifecycle = modelreadiness.Optional("unregistered_lifecycle_not_applicable", nil)
	case err != nil:
		response.PlatformModel.Registration = modelreadiness.Unavailable("model_registry_unavailable")
		response.PlatformModel.Lifecycle = modelreadiness.Unavailable("model_lifecycle_unavailable")
		response.PlatformModel.Catalog = modelreadiness.Unavailable("model_catalog_unavailable")
	default:
		response.PlatformModel.Registration = modelreadiness.Ready("model_registered", modelreadiness.Evidence("registered", true))
		if lifecycle == modelgov.LifecycleRetired {
			response.PlatformModel.Lifecycle = modelreadiness.Blocked("model_retired", modelreadiness.Evidence("lifecycle", lifecycle), registryFix, "organization_admin")
		} else {
			response.PlatformModel.Lifecycle = modelreadiness.Ready("model_callable_lifecycle", modelreadiness.Evidence("lifecycle", lifecycle))
		}
		if approvedCatalog && modelgov.IsCatalogLifecycle(lifecycle) {
			response.PlatformModel.Catalog = modelreadiness.Ready("catalog_published", modelreadiness.Evidence("visible", true))
		} else if !modelGovernanceLicensed {
			response.PlatformModel.Catalog = modelreadiness.Optional("model_governance_not_licensed_optional", modelreadiness.Evidence("visible", false, "feature_licensed", false))
		} else {
			response.PlatformModel.Catalog = modelreadiness.Blocked("catalog_not_published", modelreadiness.Evidence("visible", false), registryFix, "organization_admin")
		}
	}

	response.PlatformModel.PriceAuthority = h.readinessPriceAuthorityAxis(r, orgID, model)

	if h.routingController == nil {
		response.PlatformModel.Channel = modelreadiness.Unavailable("routing_preview_unavailable")
		response.PlatformModel.ProviderCredential = modelreadiness.Unavailable("routing_preview_unavailable")
		response.PlatformModel.BaseRoute = modelreadiness.Unavailable("routing_preview_unavailable")
	} else if preview, err := h.routingController.Preview(r.Context(), model); err != nil {
		response.PlatformModel.Channel = modelreadiness.Unavailable("routing_preview_unavailable")
		response.PlatformModel.ProviderCredential = modelreadiness.Unavailable("routing_preview_unavailable")
		response.PlatformModel.BaseRoute = modelreadiness.Unavailable("routing_preview_unavailable")
	} else {
		eligibleKeys := 0
		for _, candidate := range preview.Candidates {
			eligibleKeys += candidate.EligibleKeyCount
		}
		for _, exclusion := range preview.Exclusions {
			eligibleKeys += exclusion.EligibleKeyCount
		}
		routeFix := h.readinessFixRoute(r, "/channels?model="+url.QueryEscape(model), actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin)
		credentialFix := routeFix
		credentialChannelID := int64(0)
		if len(preview.Candidates) > 0 {
			credentialChannelID = preview.Candidates[0].ChannelID
		} else if len(preview.Exclusions) > 0 {
			credentialChannelID = preview.Exclusions[0].ChannelID
		}
		if credentialChannelID > 0 {
			credentialFix = h.readinessFixRoute(r,
				"/channel-keys?from=connections&channel_id="+strconv.FormatInt(credentialChannelID, 10)+"&model="+url.QueryEscape(model),
				actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin)
		}
		if len(preview.Candidates)+len(preview.Exclusions) > 0 {
			response.PlatformModel.Channel = modelreadiness.Ready("channel_configured", modelreadiness.Evidence("candidate_count", len(preview.Candidates), "exclusion_count", len(preview.Exclusions)))
		} else {
			response.PlatformModel.Channel = modelreadiness.Blocked("channel_missing", modelreadiness.Evidence("candidate_count", 0, "exclusion_count", 0), routeFix, "platform_admin")
		}
		if eligibleKeys > 0 {
			response.PlatformModel.ProviderCredential = modelreadiness.Ready("provider_credential_available", modelreadiness.Evidence("eligible_credential_count", eligibleKeys))
		} else {
			response.PlatformModel.ProviderCredential = modelreadiness.Blocked("provider_credential_unavailable", modelreadiness.Evidence("eligible_credential_count", 0), credentialFix, "platform_admin")
		}
		if len(preview.Candidates) > 0 && eligibleKeys > 0 {
			response.PlatformModel.BaseRoute = modelreadiness.Ready("base_route_active", modelreadiness.Evidence("candidate_count", len(preview.Candidates), "degraded", preview.Degraded))
		} else {
			response.PlatformModel.BaseRoute = modelreadiness.Blocked("base_route_unavailable", modelreadiness.Evidence("candidate_count", len(preview.Candidates), "exclusion_count", len(preview.Exclusions)), routeFix, "platform_admin")
		}
	}

	if h.runtimeChangeManager == nil {
		response.PlatformModel.RuntimeSlot = modelreadiness.Optional("runtime_slot_not_configured_optional", modelreadiness.Evidence("configured", false))
	} else if slot, err := h.runtimeChangeManager.Slot(r.Context(), model); err != nil {
		response.PlatformModel.RuntimeSlot = modelreadiness.Unavailable("runtime_slot_unavailable")
	} else if slot.StableReleaseRef == "" && slot.CanaryReleaseRef == "" {
		response.PlatformModel.RuntimeSlot = modelreadiness.Optional("runtime_slot_not_configured_optional", modelreadiness.Evidence("configured", false))
	} else {
		response.PlatformModel.RuntimeSlot = modelreadiness.Ready("runtime_slot_configured", modelreadiness.Evidence("configured", true, "canary_basis_points", slot.CanaryBasisPoints))
	}
}

func (h *Handler) readinessPriceAuthorityAxis(r *http.Request, orgID int64, model string) modelreadiness.Axis {
	mode := h.effectiveRequestPricingAuthority()
	if mode == string(billing.RequestPricingAuthorityRateCard) {
		store, err := enterprisefinance.NewPostgresStore(h.db)
		if err != nil {
			return modelreadiness.Unavailable("rate_card_authority_unavailable")
		}
		quote, err := store.ResolveRequestPriceQuote(r.Context(), orgID, model, time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			evidence := modelreadiness.Evidence(
				"configured", false,
				"request_pricing_mode", mode,
				"selection_key", enterprisefinance.RequestPricingKey,
			)
			if errors.Is(err, enterprisefinance.ErrRequestPricingNotReady) || errors.Is(err, enterprisefinance.ErrRequestPricingAmbiguous) {
				return modelreadiness.Blocked("rate_card_authority_not_ready", evidence,
					h.readinessFixRoute(r, "/enterprise-finance?section=rate-cards&model="+url.QueryEscape(model),
						actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin || actorOrganizationRole(r.Context()) == auth.RoleBillingAdmin),
					"billing_admin")
			}
			return modelreadiness.Unavailable("rate_card_authority_unavailable")
		}
		return modelreadiness.Ready("rate_card_authority_resolved", modelreadiness.Evidence(
			"configured", true,
			"request_pricing_mode", mode,
			"selection_key", quote.SelectionKey,
			"rate_card_ref", quote.RateCardRef,
			"rate_card_version", quote.RateCardVersion,
			"layer", quote.Layer,
			"provider_ref", quote.ProviderRef,
			"pricing_version", quote.PricingVersion,
		))
	}
	if mode != string(billing.RequestPricingAuthorityLegacy) {
		return modelreadiness.Unavailable("request_pricing_authority_invalid")
	}
	var priceExists bool
	if err := h.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM model_prices WHERE model_name=$1)`, model).Scan(&priceExists); err != nil {
		return modelreadiness.Unavailable("model_price_unavailable")
	}
	if priceExists {
		return modelreadiness.Ready("model_price_configured", modelreadiness.Evidence(
			"configured", true, "request_pricing_mode", mode,
		))
	}
	if h.effectiveBillingMode() == billing.BillingModeCostObservation {
		return modelreadiness.Optional("model_price_unconfigured_cost_observation", modelreadiness.Evidence(
			"configured", false,
			"request_pricing_mode", mode,
			"billing_mode", string(billing.BillingModeCostObservation),
			"traffic_admission_required", false,
			"calculated_cost_available", false,
		))
	}
	return modelreadiness.Blocked("model_price_missing", modelreadiness.Evidence(
		"configured", false, "request_pricing_mode", mode,
	), h.readinessFixRoute(r, "/model-prices?model="+url.QueryEscape(model), actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin), "platform_admin")
}

func (h *Handler) readinessWalletAxis(r *http.Request, orgID int64, response *modelreadiness.Response) {
	mode := h.effectiveBillingMode()
	if mode == billing.BillingModeCostObservation {
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
	var balance, granted, frozen int64
	err := h.db.QueryRow(r.Context(), `SELECT balance_micros,granted_balance_micros,frozen_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&balance, &granted, &frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		response.ProjectCaller.Wallet = modelreadiness.Blocked("wallet_missing", evidence("exists", false), "", "billing_admin")
	} else if err != nil {
		response.ProjectCaller.Wallet = modelreadiness.Axis{
			State:        modelreadiness.StateUnavailable,
			ReasonCode:   "wallet_unavailable",
			SafeEvidence: evidence(),
		}
	} else if balance+granted-frozen > 0 {
		response.ProjectCaller.Wallet = modelreadiness.Ready("wallet_has_spendable_balance", evidence("exists", true, "has_spendable_balance", true))
	} else {
		response.ProjectCaller.Wallet = modelreadiness.Blocked("wallet_no_spendable_balance", evidence("exists", true, "has_spendable_balance", false), "", "billing_admin")
	}
}

func (h *Handler) readinessCallerAxes(r *http.Request, orgID, projectID int64, caller, applicationRef string, response *modelreadiness.Response) {
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	workloadID := int64(0)
	accessFixRoute := "/access?project_id=" + strconv.FormatInt(projectID, 10)
	accessFixAllowed := caller == "self"
	if caller == "self" {
		response.ProjectCaller.Application = modelreadiness.Optional("self_application_not_applicable", nil)
	} else {
		accessFixRoute = "/applications?project_id=" + strconv.FormatInt(projectID, 10) + "&application_ref=" + url.QueryEscape(applicationRef)
		accessFixAllowed = h.canWriteProject(r.Context(), projectID)
		var status, identity, environment string
		var productionUse bool
		err := h.db.QueryRow(r.Context(), `
			SELECT id,status,identity_contract_version,environment,production_use
			FROM workload_applications WHERE application_ref=$1 AND org_id=$2 AND project_id=$3`, applicationRef, orgID, projectID).
			Scan(&workloadID, &status, &identity, &environment, &productionUse)
		if errors.Is(err, pgx.ErrNoRows) {
			response.ProjectCaller.Application = modelreadiness.Blocked("workload_application_not_found", modelreadiness.Evidence("present", false),
				h.readinessFixRoute(r, "/applications?project_id="+strconv.FormatInt(projectID, 10), h.canWriteProject(r.Context(), projectID)), "project_admin")
		} else if err != nil {
			response.ProjectCaller.Application = modelreadiness.Unavailable("workload_application_unavailable")
		} else if status != "active" || identity != "unirouter.workload-identity.v2" {
			response.ProjectCaller.Application = modelreadiness.Blocked("workload_application_inactive", modelreadiness.Evidence("present", true, "status", status, "identity_v2", identity == "unirouter.workload-identity.v2"),
				h.readinessFixRoute(r, "/applications?project_id="+strconv.FormatInt(projectID, 10)+"&application_ref="+url.QueryEscape(applicationRef), h.canWriteProject(r.Context(), projectID)), "project_admin")
		} else {
			response.ProjectCaller.Application = modelreadiness.Ready("workload_application_active_v2", modelreadiness.Evidence("present", true, "environment", environment, "production_use", productionUse))
		}
	}

	grant, err := h.readReadinessGrant(r, orgID, projectID, caller, actorUserID, workloadID)
	if err != nil {
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
	grantScopeAllowed := readinessGrantScopeAllows(grant, response.Scope.RequestedModel, response.Scope.ResolvedModel, response.Scope.Endpoint)
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
	snapshots, hasCandidate, hasLiveBoundary, resolveErr := h.readReadinessCredentials(
		r, orgID, projectID, grant.ID, response.Scope.RequestedModel, response.Scope.ResolvedModel, response.Scope.Endpoint)
	if resolveErr != nil {
		response.ProjectCaller.Credential = modelreadiness.Unavailable("runtime_caller_boundary_unavailable")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if !hasCandidate {
		response.ProjectCaller.Credential = modelreadiness.Blocked("access_credential_missing", modelreadiness.Evidence("present", false),
			h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if !hasLiveBoundary {
		response.ProjectCaller.Credential = modelreadiness.Blocked("runtime_caller_boundary_inactive", modelreadiness.Evidence("present", true), "", "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	if len(snapshots) == 0 {
		response.ProjectCaller.Credential = modelreadiness.Blocked("access_credential_scope_excludes_request",
			modelreadiness.Evidence("present", true), h.readinessFixRoute(r, accessFixRoute, accessFixAllowed), "subject_or_project_admin")
		h.readinessPolicyAxes(r, response, nil, actorUserID)
		return
	}
	snapshot := snapshots[0]
	response.ProjectCaller.Credential = modelreadiness.Ready("runtime_caller_boundary_active", modelreadiness.Evidence("present", true, "grant_revision", snapshot.GrantRevision))
	h.readinessPolicyAxes(r, response, snapshots, actorUserID)
}

func (h *Handler) readReadinessGrant(r *http.Request, orgID, projectID int64, caller string, userID, workloadID int64) (readinessGrantEvidence, error) {
	var grant readinessGrantEvidence
	var modelsJSON, endpointsJSON []byte
	var validUntil sql.NullTime
	err := h.db.QueryRow(r.Context(), `
		SELECT ag.id,ag.status,ag.scope_mode,ag.allowed_models,ag.allowed_endpoints,ag.valid_from,ag.valid_until
		FROM access_grants ag
		WHERE ag.org_id=$1 AND ag.project_id=$2
		  AND (($3='self' AND ag.subject_type='user' AND ag.subject_user_id=$4)
		    OR ($3='workload_application' AND ag.subject_type='workload_application' AND ag.subject_workload_application_id=$5))
		ORDER BY (ag.status<>'revoked') DESC,ag.id DESC LIMIT 1`, orgID, projectID, caller, userID, workloadID).
		Scan(&grant.ID, &grant.Status, &grant.ScopeMode, &modelsJSON, &endpointsJSON, &grant.ValidFrom, &validUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return readinessGrantEvidence{}, nil
	}
	if err != nil {
		return readinessGrantEvidence{}, err
	}
	if validUntil.Valid {
		value := validUntil.Time
		grant.ValidUntil = &value
	}
	if json.Unmarshal(modelsJSON, &grant.AllowedModels) != nil || json.Unmarshal(endpointsJSON, &grant.AllowedEndpoints) != nil {
		return readinessGrantEvidence{}, errors.New("invalid access grant scope")
	}
	return grant, nil
}

func (h *Handler) readReadinessCredentials(
	r *http.Request,
	orgID, projectID, grantID int64,
	requestedModel, resolvedModel, endpoint string,
) ([]accessgrantboundary.Snapshot, bool, bool, error) {
	rows, err := h.db.Query(r.Context(), `
		SELECT k.id
		FROM access_credentials ac
		JOIN api_keys k
		  ON k.access_credential_id=ac.id AND k.access_grant_id=ac.access_grant_id
		 AND k.org_id=ac.org_id AND k.project_id=ac.project_id AND k.credential_ref=ac.credential_ref
		WHERE ac.access_grant_id=$1 AND ac.org_id=$2 AND ac.project_id=$3
		  AND ac.status='active' AND k.status='active' AND COALESCE(k.purpose,'')=''
		ORDER BY k.id DESC`, grantID, orgID, projectID)
	if err != nil {
		return nil, false, false, err
	}
	defer rows.Close()
	hasCandidate := false
	hasLiveBoundary := false
	snapshots := make([]accessgrantboundary.Snapshot, 0)
	for rows.Next() {
		hasCandidate = true
		var keyID int64
		if err := rows.Scan(&keyID); err != nil {
			return nil, hasCandidate, hasLiveBoundary, err
		}
		snapshot, err := accessgrantboundary.Resolve(r.Context(), h.db, keyID)
		if errors.Is(err, accessgrantboundary.ErrInactive) {
			continue
		}
		if err != nil {
			return nil, hasCandidate, hasLiveBoundary, err
		}
		hasLiveBoundary = true
		if snapshot.AllowsExecution(requestedModel, resolvedModel, endpoint) {
			snapshots = append(snapshots, snapshot)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, hasCandidate, hasLiveBoundary, err
	}
	return snapshots, hasCandidate, hasLiveBoundary, nil
}

func readinessGrantScopeAllows(grant readinessGrantEvidence, requested, resolved, endpoint string) bool {
	if grant.ScopeMode == accessscope.ModeProjectDefault {
		return accessscope.IsProjectDefaultEndpoint(endpoint)
	}
	if grant.ScopeMode != accessscope.ModeExplicit || !slices.Contains(grant.AllowedEndpoints, endpoint) {
		return false
	}
	return slices.Contains(grant.AllowedModels, requested) || slices.Contains(grant.AllowedModels, resolved)
}

func (h *Handler) readinessPolicyAxes(r *http.Request, response *modelreadiness.Response, snapshots []accessgrantboundary.Snapshot, actorUserID int64) {
	if len(snapshots) == 0 {
		snapshots = []accessgrantboundary.Snapshot{{}}
	}
	allowedSnapshots := make([]accessgrantboundary.Snapshot, 0, len(snapshots))
	modelAuthorizationUnavailable := false
	deniedReason := "model_authorization_denied"
	deniedPolicyCount := 0
	if h.modelAuthorizationEvaluator == nil {
		response.ProjectCaller.ModelAuthorization = modelreadiness.Unavailable("model_authorization_unavailable")
	} else {
		for _, snapshot := range snapshots {
			input := readinessModelAuthorizationInput(response, snapshot, actorUserID)
			decision, err := h.modelAuthorizationEvaluator.Evaluate(r.Context(), input)
			if err != nil {
				modelAuthorizationUnavailable = true
				continue
			}
			if decision.Allowed {
				allowedSnapshots = append(allowedSnapshots, snapshot)
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
			if modelAuthorizationUnavailable {
				response.ProjectCaller.ModelAuthorization = modelreadiness.Unavailable("model_authorization_unavailable")
			} else {
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
