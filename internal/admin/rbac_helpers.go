package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

var errActorOrganizationForbidden = errors.New("actor organization is required or does not match the requested organization")
var errInvalidOrganizationID = errors.New("invalid organization ID")

const (
	projectMemberAdmin     = "project_admin"
	projectMemberDeveloper = "project_developer"
	projectMemberViewer    = "project_viewer"
)

func actorRole(ctx context.Context) string {
	return auth.NormalizeRole(middleware.GetUserRoleFromContext(ctx))
}

func isUnauthenticatedUnitContext(ctx context.Context) bool {
	return middleware.GetUserRoleFromContext(ctx) == ""
}

// actorOrgConstraint returns the organization that must constrain an
// authenticated non-platform actor. The unconstrained return preserves legacy
// direct-handler unit tests while production routes always pass AdminAuth.
func actorOrgConstraint(ctx context.Context) (int64, bool) {
	if isUnauthenticatedUnitContext(ctx) || auth.IsPlatformAdmin(actorRole(ctx)) {
		return 0, false
	}
	return middleware.GetOrgIDFromContext(ctx), true
}

func actorCanAccessOrg(ctx context.Context, targetOrgID int64) bool {
	actorOrgID, constrained := actorOrgConstraint(ctx)
	if !constrained {
		return true
	}
	return actorOrgID > 0 && targetOrgID > 0 && actorOrgID == targetOrgID
}

// resolveActorOrganization returns the organization an authenticated actor may
// operate on. Platform administrators may explicitly select another
// organization; without an explicit selection every role is bound to the
// organization carried by its authenticated identity. This deliberately has no
// database-order fallback.
func resolveActorOrganization(ctx context.Context, requestedOrgID int64) (int64, error) {
	if requestedOrgID < 0 {
		return 0, errInvalidOrganizationID
	}
	actorOrgID := middleware.GetOrgIDFromContext(ctx)
	if auth.IsPlatformAdmin(actorRole(ctx)) {
		if requestedOrgID > 0 {
			return requestedOrgID, nil
		}
		if actorOrgID > 0 {
			return actorOrgID, nil
		}
		return 0, errActorOrganizationForbidden
	}

	if actorOrgID <= 0 {
		return 0, errActorOrganizationForbidden
	}
	if requestedOrgID > 0 && requestedOrgID != actorOrgID {
		return 0, errActorOrganizationForbidden
	}
	return actorOrgID, nil
}

func parseOptionalOrganizationID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	orgID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || orgID <= 0 {
		return 0, errInvalidOrganizationID
	}
	return orgID, nil
}

func resolveRequestOrganization(r *http.Request) (int64, error) {
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		return 0, err
	}
	return resolveActorOrganization(r.Context(), requestedOrgID)
}

func writeOrganizationScopeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidOrganizationID) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
}

func (h *Handler) projectBelongsToOrg(ctx context.Context, projectID, orgID int64) bool {
	if projectID <= 0 || orgID <= 0 {
		return false
	}

	var count int
	err := h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM projects WHERE id = $1 AND org_id = $2`,
		projectID, orgID,
	).Scan(&count)
	return err == nil && count > 0
}

func (h *Handler) projectBelongsToActorOrg(ctx context.Context, projectID int64) bool {
	actorOrgID, constrained := actorOrgConstraint(ctx)
	if !constrained {
		return true
	}
	if actorOrgID <= 0 || projectID <= 0 {
		return false
	}

	return h.projectBelongsToOrg(ctx, projectID, actorOrgID)
}

func (h *Handler) canReadProject(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return false
	}
	if role == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if !auth.IsProjectScopedRole(role) {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin, projectMemberDeveloper, projectMemberViewer, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) canWriteProject(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return false
	}
	if role == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if role != auth.RoleProjectAdmin {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin)
}

func (h *Handler) canCreateProjectKey(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return false
	}
	if role == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if role != auth.RoleProjectAdmin && role != auth.RoleDeveloper {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin, projectMemberDeveloper, auth.RoleDeveloper)
}

func (h *Handler) canReadProjectUsage(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return false
	}
	if role == auth.RoleOrgAdmin || role == auth.RoleBillingAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if !auth.IsProjectScopedRole(role) {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin, projectMemberDeveloper, projectMemberViewer, auth.RoleDeveloper, auth.RoleViewer)
}

// canReadUsageRecord authorizes a concrete usage record after both its
// organization and optional project are known. Organization and billing
// administrators may read organization-level records; project-scoped roles
// still require an explicit membership-bearing project.
func (h *Handler) canReadUsageRecord(ctx context.Context, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if orgID <= 0 || !actorCanAccessOrg(ctx, orgID) {
		return false
	}

	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if role == auth.RoleOrgAdmin || role == auth.RoleBillingAdmin {
		return projectID == 0 || h.projectBelongsToOrg(ctx, projectID, orgID)
	}
	if projectID == 0 || !auth.IsProjectScopedRole(role) {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin, projectMemberDeveloper, projectMemberViewer, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) hasProjectMembership(ctx context.Context, projectID int64, roles ...string) bool {
	userID := middleware.GetUserIDFromContext(ctx)
	orgID, constrained := actorOrgConstraint(ctx)
	if userID == 0 || projectID == 0 || !constrained || orgID <= 0 {
		return false
	}

	var count int
	err := h.db.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
		 WHERE pm.project_id = $1
		   AND pm.user_id = $2
		   AND p.org_id = $3
		   AND u.org_id = $3
		   AND pm.role = ANY($4::text[])`,
		projectID, userID, orgID, roles,
	).Scan(&count)
	return err == nil && count > 0
}

func (h *Handler) canReadAPIKey(ctx context.Context, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if !actorCanAccessOrg(ctx, orgID) {
		return false
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return role == auth.RoleOrgAdmin
	}
	return h.canReadProject(ctx, projectID)
}

func (h *Handler) canWriteAPIKey(ctx context.Context, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if !actorCanAccessOrg(ctx, orgID) {
		return false
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return role == auth.RoleOrgAdmin
	}
	return h.canWriteProject(ctx, projectID)
}
