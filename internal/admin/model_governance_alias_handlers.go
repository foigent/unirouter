package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/modelgov"
)

func (h *Handler) ListModelAliases(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_aliases WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT id, org_id, alias, target_model, display_name, description, status, created_at, updated_at
		 FROM model_aliases WHERE org_id = $1 ORDER BY alias ASC LIMIT $2 OFFSET $3`,
		orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		item, err := scanModelAliasRow(rows.Scan)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateModelAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID       *int64 `json:"org_id"`
		Alias       string `json:"alias"`
		TargetModel string `json:"target_model"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	orgID, err := resolveModelGovernanceCreateOrganization(r.Context(), req.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	req.Alias = strings.TrimSpace(req.Alias)
	req.TargetModel = strings.TrimSpace(req.TargetModel)
	if req.Alias == "" || req.TargetModel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "alias and target_model required"})
		return
	}
	if req.Status == "" {
		req.Status = modelgov.AliasStatusActive
	}
	if !modelgov.IsValidAliasStatus(req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW()) RETURNING id`,
		orgID, req.Alias, req.TargetModel, req.DisplayName, req.Description, req.Status,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Alias already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	afterState := map[string]interface{}{
		"id": id, "org_id": orgID, "alias": req.Alias, "target_model": req.TargetModel,
		"display_name": req.DisplayName, "description": req.Description, "status": req.Status,
	}
	if err := insertAuditEntry(r.Context(), tx, "model_alias.create", "model_alias", strconv.FormatInt(id, 10),
		map[string]interface{}{"org_id": orgID, "alias": req.Alias, "target_model": req.TargetModel, "after": afterState},
		modelGovernanceAuditMeta(r, orgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": orgID, "alias": req.Alias, "target_model": req.TargetModel})
}

func (h *Handler) UpdateModelAlias(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	scope, err := resolveModelGovernancePathScope(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	var req struct {
		Alias       *string `json:"alias"`
		TargetModel *string `json:"target_model"`
		DisplayName *string `json:"display_name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	var sets []string
	var args []interface{}
	argIdx := 1
	addString := func(column string, value *string) {
		if value != nil {
			sets = append(sets, fmt.Sprintf("%s = $%d", column, argIdx))
			args = append(args, strings.TrimSpace(*value))
			argIdx++
		}
	}
	addString("alias", req.Alias)
	addString("target_model", req.TargetModel)
	addString("display_name", req.DisplayName)
	addString("description", req.Description)
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !modelgov.IsValidAliasStatus(status) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var beforeOrgID int64
	var beforeAlias, beforeTargetModel, beforeDisplayName, beforeDescription, beforeStatus string
	beforeQuery := `SELECT org_id, alias, target_model, display_name, description, status FROM model_aliases WHERE id = $1`
	beforeArgs := []interface{}{id}
	if scope.constrained {
		beforeQuery += ` AND org_id = $2`
		beforeArgs = append(beforeArgs, scope.orgID)
	}
	beforeQuery += ` FOR UPDATE`
	if err := tx.QueryRow(r.Context(), beforeQuery, beforeArgs...).Scan(
		&beforeOrgID, &beforeAlias, &beforeTargetModel, &beforeDisplayName, &beforeDescription, &beforeStatus,
	); err != nil {
		writeModelGovernanceLookupError(w, err, "Alias")
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id, beforeOrgID)
	query := fmt.Sprintf("UPDATE model_aliases SET %s WHERE id = $%d AND org_id = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Alias not found"})
		return
	}
	afterState := map[string]interface{}{
		"id": id, "org_id": beforeOrgID, "alias": beforeAlias, "target_model": beforeTargetModel,
		"display_name": beforeDisplayName, "description": beforeDescription, "status": beforeStatus,
	}
	if req.Alias != nil {
		afterState["alias"] = strings.TrimSpace(*req.Alias)
	}
	if req.TargetModel != nil {
		afterState["target_model"] = strings.TrimSpace(*req.TargetModel)
	}
	if req.DisplayName != nil {
		afterState["display_name"] = strings.TrimSpace(*req.DisplayName)
	}
	if req.Description != nil {
		afterState["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Status != nil {
		afterState["status"] = strings.TrimSpace(*req.Status)
	}
	if err := insertAuditEntry(r.Context(), tx, "model_alias.update", "model_alias", strconv.FormatInt(id, 10), map[string]interface{}{
		"id": id,
		"before": map[string]interface{}{
			"id": id, "org_id": beforeOrgID, "alias": beforeAlias, "target_model": beforeTargetModel,
			"display_name": beforeDisplayName, "description": beforeDescription, "status": beforeStatus,
		},
		"after": afterState,
	}, modelGovernanceAuditMeta(r, beforeOrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
