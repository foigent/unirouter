package admin

import (
	"fmt"
	"net/http"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

// ListOrganizations returns the canonical customer choice. The deployment
// operator organization is intentionally not part of this customer-domain API.
func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	if customer, present := middleware.GetCustomerContextFromContext(r.Context()); present {
		platform := auth.IsPlatformAdmin(actorOrganizationRole(r.Context()))
		if !platform && (!customer.Ready() || middleware.GetOrgIDFromContext(r.Context()) != customer.OrganizationID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		organizations := make([]map[string]interface{}, 0, 1)
		total := 0
		if customer.Ready() {
			total = 1
			if page == 1 && pageSize > 0 {
				organizations = append(organizations, map[string]interface{}{
					"id": customer.OrganizationID, "name": customer.OrganizationName, "status": "active",
				})
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": organizations, "total": total})
		return
	}
	offset := paginationOffset(page, pageSize)
	whereClause := ""
	args := make([]interface{}, 0, 3)
	nextArg := 1

	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		whereClause = "WHERE id = $1"
		args = append(args, actorOrgID)
		nextArg++
	}

	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM orgs "+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, name, status
		 FROM orgs
		 %s
		 ORDER BY name ASC, id ASC LIMIT $%d OFFSET $%d`,
		whereClause, nextArg, nextArg+1,
	)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	organizations := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var name, status string
		if err := rows.Scan(&id, &name, &status); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		organizations = append(organizations, map[string]interface{}{
			"id": id, "name": name, "status": status,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": organizations, "total": total})
}
