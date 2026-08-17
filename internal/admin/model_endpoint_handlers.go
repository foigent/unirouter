package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

const modelEndpointProjectionVersion = "unirouter.model-endpoints.v2"

type modelEndpointProbeView struct {
	Success      bool      `json:"success"`
	Current      bool      `json:"current"`
	StatusCode   int       `json:"status_code"`
	LatencyMS    int64     `json:"latency_ms"`
	ErrorSummary string    `json:"error_summary,omitempty"`
	TestedAt     time.Time `json:"tested_at"`
}

type modelEndpointTargetView struct {
	TargetRef               string                  `json:"target_ref"`
	ChannelID               int64                   `json:"channel_id"`
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
	StableExpectedEpoch     int64                   `json:"stable_expected_epoch,omitempty"`
	CanaryAssignment        bool                    `json:"canary_assignment"`
	CanaryExpectedEpoch     int64                   `json:"canary_expected_epoch,omitempty"`
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

// ListModelEndpoints is the only user-facing operational projection for model
// connectivity. It composes configuration, credential, probe, runtime-slot and
// the gateway's own routing preview without inventing another mutable state.
func (h *Handler) ListModelEndpoints(w http.ResponseWriter, r *http.Request) {
	models := make(map[string]*modelEndpointModelView)
	orgID := middleware.GetCustomerOrgIDFromContext(r.Context())

	rows, err := h.db.Query(r.Context(), `
WITH configured AS (
    SELECT c.id,c.name,c.provider,c.base_url,c.models,c.model_mapping,
           COALESCE(c.adapter_type,'openai_compatible') AS adapter_type,
           c.status,c.routing_epoch,c.priority,c.routing_weight,c.circuit_state,
           BTRIM(configured_model.model) AS public_model
      FROM channels c
      CROSS JOIN LATERAL regexp_split_to_table(COALESCE(c.models,''), '[,，;；\r\n]+') AS configured_model(model)
     WHERE BTRIM(configured_model.model) <> ''
)
SELECT configured.id,configured.name,configured.provider,configured.base_url,configured.adapter_type,
       configured.public_model,
       COALESCE(NULLIF(configured.model_mapping->>configured.public_model,''),configured.public_model) AS upstream_model,
       configured.status,configured.routing_epoch,configured.priority,configured.routing_weight,configured.circuit_state,
       (SELECT COUNT(*)
          FROM channel_keys ck
         WHERE ck.channel_id=configured.id AND ck.status='active'
           AND (ck.cooldown_until IS NULL OR ck.cooldown_until<=NOW())
           AND (BTRIM(COALESCE(ck.allowed_models,''))='' OR EXISTS (
               SELECT 1 FROM regexp_split_to_table(COALESCE(ck.allowed_models,''),'[,，;；\r\n]+') allowed(model)
                WHERE BTRIM(allowed.model)=configured.public_model))) AS eligible_key_count,
       COALESCE(stable_assignment.release_ref,''),COALESCE(stable_assignment.release_version,0),
	   COALESCE(stable_assignment.expected_routing_epoch,0),
	   stable_assignment.channel_id IS NOT NULL,
	   COALESCE(canary_assignment.release_ref,''),COALESCE(canary_assignment.release_version,0),
	   COALESCE(canary_assignment.expected_routing_epoch,0),
	   canary_assignment.channel_id IS NOT NULL,
       probe.success,probe.status_code,probe.latency_ms,probe.error_summary,probe.tested_at,
       CASE WHEN probe.channel_id IS NULL THEN FALSE ELSE
         probe.routing_epoch=configured.routing_epoch AND EXISTS (
           SELECT 1 FROM channel_keys probe_key
            WHERE probe_key.id=probe.credential_id
              AND probe_key.channel_id=configured.id
              AND probe_key.state_epoch=probe.credential_state_epoch
              AND probe_key.status='active'
              AND (probe_key.cooldown_until IS NULL OR probe_key.cooldown_until<=NOW())
              AND (BTRIM(COALESCE(probe_key.allowed_models,''))='' OR EXISTS (
                  SELECT 1 FROM regexp_split_to_table(COALESCE(probe_key.allowed_models,''),'[,，;；\r\n]+') allowed(model)
                   WHERE BTRIM(allowed.model)=configured.public_model))
         ) END AS probe_current
  FROM configured
  LEFT JOIN runtime_channel_assignments_v1 stable_assignment
	ON stable_assignment.model_name=configured.public_model
	AND stable_assignment.channel_id=configured.id
	AND stable_assignment.assignment_kind='stable'
	AND stable_assignment.release_stage='active'
	AND stable_assignment.admission='allow'
  LEFT JOIN runtime_channel_assignments_v1 canary_assignment
	ON canary_assignment.model_name=configured.public_model
	AND canary_assignment.channel_id=configured.id
	AND canary_assignment.assignment_kind='canary'
	AND canary_assignment.release_stage='canary'
	AND canary_assignment.admission='allow'
  LEFT JOIN channel_model_probes probe ON probe.channel_id=configured.id AND probe.public_model=configured.public_model
 ORDER BY configured.public_model,configured.priority,configured.id`)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model endpoint projection unavailable"})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var target modelEndpointTargetView
		var stableRef, canaryRef string
		var stableVersion, canaryVersion int64
		var probeSuccess sql.NullBool
		var probeStatus sql.NullInt64
		var probeLatency sql.NullInt64
		var probeError sql.NullString
		var probeAt sql.NullTime
		var probeCurrent bool
		if err := rows.Scan(
			&target.ChannelID, &target.ConnectionName, &target.Provider, &target.BaseURL, &target.AdapterType,
			&target.PublicModel, &target.UpstreamModel, &target.ConfigStatus, &target.RoutingEpoch, &target.Priority,
			&target.Weight, &target.CircuitState, &target.EligibleCredentialCount,
			&stableRef, &stableVersion, &target.StableExpectedEpoch, &target.StableAssignment,
			&canaryRef, &canaryVersion, &target.CanaryExpectedEpoch, &target.CanaryAssignment,
			&probeSuccess, &probeStatus, &probeLatency, &probeError, &probeAt, &probeCurrent,
		); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model endpoint projection unreadable"})
			return
		}
		target.TargetRef = targetReference(target.ChannelID, target.PublicModel)
		if probeAt.Valid {
			target.LastProbe = &modelEndpointProbeView{
				Success: probeSuccess.Valid && probeSuccess.Bool, Current: probeCurrent,
				StatusCode: int(probeStatus.Int64), LatencyMS: probeLatency.Int64,
				ErrorSummary: probeError.String, TestedAt: probeAt.Time,
			}
		}
		model := modelEndpointModel(models, target.PublicModel)
		model.Targets = append(model.Targets, target)
		if stableRef != "" {
			model.StableReleaseRef, model.StableReleaseVersion = stableRef, stableVersion
		}
		if canaryRef != "" {
			model.CanaryReleaseRef, model.CanaryReleaseVersion = canaryRef, canaryVersion
		}
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model endpoint projection unavailable"})
		return
	}

	if orgID > 0 {
		registryRows, registryErr := h.db.Query(r.Context(), `
SELECT model_name,display_name,lifecycle,approved_catalog
  FROM model_registry WHERE org_id=$1 ORDER BY model_name`, orgID)
		if registryErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model catalog projection unavailable"})
			return
		}
		for registryRows.Next() {
			var name, displayName, lifecycle string
			var approved bool
			if err := registryRows.Scan(&name, &displayName, &lifecycle, &approved); err != nil {
				registryRows.Close()
				writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model catalog projection unreadable"})
				return
			}
			model := models[strings.TrimSpace(name)]
			if model == nil && (!approved || lifecycle == "retired") {
				// Draft, hidden and retired catalog-only rows belong to advanced
				// governance. They must not inflate the operational model list;
				// a configured provider target remains visible for repair.
				continue
			}
			if model == nil {
				model = modelEndpointModel(models, name)
			}
			model.DisplayName, model.Lifecycle, model.CatalogApproved = displayName, lifecycle, approved
		}
		if err := registryRows.Err(); err != nil {
			registryRows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model catalog projection unavailable"})
			return
		}
		registryRows.Close()
	}

	slotRows, slotErr := h.db.Query(r.Context(), `
	SELECT model_name,
	       COALESCE(MAX(release_ref) FILTER (WHERE assignment_kind='stable'),''),
	       COALESCE(MAX(release_version) FILTER (WHERE assignment_kind='stable'),0),
	       COALESCE(MAX(release_ref) FILTER (WHERE assignment_kind='canary'),''),
	       COALESCE(MAX(release_version) FILTER (WHERE assignment_kind='canary'),0),
	       COALESCE(MAX(canary_basis_points),0)
	  FROM runtime_channel_assignments_v1
	 GROUP BY model_name
	 ORDER BY model_name`)
	if slotErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime assignment projection unavailable"})
		return
	}
	for slotRows.Next() {
		var name string
		model := &modelEndpointModelView{}
		if err := slotRows.Scan(&name, &model.StableReleaseRef, &model.StableReleaseVersion,
			&model.CanaryReleaseRef, &model.CanaryReleaseVersion, &model.CanaryBasisPoints); err != nil {
			slotRows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime assignment projection unreadable"})
			return
		}
		existing := modelEndpointModel(models, name)
		existing.StableReleaseRef, existing.StableReleaseVersion = model.StableReleaseRef, model.StableReleaseVersion
		existing.CanaryReleaseRef, existing.CanaryReleaseVersion = model.CanaryReleaseRef, model.CanaryReleaseVersion
		existing.CanaryBasisPoints, existing.ReleaseHistoryCount = model.CanaryBasisPoints, model.ReleaseHistoryCount
	}
	if err := slotRows.Err(); err != nil {
		slotRows.Close()
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime assignment projection unavailable"})
		return
	}
	slotRows.Close()

	historyRows, historyErr := h.db.Query(r.Context(), `
SELECT model_name,COUNT(*)
  FROM runtime_change_releases
 GROUP BY model_name`)
	if historyErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime release history projection unavailable"})
		return
	}
	for historyRows.Next() {
		var name string
		var count int
		if err := historyRows.Scan(&name, &count); err != nil {
			historyRows.Close()
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime release history projection unreadable"})
			return
		}
		if existing := models[strings.TrimSpace(name)]; existing != nil {
			existing.ReleaseHistoryCount = count
		}
	}
	if err := historyRows.Err(); err != nil {
		historyRows.Close()
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime release history projection unavailable"})
		return
	}
	historyRows.Close()

	items := make([]modelEndpointModelView, 0, len(models))
	for _, model := range models {
		h.applyGatewayProjection(r, model)
		items = append(items, *model)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PublicModel < items[j].PublicModel })
	summary := map[string]int{"total": len(items), "route_available": 0, "attention": 0, "canary": 0}
	for _, model := range items {
		if model.RouteAvailable {
			summary["route_available"]++
		}
		if model.State == "canary" {
			summary["canary"]++
		}
		if model.State != "active" && model.State != "canary" && model.State != "legacy_active" {
			summary["attention"]++
		}
	}
	writeJSON(w, http.StatusOK, modelEndpointProjection{ContractVersion: modelEndpointProjectionVersion, GeneratedAt: time.Now().UTC(), Models: items, Summary: summary})
}

func modelEndpointModel(models map[string]*modelEndpointModelView, name string) *modelEndpointModelView {
	name = strings.TrimSpace(name)
	if existing := models[name]; existing != nil {
		return existing
	}
	model := &modelEndpointModelView{PublicModel: name, DisplayName: name, State: "unconfigured", ReasonCode: "no_provider_target", Targets: make([]modelEndpointTargetView, 0)}
	models[name] = model
	return model
}

func (h *Handler) applyGatewayProjection(r *http.Request, model *modelEndpointModelView) {
	model.RouteAvailable = false
	candidates := map[int64]routingpolicy.PreviewCandidate{}
	exclusions := map[int64]routingpolicy.PreviewCandidate{}
	previewUnavailable := false
	if h.routingController == nil {
		previewUnavailable = true
	} else if preview, err := h.routingController.Preview(r.Context(), model.PublicModel); err != nil {
		previewUnavailable = true
	} else {
		for _, item := range preview.Candidates {
			candidates[item.ChannelID] = item
		}
		for _, item := range preview.Exclusions {
			exclusions[item.ChannelID] = item
		}
	}
	hasReady := false
	hasCredential := false
	hasFailedProbe := false
	stableCandidateCount := 0
	for index := range model.Targets {
		target := &model.Targets[index]
		_, target.RouteCandidate = candidates[target.ChannelID]
		if excluded, ok := exclusions[target.ChannelID]; ok {
			target.RouteExclusionReason = excluded.Reason
		}
		switch {
		case target.ConfigStatus == "disabled":
			target.State, target.ReasonCode = "paused", "connection_paused"
		case target.StableAssignment && target.StableExpectedEpoch != target.RoutingEpoch:
			target.State, target.ReasonCode = "stale", "stable_assignment_epoch_mismatch"
		case target.CanaryAssignment && target.CanaryExpectedEpoch != target.RoutingEpoch:
			target.State, target.ReasonCode = "stale", "canary_assignment_epoch_mismatch"
		case target.RouteCandidate && target.StableAssignment:
			target.State, target.ReasonCode = "active", "gateway_candidate"
		case target.EligibleCredentialCount == 0:
			target.State, target.ReasonCode = "needs_credential", "no_eligible_provider_credential"
		case target.LastProbe == nil || !target.LastProbe.Current:
			target.State, target.ReasonCode = "needs_test", "no_current_probe"
		case !target.LastProbe.Success:
			target.State, target.ReasonCode = "test_failed", "latest_probe_failed"
		case target.CanaryAssignment:
			target.State, target.ReasonCode = "canary", "canary_assignment"
		case target.RouteCandidate:
			target.State, target.ReasonCode = "legacy_active", "legacy_channel_candidate"
		default:
			target.State, target.ReasonCode = "ready", "probe_passed_not_routed"
		}
		if target.RouteCandidate && target.StableAssignment {
			stableCandidateCount++
		}
		if target.EligibleCredentialCount > 0 {
			hasCredential = true
		}
		if target.State == "ready" {
			hasReady = true
		}
		if target.State == "test_failed" {
			hasFailedProbe = true
		}
	}
	switch {
	case previewUnavailable && len(model.Targets) > 0:
		model.State, model.ReasonCode = "degraded", "routing_projection_unavailable"
	case model.CanaryReleaseRef != "" && stableCandidateCount > 0:
		model.State, model.ReasonCode = "canary", "canary_in_progress"
	case model.CanaryReleaseRef != "":
		model.State, model.ReasonCode = "degraded", "canary_stable_route_unavailable"
	case model.StableReleaseRef != "" && stableCandidateCount > 0:
		model.State, model.ReasonCode = "active", "stable_route_available"
	case model.StableReleaseRef != "":
		model.State, model.ReasonCode = "degraded", "stable_route_unavailable"
	case len(candidates) > 0:
		model.State, model.ReasonCode = "legacy_active", "legacy_route_available"
	case hasReady:
		model.State, model.ReasonCode = "ready", "tested_target_ready_to_enable"
	case hasFailedProbe:
		model.State, model.ReasonCode = "test_failed", "latest_probe_failed"
	case hasCredential:
		model.State, model.ReasonCode = "needs_test", "provider_probe_required"
	case len(model.Targets) > 0:
		model.State, model.ReasonCode = "needs_credential", "provider_credential_required"
	default:
		model.State, model.ReasonCode = "unconfigured", "no_provider_target"
	}
	model.RouteAvailable = len(candidates) > 0
	if model.Lifecycle == modelgov.LifecycleRetired {
		model.RouteAvailable = false
		model.State, model.ReasonCode = "governance_blocked", "model_retired"
	}
}

func targetReference(channelID int64, publicModel string) string {
	return fmt.Sprintf("target:%d:%s", channelID, publicModel)
}

type modelEndpointActivationRequest struct {
	ChannelID            int64  `json:"channel_id"`
	Model                string `json:"model"`
	ExpectedRoutingEpoch int64  `json:"expected_routing_epoch"`
	CanaryBasisPoints    int    `json:"canary_basis_points,omitempty"`
}

func (h *Handler) ActivateModelEndpoint(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeModelEndpointActivation(w, r)
	if !ok {
		return
	}
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "A current successful provider test is required", "code": "current_probe_required", "message": err.Error()})
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if _, identityErr := modelgov.ActivatedIdentity(r.Context(), h.db, orgID, request.Model); identityErr != nil {
		writePublicModelIdentityError(w, request.Model, identityErr)
		return
	}
	slot, err := h.runtimeChangeManager.Slot(r.Context(), request.Model)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	if slot.StableReleaseRef != "" || slot.CanaryReleaseRef != "" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Model already has a runtime route", "code": "canary_required", "next_step": "start a canary to add or replace a provider target"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	release, err := h.runtimeChangeManager.CreateDraft(r.Context(), runtimechange.DraftRequest{
		ModelName: request.Model, Reason: "首次启用已通过真实检测的供应商端点",
		Overrides: []routingpolicy.ChannelOverride{{ChannelID: request.ChannelID, ExpectedRoutingEpoch: request.ExpectedRoutingEpoch, Admission: routingpolicy.AdmissionAllow}},
	}, actor)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	release, err = h.runtimeChangeManager.Validate(r.Context(), release.ReleaseRef, release.Version, actor)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动首次启用未通过候选验证"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动首次启用期间真实检测证据失效"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Provider test became stale during activation", "code": "probe_became_stale", "release_ref": release.ReleaseRef, "release_stage": "rejected"})
		return
	}
	release, err = h.runtimeChangeManager.ActivateInitial(r.Context(), release.ReleaseRef, release.Version, actor, orgID)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动首次启用未能写入稳定路由"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		if errors.Is(err, modelgov.ErrModelIdentityConflict) || errors.Is(err, modelgov.ErrModelRetired) {
			writePublicModelIdentityError(w, request.Model, err)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	h.invalidateRoutingSnapshots()
	h.recordAuditEntry(r.Context(), "model_endpoint.activate_initial", "model_endpoint", targetReference(request.ChannelID, request.Model), map[string]interface{}{
		"model": request.Model, "channel_id": request.ChannelID, "release_ref": release.ReleaseRef, "routing_epoch": request.ExpectedRoutingEpoch,
	}, requestAuditMeta(r, orgID, 0))
	writeJSON(w, http.StatusOK, release)
}

func (h *Handler) StartModelEndpointCanary(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeModelEndpointActivation(w, r)
	if !ok {
		return
	}
	if request.CanaryBasisPoints == 0 {
		request.CanaryBasisPoints = 500
	}
	if request.CanaryBasisPoints < 1 || request.CanaryBasisPoints > 5000 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "canary_basis_points must be 1-5000"})
		return
	}
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "A current successful provider test is required", "code": "current_probe_required", "message": err.Error()})
		return
	}
	orgID, scopeErr := resolveActorOrganization(r.Context(), 0)
	if scopeErr != nil {
		writeOrganizationScopeError(w, scopeErr)
		return
	}
	if _, identityErr := modelgov.ActivatedIdentity(r.Context(), h.db, orgID, request.Model); identityErr != nil {
		writePublicModelIdentityError(w, request.Model, identityErr)
		return
	}
	slot, err := h.runtimeChangeManager.Slot(r.Context(), request.Model)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	if slot.StableReleaseRef == "" || slot.CanaryReleaseRef != "" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Canary requires one stable route and no current canary", "code": "canary_slot_conflict"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	release, err := h.runtimeChangeManager.CreateDraft(r.Context(), runtimechange.DraftRequest{
		ModelName: request.Model, Reason: "将已通过真实检测的供应商端点加入灰度路由",
		Overrides: []routingpolicy.ChannelOverride{{ChannelID: request.ChannelID, ExpectedRoutingEpoch: request.ExpectedRoutingEpoch, Admission: routingpolicy.AdmissionAllow}},
	}, actor)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	release, err = h.runtimeChangeManager.Validate(r.Context(), release.ReleaseRef, release.Version, actor)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动灰度未通过候选验证"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	release, err = h.runtimeChangeManager.AdvanceShadow(r.Context(), release.ReleaseRef, release.Version, actor)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动灰度未能进入零流量准备阶段"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动灰度准备期间真实检测证据失效"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Provider test became stale during canary preparation", "code": "probe_became_stale", "release_ref": release.ReleaseRef, "release_stage": "rejected"})
		return
	}
	release, err = h.runtimeChangeManager.StartCanary(r.Context(), release.ReleaseRef, release.Version, request.CanaryBasisPoints, actor)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "自动灰度未能写入灰度路由"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	h.invalidateRoutingSnapshots()
	h.recordAuditEntry(r.Context(), "model_endpoint.start_canary", "model_endpoint", targetReference(request.ChannelID, request.Model), map[string]interface{}{
		"model": request.Model, "channel_id": request.ChannelID, "release_ref": release.ReleaseRef, "routing_epoch": request.ExpectedRoutingEpoch, "canary_basis_points": request.CanaryBasisPoints,
	}, requestAuditMeta(r, middleware.GetCustomerOrgIDFromContext(r.Context()), 0))
	writeJSON(w, http.StatusOK, release)
}

// RecoverModelEndpoint replaces a retained stable assignment only when the
// gateway currently has no route candidate for the model. A working route
// must use canary instead. The old release and its validated replacement are
// switched atomically so a failed recovery cannot erase the retained slot.
func (h *Handler) RecoverModelEndpoint(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeModelEndpointActivation(w, r)
	if !ok {
		return
	}
	if h.runtimeChangeManager == nil || h.routingController == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model endpoint recovery unavailable"})
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "A current successful provider test is required", "code": "current_probe_required", "message": err.Error()})
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if _, identityErr := modelgov.ActivatedIdentity(r.Context(), h.db, orgID, request.Model); identityErr != nil {
		writePublicModelIdentityError(w, request.Model, identityErr)
		return
	}
	preview, err := h.routingController.Preview(r.Context(), request.Model)
	if err != nil && !errors.Is(err, gateway.ErrRuntimeAssignmentStale) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Current route projection unavailable"})
		return
	}
	if err == nil && len(preview.Candidates) > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Model still has an available route", "code": "canary_required",
			"message": "当前模型仍有可用路由；新增或替换端点必须使用灰度，不能走失效路由恢复。",
		})
		return
	}
	slot, err := h.runtimeChangeManager.Slot(r.Context(), request.Model)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	if slot.StableReleaseRef == "" || slot.CanaryReleaseRef != "" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Retained stable route is not recoverable", "code": "recovery_slot_conflict",
			"message": "只有“保留稳定发布但当前无路由候选”的模型可以直接恢复；当前槽位已经变化，请刷新。",
		})
		return
	}
	retained, err := h.runtimeChangeManager.Get(r.Context(), slot.StableReleaseRef)
	if err != nil || retained.Stage != "active" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Retained stable release changed", "code": "recovery_slot_conflict"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	replacementOverrides := make([]routingpolicy.ChannelOverride, 0, len(retained.Candidate.Overrides)+1)
	targetIncluded := false
	for _, existing := range retained.Candidate.Overrides {
		if existing.ChannelID == request.ChannelID {
			replacementOverrides = append(replacementOverrides, routingpolicy.ChannelOverride{
				ChannelID: request.ChannelID, ExpectedRoutingEpoch: request.ExpectedRoutingEpoch,
				Admission: routingpolicy.AdmissionAllow,
			})
			targetIncluded = true
			continue
		}
		var currentEpoch int64
		if epochErr := h.db.QueryRow(r.Context(), `SELECT routing_epoch FROM channels WHERE id=$1`, existing.ChannelID).Scan(&currentEpoch); epochErr != nil {
			if errors.Is(epochErr, pgx.ErrNoRows) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": "Retained endpoint no longer exists", "code": "recovery_channel_missing",
					"message": "旧稳定发布引用的端点已经不存在，无法自动构造可审计替换；请在高级运营中处理该发布。",
				})
				return
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Retained endpoint version unavailable"})
			return
		}
		existing.ExpectedRoutingEpoch = currentEpoch
		existing.Admission = routingpolicy.AdmissionDeny
		existing.Priority = nil
		existing.Weight = nil
		replacementOverrides = append(replacementOverrides, existing)
	}
	if !targetIncluded {
		replacementOverrides = append(replacementOverrides, routingpolicy.ChannelOverride{
			ChannelID: request.ChannelID, ExpectedRoutingEpoch: request.ExpectedRoutingEpoch,
			Admission: routingpolicy.AdmissionAllow,
		})
	}
	release, err := h.runtimeChangeManager.CreateDraft(r.Context(), runtimechange.DraftRequest{
		ModelName: request.Model, Reason: "替换已失效但保留审计记录的稳定供应商端点",
		Overrides: replacementOverrides,
	}, actor)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	release, err = h.runtimeChangeManager.Validate(r.Context(), release.ReleaseRef, release.Version, actor)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "失效路由恢复未通过候选验证"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	if err := h.requireCurrentSuccessfulProbe(r.Context(), request); err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "失效路由恢复期间真实检测证据失效"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Provider test became stale during recovery", "code": "probe_became_stale", "release_ref": release.ReleaseRef, "release_stage": "rejected"})
		return
	}
	preview, err = h.routingController.Preview(r.Context(), request.Model)
	if err != nil && !errors.Is(err, gateway.ErrRuntimeAssignmentStale) {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "失效路由恢复前无法再次确认当前路由"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Current route projection unavailable"})
		return
	}
	if err == nil && len(preview.Candidates) > 0 {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "恢复期间旧路由重新可用，必须改走灰度"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Model route recovered concurrently", "code": "canary_required",
			"message": "当前模型已经重新出现可用路由；新增或替换端点必须使用灰度，请刷新后重试。",
		})
		return
	}
	release, err = h.runtimeChangeManager.ReplaceUnavailableStable(
		r.Context(), release.ReleaseRef, release.Version,
		retained.ReleaseRef, retained.Version, actor, orgID,
	)
	if err != nil {
		if closeErr := closeAutomaticRelease(r.Context(), h.runtimeChangeManager, release.ReleaseRef, actor, "失效路由恢复未能写入新稳定路由"); closeErr != nil {
			writeAutomaticReleaseCleanupError(w, release.ReleaseRef, closeErr)
			return
		}
		if errors.Is(err, modelgov.ErrModelIdentityConflict) || errors.Is(err, modelgov.ErrModelRetired) {
			writePublicModelIdentityError(w, request.Model, err)
			return
		}
		writeRuntimeChangeError(w, err)
		return
	}
	h.invalidateRoutingSnapshots()
	h.recordAuditEntry(r.Context(), "model_endpoint.recover_unavailable", "model_endpoint", targetReference(request.ChannelID, request.Model), map[string]interface{}{
		"model": request.Model, "channel_id": request.ChannelID, "replaced_release_ref": retained.ReleaseRef,
		"release_ref": release.ReleaseRef, "routing_epoch": request.ExpectedRoutingEpoch,
	}, requestAuditMeta(r, orgID, 0))
	writeJSON(w, http.StatusOK, release)
}

func closeAutomaticRelease(ctx context.Context, manager *runtimechange.Manager, releaseRef string, actorUserID int64, reason string) error {
	release, err := manager.Get(ctx, releaseRef)
	if err != nil {
		return err
	}
	if release.Stage == "rejected" {
		return nil
	}
	if release.Stage != "draft" && release.Stage != "validated" && release.Stage != "shadow" {
		return fmt.Errorf("release is already in non-closable stage %s", release.Stage)
	}
	_, err = manager.Reject(ctx, release.ReleaseRef, release.Version, actorUserID, reason)
	return err
}

func writeAutomaticReleaseCleanupError(w http.ResponseWriter, releaseRef string, err error) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error":       "Automatic release cleanup failed",
		"code":        "automatic_release_cleanup_failed",
		"message":     "自动发布未完成，且候选记录未能安全终止。请在高级运营中处理该记录后再重试。",
		"release_ref": releaseRef,
		"detail":      err.Error(),
	})
}

func decodeModelEndpointActivation(w http.ResponseWriter, r *http.Request) (modelEndpointActivationRequest, bool) {
	var request modelEndpointActivationRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model endpoint activation request"})
		return request, false
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.ChannelID <= 0 || request.ExpectedRoutingEpoch <= 0 || request.Model == "" || len(request.Model) > 160 || strings.ContainsAny(request.Model, "\x00\r\n") {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "channel_id, model and expected_routing_epoch are required"})
		return request, false
	}
	return request, true
}

func (h *Handler) requireCurrentSuccessfulProbe(ctx context.Context, request modelEndpointActivationRequest) error {
	return requireCurrentSuccessfulProbeFrom(ctx, h.db, request)
}

func requireCurrentSuccessfulProbeFrom(ctx context.Context, store interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, request modelEndpointActivationRequest) error {
	var valid bool
	err := store.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
    FROM channels c
    JOIN channel_model_probes probe ON probe.channel_id=c.id AND probe.public_model=$2
    JOIN channel_keys credential ON credential.id=probe.credential_id AND credential.channel_id=c.id
   WHERE c.id=$1 AND c.routing_epoch=$3
     AND probe.routing_epoch=c.routing_epoch AND probe.success
     AND probe.upstream_model=COALESCE(NULLIF(c.model_mapping->>$2,''),$2)
     AND credential.state_epoch=probe.credential_state_epoch AND credential.status='active'
     AND (credential.cooldown_until IS NULL OR credential.cooldown_until<=NOW())
     AND (BTRIM(COALESCE(credential.allowed_models,''))='' OR EXISTS (
       SELECT 1 FROM regexp_split_to_table(COALESCE(credential.allowed_models,''),'[,，;；\r\n]+') allowed(model)
        WHERE BTRIM(allowed.model)=$2))
     AND EXISTS (
       SELECT 1 FROM regexp_split_to_table(COALESCE(c.models,''),'[,，;；\r\n]+') configured(model)
        WHERE BTRIM(configured.model)=$2)
)`, request.ChannelID, request.Model, request.ExpectedRoutingEpoch).Scan(&valid)
	if err != nil {
		return fmt.Errorf("read current provider test: %w", err)
	}
	if !valid {
		return fmt.Errorf("the latest successful test no longer matches the connection, credential or model mapping")
	}
	return nil
}
