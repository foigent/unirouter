package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var conditions []string
	var args []interface{}
	argIdx := 1

	if orgIDParam := r.URL.Query().Get("org_id"); orgIDParam != "" {
		orgID, err := strconv.ParseInt(orgIDParam, 10, 64)
		if err != nil || orgID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
			return
		}
		if !actorCanAccessOrg(r.Context(), orgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.org_id = $%d", argIdx))
		args = append(args, orgID)
		argIdx++
	} else if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.org_id = $%d", argIdx))
		args = append(args, actorOrgID)
		argIdx++
	} else if orgID := middleware.GetOrgIDFromContext(r.Context()); orgID > 0 {
		conditions = append(conditions, fmt.Sprintf("p.org_id = $%d", argIdx))
		args = append(args, orgID)
		argIdx++
	}

	if status := r.URL.Query().Get("status"); status != "" {
		if !validProjectStatus(status) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("p.status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	if auth.IsProjectScopedRole(actorRole(r.Context())) {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM project_members pm
			JOIN users scope_user ON scope_user.id = pm.user_id AND scope_user.org_id = p.org_id
			WHERE pm.project_id = p.id AND pm.user_id = $%d
		)`, argIdx))
		args = append(args, middleware.GetUserIDFromContext(r.Context()))
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM projects p"+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT p.id, p.org_id, p.name, p.slug, p.description, p.status,
		        COALESCE(p.budget_cents, 0), p.budget_period, p.created_at, p.updated_at,
		        COALESCE(o.name, ''), COUNT(ak.id)
		 FROM projects p
		 JOIN orgs o ON o.id = p.org_id
		 LEFT JOIN api_keys ak ON ak.project_id = p.id AND ak.org_id = p.org_id
		 %s
		 GROUP BY p.id, o.name
		 ORDER BY p.id ASC
		 LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	projects := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, keyCount int64
		var name, slug, description, status, budgetPeriod, orgName string
		var budgetCents int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &orgID, &name, &slug, &description, &status, &budgetCents, &budgetPeriod, &createdAt, &updatedAt, &orgName, &keyCount); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		projects = append(projects, projectResponse(id, orgID, name, slug, description, status, budgetCents, budgetPeriod, orgName, keyCount, createdAt, updatedAt))
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": projects, "total": total})
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID        int64  `json:"org_id"`
		Name         string `json:"name"`
		Slug         string `json:"slug"`
		Description  string `json:"description"`
		BudgetCents  *int   `json:"budget_cents"`
		BudgetPeriod string `json:"budget_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 {
		req.OrgID = middleware.GetOrgIDFromContext(r.Context())
	}
	if req.OrgID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id is required"})
		return
	}
	if !actorCanAccessOrg(r.Context(), req.OrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
		return
	}
	if req.Slug == "" {
		req.Slug = normalizeProjectSlug(req.Name)
	} else {
		req.Slug = normalizeProjectSlug(req.Slug)
	}
	if req.BudgetPeriod == "" {
		req.BudgetPeriod = "monthly"
	}
	if !validBudgetPeriod(req.BudgetPeriod) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid budget_period"})
		return
	}

	var budgetArg interface{}
	if req.BudgetCents != nil {
		budgetArg = *req.BudgetCents
	}
	var createdBy interface{}
	if userID := middleware.GetUserIDFromContext(r.Context()); userID > 0 {
		createdBy = userID
	}

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, req.Name, req.Slug, req.Description, budgetArg, req.BudgetPeriod, createdBy,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project slug already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create project"})
		return
	}

	h.recordAuditForProjectFromRequest(r, "project.create", "project", strconv.FormatInt(id, 10), id,
		map[string]interface{}{"after": map[string]interface{}{
			"org_id":        req.OrgID,
			"name":          req.Name,
			"slug":          req.Slug,
			"budget_cents":  req.BudgetCents,
			"budget_period": req.BudgetPeriod,
		}})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": req.OrgID, "name": req.Name, "slug": req.Slug})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project ID"})
		return
	}
	if !h.canReadProject(r.Context(), id) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var orgID, keyCount int64
	var name, slug, description, status, budgetPeriod, orgName string
	var budgetCents int
	var createdAt, updatedAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT p.id, p.org_id, p.name, p.slug, p.description, p.status,
		        COALESCE(p.budget_cents, 0), p.budget_period, p.created_at, p.updated_at,
		        COALESCE(o.name, ''), COUNT(ak.id)
		 FROM projects p
		 JOIN orgs o ON o.id = p.org_id
		 LEFT JOIN api_keys ak ON ak.project_id = p.id AND ak.org_id = p.org_id
		 WHERE p.id = $1
		 GROUP BY p.id, o.name`, id,
	).Scan(&id, &orgID, &name, &slug, &description, &status, &budgetCents, &budgetPeriod, &createdAt, &updatedAt, &orgName, &keyCount)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}

	writeJSON(w, http.StatusOK, projectResponse(id, orgID, name, slug, description, status, budgetCents, budgetPeriod, orgName, keyCount, createdAt, updatedAt))
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project ID"})
		return
	}
	if !h.canWriteProject(r.Context(), id) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var req struct {
		Name         *string `json:"name"`
		Slug         *string `json:"slug"`
		Description  *string `json:"description"`
		Status       *string `json:"status"`
		BudgetCents  *int    `json:"budget_cents"`
		BudgetPeriod *string `json:"budget_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	before, beforeFound := h.projectAuditSnapshot(r.Context(), id)

	var sets []string
	var args []interface{}
	var changed []string
	after := map[string]interface{}{}
	argIdx := 1

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name cannot be empty"})
			return
		}
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, name)
		changed = append(changed, "name")
		after["name"] = name
		argIdx++
	}
	if req.Slug != nil {
		slug := normalizeProjectSlug(*req.Slug)
		sets = append(sets, fmt.Sprintf("slug = $%d", argIdx))
		args = append(args, slug)
		changed = append(changed, "slug")
		after["slug"] = slug
		argIdx++
	}
	if req.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *req.Description)
		changed = append(changed, "description")
		after["description"] = *req.Description
		argIdx++
	}
	if req.Status != nil {
		if !validProjectStatus(*req.Status) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid status"})
			return
		}
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		changed = append(changed, "status")
		after["status"] = *req.Status
		argIdx++
	}
	if req.BudgetCents != nil {
		sets = append(sets, fmt.Sprintf("budget_cents = $%d", argIdx))
		args = append(args, *req.BudgetCents)
		changed = append(changed, "budget_cents")
		after["budget_cents"] = *req.BudgetCents
		argIdx++
	}
	if req.BudgetPeriod != nil {
		if !validBudgetPeriod(*req.BudgetPeriod) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid budget_period"})
			return
		}
		sets = append(sets, fmt.Sprintf("budget_period = $%d", argIdx))
		args = append(args, *req.BudgetPeriod)
		changed = append(changed, "budget_period")
		after["budget_period"] = *req.BudgetPeriod
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	idArg := argIdx
	args = append(args, id)
	whereClause := fmt.Sprintf("id = $%d", idArg)
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		whereClause += fmt.Sprintf(" AND org_id = $%d", idArg+1)
		args = append(args, actorOrgID)
	}
	query := fmt.Sprintf("UPDATE projects SET %s WHERE %s", strings.Join(sets, ", "), whereClause)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project slug already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update project"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}

	detail := map[string]interface{}{"fields": changed, "after": after}
	if beforeFound {
		detail["before"] = before
	}
	h.recordAuditForProjectFromRequest(r, "project.update", "project", strconv.FormatInt(id, 10), id, detail)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Project updated"})
}

func projectResponse(id, orgID int64, name, slug, description, status string, budgetCents int, budgetPeriod, orgName string, keyCount int64, createdAt, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":            id,
		"org_id":        orgID,
		"name":          name,
		"slug":          slug,
		"description":   description,
		"status":        status,
		"budget_cents":  budgetCents,
		"budget_period": budgetPeriod,
		"created_at":    createdAt,
		"updated_at":    updatedAt,
		"org_name":      orgName,
		"key_count":     keyCount,
	}
}

func normalizeProjectSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "project"
	}
	return slug
}

func validProjectStatus(status string) bool {
	return status == "active" || status == "disabled" || status == "archived"
}

func validBudgetPeriod(period string) bool {
	return period == "none" || period == "daily" || period == "monthly"
}

func (h *Handler) projectAuditSnapshot(ctx context.Context, projectID int64) (map[string]interface{}, bool) {
	var orgID int64
	var name, slug, description, status, budgetPeriod string
	var budgetCents sql.NullInt64
	err := h.db.QueryRow(ctx,
		`SELECT org_id, name, slug, description, status, budget_cents, budget_period
		 FROM projects
		 WHERE id = $1`,
		projectID,
	).Scan(&orgID, &name, &slug, &description, &status, &budgetCents, &budgetPeriod)
	if err != nil {
		return nil, false
	}
	snapshot := map[string]interface{}{
		"org_id":        orgID,
		"name":          name,
		"slug":          slug,
		"description":   description,
		"status":        status,
		"budget_period": budgetPeriod,
	}
	if budgetCents.Valid {
		snapshot["budget_cents"] = budgetCents.Int64
	} else {
		snapshot["budget_cents"] = nil
	}
	return snapshot, true
}
