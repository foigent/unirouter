package admin

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// ListModelEndpoints is the only user-facing operational projection for model
// connectivity. It composes configuration, credential, probe, runtime-slot and
// the gateway's own routing preview without inventing another mutable state.
func (h *Handler) ListModelEndpoints(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetCustomerOrgIDFromContext(r.Context())
	projection, err := h.loadModelEndpointProjection(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Model endpoint projection unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, projection)
}

// loadModelEndpointProjection is the canonical read model for model and
// provider-endpoint operational state. HTTP surfaces must consume this method
// instead of independently reinterpreting channel, credential, release and
// gateway state.
func (h *Handler) loadModelEndpointProjection(ctx context.Context, orgID int64) (modelEndpointProjection, error) {
	models := make(map[string]*modelEndpointModelView)

	rows, err := h.db.Query(ctx, `
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
		return modelEndpointProjection{}, fmt.Errorf("query configured model endpoints: %w", err)
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
			return modelEndpointProjection{}, fmt.Errorf("scan configured model endpoint: %w", err)
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
		return modelEndpointProjection{}, fmt.Errorf("read configured model endpoints: %w", err)
	}

	if orgID > 0 {
		registryRows, registryErr := h.db.Query(ctx, `
SELECT model_name,display_name,lifecycle,approved_catalog
  FROM model_registry WHERE org_id=$1 ORDER BY model_name`, orgID)
		if registryErr != nil {
			return modelEndpointProjection{}, fmt.Errorf("query model catalog projection: %w", registryErr)
		}
		for registryRows.Next() {
			var name, displayName, lifecycle string
			var approved bool
			if err := registryRows.Scan(&name, &displayName, &lifecycle, &approved); err != nil {
				registryRows.Close()
				return modelEndpointProjection{}, fmt.Errorf("scan model catalog projection: %w", err)
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
			return modelEndpointProjection{}, fmt.Errorf("read model catalog projection: %w", err)
		}
		registryRows.Close()
	}

	slotRows, slotErr := h.db.Query(ctx, `
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
		return modelEndpointProjection{}, fmt.Errorf("query runtime assignment projection: %w", slotErr)
	}
	for slotRows.Next() {
		var name string
		model := &modelEndpointModelView{}
		if err := slotRows.Scan(&name, &model.StableReleaseRef, &model.StableReleaseVersion,
			&model.CanaryReleaseRef, &model.CanaryReleaseVersion, &model.CanaryBasisPoints); err != nil {
			slotRows.Close()
			return modelEndpointProjection{}, fmt.Errorf("scan runtime assignment projection: %w", err)
		}
		existing := modelEndpointModel(models, name)
		existing.StableReleaseRef, existing.StableReleaseVersion = model.StableReleaseRef, model.StableReleaseVersion
		existing.CanaryReleaseRef, existing.CanaryReleaseVersion = model.CanaryReleaseRef, model.CanaryReleaseVersion
		existing.CanaryBasisPoints, existing.ReleaseHistoryCount = model.CanaryBasisPoints, model.ReleaseHistoryCount
	}
	if err := slotRows.Err(); err != nil {
		slotRows.Close()
		return modelEndpointProjection{}, fmt.Errorf("read runtime assignment projection: %w", err)
	}
	slotRows.Close()

	historyRows, historyErr := h.db.Query(ctx, `
SELECT model_name,COUNT(*)
  FROM runtime_change_releases
	 GROUP BY model_name`)
	if historyErr != nil {
		return modelEndpointProjection{}, fmt.Errorf("query runtime release history projection: %w", historyErr)
	}
	for historyRows.Next() {
		var name string
		var count int
		if err := historyRows.Scan(&name, &count); err != nil {
			historyRows.Close()
			return modelEndpointProjection{}, fmt.Errorf("scan runtime release history projection: %w", err)
		}
		if existing := models[strings.TrimSpace(name)]; existing != nil {
			existing.ReleaseHistoryCount = count
		}
	}
	if err := historyRows.Err(); err != nil {
		historyRows.Close()
		return modelEndpointProjection{}, fmt.Errorf("read runtime release history projection: %w", err)
	}
	historyRows.Close()

	items := make([]modelEndpointModelView, 0, len(models))
	for _, model := range models {
		h.applyGatewayProjection(ctx, model)
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
	return modelEndpointProjection{ContractVersion: modelEndpointProjectionVersion, GeneratedAt: time.Now().UTC(), Models: items, Summary: summary}, nil
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

func (h *Handler) applyGatewayProjection(ctx context.Context, model *modelEndpointModelView) {
	model.RouteAvailable = false
	candidates := map[int64]routingpolicy.PreviewCandidate{}
	exclusions := map[int64]routingpolicy.PreviewCandidate{}
	previewUnavailable := false
	if h.routingController == nil {
		previewUnavailable = true
	} else if preview, err := h.routingController.Preview(ctx, model.PublicModel); err != nil {
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
