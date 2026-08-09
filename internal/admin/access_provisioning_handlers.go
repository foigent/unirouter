package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) accessProvisioningService() (*accessprovisioning.Service, error) {
	authorizer := accessprovisioning.CommitAuthorizerFunc(
		func(ctx context.Context, tx pgx.Tx, input accessprovisioning.AuthorizationRequirement) (accessprovisioning.Actor, error) {
			requirement := committingActorRequirement{
				TargetOrgID:                             input.OrgID,
				AllowedOrganizationRoles:                input.AllowedOrganizationRoles,
				AllowSelfRequestWithoutMembershipUserID: input.AllowSelfRequestWithoutMembershipUserID,
				AdditionalUserIDs:                       input.AdditionalUserIDs,
				AdditionalWorkloadIDs:                   input.AdditionalWorkloadIDs,
			}
			if input.ProjectID > 0 {
				requirement.Project = &committingActorProjectRequirement{
					ProjectID: input.ProjectID, AllowedRoles: input.AllowedProjectRoles,
					MustBeActive: !input.AllowInactiveProject,
				}
			}
			state, err := h.reauthorizeCommittingActorTx(ctx, tx, requirement)
			if err != nil {
				if errors.Is(err, errCommittingActorScopeNotFound) {
					return accessprovisioning.Actor{}, accessprovisioning.ErrNotFound
				}
				if errors.Is(err, errCommittingActorAuthorizationChanged) {
					return accessprovisioning.Actor{}, accessprovisioning.ErrAuthorizationChanged
				}
				return accessprovisioning.Actor{}, err
			}
			return accessprovisioning.Actor{
				UserID: state.UserID, OrgID: state.OrgID, OrganizationRole: state.OrganizationRole,
				ProjectRole: state.ProjectRole,
			}, nil
		},
	)
	memberships := accessprovisioning.MembershipProvisionerFunc(h.ensureAccessProjectMembershipTx)
	return accessprovisioning.NewService(h.db, h.apiKeyHashSecret, authorizer, memberships)
}

// ensureAccessProjectMembershipTx is the access-provisioning adapter for the
// canonical project membership invariants. The shared commit authorizer has
// already locked the actor, admitted session, target user lane, and project.
func (h *Handler) ensureAccessProjectMembershipTx(
	ctx context.Context,
	tx pgx.Tx,
	actor accessprovisioning.Actor,
	orgID, projectID, userID int64,
	requestedRole string,
) (accessprovisioning.MembershipChange, error) {
	role, ok := auth.NormalizeProjectMembershipRole(requestedRole)
	if !ok || role == auth.ProjectRoleViewer {
		return accessprovisioning.MembershipChange{}, accessprovisioning.ErrInvalid
	}

	var targetStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 AND org_id=$2 FOR UPDATE`, userID, orgID).Scan(&targetStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accessprovisioning.MembershipChange{}, accessprovisioning.ErrNotFound
		}
		return accessprovisioning.MembershipChange{}, err
	}
	if targetStatus != "active" {
		return accessprovisioning.MembershipChange{}, accessprovisioning.ErrConflict
	}

	var existing string
	err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2 FOR UPDATE`, projectID, userID).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return accessprovisioning.MembershipChange{}, err
	}
	hasExisting := err == nil
	if hasExisting {
		existing = canonicalProjectMembershipRole(existing)
	}
	change := accessprovisioning.MembershipChange{BeforeRole: existing, AfterRole: role}
	// An already-satisfied self request is a no-op, not a delegated
	// membership mutation. Apply administrative authority only when the
	// approval would actually add or elevate membership.
	if hasExisting && accessProjectRoleRank(existing) >= accessProjectRoleRank(role) {
		change.AfterRole = existing
		return change, nil
	}
	organizationWide := auth.RoleIn(actor.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	if !organizationWide {
		if actor.ProjectRole != auth.ProjectRoleAdmin || actor.UserID == userID {
			return accessprovisioning.MembershipChange{}, accessprovisioning.ErrForbidden
		}
		if role == auth.ProjectRoleAdmin || (hasExisting && existing == auth.ProjectRoleAdmin) {
			return accessprovisioning.MembershipChange{}, accessprovisioning.ErrForbidden
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role,created_at,updated_at)
		VALUES ($1,$2,$3,NOW(),NOW())
		ON CONFLICT (project_id,user_id) DO UPDATE
		SET role=EXCLUDED.role,updated_at=NOW()`, projectID, userID, role); err != nil {
		return accessprovisioning.MembershipChange{}, err
	}
	transition, err := syncUserCompatibilityRoleTx(ctx, tx, userID)
	if err != nil {
		return accessprovisioning.MembershipChange{}, err
	}
	change.Changed = true
	change.CompatibilityBefore = transition.BeforeRole
	change.CompatibilityAfter = transition.AfterRole
	change.RevokedSessions = transition.RevokedSessions
	return change, nil
}

func accessProjectRoleRank(role string) int {
	switch canonicalProjectMembershipRole(role) {
	case auth.ProjectRoleAdmin:
		return 3
	case auth.ProjectRoleUser:
		return 2
	case auth.ProjectRoleViewer:
		return 1
	default:
		return 0
	}
}

func (h *Handler) CreateAccessRequest(w http.ResponseWriter, r *http.Request) {
	var input accessprovisioning.ProvisionInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	if input.SubmissionMode == accessprovisioning.SubmissionMigration ||
		input.SubmissionMode == accessprovisioning.SubmissionBootstrap ||
		input.SubmissionMode == accessprovisioning.SubmissionRecovery {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	clientRequestID := middleware.GetClientRequestIDFromContext(r.Context())
	if clientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	orgID, err := resolveAccessProvisioningOrg(r, input.OrgID)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	input.OrgID = orgID
	input.ClientRequestID = clientRequestID
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	result, err := service.Provision(r.Context(), input)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) ListAccessRequests(w http.ResponseWriter, r *http.Request) {
	pageInput, err := accessProvisioningPageInput(r)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	scopeMode := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scopeMode == "my" {
		if strings.TrimSpace(r.URL.Query().Get("project_id")) != "" || strings.TrimSpace(r.URL.Query().Get("org_id")) != "" ||
			strings.TrimSpace(r.URL.Query().Get("status")) != "" || strings.TrimSpace(r.URL.Query().Get("workload_application_id")) != "" {
			writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
			return
		}
		service, err := h.accessProvisioningService()
		if err != nil {
			writeAccessProvisioningUnavailable(w)
			return
		}
		page, err := service.ListMyRequests(r.Context(), accessActorScope(r.Context()), pageInput)
		if err != nil {
			writeAccessProvisioningError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if scopeMode != "" && scopeMode != "project" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	orgID, projectID, ok := h.resolveAccessProjectRead(w, r)
	if !ok {
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	workloadID, err := optionalPositiveInt64Query(r, "workload_application_id")
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	page, err := service.ListRequestsFiltered(r.Context(), accessActorScope(r.Context()), orgID, projectID,
		accessprovisioning.RequestListFilter{
			Status:                       strings.TrimSpace(r.URL.Query().Get("status")),
			SubjectWorkloadApplicationID: workloadID,
		}, pageInput)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) GetAccessRequest(w http.ResponseWriter, r *http.Request) {
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	request, grant, err := service.GetRequest(r.Context(), accessActorScope(r.Context()), r.PathValue("ref"))
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": request, "grant": grant})
}

func (h *Handler) DecideAccessRequest(w http.ResponseWriter, r *http.Request) {
	var input accessprovisioning.DecisionInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	input.RequestRef = r.PathValue("ref")
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	input.ClientRequestID = middleware.GetClientRequestIDFromContext(r.Context())
	if input.ClientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	result, err := service.Decide(r.Context(), accessActorScope(r.Context()), input)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CancelAccessRequest(w http.ResponseWriter, r *http.Request) {
	var input accessprovisioning.CancelRequestInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	input.RequestRef = r.PathValue("ref")
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	input.ClientRequestID = middleware.GetClientRequestIDFromContext(r.Context())
	if input.ClientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	result, err := service.CancelRequest(r.Context(), accessActorScope(r.Context()), input)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListAccessGrants(w http.ResponseWriter, r *http.Request) {
	pageInput, err := accessProvisioningPageInput(r)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	scopeMode := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scopeMode == "my" {
		if strings.TrimSpace(r.URL.Query().Get("project_id")) != "" || strings.TrimSpace(r.URL.Query().Get("org_id")) != "" ||
			strings.TrimSpace(r.URL.Query().Get("workload_application_id")) != "" {
			writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
			return
		}
		page, err := service.ListMyGrants(r.Context(), accessActorScope(r.Context()), pageInput)
		if err != nil {
			writeAccessProvisioningError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if scopeMode != "" && scopeMode != "project" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	orgID, projectID, ok := h.resolveAccessProjectRead(w, r)
	if !ok {
		return
	}
	workloadID, err := optionalPositiveInt64Query(r, "workload_application_id")
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	page, err := service.ListGrantsFiltered(r.Context(), accessActorScope(r.Context()), orgID, projectID,
		accessprovisioning.GrantListFilter{SubjectWorkloadApplicationID: workloadID}, pageInput)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func optionalPositiveInt64Query(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, accessprovisioning.ErrInvalid
	}
	return value, nil
}

func (h *Handler) GetAccessGrant(w http.ResponseWriter, r *http.Request) {
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	grant, err := service.GetGrant(r.Context(), accessActorScope(r.Context()), r.PathValue("ref"))
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (h *Handler) ListAccessGrantCredentials(w http.ResponseWriter, r *http.Request) {
	pageInput, err := accessProvisioningPageInput(r)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	page, err := service.ListGrantCredentials(r.Context(), accessActorScope(r.Context()), r.PathValue("ref"), pageInput)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func accessProvisioningPageInput(r *http.Request) (accessprovisioning.PageInput, error) {
	input := accessprovisioning.PageInput{}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 1 || value > accessprovisioning.MaxPageLimit {
			return accessprovisioning.PageInput{}, accessprovisioning.ErrInvalid
		}
		input.Limit = int(value)
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("after_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return accessprovisioning.PageInput{}, accessprovisioning.ErrInvalid
		}
		input.AfterID = value
	}
	return accessprovisioning.NormalizePageInput(input)
}

func (h *Handler) SuspendAccessGrant(w http.ResponseWriter, r *http.Request) {
	h.changeAccessGrantLifecycle(w, r, "suspend")
}

func (h *Handler) ResumeAccessGrant(w http.ResponseWriter, r *http.Request) {
	h.changeAccessGrantLifecycle(w, r, "resume")
}

func (h *Handler) RevokeAccessGrant(w http.ResponseWriter, r *http.Request) {
	h.changeAccessGrantLifecycle(w, r, "revoke")
}

func (h *Handler) changeAccessGrantLifecycle(w http.ResponseWriter, r *http.Request, action string) {
	var input accessprovisioning.GrantLifecycleInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	input.GrantRef, input.Action = r.PathValue("ref"), action
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	input.ClientRequestID = middleware.GetClientRequestIDFromContext(r.Context())
	if input.ClientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	grant, err := service.ChangeGrantLifecycle(r.Context(), accessActorScope(r.Context()), input)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (h *Handler) IssueAccessGrantCredential(w http.ResponseWriter, r *http.Request) {
	h.issueAccessGrantCredential(w, r)
}

func (h *Handler) RotateAccessGrantCredential(w http.ResponseWriter, r *http.Request) {
	var input accessprovisioning.RotateCredentialInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	pathKeyID, err := strconv.ParseInt(r.PathValue("key_id"), 10, 64)
	if err != nil || pathKeyID <= 0 {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	input.PredecessorKeyID = pathKeyID
	input.GrantRef = r.PathValue("ref")
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	input.ClientRequestID = middleware.GetClientRequestIDFromContext(r.Context())
	if input.ClientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	result, err := service.RotateCredential(r.Context(), accessActorScope(r.Context()), input)
	if err != nil {
		writeCredentialCreationError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) issueAccessGrantCredential(w http.ResponseWriter, r *http.Request) {
	var input accessprovisioning.CredentialInput
	if err := decodeStrictJSONBody(r, &input); err != nil {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	input.GrantRef = r.PathValue("ref")
	input.RequestID = middleware.GetRequestIDFromContext(r.Context())
	input.ClientRequestID = middleware.GetClientRequestIDFromContext(r.Context())
	if input.ClientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	credential, err := service.IssueCredential(r.Context(), accessActorScope(r.Context()), input)
	if err != nil {
		writeCredentialCreationError(w, err)
		return
	}
	status := http.StatusCreated
	if credential.Idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, credential)
}

func (h *Handler) RevokeAccessGrantCredential(w http.ResponseWriter, r *http.Request) {
	keyID, err := strconv.ParseInt(r.PathValue("key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	clientRequestID := middleware.GetClientRequestIDFromContext(r.Context())
	if clientRequestID == "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return
	}
	err = service.RevokeCredential(r.Context(), accessActorScope(r.Context()), r.PathValue("ref"), keyID,
		middleware.GetRequestIDFromContext(r.Context()), clientRequestID)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}

func (h *Handler) PreviewLegacyAccessClassification(w http.ResponseWriter, r *http.Request) {
	requestedOrg, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("org_id")), 10, 64)
	orgID, err := resolveAccessProvisioningOrg(r, requestedOrg)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return
	}
	service, err := h.accessProvisioningService()
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	items, err := service.LegacyPreview(r.Context(), orgID)
	if err != nil {
		writeAccessProvisioningUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mutation": false, "data": items, "total": len(items)})
}

func (h *Handler) resolveAccessProjectRead(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	requestedOrg, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("org_id")), 10, 64)
	if err != nil && strings.TrimSpace(r.URL.Query().Get("org_id")) != "" {
		writeAccessProvisioningError(w, accessprovisioning.ErrInvalid)
		return 0, 0, false
	}
	orgID, err := resolveAccessProvisioningOrg(r, requestedOrg)
	if err != nil {
		writeAccessProvisioningError(w, err)
		return 0, 0, false
	}
	projectID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("project_id")), 10, 64)
	if err != nil || projectID <= 0 || !h.canReadAccessProject(r.Context(), orgID, projectID) {
		writeAccessProvisioningError(w, accessprovisioning.ErrNotFound)
		return 0, 0, false
	}
	return orgID, projectID, true
}

func (h *Handler) canReadAccessProject(ctx context.Context, orgID, projectID int64) bool {
	if projectID <= 0 || !h.canReadProject(ctx, projectID) {
		return false
	}
	if actorOrganizationRole(ctx) == auth.RolePlatformAdmin {
		return true
	}
	return middleware.GetOrgIDFromContext(ctx) == orgID
}

func resolveAccessProvisioningOrg(r *http.Request, requested int64) (int64, error) {
	actorOrgID := middleware.GetOrgIDFromContext(r.Context())
	if actorOrganizationRole(r.Context()) == auth.RolePlatformAdmin {
		if requested > 0 {
			return requested, nil
		}
		if actorOrgID > 0 {
			return actorOrgID, nil
		}
		return 0, accessprovisioning.ErrInvalid
	}
	if actorOrgID <= 0 || (requested > 0 && requested != actorOrgID) {
		return 0, accessprovisioning.ErrNotFound
	}
	return actorOrgID, nil
}

func accessActorScope(ctx context.Context) accessprovisioning.ActorScope {
	return accessprovisioning.ActorScope{
		UserID: middleware.GetUserIDFromContext(ctx), OrgID: middleware.GetOrgIDFromContext(ctx),
		OrganizationRole: actorOrganizationRole(ctx),
	}
}

func writeAccessProvisioningError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accessprovisioning.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid access request", "reason": "invalid_request"})
	case errors.Is(err, accessprovisioning.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Access object not found", "reason": "not_found"})
	case errors.Is(err, accessprovisioning.ErrForbidden), errors.Is(err, accessprovisioning.ErrAuthorizationChanged):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Access forbidden", "reason": "forbidden"})
	case errors.Is(err, accessprovisioning.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Access state conflict", "reason": "state_conflict"})
	default:
		writeAccessProvisioningUnavailable(w)
	}
}

func writeCredentialCreationError(w http.ResponseWriter, err error) {
	if writeCommercialLimitViolation(w, err) {
		return
	}
	if errors.Is(err, accessprovisioning.ErrCredentialNameConflict) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "Credential name already exists", "reason": "credential_name_conflict",
		})
		return
	}
	writeAccessProvisioningError(w, err)
}

func writeAccessProvisioningUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Access provisioning unavailable", "reason": "service_unavailable"})
}
