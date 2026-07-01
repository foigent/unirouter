package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
)

type modelSetItemRequest struct {
	ItemType string `json:"item_type"`
	ItemName string `json:"item_name"`
}

func (h *Handler) ListModelRegistry(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}

	conditions := []string{"org_id = $1"}
	args := []interface{}{orgID}
	argIdx := 2

	if lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle")); lifecycle != "" {
		if !modelgov.IsValidLifecycle(lifecycle) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid lifecycle"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("lifecycle = $%d", argIdx))
		args = append(args, lifecycle)
		argIdx++
	}
	if approvedOnly := r.URL.Query().Get("approved_only"); approvedOnly == "true" || approvedOnly == "1" {
		conditions = append(conditions, "approved_catalog = TRUE")
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		conditions = append(conditions, fmt.Sprintf("(model_name ILIKE $%d OR display_name ILIKE $%d OR provider_family ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, "%"+q+"%")
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM model_registry "+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, org_id, model_name, display_name, provider_family, lifecycle,
		        context_length, output_limit, capability_tags, data_residency,
		        pricing_model_name, approved_catalog, migration_recommendation,
		        created_at, updated_at
		 FROM model_registry %s
		 ORDER BY model_name ASC
		 LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		item, err := scanModelRegistryRow(rows.Scan)
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

func (h *Handler) CreateModelRegistry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID                   int64  `json:"org_id"`
		ModelName               string `json:"model_name"`
		DisplayName             string `json:"display_name"`
		ProviderFamily          string `json:"provider_family"`
		Lifecycle               string `json:"lifecycle"`
		ContextLength           int    `json:"context_length"`
		OutputLimit             int    `json:"output_limit"`
		CapabilityTags          string `json:"capability_tags"`
		DataResidency           string `json:"data_residency"`
		PricingModelName        string `json:"pricing_model_name"`
		ApprovedCatalog         bool   `json:"approved_catalog"`
		MigrationRecommendation string `json:"migration_recommendation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 {
		req.OrgID = middleware.GetOrgIDFromContext(r.Context())
	}
	if req.OrgID == 0 {
		req.OrgID = 1
	}
	req.ModelName = strings.TrimSpace(req.ModelName)
	if req.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name required"})
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.ModelName
	}
	if req.Lifecycle == "" {
		req.Lifecycle = modelgov.LifecycleStaging
	}
	if !modelgov.IsValidLifecycle(req.Lifecycle) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid lifecycle"})
		return
	}
	if req.ContextLength < 0 || req.OutputLimit < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "context_length and output_limit must be >= 0"})
		return
	}
	req.CapabilityTags = modelgov.NormalizeCSV(req.CapabilityTags)
	if req.PricingModelName == "" {
		req.PricingModelName = req.ModelName
	}

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_registry (
		    org_id, model_name, display_name, provider_family, lifecycle,
		    context_length, output_limit, capability_tags, data_residency,
		    pricing_model_name, approved_catalog, migration_recommendation,
		    created_at, updated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, req.ModelName, req.DisplayName, req.ProviderFamily, req.Lifecycle,
		req.ContextLength, req.OutputLimit, req.CapabilityTags, req.DataResidency,
		req.PricingModelName, req.ApprovedCatalog, req.MigrationRecommendation,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Model already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}

	h.recordAudit(r.Context(), "model_registry.create", "model_registry", strconv.FormatInt(id, 10),
		map[string]interface{}{"org_id": req.OrgID, "model_name": req.ModelName, "lifecycle": req.Lifecycle}, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": req.OrgID, "model_name": req.ModelName})
}

func (h *Handler) UpdateModelRegistry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		ModelName               *string `json:"model_name"`
		DisplayName             *string `json:"display_name"`
		ProviderFamily          *string `json:"provider_family"`
		Lifecycle               *string `json:"lifecycle"`
		ContextLength           *int    `json:"context_length"`
		OutputLimit             *int    `json:"output_limit"`
		CapabilityTags          *string `json:"capability_tags"`
		DataResidency           *string `json:"data_residency"`
		PricingModelName        *string `json:"pricing_model_name"`
		ApprovedCatalog         *bool   `json:"approved_catalog"`
		MigrationRecommendation *string `json:"migration_recommendation"`
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
	addString("model_name", req.ModelName)
	addString("display_name", req.DisplayName)
	addString("provider_family", req.ProviderFamily)
	if req.Lifecycle != nil {
		lifecycle := strings.TrimSpace(*req.Lifecycle)
		if !modelgov.IsValidLifecycle(lifecycle) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid lifecycle"})
			return
		}
		sets = append(sets, fmt.Sprintf("lifecycle = $%d", argIdx))
		args = append(args, lifecycle)
		argIdx++
	}
	if req.ContextLength != nil {
		if *req.ContextLength < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "context_length must be >= 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("context_length = $%d", argIdx))
		args = append(args, *req.ContextLength)
		argIdx++
	}
	if req.OutputLimit != nil {
		if *req.OutputLimit < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "output_limit must be >= 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("output_limit = $%d", argIdx))
		args = append(args, *req.OutputLimit)
		argIdx++
	}
	if req.CapabilityTags != nil {
		sets = append(sets, fmt.Sprintf("capability_tags = $%d", argIdx))
		args = append(args, modelgov.NormalizeCSV(*req.CapabilityTags))
		argIdx++
	}
	addString("data_residency", req.DataResidency)
	addString("pricing_model_name", req.PricingModelName)
	if req.ApprovedCatalog != nil {
		sets = append(sets, fmt.Sprintf("approved_catalog = $%d", argIdx))
		args = append(args, *req.ApprovedCatalog)
		argIdx++
	}
	addString("migration_recommendation", req.MigrationRecommendation)
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	beforeState := map[string]interface{}{"id": id}
	var beforeModelName, beforeLifecycle, beforeMigrationRecommendation string
	var beforeApprovedCatalog bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT model_name, lifecycle, approved_catalog, migration_recommendation FROM model_registry WHERE id = $1`,
		id,
	).Scan(&beforeModelName, &beforeLifecycle, &beforeApprovedCatalog, &beforeMigrationRecommendation); err == nil {
		beforeState = map[string]interface{}{
			"id":                       id,
			"model_name":               beforeModelName,
			"lifecycle":                beforeLifecycle,
			"approved_catalog":         beforeApprovedCatalog,
			"migration_recommendation": beforeMigrationRecommendation,
		}
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_registry SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model not found"})
		return
	}
	afterState := map[string]interface{}{"id": id}
	if req.ModelName != nil {
		afterState["model_name"] = strings.TrimSpace(*req.ModelName)
	}
	if req.Lifecycle != nil {
		afterState["lifecycle"] = strings.TrimSpace(*req.Lifecycle)
	}
	if req.ApprovedCatalog != nil {
		afterState["approved_catalog"] = *req.ApprovedCatalog
	}
	if req.MigrationRecommendation != nil {
		afterState["migration_recommendation"] = strings.TrimSpace(*req.MigrationRecommendation)
	}
	h.recordAuditFromRequest(r, "model_registry.update", "model_registry", strconv.FormatInt(id, 10), map[string]interface{}{
		"id":     id,
		"before": beforeState,
		"after":  afterState,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListApprovedModelCatalog(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}

	var projectID int64
	if projectParam := r.URL.Query().Get("project_id"); projectParam != "" {
		projectID, err = strconv.ParseInt(projectParam, 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canReadProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}

	conditions := []string{"mr.org_id = $1", "mr.approved_catalog = TRUE", "mr.lifecycle IN ('ga', 'deprecated')"}
	args := []interface{}{orgID}
	argIdx := 2
	if projectID > 0 {
		hasSets, err := h.projectHasAssignedModelSets(r.Context(), projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project model set query failed"})
			return
		}
		if hasSets {
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM project_model_sets pms
				JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.status = 'active'
				JOIN model_set_items msi ON msi.model_set_id = ms.id
				LEFT JOIN model_aliases ma2 ON ma2.org_id = mr.org_id AND ma2.target_model = mr.model_name AND ma2.status = 'active'
				WHERE pms.project_id = $%d
				  AND (
				    (msi.item_type = 'model' AND msi.item_name = mr.model_name)
				    OR (msi.item_type = 'alias' AND msi.item_name = ma2.alias)
				  )
			)`, argIdx))
			args = append(args, projectID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	var total int
	countQuery := "SELECT COUNT(DISTINCT mr.id) FROM model_registry mr " + where
	if err := h.db.QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT mr.id, mr.org_id, mr.model_name, mr.display_name, mr.provider_family, mr.lifecycle,
		        mr.context_length, mr.output_limit, mr.capability_tags, mr.data_residency,
		        mr.pricing_model_name, mr.approved_catalog, mr.migration_recommendation,
		        mr.created_at, mr.updated_at, COALESCE(string_agg(DISTINCT ma.alias, ','), '') AS aliases
		 FROM model_registry mr
		 LEFT JOIN model_aliases ma ON ma.org_id = mr.org_id AND ma.target_model = mr.model_name AND ma.status = 'active'
		 %s
		 GROUP BY mr.id
		 ORDER BY mr.model_name ASC
		 LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		item, err := scanModelRegistryRowWithAliases(rows.Scan)
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

func (h *Handler) ListModelAliases(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateModelAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID       int64  `json:"org_id"`
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
	if req.OrgID == 0 {
		req.OrgID = middleware.GetOrgIDFromContext(r.Context())
	}
	if req.OrgID == 0 {
		req.OrgID = 1
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
	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW()) RETURNING id`,
		req.OrgID, req.Alias, req.TargetModel, req.DisplayName, req.Description, req.Status,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Alias already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	h.recordAudit(r.Context(), "model_alias.create", "model_alias", strconv.FormatInt(id, 10), map[string]interface{}{"alias": req.Alias, "target_model": req.TargetModel}, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "alias": req.Alias, "target_model": req.TargetModel})
}

func (h *Handler) UpdateModelAlias(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
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
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_aliases SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Alias not found"})
		return
	}
	h.recordAudit(r.Context(), "model_alias.update", "model_alias", strconv.FormatInt(id, 10), map[string]interface{}{"id": id}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListModelSets(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateModelSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID       int64                 `json:"org_id"`
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
	if req.OrgID == 0 {
		req.OrgID = middleware.GetOrgIDFromContext(r.Context())
	}
	if req.OrgID == 0 {
		req.OrgID = 1
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
	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_sets (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW()) RETURNING id`,
		req.OrgID, req.Name, req.Slug, req.Description, req.Status,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Model set slug already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	if err := h.replaceModelSetItems(r.Context(), id, req.Items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create items failed"})
		return
	}
	h.recordAudit(r.Context(), "model_set.create", "model_set", strconv.FormatInt(id, 10), map[string]interface{}{"name": req.Name, "slug": req.Slug}, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": req.OrgID, "name": req.Name, "slug": req.Slug})
}

func (h *Handler) ReplaceModelSetItems(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
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
	if err := h.replaceModelSetItems(r.Context(), id, req.Items); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update items failed"})
		return
	}
	h.recordAudit(r.Context(), "model_set.items.replace", "model_set", strconv.FormatInt(id, 10), map[string]interface{}{"item_count": len(req.Items)}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
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
		 JOIN model_sets ms ON ms.id = pms.model_set_id
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
	if _, err := h.db.Exec(r.Context(), `DELETE FROM project_model_sets WHERE project_id = $1`, projectID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Clear assignment failed"})
		return
	}
	seen := make(map[int64]bool)
	for _, modelSetID := range req.ModelSetIDs {
		if modelSetID <= 0 || seen[modelSetID] {
			continue
		}
		seen[modelSetID] = true
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO project_model_sets (project_id, model_set_id, created_at)
			 SELECT $1, ms.id, NOW()
			 FROM model_sets ms
			 JOIN projects p ON p.id = $1 AND p.org_id = ms.org_id
			 WHERE ms.id = $2 AND ms.status = 'active'
			 ON CONFLICT DO NOTHING`,
			projectID, modelSetID,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Assign model set failed"})
			return
		}
	}
	h.recordAuditForProject(r.Context(), "project.model_sets.replace", "project", strconv.FormatInt(projectID, 10), projectID,
		map[string]interface{}{"model_set_ids": req.ModelSetIDs}, r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func orgIDFromRequest(r *http.Request) (int64, error) {
	if value := r.URL.Query().Get("org_id"); value != "" {
		orgID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || orgID <= 0 {
			return 0, fmt.Errorf("invalid org_id")
		}
		return orgID, nil
	}
	if orgID := middleware.GetOrgIDFromContext(r.Context()); orgID > 0 {
		return orgID, nil
	}
	return 1, nil
}

type rowScanner func(dest ...any) error

func scanModelRegistryRow(scan rowScanner) (map[string]interface{}, error) {
	item, _, err := scanModelRegistryRowInternal(scan, false)
	return item, err
}

func scanModelRegistryRowWithAliases(scan rowScanner) (map[string]interface{}, error) {
	item, aliases, err := scanModelRegistryRowInternal(scan, true)
	if err != nil {
		return nil, err
	}
	item["aliases"] = splitCSV(aliases)
	return item, nil
}

func scanModelRegistryRowInternal(scan rowScanner, withAliases bool) (map[string]interface{}, string, error) {
	var id, orgID int64
	var modelName, displayName, providerFamily, lifecycle, capabilityTags, dataResidency, pricingModelName, migrationRecommendation string
	var contextLength, outputLimit int
	var approved bool
	var createdAt, updatedAt time.Time
	var aliases string
	dest := []any{&id, &orgID, &modelName, &displayName, &providerFamily, &lifecycle, &contextLength, &outputLimit, &capabilityTags, &dataResidency, &pricingModelName, &approved, &migrationRecommendation, &createdAt, &updatedAt}
	if withAliases {
		dest = append(dest, &aliases)
	}
	if err := scan(dest...); err != nil {
		return nil, "", err
	}
	item := map[string]interface{}{
		"id": id, "org_id": orgID, "model_name": modelName, "display_name": displayName,
		"provider_family": providerFamily, "lifecycle": lifecycle,
		"context_length": contextLength, "output_limit": outputLimit,
		"capability_tags": splitCSV(capabilityTags), "data_residency": dataResidency,
		"pricing_model_name": pricingModelName, "approved_catalog": approved,
		"migration_recommendation": migrationRecommendation,
		"created_at":               createdAt, "updated_at": updatedAt,
	}
	return item, aliases, nil
}

func scanModelAliasRow(scan rowScanner) (map[string]interface{}, error) {
	var id, orgID int64
	var alias, targetModel, displayName, description, status string
	var createdAt, updatedAt time.Time
	if err := scan(&id, &orgID, &alias, &targetModel, &displayName, &description, &status, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "org_id": orgID, "alias": alias, "target_model": targetModel,
		"display_name": displayName, "description": description, "status": status,
		"created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func scanModelSetRow(scan rowScanner) (map[string]interface{}, error) {
	var id, orgID, itemCount int64
	var name, slug, description, status string
	var createdAt, updatedAt time.Time
	if err := scan(&id, &orgID, &name, &slug, &description, &status, &itemCount, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "org_id": orgID, "name": name, "slug": slug,
		"description": description, "status": status, "item_count": itemCount,
		"created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func splitCSV(value string) []string {
	normalized := modelgov.NormalizeCSV(value)
	if normalized == "" {
		return []string{}
	}
	return strings.Split(normalized, ",")
}

func validModelSetItems(items []modelSetItemRequest) bool {
	for _, item := range items {
		if !modelgov.IsValidModelSetItemType(item.ItemType) || strings.TrimSpace(item.ItemName) == "" {
			return false
		}
	}
	return true
}

func (h *Handler) replaceModelSetItems(ctx context.Context, modelSetID int64, items []modelSetItemRequest) error {
	if _, err := h.db.Exec(ctx, `DELETE FROM model_set_items WHERE model_set_id = $1`, modelSetID); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, item := range items {
		item.ItemType = strings.TrimSpace(item.ItemType)
		item.ItemName = strings.TrimSpace(item.ItemName)
		key := item.ItemType + ":" + item.ItemName
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := h.db.Exec(ctx,
			`INSERT INTO model_set_items (model_set_id, item_type, item_name, created_at)
			 VALUES ($1, $2, $3, NOW())
			 ON CONFLICT DO NOTHING`,
			modelSetID, item.ItemType, item.ItemName,
		); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) projectHasAssignedModelSets(ctx context.Context, projectID int64) (bool, error) {
	var count int
	err := h.db.QueryRow(ctx, `SELECT COUNT(*) FROM project_model_sets WHERE project_id = $1`, projectID).Scan(&count)
	return count > 0, err
}
