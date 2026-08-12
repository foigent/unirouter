package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
)

type modelSetItemRequest struct {
	ItemType string `json:"item_type"`
	ItemName string `json:"item_name"`
}

const maxContextLengthTokens int64 = 2147483647

const (
	contextLengthErrorCode  = "invalid_context_length"
	contextLengthReasonCode = "integer_tokens_0_to_2147483647_required"
)

var errInvalidContextLength = errors.New("invalid context_length")

// contextLengthInput keeps the public JSON contract on exact integer tokens.
// K/M shorthand is a UI-only input convenience and is never accepted by the
// canonical API.
type contextLengthInput int

func (value *contextLengthInput) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return errInvalidContextLength
	}
	for _, char := range raw {
		if char < '0' || char > '9' {
			return errInvalidContextLength
		}
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed > maxContextLengthTokens {
		return errInvalidContextLength
	}
	*value = contextLengthInput(parsed)
	return nil
}

func writeContextLengthValidationError(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":       "context_length must be an integer token count between 0 and 2147483647",
		"code":        contextLengthErrorCode,
		"field":       "context_length",
		"reason_code": contextLengthReasonCode,
	})
}

type modelRegistryAuditState struct {
	OrgID                   int64
	ModelName               string
	DisplayName             string
	ProviderFamily          string
	Lifecycle               string
	ContextLength           int
	OutputLimit             int
	CapabilityTags          string
	DataResidency           string
	PricingModelName        string
	ApprovedCatalog         bool
	MigrationRecommendation string
}

func scanModelRegistryAuditState(row pgx.Row) (modelRegistryAuditState, error) {
	var state modelRegistryAuditState
	err := row.Scan(
		&state.OrgID, &state.ModelName, &state.DisplayName, &state.ProviderFamily,
		&state.Lifecycle, &state.ContextLength, &state.OutputLimit, &state.CapabilityTags,
		&state.DataResidency, &state.PricingModelName, &state.ApprovedCatalog,
		&state.MigrationRecommendation,
	)
	return state, err
}

func (state modelRegistryAuditState) auditMap(id int64) map[string]interface{} {
	return map[string]interface{}{
		"id": id, "org_id": state.OrgID, "model_name": state.ModelName,
		"display_name": state.DisplayName, "provider_family": state.ProviderFamily,
		"lifecycle": state.Lifecycle, "context_length": state.ContextLength,
		"output_limit": state.OutputLimit, "capability_tags": state.CapabilityTags,
		"data_residency": state.DataResidency, "pricing_model_name": state.PricingModelName,
		"approved_catalog":         state.ApprovedCatalog,
		"migration_recommendation": state.MigrationRecommendation,
	}
}

func (h *Handler) ListModelRegistry(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
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
		OrgID                   *int64             `json:"org_id"`
		ModelName               string             `json:"model_name"`
		DisplayName             string             `json:"display_name"`
		ProviderFamily          string             `json:"provider_family"`
		Lifecycle               string             `json:"lifecycle"`
		ContextLength           contextLengthInput `json:"context_length"`
		OutputLimit             int                `json:"output_limit"`
		CapabilityTags          string             `json:"capability_tags"`
		DataResidency           string             `json:"data_residency"`
		PricingModelName        string             `json:"pricing_model_name"`
		ApprovedCatalog         bool               `json:"approved_catalog"`
		MigrationRecommendation string             `json:"migration_recommendation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if errors.Is(err, errInvalidContextLength) {
			writeContextLengthValidationError(w)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	orgID, err := resolveModelGovernanceCreateOrganization(r.Context(), req.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
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
	if req.OutputLimit < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "output_limit must be >= 0"})
		return
	}
	req.CapabilityTags = modelgov.NormalizeCSV(req.CapabilityTags)
	if req.PricingModelName == "" {
		req.PricingModelName = req.ModelName
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO model_registry (
		    org_id, model_name, display_name, provider_family, lifecycle,
		    context_length, output_limit, capability_tags, data_residency,
		    pricing_model_name, approved_catalog, migration_recommendation,
		    created_at, updated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
		 RETURNING id`,
		orgID, req.ModelName, req.DisplayName, req.ProviderFamily, req.Lifecycle,
		int(req.ContextLength), req.OutputLimit, req.CapabilityTags, req.DataResidency,
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

	afterState := map[string]interface{}{
		"id": id, "org_id": orgID, "model_name": req.ModelName, "display_name": req.DisplayName,
		"provider_family": req.ProviderFamily, "lifecycle": req.Lifecycle,
		"context_length": int(req.ContextLength), "output_limit": req.OutputLimit,
		"capability_tags": req.CapabilityTags, "data_residency": req.DataResidency,
		"pricing_model_name": req.PricingModelName, "approved_catalog": req.ApprovedCatalog,
		"migration_recommendation": req.MigrationRecommendation,
	}
	if err := insertAuditEntry(r.Context(), tx, "model_registry.create", "model_registry", strconv.FormatInt(id, 10),
		map[string]interface{}{"org_id": orgID, "model_name": req.ModelName, "lifecycle": req.Lifecycle, "after": afterState},
		modelGovernanceAuditMeta(r, orgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "org_id": orgID, "model_name": req.ModelName})
}

func (h *Handler) UpdateModelRegistry(w http.ResponseWriter, r *http.Request) {
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
		ModelName               *string             `json:"model_name"`
		DisplayName             *string             `json:"display_name"`
		ProviderFamily          *string             `json:"provider_family"`
		Lifecycle               *string             `json:"lifecycle"`
		ContextLength           *contextLengthInput `json:"context_length"`
		OutputLimit             *int                `json:"output_limit"`
		CapabilityTags          *string             `json:"capability_tags"`
		DataResidency           *string             `json:"data_residency"`
		PricingModelName        *string             `json:"pricing_model_name"`
		ApprovedCatalog         *bool               `json:"approved_catalog"`
		MigrationRecommendation *string             `json:"migration_recommendation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if errors.Is(err, errInvalidContextLength) {
			writeContextLengthValidationError(w)
			return
		}
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
		sets = append(sets, fmt.Sprintf("context_length = $%d", argIdx))
		args = append(args, int(*req.ContextLength))
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
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	defer tx.Rollback(r.Context())

	beforeQuery := `SELECT org_id, model_name, display_name, provider_family, lifecycle,
		context_length, output_limit, capability_tags, data_residency, pricing_model_name,
		approved_catalog, migration_recommendation
		FROM model_registry WHERE id = $1`
	beforeArgs := []interface{}{id}
	if scope.constrained {
		beforeQuery += ` AND org_id = $2`
		beforeArgs = append(beforeArgs, scope.orgID)
	}
	beforeQuery += ` FOR UPDATE`
	beforeAuditState, err := scanModelRegistryAuditState(tx.QueryRow(r.Context(), beforeQuery, beforeArgs...))
	if err != nil {
		writeModelGovernanceLookupError(w, err, "Model")
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id, beforeAuditState.OrgID)
	query := fmt.Sprintf("UPDATE model_registry SET %s WHERE id = $%d AND org_id = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model not found"})
		return
	}
	afterAuditState, err := scanModelRegistryAuditState(tx.QueryRow(r.Context(),
		`SELECT org_id, model_name, display_name, provider_family, lifecycle,
		        context_length, output_limit, capability_tags, data_residency, pricing_model_name,
		        approved_catalog, migration_recommendation
		 FROM model_registry WHERE id = $1 AND org_id = $2`, id, beforeAuditState.OrgID,
	))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update readback failed"})
		return
	}
	if err := insertAuditEntry(r.Context(), tx, "model_registry.update", "model_registry", strconv.FormatInt(id, 10), map[string]interface{}{
		"id":     id,
		"before": beforeAuditState.auditMap(id),
		"after":  afterAuditState.auditMap(id),
	}, modelGovernanceAuditMeta(r, beforeAuditState.OrgID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if req.Lifecycle != nil {
		lifecycle := strings.TrimSpace(*req.Lifecycle)
		modelName := beforeAuditState.ModelName
		if req.ModelName != nil {
			modelName = strings.TrimSpace(*req.ModelName)
		}
		recommendation := beforeAuditState.MigrationRecommendation
		if req.MigrationRecommendation != nil {
			recommendation = strings.TrimSpace(*req.MigrationRecommendation)
		}
		if beforeAuditState.OrgID > 0 && lifecycle != beforeAuditState.Lifecycle {
			h.emitModelLifecycleEvent(r.Context(), beforeAuditState.OrgID, id, modelName, lifecycle, recommendation)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

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

type modelGovernanceOrgScope struct {
	orgID       int64
	constrained bool
}

type modelGovernanceQueryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func resolveModelGovernanceCreateOrganization(ctx context.Context, requestedOrgID *int64) (int64, error) {
	if requestedOrgID == nil {
		return resolveActorOrganization(ctx, 0)
	}
	if *requestedOrgID <= 0 {
		return 0, errInvalidOrganizationID
	}
	return resolveActorOrganization(ctx, *requestedOrgID)
}

// resolveModelGovernancePathScope binds production requests to the canonical
// customer context. The platform-only ID fallback is retained solely for
// rolling-upgrade/direct-handler compatibility when CustomerContext is absent;
// it must never let a deployment operator use a globally unique path ID to
// escape the single-customer boundary.
func resolveModelGovernancePathScope(r *http.Request) (modelGovernanceOrgScope, error) {
	requestedOrgID, err := parseOptionalOrganizationID(r.URL.Query().Get("org_id"))
	if err != nil {
		return modelGovernanceOrgScope{}, err
	}
	if customer, present := middleware.GetCustomerContextFromContext(r.Context()); present {
		if !customer.Ready() {
			return modelGovernanceOrgScope{}, errActorOrganizationForbidden
		}
		orgID, resolveErr := resolveActorOrganization(r.Context(), requestedOrgID)
		if resolveErr != nil {
			return modelGovernanceOrgScope{}, resolveErr
		}
		return modelGovernanceOrgScope{orgID: orgID, constrained: true}, nil
	}
	if auth.IsPlatformAdmin(actorOrganizationRole(r.Context())) {
		return modelGovernanceOrgScope{orgID: requestedOrgID, constrained: requestedOrgID > 0}, nil
	}
	orgID, err := resolveActorOrganization(r.Context(), requestedOrgID)
	if err != nil {
		return modelGovernanceOrgScope{}, err
	}
	return modelGovernanceOrgScope{orgID: orgID, constrained: true}, nil
}

func readModelSetTargetOrg(ctx context.Context, q modelGovernanceQueryRower, modelSetID int64, scope modelGovernanceOrgScope, forUpdate bool) (int64, error) {
	query := `SELECT org_id FROM model_sets WHERE id = $1`
	args := []interface{}{modelSetID}
	if scope.constrained {
		query += ` AND org_id = $2`
		args = append(args, scope.orgID)
	}
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var orgID int64
	if err := q.QueryRow(ctx, query, args...).Scan(&orgID); err != nil {
		return 0, err
	}
	return orgID, nil
}

func writeModelGovernanceLookupError(w http.ResponseWriter, err error, resource string) {
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "no rows") {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": resource + " not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": resource + " query failed"})
}

func modelGovernanceAuditMeta(r *http.Request, orgID int64) auditMeta {
	return auditMeta{
		OrgID:     orgID,
		IP:        normalizeGlobalControlAuditIP(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Source:    auditSourceFromContext(r.Context()),
	}
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

func normalizeModelSetItems(items []modelSetItemRequest) []modelSetItemRequest {
	normalized := make([]modelSetItemRequest, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.ItemType = strings.TrimSpace(item.ItemType)
		item.ItemName = strings.TrimSpace(item.ItemName)
		key := item.ItemType + ":" + item.ItemName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func replaceModelSetItems(ctx context.Context, execer auditExecer, modelSetID, orgID int64, items []modelSetItemRequest) error {
	if _, err := execer.Exec(ctx,
		`DELETE FROM model_set_items msi
		 USING model_sets ms
		 WHERE msi.model_set_id = $1 AND ms.id = msi.model_set_id AND ms.org_id = $2`,
		modelSetID, orgID,
	); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := execer.Exec(ctx,
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

func (h *Handler) projectHasAssignedModelSets(ctx context.Context, projectID, orgID int64) (bool, error) {
	var count int
	err := h.db.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM project_model_sets pms
		 JOIN projects p ON p.id = pms.project_id
		 JOIN model_sets ms ON ms.id = pms.model_set_id AND ms.org_id = p.org_id
		 WHERE pms.project_id = $1 AND p.org_id = $2`,
		projectID, orgID,
	).Scan(&count)
	return count > 0, err
}
