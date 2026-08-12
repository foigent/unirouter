package admin

import (
	"context"
	"database/sql"
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
	organizationRole := actorOrganizationRole(r.Context())
	if organizationRole != auth.RolePlatformAdmin && organizationRole != auth.RoleOrgAdmin && organizationRole != auth.RoleBillingAdmin && organizationRole != auth.RoleOrgViewer {
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
		        COALESCE(o.name, ''), COUNT(ak.id),
		        COALESCE((
		            SELECT pm.role FROM project_members pm
		            WHERE pm.project_id = p.id AND pm.user_id = $%d
		        ), '') AS current_user_membership_role
		 FROM projects p
		 JOIN orgs o ON o.id = p.org_id
		 LEFT JOIN api_keys ak ON ak.project_id = p.id AND ak.org_id = p.org_id
		 %s
		 GROUP BY p.id, o.name
		 ORDER BY p.id ASC
		 LIMIT $%d OFFSET $%d`,
		argIdx, whereClause, argIdx+1, argIdx+2,
	)
	args = append(args, middleware.GetUserIDFromContext(r.Context()), pageSize, offset)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	projects := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, keyCount int64
		var name, slug, description, status, budgetPeriod, orgName, membershipRole string
		var budgetCents int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &orgID, &name, &slug, &description, &status, &budgetCents, &budgetPeriod, &createdAt, &updatedAt, &orgName, &keyCount, &membershipRole); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		switch actorOrganizationRole(r.Context()) {
		case auth.RoleBillingAdmin:
			projects = append(projects, map[string]interface{}{
				"id": id, "org_id": orgID, "name": name, "slug": slug, "status": status,
				"budget_cents": budgetCents, "budget_period": budgetPeriod,
				"current_user_membership_role": canonicalProjectMembershipRole(membershipRole),
			})
		case auth.RoleOrgViewer:
			response := projectMetadataResponse(id, orgID, name, slug, description, status, orgName, createdAt, updatedAt)
			response["current_user_membership_role"] = canonicalProjectMembershipRole(membershipRole)
			projects = append(projects, response)
		default:
			response := projectResponse(id, orgID, name, slug, description, status, budgetCents, budgetPeriod, orgName, keyCount, createdAt, updatedAt)
			response["current_user_membership_role"] = canonicalProjectMembershipRole(membershipRole)
			projects = append(projects, response)
		}
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
		if _, present := middleware.GetCustomerContextFromContext(r.Context()); present {
			resolvedOrgID, resolveErr := resolveActorOrganization(r.Context(), 0)
			if resolveErr != nil {
				writeOrganizationScopeError(w, resolveErr)
				return
			}
			req.OrgID = resolvedOrgID
		} else {
			req.OrgID = middleware.GetOrgIDFromContext(r.Context())
		}
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
	if req.BudgetCents != nil && (*req.BudgetCents < 0 || int64(*req.BudgetCents) > maxProjectBudgetCents) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "budget_cents must be between 0 and 2147483647"})
		return
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

	var err error
	var tx pgx.Tx
	queryer := projectQueryRower(h.db)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create project"})
			return
		}
		defer tx.Rollback(r.Context())
		queryer = tx
		if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
			TargetOrgID:              req.OrgID,
			AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
		}); err != nil {
			if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project authorization failed"})
			}
			return
		}
	}

	var id int64
	err = queryer.QueryRow(r.Context(),
		`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, req.Name, req.Slug, req.Description, budgetArg, req.BudgetPeriod, createdBy,
	).Scan(&id)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Project slug already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create project"})
		return
	}

	detail := map[string]interface{}{"after": map[string]interface{}{
		"org_id":        req.OrgID,
		"name":          req.Name,
		"slug":          req.Slug,
		"budget_cents":  req.BudgetCents,
		"budget_period": req.BudgetPeriod,
	}}
	if tx != nil {
		if err := insertAuditEntry(r.Context(), tx, "project.create", "project", strconv.FormatInt(id, 10), detail,
			requestAuditMeta(r, req.OrgID, id)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create project"})
			return
		}
	} else {
		h.recordAuditForProjectFromRequest(r, "project.create", "project", strconv.FormatInt(id, 10), id, detail)
	}

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

	if actorOrganizationRole(r.Context()) == auth.RoleOrgViewer {
		writeJSON(w, http.StatusOK, projectMetadataResponse(id, orgID, name, slug, description, status, orgName, createdAt, updatedAt))
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
		if *req.BudgetCents < 0 || int64(*req.BudgetCents) > maxProjectBudgetCents {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "budget_cents must be between 0 and 2147483647"})
			return
		}
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

	var tx pgx.Tx
	execer := auditExecer(h.db)
	queryer := projectQueryRower(h.db)
	if !isUnauthenticatedUnitContext(r.Context()) {
		tx, err = h.db.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update project"})
			return
		}
		defer tx.Rollback(r.Context())
		execer = tx
		queryer = tx
		if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{
			AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin},
			Project: &committingActorProjectRequirement{
				ProjectID:    id,
				AllowedRoles: []string{projectMemberAdmin},
			},
		}); err != nil {
			if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project authorization failed"})
			}
			return
		}
	}
	before, err := projectAuditSnapshot(r.Context(), queryer, id, tx != nil)
	if err != nil {
		if tx == nil {
			before = nil
		} else if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read project"})
		}
		if tx != nil {
			return
		}
	}
	targetOrgID := middleware.GetOrgIDFromContext(r.Context())
	if before != nil {
		targetOrgID, _ = before["org_id"].(int64)
	}
	if tx != nil && !actorCanAccessOrg(r.Context(), targetOrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
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
	tag, err := execer.Exec(r.Context(), query, args...)
	if err != nil {
		if writeCommercialLimitViolation(w, err) {
			return
		}
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
	if before != nil {
		detail["before"] = before
	}
	if tx != nil {
		if err := insertAuditEntry(r.Context(), tx, "project.update", "project", strconv.FormatInt(id, 10), detail,
			requestAuditMeta(r, targetOrgID, id)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update project"})
			return
		}
	} else {
		h.recordAuditForProjectFromRequest(r, "project.update", "project", strconv.FormatInt(id, 10), id, detail)
	}

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

func projectMetadataResponse(id, orgID int64, name, slug, description, status, orgName string, createdAt, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":          id,
		"org_id":      orgID,
		"name":        name,
		"slug":        slug,
		"description": description,
		"status":      status,
		"created_at":  createdAt,
		"updated_at":  updatedAt,
		"org_name":    orgName,
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

type projectQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func projectAuditSnapshot(ctx context.Context, q projectQueryRower, projectID int64, lock bool) (map[string]interface{}, error) {
	var orgID int64
	var name, slug, description, status, budgetPeriod string
	var budgetCents sql.NullInt64
	query := `SELECT org_id, name, slug, description, status, budget_cents, budget_period
		 FROM projects
		 WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	err := q.QueryRow(ctx,
		query,
		projectID,
	).Scan(&orgID, &name, &slug, &description, &status, &budgetCents, &budgetPeriod)
	if err != nil {
		return nil, err
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
	return snapshot, nil
}
