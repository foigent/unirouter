package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
)

func (h *Handler) ListApprovedModelCatalog(w http.ResponseWriter, r *http.Request) {
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
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}

	var projectID int64
	var orgID int64
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
		if err := h.db.QueryRow(r.Context(), `SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&orgID); err != nil {
			writeModelGovernanceLookupError(w, err, "Project")
			return
		}
		if !actorCanAccessOrg(r.Context(), orgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		if requestedOrgID > 0 && requestedOrgID != orgID {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Project organization mismatch"})
			return
		}
	} else {
		if organizationRole == auth.RoleMember {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "project_id is required"})
			return
		}
		orgID, err = resolveActorOrganization(r.Context(), requestedOrgID)
		if err != nil {
			writeOrganizationScopeError(w, err)
			return
		}
	}

	conditions := []string{"mr.org_id = $1", "mr.approved_catalog = TRUE", "mr.lifecycle IN ('ga', 'deprecated')"}
	args := []interface{}{orgID}
	argIdx := 2
	if routableOnly := r.URL.Query().Get("routable_only"); routableOnly == "true" || routableOnly == "1" {
		// Discovery projects existing manifests; it does not authorize execution.
		// V2 has normalized deployment/operation items, not legacy overrides.
		// Keep the historical v1 branch explicit, never as fallback evidence for v2.
		conditions = append(conditions, `(EXISTS (
			SELECT 1
			FROM runtime_change_slots slot
			JOIN runtime_change_releases release ON release.id = slot.stable_release_id
			JOIN runtime_route_revision_items item ON item.release_id = release.id
			JOIN model_deployments deployment ON deployment.id = item.deployment_id
			WHERE slot.public_model_id = mr.id
			  AND release.public_model_id = mr.id
			  AND deployment.public_model_id = mr.id
			  AND deployment.customer_org_id = mr.org_id
			  AND release.stage = 'active'
			  AND release.candidate_version = 'unirouter.runtime-change-candidate.v2'
			  AND release.route_items_sealed_at IS NOT NULL
			  AND item.source_candidate_version = release.candidate_version
			  AND item.admission = 'allow'
		) OR EXISTS (
			SELECT 1
			FROM runtime_channel_assignments_v1 assignment
			JOIN runtime_change_releases legacy ON legacy.id = assignment.release_id
			WHERE assignment.model_name = mr.model_name
			  AND legacy.candidate_version = 'unirouter.runtime-change-candidate.v1'
			  AND assignment.assignment_kind = 'stable'
			  AND assignment.release_stage = 'active'
			  AND assignment.admission = 'allow'
		))`)
	}
	if projectID > 0 {
		hasSets, err := h.projectHasAssignedModelSets(r.Context(), projectID, orgID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Project model set query failed"})
			return
		}
		if hasSets {
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM project_model_sets pms
				JOIN projects p ON p.id = pms.project_id AND p.org_id = mr.org_id
				JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.status = 'active' AND ms.org_id = p.org_id
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
		        mr.execution_profile_id, mr.execution_profile_version,
		        mr.created_at, mr.updated_at, COALESCE(string_agg(DISTINCT ma.alias, ','), '') AS aliases,
		        COALESCE(MAX(ma.alias) FILTER (WHERE ma.is_primary), mr.model_name) AS request_model
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
	canSeeCanonical := auth.RoleIn(organizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	for rows.Next() {
		item, requestModel, err := scanModelRegistryRowWithAliasesAndRequestIdentity(rows.Scan)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		canonicalModel, _ := item["model_name"].(string)
		item["request_model"] = requestModel
		if canSeeCanonical {
			item["canonical_model"] = canonicalModel
		} else {
			item["model_name"] = requestModel
			item["aliases"] = ""
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": total, "page": page, "page_size": pageSize})
}
