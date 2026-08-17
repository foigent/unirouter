package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
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
