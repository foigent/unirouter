package admin

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
)

const modelEndpointProjectionVersion = "unirouter.model-endpoints.canonical-compat.v3"

type modelEndpointProbeView struct {
	Success       bool      `json:"success"`
	Current       bool      `json:"current"`
	StatusCode    int       `json:"status_code"`
	LatencyMS     int64     `json:"latency_ms"`
	ReportedModel string    `json:"reported_model,omitempty"`
	ErrorSummary  string    `json:"error_summary,omitempty"`
	TestedAt      time.Time `json:"tested_at"`
}

// modelEndpointTargetView retains the legacy selector shape without retaining
// legacy authority. channel_id and routing_epoch are always zero; consumers
// must use the explicit canonical identities added in v3.
type modelEndpointTargetView struct {
	TargetRef               string                  `json:"target_ref"`
	ChannelID               int64                   `json:"channel_id"`
	DeploymentID            int64                   `json:"deployment_id"`
	ConnectionID            int64                   `json:"connection_id"`
	ModelOperationID        string                  `json:"model_operation_id"`
	ConnectionName          string                  `json:"connection_name"`
	Provider                string                  `json:"provider"`
	BaseURL                 string                  `json:"base_url"`
	AdapterType             string                  `json:"adapter_type"`
	PublicModel             string                  `json:"public_model"`
	UpstreamModel           string                  `json:"upstream_model"`
	ConfigStatus            string                  `json:"config_status"`
	RoutingEpoch            int64                   `json:"routing_epoch"`
	Priority                int                     `json:"priority"`
	Weight                  int                     `json:"weight"`
	CircuitState            string                  `json:"circuit_state"`
	EligibleCredentialCount int                     `json:"eligible_credential_count"`
	StableAssignment        bool                    `json:"stable_assignment"`
	CanaryAssignment        bool                    `json:"canary_assignment"`
	RouteCandidate          bool                    `json:"route_candidate"`
	RouteExclusionReason    string                  `json:"route_exclusion_reason,omitempty"`
	State                   string                  `json:"state"`
	ReasonCode              string                  `json:"reason_code"`
	LastProbe               *modelEndpointProbeView `json:"last_probe,omitempty"`
}

type modelEndpointModelView struct {
	PublicModel          string                    `json:"public_model"`
	DisplayName          string                    `json:"display_name"`
	Lifecycle            string                    `json:"lifecycle,omitempty"`
	CatalogApproved      bool                      `json:"catalog_approved"`
	State                string                    `json:"state"`
	ReasonCode           string                    `json:"reason_code"`
	RouteAvailable       bool                      `json:"route_available"`
	StableReleaseRef     string                    `json:"stable_release_ref,omitempty"`
	StableReleaseVersion int64                     `json:"stable_release_version,omitempty"`
	CanaryReleaseRef     string                    `json:"canary_release_ref,omitempty"`
	CanaryReleaseVersion int64                     `json:"canary_release_version,omitempty"`
	CanaryBasisPoints    int                       `json:"canary_basis_points"`
	ReleaseHistoryCount  int                       `json:"release_history_count"`
	Targets              []modelEndpointTargetView `json:"targets"`
}

type modelEndpointProjection struct {
	ContractVersion string                   `json:"contract_version"`
	GeneratedAt     time.Time                `json:"generated_at"`
	Models          []modelEndpointModelView `json:"models"`
	Summary         map[string]int           `json:"summary"`
}

// ListModelEndpoints is a compatibility selector backed exclusively by the
// canonical model-deployment application projection. The richer operational
// workspace consumes /model-deployments directly.
func (h *Handler) ListModelEndpoints(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetCustomerOrgIDFromContext(r.Context())
	applications, err := h.loadCanonicalModelApplications(r.Context(), orgID, 0, "")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":  "Canonical model endpoint projection unavailable",
			"reason": "canonical_projection_unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, canonicalModelEndpointProjection(applications))
}

func canonicalModelEndpointProjection(source canonicalModelApplicationProjection) modelEndpointProjection {
	byModel := make(map[string]*modelEndpointModelView)
	for _, application := range source.Applications {
		model := byModel[application.PublicModel.ModelName]
		if model == nil {
			model = &modelEndpointModelView{
				PublicModel:     application.PublicModel.ModelName,
				DisplayName:     application.PublicModel.DisplayName,
				Lifecycle:       application.PublicModel.Lifecycle,
				CatalogApproved: application.PublicModel.ApprovedCatalog,
				State:           "unavailable", ReasonCode: "canonical_route_unavailable",
				Targets: make([]modelEndpointTargetView, 0),
			}
			byModel[application.PublicModel.ModelName] = model
		}
		model.StableReleaseRef = application.Runtime.StableReleaseRef
		model.StableReleaseVersion = application.Runtime.StableReleaseVersion
		model.CanaryReleaseRef = application.Runtime.CanaryReleaseRef
		model.CanaryReleaseVersion = application.Runtime.CanaryReleaseVersion
		model.CanaryBasisPoints = application.Runtime.CanaryBasisPoints
		for _, operation := range application.Operations {
			target := canonicalModelEndpointTarget(application, operation)
			model.Targets = append(model.Targets, target)
			if target.RouteCandidate {
				model.RouteAvailable = true
				if target.CanaryAssignment {
					model.State, model.ReasonCode = "canary", "canonical_canary_route_available"
				} else if model.State != "canary" {
					model.State, model.ReasonCode = "active", "canonical_stable_route_available"
				}
			}
		}
	}
	models := make([]modelEndpointModelView, 0, len(byModel))
	for _, model := range byModel {
		sort.Slice(model.Targets, func(i, j int) bool {
			if model.Targets[i].DeploymentID != model.Targets[j].DeploymentID {
				return model.Targets[i].DeploymentID < model.Targets[j].DeploymentID
			}
			return model.Targets[i].ModelOperationID < model.Targets[j].ModelOperationID
		})
		models = append(models, *model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].PublicModel < models[j].PublicModel })
	summary := map[string]int{"total": len(models), "route_available": 0, "attention": 0, "canary": 0}
	for _, model := range models {
		if model.RouteAvailable {
			summary["route_available"]++
		} else {
			summary["attention"]++
		}
		if model.State == "canary" {
			summary["canary"]++
		}
	}
	return modelEndpointProjection{
		ContractVersion: modelEndpointProjectionVersion,
		GeneratedAt:     source.GeneratedAt.UTC(),
		Models:          models,
		Summary:         summary,
	}
}

func canonicalModelEndpointTarget(application canonicalModelApplication, operation canonicalOperationAxis) modelEndpointTargetView {
	stable := operation.Runtime.StableMembership != nil
	canary := operation.Runtime.CanaryMembership != nil
	ready := application.PublicModel.Lifecycle != "retired" &&
		application.Connection.AdminState == "enabled" && application.Connection.RetiredAt == nil &&
		application.Deployment.AdminState == "enabled" && application.Deployment.RetiredAt == nil &&
		application.Credential.EligibleCount > 0 &&
		application.Deployment.CircuitState == "healthy" &&
		canonicalReadinessOperationVerified(operation) && (stable || canary)
	state, reason := "unavailable", "canonical_route_unavailable"
	if ready {
		state, reason = "active", "canonical_stable_route_available"
		if canary {
			state, reason = "canary", "canonical_canary_route_available"
		}
	}
	target := modelEndpointTargetView{
		TargetRef:               "deployment:" + strconv.FormatInt(application.Deployment.ID, 10) + ":" + operation.OperationID,
		DeploymentID:            application.Deployment.ID,
		ConnectionID:            application.Connection.ID,
		ModelOperationID:        operation.OperationID,
		ConnectionName:          application.Connection.Name,
		Provider:                application.Connection.Provider,
		BaseURL:                 application.Connection.BaseURL,
		AdapterType:             application.Connection.AdapterType,
		PublicModel:             application.PublicModel.ModelName,
		UpstreamModel:           application.Deployment.UpstreamModel,
		ConfigStatus:            application.Deployment.AdminState,
		Priority:                application.Deployment.Priority,
		Weight:                  application.Deployment.RoutingWeight,
		CircuitState:            application.Deployment.CircuitState,
		EligibleCredentialCount: application.Credential.EligibleCount,
		StableAssignment:        stable,
		CanaryAssignment:        canary,
		RouteCandidate:          ready,
		State:                   state,
		ReasonCode:              reason,
	}
	if operation.LatestProbe != nil {
		target.LastProbe = &modelEndpointProbeView{
			Success:       operation.LatestProbe.Success,
			Current:       operation.LatestProbe.Current,
			StatusCode:    operation.LatestProbe.StatusCode,
			LatencyMS:     operation.LatestProbe.LatencyMS,
			ReportedModel: operation.LatestProbe.ReportedModel,
			ErrorSummary:  operation.LatestProbe.ErrorSummary,
			TestedAt:      operation.LatestProbe.TestedAt,
		}
	}
	return target
}
