package admin

import (
	"context"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

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

func (h *Handler) canReadProject(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	role := actorRole(ctx)
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin {
		return true
	}
	if projectID == 0 {
		return false
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
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin {
		return true
	}
	if projectID == 0 {
		return false
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
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin {
		return true
	}
	if projectID == 0 {
		return false
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
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin || role == auth.RoleBillingAdmin {
		return true
	}
	if projectID == 0 {
		return false
	}
	if !auth.IsProjectScopedRole(role) {
		return false
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin, projectMemberDeveloper, projectMemberViewer, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) hasProjectMembership(ctx context.Context, projectID int64, roles ...string) bool {
	userID := middleware.GetUserIDFromContext(ctx)
	if userID == 0 || projectID == 0 {
		return false
	}

	var count int
	err := h.db.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_members
		 WHERE project_id = $1
		   AND user_id = $2
		   AND role = ANY($3::text[])`,
		projectID, userID, roles,
	).Scan(&count)
	return err == nil && count > 0
}
