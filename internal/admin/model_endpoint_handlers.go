package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
