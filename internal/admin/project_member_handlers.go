package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

// ListProjectMembers returns the current project memberships and a bounded,
// searchable list of same-organization users that the actor may add. The
// candidate list is bounded and same-organization only. It returns the
// account's organization_role for operator context, but this endpoint can
// grant only a project membership role and never organization authority.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	if !h.canManageProjectMembers(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var orgID int64
	var projectName string
	if err := h.db.QueryRow(r.Context(),
		`SELECT org_id, name FROM projects WHERE id = $1`, projectID,
	).Scan(&orgID, &projectName); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}
	if !actorCanAccessOrg(r.Context(), orgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	var total int
	if err := h.db.QueryRow(r.Context(),
		`SELECT COUNT(*)
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 WHERE pm.project_id = $1 AND u.org_id = $2`,
		projectID, orgID,
	).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.username, COALESCE(u.email, ''),
		        COALESCE(NULLIF(u.auth_provider, ''), 'local'),
		        u.organization_role, u.role, u.status, pm.role, pm.created_at, pm.updated_at
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 JOIN projects p ON p.id = pm.project_id AND p.org_id = u.org_id
		 WHERE pm.project_id = $1 AND p.org_id = $2
		 ORDER BY CASE pm.role WHEN 'project_admin' THEN 0 WHEN 'project_user' THEN 1 WHEN 'project_developer' THEN 1 ELSE 2 END,
		          u.username ASC, u.id ASC
		 LIMIT $3 OFFSET $4`,
		projectID, orgID, pageSize, offset,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	members := make([]map[string]interface{}, 0)
	for rows.Next() {
		var userID int64
		var username, email, authProvider, organizationRole, compatibilityRole, status, membershipRole string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&userID, &username, &email, &authProvider, &organizationRole, &compatibilityRole, &status, &membershipRole, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		members = append(members, map[string]interface{}{
			"user_id": userID, "username": username, "email": email,
			"auth_provider": authProvider, "organization_role": organizationRole,
			"role": compatibilityRole, "user_role": compatibilityRole, "status": status,
			"membership_role": canonicalProjectMembershipRole(membershipRole), "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	likeQuery := "%" + query + "%"
	var eligibleTotal int
	if err := h.db.QueryRow(r.Context(),
		`SELECT COUNT(*)
		 FROM users u
		 WHERE u.org_id = $1
		   AND u.status = 'active'
		   AND ($2 = '' OR u.username ILIKE $3 OR COALESCE(u.email, '') ILIKE $3)
		   AND NOT EXISTS (
		       SELECT 1 FROM project_members pm WHERE pm.project_id = $4 AND pm.user_id = u.id
		   )`,
		orgID, query, likeQuery, projectID,
	).Scan(&eligibleTotal); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate count failed"})
		return
	}

	candidateRows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.username, COALESCE(u.email, ''), u.organization_role, u.role, u.status
		 FROM users u
		 WHERE u.org_id = $1
		   AND u.status = 'active'
		   AND ($2 = '' OR u.username ILIKE $3 OR COALESCE(u.email, '') ILIKE $3)
		   AND NOT EXISTS (
		       SELECT 1 FROM project_members pm WHERE pm.project_id = $4 AND pm.user_id = u.id
		   )
		 ORDER BY CASE u.status WHEN 'active' THEN 0 ELSE 1 END, u.username ASC, u.id ASC
		 LIMIT 100`,
		orgID, query, likeQuery, projectID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate query failed"})
		return
	}
	defer candidateRows.Close()

	eligibleUsers := make([]map[string]interface{}, 0)
	for candidateRows.Next() {
		var userID int64
		var username, email, organizationRole, compatibilityRole, status string
		if err := candidateRows.Scan(&userID, &username, &email, &organizationRole, &compatibilityRole, &status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate scan failed"})
			return
		}
		eligibleUsers = append(eligibleUsers, map[string]interface{}{
			"user_id": userID, "username": username, "email": email,
			"organization_role": organizationRole, "role": compatibilityRole,
			"user_role": compatibilityRole, "status": status,
		})
	}
	if err := candidateRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": members, "total": total, "page": page, "page_size": pageSize,
		"eligible_users": eligibleUsers, "eligible_total": eligibleTotal,
		"project_id": projectID, "project_name": projectName,
	})
}

func (h *Handler) UpsertProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	userID, ok := parsePositivePathID(w, r, "user_id", "Invalid user ID")
	if !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	membershipRole, valid := auth.NormalizeProjectMembershipRole(strings.TrimSpace(req.Role))
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project membership role"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save project membership"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockProjectMembershipMutation(r.Context(), tx, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save project membership"})
		return
	}

	var orgID int64
	if err := tx.QueryRow(r.Context(), `SELECT org_id FROM projects WHERE id = $1 FOR SHARE`, projectID).Scan(&orgID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}
	allowed, err := canManageProjectMembersTx(r.Context(), tx, projectID, orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership authorization failed"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var targetOrgID int64
	var targetOrganizationRole, targetRole, targetStatus string
	if err := tx.QueryRow(r.Context(),
		`SELECT org_id, organization_role, role, status FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&targetOrgID, &targetOrganizationRole, &targetRole, &targetStatus); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	if targetOrgID != orgID {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "User and project must belong to the same organization"})
		return
	}
	if targetStatus != "active" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Only active accounts can receive project membership"})
		return
	}
	var existingMembershipRole string
	existingMembershipErr := tx.QueryRow(r.Context(),
		`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2 FOR UPDATE`,
		projectID, userID,
	).Scan(&existingMembershipRole)
	if existingMembershipErr != nil && !errors.Is(existingMembershipErr, pgx.ErrNoRows) {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership lookup failed"})
		return
	}
	hasExistingMembership := existingMembershipErr == nil
	if actorOrganizationRole(r.Context()) != auth.RolePlatformAdmin && actorOrganizationRole(r.Context()) != auth.RoleOrgAdmin {
		if middleware.GetUserIDFromContext(r.Context()) == userID {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project administrators cannot change their own membership"})
			return
		}
		if membershipRole == projectMemberAdmin ||
			(hasExistingMembership && canonicalProjectMembershipRole(existingMembershipRole) == projectMemberAdmin) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Project administrators cannot create or modify project_admin memberships"})
			return
		}
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (project_id, user_id) DO UPDATE
		 SET role = EXCLUDED.role, updated_at = NOW()`,
		projectID, userID, membershipRole,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save project membership"})
		return
	}
	disabledPersonalCredentials := int64(0)
	if membershipRole == projectMemberViewer {
		disabledPersonalCredentials, err = disablePersonalCredentialsForMembershipTx(r.Context(), tx, userID, orgID, projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to enforce non-calling membership"})
			return
		}
	}
	roleTransition, err := syncUserCompatibilityRoleTx(r.Context(), tx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update compatibility role"})
		return
	}
	auditDetail := map[string]interface{}{
		"after":                         map[string]interface{}{"user_id": userID, "role": membershipRole, "status": targetStatus},
		"compatibility_role":            map[string]interface{}{"before": roleTransition.BeforeRole, "after": roleTransition.AfterRole},
		"live_sessions_revoked":         roleTransition.RevokedSessions,
		"personal_credentials_disabled": disabledPersonalCredentials,
	}
	if hasExistingMembership {
		auditDetail["before"] = map[string]interface{}{"user_id": userID, "role": canonicalProjectMembershipRole(existingMembershipRole)}
	}
	if err := insertAuditEntry(r.Context(), tx, "project.member.upsert", "project_member",
		fmt.Sprintf("%d:%d", projectID, userID), auditDetail, auditMeta{
			OrgID:           orgID,
			ProjectID:       projectID,
			IP:              r.RemoteAddr,
			UserAgent:       r.UserAgent(),
			RequestID:       middleware.GetRequestIDFromContext(r.Context()),
			ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
			Source:          auditSourceFromContext(r.Context()),
		}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership audit failed; no change was committed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to save project membership"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "project_id": projectID, "user_id": userID, "role": membershipRole})
}

func (h *Handler) DeleteProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	userID, ok := parsePositivePathID(w, r, "user_id", "Invalid user ID")
	if !ok {
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to remove project membership"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockProjectMembershipMutation(r.Context(), tx, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to remove project membership"})
		return
	}

	var orgID int64
	var targetOrganizationRole, targetRole, targetStatus, membershipRole string
	err = tx.QueryRow(r.Context(),
		`SELECT p.org_id, u.organization_role, u.role, u.status, pm.role
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
		 WHERE pm.project_id = $1 AND pm.user_id = $2
		 FOR UPDATE OF pm, u`,
		projectID, userID,
	).Scan(&orgID, &targetOrganizationRole, &targetRole, &targetStatus, &membershipRole)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project membership not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership lookup failed"})
		return
	}
	allowed, authErr := canManageProjectMembersTx(r.Context(), tx, projectID, orgID)
	if authErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership authorization failed"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if actorOrganizationRole(r.Context()) != auth.RolePlatformAdmin && actorOrganizationRole(r.Context()) != auth.RoleOrgAdmin {
		if membershipRole == projectMemberAdmin {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Project administrators cannot remove project administrators"})
			return
		}
		if middleware.GetUserIDFromContext(r.Context()) == userID {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project administrators cannot remove their own membership"})
			return
		}
	}
	tag, err := tx.Exec(r.Context(),
		`DELETE FROM project_members pm
		 USING projects p, users u
		 WHERE pm.project_id = $1 AND pm.user_id = $2
		   AND p.id = pm.project_id AND p.org_id = $3
		   AND u.id = pm.user_id AND u.org_id = p.org_id`,
		projectID, userID, orgID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to remove project membership"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project membership not found"})
		return
	}
	disabledPersonalCredentials, err := disablePersonalCredentialsForMembershipTx(r.Context(), tx, userID, orgID, projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to revoke project calling credentials"})
		return
	}
	roleTransition, err := syncUserCompatibilityRoleTx(r.Context(), tx, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update compatibility role"})
		return
	}
	auditDetail := map[string]interface{}{
		"before":                        map[string]interface{}{"user_id": userID, "role": canonicalProjectMembershipRole(membershipRole)},
		"organization_role":             targetOrganizationRole,
		"compatibility_role":            map[string]interface{}{"before": roleTransition.BeforeRole, "after": roleTransition.AfterRole},
		"live_sessions_revoked":         roleTransition.RevokedSessions,
		"personal_credentials_disabled": disabledPersonalCredentials,
	}
	if err := insertAuditEntry(r.Context(), tx, "project.member.remove", "project_member",
		fmt.Sprintf("%d:%d", projectID, userID), auditDetail, auditMeta{
			OrgID:           orgID,
			ProjectID:       projectID,
			IP:              r.RemoteAddr,
			UserAgent:       r.UserAgent(),
			RequestID:       middleware.GetRequestIDFromContext(r.Context()),
			ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
			Source:          auditSourceFromContext(r.Context()),
		}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership audit failed; no change was committed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to remove project membership"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func canonicalProjectMembershipRole(role string) string {
	if normalized, ok := auth.NormalizeProjectMembershipRole(role); ok {
		return normalized
	}
	return role
}

func parsePositivePathID(w http.ResponseWriter, r *http.Request, name, message string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": message})
		return 0, false
	}
	return id, true
}
