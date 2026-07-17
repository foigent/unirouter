package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

const activePlatformAdminMutationLockID int64 = 8675309001

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	whereClause := ""
	args := []interface{}{}
	argIdx := 1
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		whereClause = " WHERE u.org_id = $1"
		args = append(args, actorOrgID)
		argIdx++
	}

	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users u`+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT u.id, u.org_id, o.name AS org_name, u.username, u.role, u.status,
		        COALESCE(u.email, ''), COALESCE(NULLIF(u.auth_provider, ''), 'local'),
		        u.created_at, u.updated_at,
		        COALESCE(member_scope.project_ids, '{}'::bigint[]),
		        COALESCE(member_scope.project_names, '')
		 FROM users u
		 LEFT JOIN orgs o ON o.id = u.org_id
		 LEFT JOIN LATERAL (
		     SELECT array_agg(pm.project_id ORDER BY pm.project_id) AS project_ids,
		            string_agg(p.name, ', ' ORDER BY p.name, p.id) AS project_names
		     FROM project_members pm
		     JOIN projects p ON p.id = pm.project_id AND p.org_id = u.org_id
		     WHERE pm.user_id = u.id
		 ) member_scope ON TRUE
		 %s
		 ORDER BY u.id DESC LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId int64
		var username, role, status, email, authProvider string
		var orgName *string
		var createdAt, updatedAt time.Time
		var projectIDs []int64
		var projectNames string
		if err := rows.Scan(&id, &orgId, &orgName, &username, &role, &status, &email, &authProvider, &createdAt, &updatedAt, &projectIDs, &projectNames); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		oName := ""
		if orgName != nil {
			oName = *orgName
		}
		users = append(users, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"username": username, "role": role, "status": status, "email": email, "auth_provider": authProvider,
			"created_at": createdAt, "updated_at": updatedAt,
			"project_ids": projectIDs, "project_names": projectNames,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": users, "total": total})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgId      int64   `json:"org_id"`
		Username   string  `json:"username"`
		Password   string  `json:"password"`
		Role       string  `json:"role"`
		Status     string  `json:"status"`
		ProjectIDs []int64 `json:"project_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	username, err := auth.NormalizeLocalUsername(req.Username)
	if err != nil || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Username and password required"})
		return
	}
	req.Username = username
	if req.Role == "" {
		req.Role = auth.RoleOrgViewer
	}
	if !auth.IsValidRole(req.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid role"})
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
		return
	}
	if req.OrgId < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if req.OrgId == 0 {
			req.OrgId = actorOrgID
		}
	} else if req.OrgId == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id is required"})
		return
	}
	if !actorCanAccessOrg(r.Context(), req.OrgId) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(req.Role) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	projectIDs, err := normalizeProjectIDs(req.ProjectIDs)
	if err == nil {
		err = validateProjectRoleMembership(req.Role, req.Status, projectIDs)
	}
	if err != nil {
		status, message := projectMembershipErrorResponse(err)
		writeJSON(w, status, map[string]interface{}{"error": message})
		return
	}
	if err := auth.ValidateLocalPassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Hash failed"})
		return
	}

	// Preserve direct-handler unit compatibility. Production routes always
	// inject an authenticated actor and use the atomic transaction below.
	if isUnauthenticatedUnitContext(r.Context()) {
		var id int64
		err = h.db.QueryRow(r.Context(),
			`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			req.OrgId, req.Username, string(hash), req.Role, req.Status).Scan(&id)
		if err != nil {
			if writeCommercialLimitViolation(w, err) {
				return
			}
			if writeLocalLoginIdentityConflict(w, err) {
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "project_ids": projectIDs})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := validateProjectIDsForOrgTx(r.Context(), tx, req.OrgId, projectIDs); err != nil {
		status, message := projectMembershipErrorResponse(err)
		writeJSON(w, status, map[string]interface{}{"error": message})
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		req.OrgId, req.Username, string(hash), req.Role, req.Status).Scan(&id)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		if writeLocalLoginIdentityConflict(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := lockProjectMembershipMutation(r.Context(), tx, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := replaceUserProjectMembershipsTx(r.Context(), tx, id, req.OrgId, req.Role, projectIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "user.create", "user", strconv.FormatInt(id, 10), map[string]interface{}{
		"after": map[string]interface{}{
			"org_id":      req.OrgId,
			"username":    req.Username,
			"role":        req.Role,
			"status":      req.Status,
			"project_ids": projectIDs,
		},
	}, requestAuditMeta(r, req.OrgId, 0)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "project_ids": projectIDs})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Role       *string  `json:"role"`
		Status     *string  `json:"status"`
		ProjectIDs *[]int64 `json:"project_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	if req.Role != nil {
		if !auth.IsValidRole(*req.Role) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid role"})
			return
		}
		if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(*req.Role) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		sets = append(sets, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, *req.Role)
		argIdx++
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if len(sets) == 0 && req.ProjectIDs == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	var requestedProjectIDs []int64
	if req.ProjectIDs != nil {
		requestedProjectIDs, err = normalizeProjectIDs(*req.ProjectIDs)
		if err != nil {
			status, message := projectMembershipErrorResponse(err)
			writeJSON(w, status, map[string]interface{}{"error": message})
			return
		}
	}
	var targetOrgID int64
	var targetRole, targetStatus string
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err := h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		defer tx.Rollback(r.Context())

		if err := lockProjectMembershipMutation(r.Context(), tx, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		if err := tx.QueryRow(r.Context(), `SELECT org_id, role, status FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&targetOrgID, &targetRole, &targetStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			}
			return
		}
		if !actorCanAccessOrg(r.Context(), targetOrgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(targetRole) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}

		actorUserID := middleware.GetUserIDFromContext(r.Context())
		if actorUserID == id {
			roleChanged := req.Role != nil && auth.NormalizeRole(*req.Role) != auth.NormalizeRole(targetRole)
			selfDisabled := req.Status != nil && *req.Status != "active"
			if roleChanged || selfDisabled {
				writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Cannot change your own role or disable the current account"})
				return
			}
		}

		nextRole, nextStatus := targetRole, targetStatus
		if req.Role != nil {
			nextRole = *req.Role
		}
		if req.Status != nil {
			nextStatus = *req.Status
		}
		existingProjectIDs, err := listUserProjectIDsTx(r.Context(), tx, id, targetOrgID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		desiredProjectIDs := existingProjectIDs
		if req.ProjectIDs != nil {
			desiredProjectIDs = requestedProjectIDs
		}
		if !auth.IsProjectScopedRole(nextRole) {
			if req.ProjectIDs != nil && len(desiredProjectIDs) > 0 {
				status, message := projectMembershipErrorResponse(errProjectIDsNotAllowed)
				writeJSON(w, status, map[string]interface{}{"error": message})
				return
			}
			desiredProjectIDs = nil
		}
		if err := validateProjectRoleMembership(nextRole, nextStatus, desiredProjectIDs); err != nil {
			status, message := projectMembershipErrorResponse(err)
			writeJSON(w, status, map[string]interface{}{"error": message})
			return
		}
		if err := validateProjectIDsForOrgTx(r.Context(), tx, targetOrgID, desiredProjectIDs); err != nil {
			status, message := projectMembershipErrorResponse(err)
			writeJSON(w, status, map[string]interface{}{"error": message})
			return
		}
		removesActivePlatformAdmin := auth.IsPlatformAdmin(targetRole) && targetStatus == "active" &&
			(!auth.IsPlatformAdmin(nextRole) || nextStatus != "active")
		if removesActivePlatformAdmin {
			if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock($1)`, activePlatformAdminMutationLockID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
				return
			}
			var activePlatformAdmins int
			if err := tx.QueryRow(r.Context(),
				`SELECT COUNT(*) FROM users WHERE role IN ('admin', 'platform_admin') AND status = 'active'`,
			).Scan(&activePlatformAdmins); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
				return
			}
			if activePlatformAdmins <= 1 {
				writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "At least one active platform admin is required"})
				return
			}
		}

		if len(sets) > 0 {
			sets = append(sets, "updated_at = NOW()")
			idArg := argIdx
			args = append(args, id)
			whereClause := fmt.Sprintf("id = $%d", idArg)
			if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
				whereClause += fmt.Sprintf(" AND org_id = $%d", idArg+1)
				args = append(args, actorOrgID)
				whereClause += " AND role NOT IN ('admin', 'platform_admin')"
			}
			query := fmt.Sprintf("UPDATE users SET %s WHERE %s", joinStrings(sets, ", "), whereClause)
			tag, err := tx.Exec(r.Context(), query, args...)
			if err != nil {
				if writeCommercialLimitViolation(w, err) {
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
				return
			}
			if tag.RowsAffected() == 0 {
				writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
				return
			}
		}
		if err := replaceUserProjectMembershipsTx(r.Context(), tx, id, targetOrgID, nextRole, desiredProjectIDs); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		if err := insertAuditEntry(r.Context(), tx, "user.update", "user", idStr, map[string]interface{}{
			"before": map[string]interface{}{
				"role":        targetRole,
				"status":      targetStatus,
				"project_ids": existingProjectIDs,
			},
			"after": map[string]interface{}{
				"role":        nextRole,
				"status":      nextStatus,
				"project_ids": desiredProjectIDs,
			},
		}, requestAuditMeta(r, targetOrgID, 0)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "project_ids": desiredProjectIDs})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	idArg := argIdx
	args = append(args, id)
	whereClause := fmt.Sprintf("id = $%d", idArg)
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		whereClause += fmt.Sprintf(" AND org_id = $%d", idArg+1)
		args = append(args, actorOrgID)
		whereClause += " AND role NOT IN ('admin', 'platform_admin')"
	}
	query := fmt.Sprintf("UPDATE users SET %s WHERE %s", joinStrings(sets, ", "), whereClause)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockProjectMembershipMutation(r.Context(), tx, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	var targetOrgID int64
	var targetRole, targetStatus string
	if err := tx.QueryRow(r.Context(), `SELECT org_id, role, status FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&targetOrgID, &targetRole, &targetStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		}
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) {
		if !actorCanAccessOrg(r.Context(), targetOrgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(targetRole) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if middleware.GetUserIDFromContext(r.Context()) == id {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Cannot delete the current account"})
			return
		}
	}
	if auth.IsPlatformAdmin(targetRole) && targetStatus == "active" {
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock($1)`, activePlatformAdminMutationLockID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
			return
		}
		var activePlatformAdmins int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM users WHERE role IN ('admin', 'platform_admin') AND status = 'active'`,
		).Scan(&activePlatformAdmins); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
			return
		}
		if activePlatformAdmins <= 1 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "At least one active platform admin is required"})
			return
		}
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE api_keys
		 SET status = 'disabled', updated_at = NOW()
		 WHERE owner_user_id = $1 AND org_id = $2 AND key_type = 'personal'`, id, targetOrgID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM users WHERE id = $1 AND org_id = $2`, id, targetOrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) {
		if err := insertAuditEntry(r.Context(), tx, "user.delete", "user", idStr, map[string]interface{}{
			"before": map[string]interface{}{
				"org_id": targetOrgID,
				"role":   targetRole,
				"status": targetStatus,
			},
			"after": map[string]interface{}{"deleted": true, "personal_keys_disabled": true},
		}, requestAuditMeta(r, targetOrgID, 0)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
