package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

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
		        u.created_at, u.updated_at
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
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
		if err := rows.Scan(&id, &orgId, &orgName, &username, &role, &status, &email, &authProvider, &createdAt, &updatedAt); err != nil {
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
		OrgId    int64  `json:"org_id"`
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Username and password required"})
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleOrgViewer
	}
	if !auth.IsValidRole(req.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid role"})
		return
	}
	if req.OrgId == 0 {
		if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
			req.OrgId = actorOrgID
		} else {
			h.db.QueryRow(r.Context(), `SELECT id FROM orgs LIMIT 1`).Scan(&req.OrgId)
		}
	}
	if !actorCanAccessOrg(r.Context(), req.OrgId) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if _, constrained := actorOrgConstraint(r.Context()); constrained && auth.IsPlatformAdmin(req.Role) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Hash failed"})
		return
	}

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO users (org_id, username, password_hash, role) VALUES ($1, $2, $3, $4) RETURNING id`,
		req.OrgId, req.Username, string(hash), req.Role).Scan(&id)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Username already exists"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Role   *string `json:"role"`
		Status *string `json:"status"`
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
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	var targetOrgID int64
	var targetRole string
	if !isUnauthenticatedUnitContext(r.Context()) {
		if err := h.db.QueryRow(r.Context(), `SELECT org_id, role FROM users WHERE id = $1`, id).Scan(&targetOrgID, &targetRole); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
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
	var targetOrgID int64
	var targetRole string
	if err := tx.QueryRow(r.Context(), `SELECT org_id, role FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&targetOrgID, &targetRole); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
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
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
