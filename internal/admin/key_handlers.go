package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	var conditions []string
	var args []interface{}
	argIdx := 1
	if projectIDParam := r.URL.Query().Get("project_id"); projectIDParam != "" {
		projectID, err := strconv.ParseInt(projectIDParam, 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canReadProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("ak.project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if auth.IsProjectScopedRole(actorRole(r.Context())) {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM project_members pm
			WHERE pm.project_id = ak.project_id AND pm.user_id = $%d
		)`, argIdx))
		args = append(args, middleware.GetUserIDFromContext(r.Context()))
		argIdx++
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinStrings(conditions, " AND ")
	}

	rows, err := h.db.Query(r.Context(),
		fmt.Sprintf(`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        COALESCE(ak.budget_limit, 0), ak.allowed_ips, ak.allowed_models, ak.created_at,
		        o.name as org_name, COALESCE(p.name, '') as project_name
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id
		 %s
		 ORDER BY ak.id ASC`, whereClause),
		args...,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID int64
		var name, keyPrefix, status, orgName, projectName string
		var rpmLimit, qpsLimit, budgetLimit int
		var allowedIPs, allowedModels string
		var createdAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &name, &keyPrefix, &status, &rpmLimit, &qpsLimit, &budgetLimit, &allowedIPs, &allowedModels, &createdAt, &orgName, &projectName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "org_id": orgID, "project_id": projectID, "name": name, "key_prefix": keyPrefix,
			"status": status, "rpm_limit": rpmLimit, "qps_limit": qpsLimit,
			"budget_limit": budgetLimit, "allowed_ips": allowedIPs, "allowed_models": allowedModels,
			"created_at": createdAt, "org_name": orgName, "project_name": projectName,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": len(keys)})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID         int64  `json:"org_id"`
		ProjectID     int64  `json:"project_id"`
		Name          string `json:"name"`
		RPMLimit      int    `json:"rpm_limit"`
		QPSLimit      int    `json:"qps_limit"`
		AllowedIPs    string `json:"allowed_ips"`
		AllowedModels string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.OrgID == 0 {
		req.OrgID = 1
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
		return
	}
	if req.RPMLimit == 0 {
		req.RPMLimit = 60
	}
	if req.QPSLimit == 0 {
		req.QPSLimit = 10
	}

	existingRows, err := h.db.Query(r.Context(),
		`SELECT 1 FROM api_keys WHERE org_id = $1 AND name = $2 LIMIT 1`,
		req.OrgID, req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to check duplicate key name"})
		return
	}
	defer existingRows.Close()
	if existingRows.Next() {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "API key name already exists"})
		return
	}
	if err := existingRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to check duplicate key name"})
		return
	}

	if auth.IsProjectScopedRole(actorRole(r.Context())) && req.ProjectID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "project_id is required"})
		return
	}

	projectID, err := h.resolveAPIKeyProjectID(r.Context(), req.OrgID, req.ProjectID)
	if err != nil {
		if req.ProjectID > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to resolve default project"})
		}
		return
	}
	if !h.canCreateProjectKey(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	rawKey, keyPrefix, err := auth.GenerateAPIKey(req.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate key"})
		return
	}

	keyHash := auth.HashAPIKey(rawKey, h.jwtSecret)

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO api_keys (org_id, project_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, $9, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, projectID, req.Name, keyHash, keyPrefix, req.RPMLimit, req.QPSLimit, req.AllowedIPs, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create key"})
		return
	}

	h.recordAuditForProjectFromRequest(r, "apikey.create", "apikey", strconv.FormatInt(id, 10), projectID,
		map[string]interface{}{
			"org_id":     req.OrgID,
			"project_id": projectID,
			"name":       req.Name,
			"after": map[string]interface{}{
				"org_id":         req.OrgID,
				"project_id":     projectID,
				"name":           req.Name,
				"status":         "active",
				"rpm_limit":      req.RPMLimit,
				"qps_limit":      req.QPSLimit,
				"allowed_ips":    req.AllowedIPs,
				"allowed_models": req.AllowedModels,
			},
		})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "key": rawKey, "key_prefix": keyPrefix, "project_id": projectID})
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var req struct {
		Name          *string `json:"name"`
		AllowedIPs    *string `json:"allowed_ips"`
		AllowedModels *string `json:"allowed_models"`
		Status        *string `json:"status"`
		ProjectID     *int64  `json:"project_id"`
		RPMLimit      *int    `json:"rpm_limit"`
		QPSLimit      *int    `json:"qps_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.AllowedIPs != nil {
		sets = append(sets, fmt.Sprintf("allowed_ips = $%d", argIdx))
		args = append(args, *req.AllowedIPs)
		argIdx++
	}
	if req.AllowedModels != nil {
		sets = append(sets, fmt.Sprintf("allowed_models = $%d", argIdx))
		args = append(args, *req.AllowedModels)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.ProjectID != nil {
		var orgID int64
		var currentProjectID int64
		err := h.db.QueryRow(r.Context(), `SELECT org_id, COALESCE(project_id, 0) FROM api_keys WHERE id = $1`, id).Scan(&orgID, &currentProjectID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
			return
		}
		if !h.canWriteProject(r.Context(), currentProjectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		projectID, err := h.resolveAPIKeyProjectID(r.Context(), orgID, *req.ProjectID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		sets = append(sets, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if req.ProjectID == nil && auth.IsProjectScopedRole(actorRole(r.Context())) {
		var currentProjectID int64
		err := h.db.QueryRow(r.Context(), `SELECT COALESCE(project_id, 0) FROM api_keys WHERE id = $1`, id).Scan(&currentProjectID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
			return
		}
		if !h.canWriteProject(r.Context(), currentProjectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}
	if req.RPMLimit != nil {
		sets = append(sets, fmt.Sprintf("rpm_limit = $%d", argIdx))
		args = append(args, *req.RPMLimit)
		argIdx++
	}
	if req.QPSLimit != nil {
		sets = append(sets, fmt.Sprintf("qps_limit = $%d", argIdx))
		args = append(args, *req.QPSLimit)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	changedFields := []string{}
	if req.Name != nil {
		changedFields = append(changedFields, "name")
	}
	if req.AllowedIPs != nil {
		changedFields = append(changedFields, "allowed_ips")
	}
	if req.AllowedModels != nil {
		changedFields = append(changedFields, "allowed_models")
	}
	if req.Status != nil {
		changedFields = append(changedFields, "status")
	}
	if req.ProjectID != nil {
		changedFields = append(changedFields, "project_id")
	}
	if req.RPMLimit != nil {
		changedFields = append(changedFields, "rpm_limit")
	}
	if req.QPSLimit != nil {
		changedFields = append(changedFields, "qps_limit")
	}
	h.recordAudit(r.Context(), "apikey.update", "apikey", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key updated"})
}

func (h *Handler) DisableAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	if auth.IsProjectScopedRole(actorRole(r.Context())) {
		var projectID int64
		err := h.db.QueryRow(r.Context(), `SELECT COALESCE(project_id, 0) FROM api_keys WHERE id = $1`, id).Scan(&projectID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
			return
		}
		if !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}

	tag, err := h.db.Exec(r.Context(),
		`UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	h.recordAudit(r.Context(), "apikey.disable", "apikey", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key disabled"})
}

func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}
	var k struct {
		ID, OrgID, ProjectID            int64
		Name, KeyPrefix                 string
		Status                          string
		RPMLimit, QPSLimit, BudgetLimit int
		AllowedIPs, AllowedModels       string
		OrgName, ProjectName            string
		CreatedAt                       time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        ak.budget_limit, ak.allowed_ips, ak.allowed_models, ak.created_at, o.name, COALESCE(p.name, '')
		 FROM api_keys ak JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id
		 WHERE ak.id = $1`, id).Scan(
		&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.KeyPrefix, &k.Status, &k.RPMLimit, &k.QPSLimit,
		&k.BudgetLimit, &k.AllowedIPs, &k.AllowedModels, &k.CreatedAt, &k.OrgName, &k.ProjectName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
		return
	}
	if !h.canReadProject(r.Context(), k.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": k.ID, "org_id": k.OrgID, "project_id": k.ProjectID, "name": k.Name, "key_prefix": k.KeyPrefix,
		"status": k.Status, "rpm_limit": k.RPMLimit, "qps_limit": k.QPSLimit,
		"budget_limit": k.BudgetLimit, "allowed_ips": k.AllowedIPs, "allowed_models": k.AllowedModels,
		"created_at": k.CreatedAt, "org_name": k.OrgName, "project_name": k.ProjectName,
	})
}

func (h *Handler) resolveAPIKeyProjectID(ctx context.Context, orgID, requestedProjectID int64) (int64, error) {
	if orgID <= 0 {
		return 0, fmt.Errorf("invalid org_id")
	}
	if requestedProjectID > 0 {
		var projectID int64
		err := h.db.QueryRow(ctx,
			`SELECT id FROM projects WHERE id = $1 AND org_id = $2 AND status = 'active'`,
			requestedProjectID, orgID,
		).Scan(&projectID)
		return projectID, err
	}

	var projectID int64
	err := h.db.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, 'Default Project', 'default', 'Default project created for PRD2 compatibility', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE SET updated_at = projects.updated_at
		 RETURNING id`,
		orgID,
	).Scan(&projectID)
	return projectID, err
}
