package admin

import (
	"encoding/json"
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
// candidate list is intentionally authorization-filtered on the server so a
// project administrator never learns or grants organization-level roles.
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
		        u.role, u.status, pm.role, pm.created_at, pm.updated_at
		 FROM project_members pm
		 JOIN users u ON u.id = pm.user_id
		 JOIN projects p ON p.id = pm.project_id AND p.org_id = u.org_id
		 WHERE pm.project_id = $1 AND p.org_id = $2
		 ORDER BY CASE pm.role WHEN 'project_admin' THEN 0 WHEN 'project_developer' THEN 1 ELSE 2 END,
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
		var username, email, authProvider, userRole, status, membershipRole string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&userID, &username, &email, &authProvider, &userRole, &status, &membershipRole, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		members = append(members, map[string]interface{}{
			"user_id": userID, "username": username, "email": email,
			"auth_provider": authProvider, "user_role": userRole, "status": status,
			"membership_role": membershipRole, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	eligibleRoles := []string{auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}
	if actorRole(r.Context()) == auth.RoleProjectAdmin {
		eligibleRoles = []string{auth.RoleDeveloper, auth.RoleViewer}
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	likeQuery := "%" + query + "%"
	var eligibleTotal int
	if err := h.db.QueryRow(r.Context(),
		`SELECT COUNT(*)
		 FROM users u
		 WHERE u.org_id = $1
		   AND u.role = ANY($2::text[])
		   AND ($3 = '' OR u.username ILIKE $4 OR COALESCE(u.email, '') ILIKE $4)
		   AND NOT EXISTS (
		       SELECT 1 FROM project_members pm WHERE pm.project_id = $5 AND pm.user_id = u.id
		   )`,
		orgID, eligibleRoles, query, likeQuery, projectID,
	).Scan(&eligibleTotal); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate count failed"})
		return
	}

	candidateRows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.username, COALESCE(u.email, ''), u.role, u.status
		 FROM users u
		 WHERE u.org_id = $1
		   AND u.role = ANY($2::text[])
		   AND ($3 = '' OR u.username ILIKE $4 OR COALESCE(u.email, '') ILIKE $4)
		   AND NOT EXISTS (
		       SELECT 1 FROM project_members pm WHERE pm.project_id = $5 AND pm.user_id = u.id
		   )
		 ORDER BY CASE u.status WHEN 'active' THEN 0 ELSE 1 END, u.username ASC, u.id ASC
		 LIMIT 100`,
		orgID, eligibleRoles, query, likeQuery, projectID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate query failed"})
		return
	}
	defer candidateRows.Close()

	eligibleUsers := make([]map[string]interface{}, 0)
	for candidateRows.Next() {
		var userID int64
		var username, email, userRole, status string
		if err := candidateRows.Scan(&userID, &username, &email, &userRole, &status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Candidate scan failed"})
			return
		}
		membershipRole, _ := projectMemberRoleForUserRole(userRole)
		eligibleUsers = append(eligibleUsers, map[string]interface{}{
			"user_id": userID, "username": username, "email": email,
			"user_role": userRole, "membership_role": membershipRole, "status": status,
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
	if !auth.RoleIn(actorRole(r.Context()), auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
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
	var targetRole, targetStatus string
	if err := tx.QueryRow(r.Context(),
		`SELECT org_id, role, status FROM users WHERE id = $1 FOR UPDATE`, userID,
	).Scan(&targetOrgID, &targetRole, &targetStatus); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	if targetOrgID != orgID {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "User and project must belong to the same organization"})
		return
	}
	membershipRole, valid := projectMemberRoleForUserRole(targetRole)
	if !valid || req.Role != membershipRole {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Membership role must match the user's project role"})
		return
	}
	if actorRole(r.Context()) == auth.RoleProjectAdmin {
		if membershipRole == projectMemberAdmin {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Project administrators cannot grant project_admin"})
			return
		}
		if middleware.GetUserIDFromContext(r.Context()) == userID {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project administrators cannot change their own membership"})
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
	auditDetail := map[string]interface{}{
		"after": map[string]interface{}{"user_id": userID, "role": membershipRole, "status": targetStatus},
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
	if !auth.RoleIn(actorRole(r.Context()), auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
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
	var targetRole, targetStatus, membershipRole string
	err = tx.QueryRow(r.Context(),
		`SELECT p.org_id, u.role, u.status, pm.role
		 FROM project_members pm
		 JOIN projects p ON p.id = pm.project_id
		 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
		 WHERE pm.project_id = $1 AND pm.user_id = $2
		 FOR UPDATE OF pm, u`,
		projectID, userID,
	).Scan(&orgID, &targetRole, &targetStatus, &membershipRole)
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
	if actorRole(r.Context()) == auth.RoleProjectAdmin {
		if membershipRole == projectMemberAdmin {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Project administrators cannot remove project administrators"})
			return
		}
		if middleware.GetUserIDFromContext(r.Context()) == userID {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project administrators cannot remove their own membership"})
			return
		}
	}
	if auth.IsProjectScopedRole(targetRole) && targetStatus == "active" {
		var otherMemberships int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*)
			 FROM project_members pm
			 JOIN projects p ON p.id = pm.project_id
			 WHERE pm.user_id = $1 AND pm.project_id <> $2 AND p.org_id = $3`,
			userID, projectID, orgID,
		).Scan(&otherMemberships); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Membership check failed"})
			return
		}
		if otherMemberships == 0 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "An active project user must keep at least one project; assign another project or disable the account first"})
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
	auditDetail := map[string]interface{}{
		"before": map[string]interface{}{"user_id": userID, "role": membershipRole},
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

func parsePositivePathID(w http.ResponseWriter, r *http.Request, name, message string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": message})
		return 0, false
	}
	return id, true
}
