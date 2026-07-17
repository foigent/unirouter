package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

const maxProjectBudgetCents int64 = 2147483647

func (h *Handler) GetProjectBudget(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	role := actorRole(r.Context())
	allowed := false
	switch role {
	case auth.RolePlatformAdmin:
		allowed = true
	case auth.RoleOrgAdmin, auth.RoleBillingAdmin:
		allowed = h.projectBelongsToActorOrg(r.Context(), projectID)
	case auth.RoleProjectAdmin:
		allowed = h.hasProjectMembership(r.Context(), projectID, projectMemberAdmin)
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var orgID int64
	var name, status, budgetPeriod string
	var budgetCents int64
	if err := h.db.QueryRow(r.Context(),
		`SELECT org_id, name, status, COALESCE(budget_cents, 0), budget_period
		 FROM projects WHERE id = $1`, projectID,
	).Scan(&orgID, &name, &status, &budgetCents, &budgetPeriod); err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget lookup failed"})
		return
	}
	if !actorCanAccessOrg(r.Context(), orgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id": projectID, "org_id": orgID, "project_name": name, "status": status,
		"budget_cents": budgetCents, "budget_period": budgetPeriod,
	})
}

func (h *Handler) UpdateProjectBudget(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "id", "Invalid project ID")
	if !ok {
		return
	}
	role := actorRole(r.Context())
	if !auth.RoleIn(role, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	var req struct {
		BudgetCents  *int64  `json:"budget_cents"`
		BudgetPeriod *string `json:"budget_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.BudgetCents == nil && req.BudgetPeriod == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No budget fields to update"})
		return
	}
	if req.BudgetCents != nil && (*req.BudgetCents < 0 || *req.BudgetCents > maxProjectBudgetCents) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "budget_cents must be between 0 and 2147483647"})
		return
	}
	if req.BudgetPeriod != nil && !validBudgetPeriod(*req.BudgetPeriod) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid budget_period"})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget update failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var orgID int64
	var name, oldPeriod string
	var oldBudget int64
	if err := tx.QueryRow(r.Context(),
		`SELECT org_id, name, COALESCE(budget_cents, 0), budget_period
		 FROM projects WHERE id = $1 FOR UPDATE`, projectID,
	).Scan(&orgID, &name, &oldBudget, &oldPeriod); err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget update failed"})
		return
	}

	allowed := false
	switch role {
	case auth.RolePlatformAdmin:
		allowed = true
	case auth.RoleOrgAdmin, auth.RoleBillingAdmin:
		allowed = actorCanAccessOrg(r.Context(), orgID)
	case auth.RoleProjectAdmin:
		allowed, err = canManageProjectMembersTx(r.Context(), tx, projectID, orgID)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget authorization failed"})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	sets := make([]string, 0, 3)
	args := make([]interface{}, 0, 3)
	afterBudget, afterPeriod := oldBudget, oldPeriod
	if req.BudgetCents != nil {
		args = append(args, *req.BudgetCents)
		sets = append(sets, fmt.Sprintf("budget_cents = $%d", len(args)))
		afterBudget = *req.BudgetCents
	}
	if req.BudgetPeriod != nil {
		args = append(args, *req.BudgetPeriod)
		sets = append(sets, fmt.Sprintf("budget_period = $%d", len(args)))
		afterPeriod = *req.BudgetPeriod
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, projectID, orgID)
	query := fmt.Sprintf("UPDATE projects SET %s WHERE id = $%d AND org_id = $%d", strings.Join(sets, ", "), len(args)-1, len(args))
	tag, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget update failed"})
		return
	}
	if tag.RowsAffected() != 1 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Project not found"})
		return
	}
	auditDetail := map[string]interface{}{
		"before": map[string]interface{}{"budget_cents": oldBudget, "budget_period": oldPeriod},
		"after":  map[string]interface{}{"budget_cents": afterBudget, "budget_period": afterPeriod},
	}
	if err := insertAuditEntry(r.Context(), tx, "project.budget.update", "project", fmt.Sprintf("%d", projectID), auditDetail, auditMeta{
		OrgID:           orgID,
		ProjectID:       projectID,
		IP:              r.RemoteAddr,
		UserAgent:       r.UserAgent(),
		RequestID:       middleware.GetRequestIDFromContext(r.Context()),
		ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
		Source:          auditSourceFromContext(r.Context()),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget audit failed; no change was committed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project budget update failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project_id": projectID, "org_id": orgID, "project_name": name,
		"budget_cents": afterBudget, "budget_period": afterPeriod,
	})
}
