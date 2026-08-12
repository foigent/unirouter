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
	projectMemberAdmin           = auth.ProjectRoleAdmin
	projectMemberUser            = auth.ProjectRoleUser
	projectMemberDeveloper       = projectMemberUser // compatibility symbol; persisted value is project_user
	projectMemberViewer          = auth.ProjectRoleViewer
	projectMemberLegacyDeveloper = auth.LegacyProjectRoleDeveloper
)

func actorOrganizationRole(ctx context.Context) string {
	role := middleware.GetOrganizationRoleFromContext(ctx)
	if role != "" {
		return role
	}
	// Compatibility for direct-handler tests and accepted v2 JWTs.
	canonical, _ := auth.NormalizeOrganizationRole(middleware.GetUserRoleFromContext(ctx))
	return canonical
}

func actorHasOrganizationUsageScope(ctx context.Context) bool {
	return auth.RoleIn(actorOrganizationRole(ctx),
		auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleOrgViewer)
}

func isUnauthenticatedUnitContext(ctx context.Context) bool {
	return middleware.GetUserRoleFromContext(ctx) == ""
}

// actorOrgConstraint returns the customer organization that must constrain an
// authenticated actor. In production, even platform administrators operate on
// the canonical customer for customer-domain resources; their identity
// organization remains independently available through CtxOrgID. The legacy
// branch preserves direct-handler tests that do not install CustomerContext.
func actorOrgConstraint(ctx context.Context) (int64, bool) {
	if isUnauthenticatedUnitContext(ctx) {
		return 0, false
	}
	if customer, present := middleware.GetCustomerContextFromContext(ctx); present {
		if !customer.Ready() {
			return 0, true
		}
		if auth.IsPlatformAdmin(actorOrganizationRole(ctx)) {
			return customer.OrganizationID, true
		}
		if middleware.GetOrgIDFromContext(ctx) != customer.OrganizationID {
			return 0, true
		}
		return customer.OrganizationID, true
	}
	if auth.IsPlatformAdmin(actorOrganizationRole(ctx)) {
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

func actorCustomerScopeUnavailable(ctx context.Context) bool {
	organizationID, constrained := actorOrgConstraint(ctx)
	return constrained && organizationID <= 0
}

// resolveActorOrganization returns the organization an authenticated actor may
// operate on. Production requests are bound to the separately resolved
// canonical customer: platform administrators default to it and cannot select
// the deployment operator organization, while customer identities must belong
// to it. This deliberately has no database-order fallback.
func resolveActorOrganization(ctx context.Context, requestedOrgID int64) (int64, error) {
	if requestedOrgID < 0 {
		return 0, errInvalidOrganizationID
	}
	actorOrgID := middleware.GetOrgIDFromContext(ctx)
	if customer, present := middleware.GetCustomerContextFromContext(ctx); present {
		if !customer.Ready() {
			return 0, errActorOrganizationForbidden
		}
		customerOrgID := customer.OrganizationID
		if auth.IsPlatformAdmin(actorOrganizationRole(ctx)) {
			if requestedOrgID > 0 && requestedOrgID != customerOrgID {
				return 0, errActorOrganizationForbidden
			}
			return customerOrgID, nil
		}
		if actorOrgID != customerOrgID {
			return 0, errActorOrganizationForbidden
		}
		if requestedOrgID > 0 && requestedOrgID != customerOrgID {
			return 0, errActorOrganizationForbidden
		}
		return customerOrgID, nil
	}
	if auth.IsPlatformAdmin(actorOrganizationRole(ctx)) {
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

func actorIdentityScope(ctx context.Context, organizationRole string) string {
	if !auth.IsPlatformAdmin(organizationRole) {
		return "customer_identity"
	}
	// CtxOrgID is the authenticated identity organization, not an effective
	// customer authority. Only an identity actually stored in the canonical
	// customer can claim both scopes; a deployment operator that is merely
	// authorized to administer that customer remains a deployment identity.
	customer, present := middleware.GetCustomerContextFromContext(ctx)
	if present && customer.Ready() && middleware.GetOrgIDFromContext(ctx) == customer.OrganizationID {
		return "deployment_operator_and_customer"
	}
	return "deployment_operator"
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
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID == 0 {
		return false
	}
	if organizationRole == auth.RoleOrgAdmin || organizationRole == auth.RoleOrgViewer {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) canWriteProject(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID == 0 {
		return false
	}
	if organizationRole == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin)
}

// canManageProjectMembers deliberately stays narrower than canWriteProject:
// billing administrators may update project budget fields, but never project
// membership. Project administrators are constrained to projects where their
// own membership is still project_admin.
func (h *Handler) canManageProjectMembers(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID <= 0 {
		return false
	}
	if organizationRole == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID, projectMemberAdmin)
}

func (h *Handler) canCreateProjectKey(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID == 0 {
		return false
	}
	if organizationRole == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberLegacyDeveloper, auth.RoleDeveloper)
}

func (h *Handler) canReadProjectUsage(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID == 0 {
		return false
	}
	if organizationRole == auth.RoleOrgAdmin || organizationRole == auth.RoleBillingAdmin || organizationRole == auth.RoleOrgViewer {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer)
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

	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return projectID == 0 || h.projectBelongsToOrg(ctx, projectID, orgID)
	}
	if organizationRole == auth.RoleOrgAdmin || organizationRole == auth.RoleBillingAdmin || organizationRole == auth.RoleOrgViewer {
		return projectID == 0 || h.projectBelongsToOrg(ctx, projectID, orgID)
	}
	if projectID == 0 {
		return false
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer)
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
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return projectID == 0 || h.projectBelongsToActorOrg(ctx, projectID)
	}
	if projectID == 0 {
		return organizationRole == auth.RoleOrgAdmin
	}
	return h.canReadProjectCredentialMetadata(ctx, projectID)
}

// canReadProjectCredentialMetadata is intentionally narrower than
// canReadProject. Organization viewers may read ordinary project metadata, but
// credential metadata still requires an explicit live project membership.
func (h *Handler) canReadProjectCredentialMetadata(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if actorCustomerScopeUnavailable(ctx) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	if organizationRole == auth.RoleOrgAdmin {
		return h.projectBelongsToActorOrg(ctx, projectID)
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) canWriteAPIKey(ctx context.Context, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	if !actorCanAccessOrg(ctx, orgID) {
		return false
	}
	organizationRole := actorOrganizationRole(ctx)
	if organizationRole == auth.RolePlatformAdmin {
		return true
	}
	if projectID == 0 {
		return organizationRole == auth.RoleOrgAdmin
	}
	return h.canWriteProject(ctx, projectID)
}

func (h *Handler) canManageOwnPersonalAPIKey(ctx context.Context, orgID, projectID, ownerUserID int64, keyType string) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	actorUserID := middleware.GetUserIDFromContext(ctx)
	if actorUserID <= 0 || actorUserID != ownerUserID || keyType != "personal" || projectID <= 0 || !actorCanAccessOrg(ctx, orgID) {
		return false
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberLegacyDeveloper, auth.RoleDeveloper)
}
