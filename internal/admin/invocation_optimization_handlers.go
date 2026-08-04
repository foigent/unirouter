package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/middleware"
)

type invocationOptimizationTransition struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}

func (h *Handler) InvocationOptimizationContract(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"contract_version":             invocationopt.PolicyContractVersion,
		"receipt_contract_version":     invocationopt.ReceiptContractVersion,
		"commercial_receipt_version":   invocationopt.CommercialReceiptVersion,
		"response_cache_price_version": invocationopt.ResponseCachePriceVersion,
		"response_cache_runtime_ready": h != nil && h.responseCacheRuntimeReady,
		"request_headers": map[string]string{
			"response_cache": "X-UniRouter-Response-Cache: allow",
			"compression":    "X-UniRouter-Prompt-Compression: allow",
		},
		"stages":                         []string{invocationopt.StageDraft, invocationopt.StageValidated, invocationopt.StageShadow, invocationopt.StageCanary, invocationopt.StageRolledBack},
		"maximum_canary_basis_points":    1000,
		"raw_content_in_receipts":        false,
		"request_opt_in_always_required": true,
		"x_insight_in_request_path":      false,
	})
}

func (h *Handler) ListInvocationOptimizationPolicies(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if h.invocationOptimization == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	items, err := h.invocationOptimization.List(r.Context(), orgID, 500)
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": items, "total": len(items),
		"response_cache_runtime_ready": h.responseCacheRuntimeReady,
	})
}

func (h *Handler) GetInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if h.invocationOptimization == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	item, err := h.invocationOptimization.Get(r.Context(), orgID, strings.TrimSpace(r.PathValue("ref")))
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// GetInvocationOptimizationReadiness reports the exact technical Canary
// admission gates for one tenant-scoped policy revision. It is read-only and
// deliberately cannot grant activation or traffic authority.
func (h *Handler) GetInvocationOptimizationReadiness(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if h == nil || h.invocationOptimization == nil || h.db == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	policy, err := h.invocationOptimization.Get(r.Context(), orgID, strings.TrimSpace(r.PathValue("ref")))
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	result, err := h.invocationOptimizationCanaryReadiness(r.Context(), h.db, policy)
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	if h.invocationOptimization == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	var draft invocationopt.Draft
	if err := decodeStrictJSONBody(r, &draft); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), draft.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	draft.OrgID = orgID
	draft, err = invocationopt.NormalizeDraft(draft)
	if err != nil || (draft.ProjectID > 0 && !h.projectBelongsToOrg(r.Context(), draft.ProjectID, orgID)) {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	now := time.Now().UTC()
	item := invocationopt.Policy{
		ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageDraft, Revision: 1, CreatedBy: actor, UpdatedBy: actor,
		CreatedAt: now, UpdatedAt: now,
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `
INSERT INTO invocation_optimization_policies (
 contract_version,org_id,project_id,policy_ref,display_name,model_selector,region,residency,
 response_cache_target,response_cache_canary_basis_points,response_cache_ttl_seconds,
 response_cache_persistence_allowed,response_cache_require_request_opt_in,
 compression_target,compression_canary_basis_points,compression_require_request_opt_in,
 compression_minimum_input_tokens,compression_minimum_saved_tokens,
 compression_maximum_reduction_basis_points,minimum_shadow_samples,status,stage,revision,
 created_by,updated_by,created_at,updated_at)
VALUES ($1,$2,NULLIF($3,0),$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,1,$23,$23,$24,$24)
RETURNING id`, item.ContractVersion, draft.OrgID, draft.ProjectID, draft.PolicyRef,
		draft.DisplayName, draft.ModelSelector, draft.Region, draft.Residency,
		draft.ResponseCacheTarget, draft.ResponseCacheCanaryBPS, draft.ResponseCacheTTLSeconds,
		draft.ResponseCachePersistence, draft.ResponseCacheRequireOptIn,
		draft.CompressionTarget, draft.CompressionCanaryBPS, draft.CompressionRequireOptIn,
		draft.CompressionMinimumInputTokens, draft.CompressionMinimumSavedTokens,
		draft.CompressionMaximumReductionBPS, draft.MinimumShadowSamples, draft.Status,
		item.Stage, actor, now).Scan(&item.ID)
	if err != nil {
		writeInvocationOptimizationError(w, mutationInvocationOptimizationError(err))
		return
	}
	if err := insertInvocationOptimizationRevision(r, tx, item, "created"); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "invocation_optimization.create", "invocation_optimization_policy", item.PolicyRef,
		map[string]any{"after": item, "runtime_mutation": "none"}, requestAuditMeta(r, orgID, draft.ProjectID)); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	h.invocationOptimization.Invalidate()
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) UpdateInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.invocationOptimization == nil || h.db == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	ref := strings.TrimSpace(r.PathValue("ref"))
	current, err := h.invocationOptimization.Get(r.Context(), orgID, ref)
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	var request struct {
		invocationopt.Draft
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeStrictJSONBody(r, &request); err != nil || request.ExpectedRevision <= 0 {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	if current.Stage != invocationopt.StageDraft && current.Stage != invocationopt.StageRolledBack {
		writeInvocationOptimizationError(w, invocationopt.ErrConflict)
		return
	}
	draft := request.Draft
	draft.OrgID, draft.PolicyRef = orgID, ref
	draft, err = invocationopt.NormalizeDraft(draft)
	if err != nil || (draft.ProjectID > 0 && !h.projectBelongsToOrg(r.Context(), draft.ProjectID, orgID)) {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	next := current
	next.Draft, next.Stage, next.Revision = draft, invocationopt.StageDraft, current.Revision+1
	next.UpdatedBy, next.UpdatedAt, next.ValidatedAt = middleware.GetUserIDFromContext(r.Context()), time.Now().UTC(), nil
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `
UPDATE invocation_optimization_policies SET
 project_id=NULLIF($4,0),display_name=$5,model_selector=$6,region=$7,residency=$8,
 response_cache_target=$9,response_cache_canary_basis_points=$10,response_cache_ttl_seconds=$11,
 response_cache_persistence_allowed=$12,response_cache_require_request_opt_in=$13,
 compression_target=$14,compression_canary_basis_points=$15,compression_require_request_opt_in=$16,
 compression_minimum_input_tokens=$17,compression_minimum_saved_tokens=$18,
 compression_maximum_reduction_basis_points=$19,minimum_shadow_samples=$20,status=$21,
 stage='draft',revision=revision+1,validated_at=NULL,updated_by=$22,updated_at=$23
WHERE org_id=$1 AND policy_ref=$2 AND revision=$3 AND stage IN ('draft','rolled_back')`,
		orgID, ref, request.ExpectedRevision, draft.ProjectID, draft.DisplayName,
		draft.ModelSelector, draft.Region, draft.Residency, draft.ResponseCacheTarget,
		draft.ResponseCacheCanaryBPS, draft.ResponseCacheTTLSeconds, draft.ResponseCachePersistence,
		draft.ResponseCacheRequireOptIn, draft.CompressionTarget, draft.CompressionCanaryBPS,
		draft.CompressionRequireOptIn, draft.CompressionMinimumInputTokens,
		draft.CompressionMinimumSavedTokens, draft.CompressionMaximumReductionBPS,
		draft.MinimumShadowSamples, draft.Status, next.UpdatedBy, next.UpdatedAt)
	if err != nil {
		log.Printf("[invocation-optimization] draft update failed: %v", err)
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if result.RowsAffected() != 1 {
		writeInvocationOptimizationError(w, invocationopt.ErrConflict)
		return
	}
	if err := insertInvocationOptimizationRevision(r, tx, next, "draft_updated"); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "invocation_optimization.update", "invocation_optimization_policy", ref,
		map[string]any{"before": current, "after": next, "runtime_mutation": "none"}, requestAuditMeta(r, orgID, next.ProjectID)); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	h.invocationOptimization.Invalidate()
	writeJSON(w, http.StatusOK, next)
}

func (h *Handler) ValidateInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	h.transitionInvocationOptimization(w, r, "validate")
}

func (h *Handler) ShadowInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	h.transitionInvocationOptimization(w, r, "shadow")
}

func (h *Handler) CanaryInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	h.transitionInvocationOptimization(w, r, "canary")
}

func (h *Handler) RollbackInvocationOptimizationPolicy(w http.ResponseWriter, r *http.Request) {
	h.transitionInvocationOptimization(w, r, "rollback")
}

func (h *Handler) transitionInvocationOptimization(w http.ResponseWriter, r *http.Request, action string) {
	if h == nil || h.invocationOptimization == nil || h.db == nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	ref := strings.TrimSpace(r.PathValue("ref"))
	current, err := h.invocationOptimization.Get(r.Context(), orgID, ref)
	if err != nil {
		writeInvocationOptimizationError(w, err)
		return
	}
	var request invocationOptimizationTransition
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if request.ExpectedRevision <= 0 || len(reason) < 1 || len(reason) > 500 {
		writeInvocationOptimizationError(w, invocationopt.ErrInvalid)
		return
	}
	nextStage := ""
	hasTarget := current.ResponseCacheTarget == invocationopt.TargetCanary ||
		current.CompressionTarget == invocationopt.TargetCanary
	switch action {
	case "validate":
		if current.Stage == invocationopt.StageDraft && current.Status == "active" && hasTarget {
			nextStage = invocationopt.StageValidated
		}
	case "shadow":
		if current.Stage == invocationopt.StageValidated && current.Status == "active" && hasTarget {
			nextStage = invocationopt.StageShadow
		}
	case "canary":
		if current.Stage == invocationopt.StageShadow && current.Status == "active" && hasTarget {
			nextStage = invocationopt.StageCanary
		}
	case "rollback":
		if current.Stage == invocationopt.StageShadow || current.Stage == invocationopt.StageCanary {
			nextStage = invocationopt.StageRolledBack
		}
	}
	if nextStage == "" || request.ExpectedRevision != current.Revision {
		writeInvocationOptimizationError(w, invocationopt.ErrConflict)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	defer tx.Rollback(r.Context())
	if action == "canary" {
		if err := h.requireInvocationOptimizationCanaryEvidence(r, tx, current); err != nil {
			writeInvocationOptimizationError(w, err)
			return
		}
	}
	next := current
	next.Stage, next.Revision, next.UpdatedBy, next.UpdatedAt = nextStage, current.Revision+1, middleware.GetUserIDFromContext(r.Context()), time.Now().UTC()
	if nextStage == invocationopt.StageValidated {
		validated := next.UpdatedAt
		next.ValidatedAt = &validated
	}
	result, err := tx.Exec(r.Context(), `
UPDATE invocation_optimization_policies
   SET stage=$4::varchar,revision=revision+1,updated_by=$5,updated_at=$6::timestamptz,
       validated_at=CASE WHEN $4::varchar='validated'::varchar THEN $6::timestamptz ELSE validated_at END
 WHERE org_id=$1 AND policy_ref=$2 AND revision=$3`, orgID, ref, request.ExpectedRevision,
		nextStage, next.UpdatedBy, next.UpdatedAt)
	if err != nil {
		log.Printf("[invocation-optimization] transition update failed: %v", err)
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if result.RowsAffected() != 1 {
		writeInvocationOptimizationError(w, invocationopt.ErrConflict)
		return
	}
	if err := insertInvocationOptimizationRevision(r, tx, next, reason); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	runtimeMutation := "none"
	if nextStage == invocationopt.StageCanary || nextStage == invocationopt.StageRolledBack {
		runtimeMutation = "bounded_request_assignment"
	}
	if err := insertAuditEntry(r.Context(), tx, "invocation_optimization."+action, "invocation_optimization_policy", ref,
		map[string]any{"before": current, "after": next, "runtime_mutation": runtimeMutation}, requestAuditMeta(r, orgID, next.ProjectID)); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeInvocationOptimizationError(w, invocationopt.ErrUnavailable)
		return
	}
	h.invocationOptimization.Invalidate()
	writeJSON(w, http.StatusOK, next)
}

func (h *Handler) requireInvocationOptimizationCanaryEvidence(r *http.Request, tx pgx.Tx, policy invocationopt.Policy) error {
	result, err := h.invocationOptimizationCanaryReadiness(r.Context(), tx, policy)
	if err != nil {
		return err
	}
	if result.CanaryAdmissionReady {
		return nil
	}
	for _, item := range result.Blockers {
		if item.Code == "response_cache_runtime_unavailable" {
			return fmt.Errorf("%w: response-cache key backend is unavailable", invocationopt.ErrUnavailable)
		}
	}
	code := "readiness_unavailable"
	if len(result.Blockers) > 0 {
		code = result.Blockers[0].Code
	}
	return fmt.Errorf("%w: canary readiness blocked by %s", invocationopt.ErrConflict, code)
}

type invocationOptimizationEvidenceQuerier interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
}

func (h *Handler) invocationOptimizationCanaryReadiness(
	ctx context.Context,
	querier invocationOptimizationEvidenceQuerier,
	policy invocationopt.Policy,
) (invocationopt.CanaryReadinessResult, error) {
	evidence := map[string]invocationopt.ShadowEvidence{
		invocationopt.DriverResponseCache: {},
		invocationopt.DriverCompression:   {},
	}
	if policy.Stage == invocationopt.StageShadow {
		rows, err := querier.Query(ctx, `
SELECT driver,COUNT(*),COUNT(*) FILTER (WHERE eligible),
       COUNT(*) FILTER (WHERE mutation_applied OR assigned OR
         (eligible AND (NOT safety_passed OR NOT quality_passed)))
  FROM invocation_optimization_receipts
 WHERE org_id=$1 AND policy_ref=$2 AND policy_revision=$3
   AND driver IN ('response_cache','compression')
   AND stage='shadow' AND effective_mode='shadow'
 GROUP BY driver`, policy.OrgID, policy.PolicyRef, policy.Revision)
		if err != nil {
			return invocationopt.CanaryReadinessResult{}, invocationopt.ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var driver string
			var item invocationopt.ShadowEvidence
			if err := rows.Scan(&driver, &item.Samples, &item.Eligible, &item.Failures); err != nil {
				return invocationopt.CanaryReadinessResult{}, invocationopt.ErrUnavailable
			}
			evidence[driver] = item
		}
		if err := rows.Err(); err != nil {
			return invocationopt.CanaryReadinessResult{}, invocationopt.ErrUnavailable
		}
	}
	return invocationopt.ResolveCanaryReadiness(invocationopt.CanaryReadinessInput{
		Policy: policy, ResponseCacheRuntimeReady: h.responseCacheRuntimeReady, Evidence: evidence,
	}), nil
}

func insertInvocationOptimizationRevision(r *http.Request, tx pgx.Tx, item invocationopt.Policy, reason string) error {
	snapshot, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = tx.Exec(r.Context(), `
INSERT INTO invocation_optimization_policy_revisions
 (org_id,policy_ref,revision,stage,snapshot,actor_user_id,reason,created_at)
VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8)`, item.OrgID, item.PolicyRef, item.Revision,
		item.Stage, snapshot, middleware.GetUserIDFromContext(r.Context()), reason, item.UpdatedAt)
	return err
}

func mutationInvocationOptimizationError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
		return invocationopt.ErrConflict
	}
	return invocationopt.ErrUnavailable
}

func writeInvocationOptimizationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invocationopt.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invocation optimization policy is invalid", "message": err.Error()})
	case errors.Is(err, invocationopt.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Invocation optimization policy not found"})
	case errors.Is(err, invocationopt.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Invocation optimization policy state conflict", "message": err.Error()})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Invocation optimization policy service unavailable", "message": err.Error()})
	}
}
