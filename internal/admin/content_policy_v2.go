package admin

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
)

type scopedContentPolicyController interface {
	EnterpriseManagementTx(context.Context, pgx.Tx, int64, string) (contentcapture.ScopedManagementView, error)
	PutEnterpriseTx(context.Context, pgx.Tx, int64, contentcapture.ScopedManagementPut, contentcapture.PolicyApplication) (contentcapture.ScopedManagementView, error)
	ScopedManagementTx(context.Context, pgx.Tx, int64, int64, int64, string) (contentcapture.ScopedManagementView, error)
	PutScopedTx(context.Context, pgx.Tx, string, int64, int64, int64, contentcapture.ScopedManagementPut, contentcapture.PolicyApplication) (contentcapture.ScopedManagementView, error)
}

func (h *Handler) GetEnterprisePayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.enterprisePayloadPolicyV2(w, r, false)
}
func (h *Handler) PutEnterprisePayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.enterprisePayloadPolicyV2(w, r, true)
}
func (h *Handler) GetProjectPayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicyV2(w, r, contentcapture.PolicyScopeProject, false)
}
func (h *Handler) PutProjectPayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicyV2(w, r, contentcapture.PolicyScopeProject, true)
}
func (h *Handler) GetUserPayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicyV2(w, r, contentcapture.PolicyScopeUser, false)
}
func (h *Handler) PutUserPayloadPolicyV2(w http.ResponseWriter, r *http.Request) {
	h.projectPayloadPolicyV2(w, r, contentcapture.PolicyScopeUser, true)
}

func (h *Handler) scopedPolicyReady(w http.ResponseWriter, r *http.Request) (scopedContentPolicyController, contentcapture.PolicyApplication, bool) {
	w.Header().Set("Cache-Control", "no-store")
	controller, ok := h.contentPolicyController.(scopedContentPolicyController)
	if !ok || h.contentPolicyReadiness == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Content policy is not configured"})
		return nil, contentcapture.PolicyApplication{}, false
	}
	if middleware.GetUserIDFromContext(r.Context()) <= 0 || middleware.GetUserRoleFromContext(r.Context()) == "" {
		writeContentPolicyError(w, errCommittingActorAuthorizationChanged, false)
		return nil, contentcapture.PolicyApplication{}, false
	}
	application, err := h.contentPolicyReadiness(r.Context())
	if err != nil {
		writeContentPolicyError(w, err, false)
		return nil, contentcapture.PolicyApplication{}, false
	}
	application.Now = time.Now().UTC()
	return controller, application, true
}

func decodeScopedPolicyBody(w http.ResponseWriter, r *http.Request) (contentcapture.ScopedManagementPut, bool) {
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contentcapture.MaxPolicyJSONBytes))
	if err != nil {
		writeContentPolicyError(w, contentcapture.ErrInvalidPolicy, true)
		return contentcapture.ScopedManagementPut{}, false
	}
	put, err := contentcapture.DecodeScopedManagementPut(wire)
	if err != nil {
		writeContentPolicyError(w, err, true)
		return contentcapture.ScopedManagementPut{}, false
	}
	return put, true
}

func (h *Handler) enterprisePayloadPolicyV2(w http.ResponseWriter, r *http.Request, update bool) {
	controller, application, ok := h.scopedPolicyReady(w, r)
	if !ok {
		return
	}
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	var put contentcapture.ScopedManagementPut
	if update {
		put, ok = decodeScopedPolicyBody(w, r)
		if !ok {
			return
		}
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	defer tx.Rollback(r.Context())
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
		TargetOrgID: orgID, OrganizationForUpdate: update,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
	})
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	var view contentcapture.ScopedManagementView
	if update {
		view, err = h.putScopedPolicyForActorTx(r.Context(), tx, controller, contentcapture.PolicyScopeEnterprise, actor.TargetOrgID, 0, 0, put, application, actor.UserID, requestAuditMeta(r, orgID, 0))
	} else {
		view, err = controller.EnterpriseManagementTx(r.Context(), tx, actor.TargetOrgID, application.StorageStatus)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeContentPolicyError(w, err, update)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) projectPayloadPolicyV2(w http.ResponseWriter, r *http.Request, scope string, update bool) {
	controller, application, ok := h.scopedPolicyReady(w, r)
	if !ok {
		return
	}
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	var userID int64
	if scope == contentcapture.PolicyScopeUser {
		userID, ok = parsePositivePathID(w, r, "user_id", "Invalid user ID")
		if !ok {
			return
		}
	}
	var put contentcapture.ScopedManagementPut
	if update {
		put, ok = decodeScopedPolicyBody(w, r)
		if !ok {
			return
		}
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	defer tx.Rollback(r.Context())
	requirement := committingActorRequirement{
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		Project: &committingActorProjectRequirement{ProjectID: projectID,
			AllowedRoles: []string{projectMemberAdmin}, MustBeActive: update},
	}
	if userID > 0 {
		requirement.AdditionalUserIDs = []int64{userID}
	}
	actor, err := h.reauthorizeCommittingActorTx(r.Context(), tx, requirement)
	if err != nil {
		writeContentPolicyError(w, err, false)
		return
	}
	var view contentcapture.ScopedManagementView
	if update {
		view, err = h.putScopedPolicyForActorTx(r.Context(), tx, controller, scope, actor.TargetOrgID, projectID, userID, put, application, actor.UserID, requestAuditMeta(r, actor.TargetOrgID, projectID))
	} else {
		view, err = controller.ScopedManagementTx(r.Context(), tx, actor.TargetOrgID, projectID, userID, application.StorageStatus)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeContentPolicyError(w, err, update)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) putScopedPolicyForActorTx(ctx context.Context, tx pgx.Tx, controller scopedContentPolicyController, scope string, orgID, projectID, userID int64, put contentcapture.ScopedManagementPut, application contentcapture.PolicyApplication, actorID int64, meta auditMeta) (contentcapture.ScopedManagementView, error) {
	var view contentcapture.ScopedManagementView
	var err error
	if scope == contentcapture.PolicyScopeEnterprise {
		view, err = controller.PutEnterpriseTx(ctx, tx, orgID, put, application)
		if err == nil {
			err = insertAuditEntryForActor(ctx, tx, actorID, "content.policy.enterprise.update", "organization", strconv.FormatInt(orgID, 10), map[string]any{"policy_ref": view.Enterprise.PolicyRef, "desired": view.Enterprise.Desired, "effective": view.Enterprise.Effective, "execution_storage_mode": view.Enterprise.EffectiveExecutionMode, "default_project_quota_bytes": view.Enterprise.EffectiveDefaultProjectQuota}, meta)
		}
	} else {
		view, err = controller.PutScopedTx(ctx, tx, scope, orgID, projectID, userID, put, application)
		if err == nil {
			err = insertAuditEntryForActor(ctx, tx, actorID, "content.policy.scoped.update", scope, strconv.FormatInt(projectID, 10), map[string]any{"scope": scope, "policy_ref": put.ExpectedPolicyRef, "desired": put.Desired, "execution_storage_mode": put.ExecutionStorageMode, "project_quota_bytes": put.ProjectQuotaBytes}, meta)
		}
	}
	return view, err
}
