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

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.org_id, o.name AS org_name, u.username, u.role, u.status, u.created_at, u.updated_at
		 FROM users u LEFT JOIN orgs o ON o.id = u.org_id
		 ORDER BY u.id DESC LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	users := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgId int64
		var username, role, status string
		var orgName *string
		var createdAt, updatedAt time.Time
		rows.Scan(&id, &orgId, &orgName, &username, &role, &status, &createdAt, &updatedAt)
		oName := ""
		if orgName != nil {
			oName = *orgName
		}
		users = append(users, map[string]interface{}{
			"id": id, "org_id": orgId, "org_name": oName,
			"username": username, "role": role, "status": status,
			"created_at": createdAt, "updated_at": updatedAt,
		})
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
		h.db.QueryRow(r.Context(), `SELECT id FROM orgs LIMIT 1`).Scan(&req.OrgId)
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
		sets = append(sets, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, *req.Role)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
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
	tag, err := h.db.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
