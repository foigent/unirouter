package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modelgov"
)

func (h *Handler) ListModelSets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	organizationRole := actorOrganizationRole(r.Context())
	if !isUnauthenticatedUnitContext(r.Context()) && organizationRole == auth.RoleBillingAdmin {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) && !auth.RoleIn(organizationRole,
		auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleOrgViewer, auth.RoleMember) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if organizationRole == auth.RoleMember {
		projectID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("project_id")), 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "project_id is required"})
			return
		}
		if !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	var total int
	if err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_sets WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT ms.id, ms.org_id, ms.name, ms.slug, ms.description, ms.status,
		        COUNT(msi.item_name), ms.created_at, ms.updated_at
		 FROM model_sets ms
		 LEFT JOIN model_set_items msi ON msi.model_set_id = ms.id
		 WHERE ms.org_id = $1
		 GROUP BY ms.id
		 ORDER BY ms.name ASC
		 LIMIT $2 OFFSET $3`,
		orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		item, err := scanModelSetRow(rows.Scan)
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

func (h *Handler) CreateModelSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID       *int64                `json:"org_id"`
		Name        string                `json:"name"`
		Slug        string                `json:"slug"`
		Description string                `json:"description"`
		Status      string                `json:"status"`
		Items       []modelSetItemRequest `json:"items"`
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
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name required"})
		return
	}
	if req.Slug == "" {
		req.Slug = modelgov.NormalizeSlug(req.Name)
	} else {
		req.Slug = modelgov.NormalizeSlug(req.Slug)
	}
	if req.Status == "" {
		req.Status = modelgov.ModelSetStatusActive
	}
	if !modelgov.IsValidModelSetStatus(req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
		return
	}
	if !validModelSetItems(req.Items) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model set item"})
		return
	}
	req.Items = normalizeModelSetItems(req.Items)
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW()) RETURNING id`,
		orgID, req.Name, req.Slug, req.Description, req.Status,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Model set slug already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := replaceModelSetItems(r.Context(), tx, id, orgID, req.Items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create items failed"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_set.create", "model_set", strconv.FormatInt(id, 10), map[string]interface{}{
		"org_id": orgID,
		"after":  map[string]interface{}{"id": id, "org_id": orgID, "name": req.Name, "slug": req.Slug, "status": req.Status, "item_count": len(req.Items)},
	}, modelGovernanceAuditMeta(r, orgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": orgID, "name": req.Name, "slug": req.Slug})
}

func (h *Handler) ReplaceModelSetItems(w http.ResponseWriter, r *http.Request) {
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
		Items []modelSetItemRequest `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if !validModelSetItems(req.Items) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model set item"})
		return
	}
	req.Items = normalizeModelSetItems(req.Items)
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update items failed"})
		return
	}
	defer tx.Rollback(r.Context())
	targetOrgID, err := readModelSetTargetOrg(r.Context(), tx, id, scope, true)
	if err != nil {
		writeModelGovernanceLookupError(w, err, "Model set")
		return
	}
	var beforeCount int
	if err := tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_set_items WHERE model_set_id = $1`, id).Scan(&beforeCount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Read items failed"})
		return
	}
	if err := replaceModelSetItems(r.Context(), tx, id, targetOrgID, req.Items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update items failed"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_set.items.replace", "model_set", strconv.FormatInt(id, 10), map[string]interface{}{
		"org_id": targetOrgID,
		"before": map[string]interface{}{"item_count": beforeCount},
		"after":  map[string]interface{}{"item_count": len(req.Items)},
	}, modelGovernanceAuditMeta(r, targetOrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update items failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListModelSetItems(w http.ResponseWriter, r *http.Request) {
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
	orgID, err := readModelSetTargetOrg(r.Context(), h.db, id, scope, false)
	if err != nil {
		writeModelGovernanceLookupError(w, err, "Model set")
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT msi.item_type, msi.item_name
		 FROM model_set_items msi
		 JOIN model_sets ms ON ms.id = msi.model_set_id
		 WHERE msi.model_set_id = $1 AND ms.org_id = $2
		 ORDER BY msi.item_type ASC, msi.item_name ASC`,
		id, orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var itemType, itemName string
		if err := rows.Scan(&itemType, &itemName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		items = append(items, map[string]interface{}{"item_type": itemType, "item_name": itemName})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) ListProjectModelSets(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || projectID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project ID"})
		return
	}
	if !h.canReadProject(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT ms.id, ms.org_id, ms.name, ms.slug, ms.description, ms.status,
		        COUNT(msi.item_name), ms.created_at, ms.updated_at
		 FROM project_model_sets pms
		 JOIN projects p ON p.id = pms.project_id
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.org_id = p.org_id
		 LEFT JOIN model_set_items msi ON msi.model_set_id = ms.id
		 WHERE pms.project_id = $1
		 GROUP BY ms.id
		 ORDER BY ms.name ASC`,
		projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		item, err := scanModelSetRow(rows.Scan)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
}

func (h *Handler) ReplaceProjectModelSets(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || projectID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project ID"})
		return
	}
	if !h.canWriteProject(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	var req struct {
		ModelSetIDs []int64 `json:"model_set_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	seen := make(map[int64]bool)
	modelSetIDs := make([]int64, 0, len(req.ModelSetIDs))
	for _, modelSetID := range req.ModelSetIDs {
		if modelSetID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model_set_id"})
			return
		}
		if seen[modelSetID] {
			continue
		}
		seen[modelSetID] = true
		modelSetIDs = append(modelSetIDs, modelSetID)
	}
	// Lock model sets in one deterministic order. FOR SHARE prevents a
	// concurrent status update from disabling a set after validation but before
	// this transaction commits its assignment.
	sort.Slice(modelSetIDs, func(i, j int) bool { return modelSetIDs[i] < modelSetIDs[j] })

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Replace assignment failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var projectOrgID int64
	if err := tx.QueryRow(r.Context(), `SELECT org_id FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&projectOrgID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}
	if !actorCanAccessOrg(r.Context(), projectOrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	for _, modelSetID := range modelSetIDs {
		var validatedID int64
		if err := tx.QueryRow(r.Context(),
			`SELECT id FROM model_sets WHERE id = $1 AND org_id = $2 AND status = 'active' FOR SHARE`,
			modelSetID, projectOrgID,
		).Scan(&validatedID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid model_set_id"})
			return
		}
	}

	if _, err := tx.Exec(r.Context(),
		`DELETE FROM project_model_sets pms
		 USING projects p
		 WHERE pms.project_id = $1 AND p.id = pms.project_id AND p.org_id = $2`,
		projectID, projectOrgID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Clear assignment failed"})
		return
	}
	for _, modelSetID := range modelSetIDs {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO project_model_sets (project_id, model_set_id, created_at)
			 VALUES ($1, $2, NOW())
			 ON CONFLICT DO NOTHING`,
			projectID, modelSetID,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Assign model set failed"})
			return
		}
	}
	audit := modelGovernanceAuditMeta(r, projectOrgID)
	audit.ProjectID = projectID
	if err := insertAuditEntry(r.Context(), tx, "project.model_sets.replace", "project", strconv.FormatInt(projectID, 10),
		map[string]interface{}{"model_set_ids": modelSetIDs}, audit); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Replace assignment failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
