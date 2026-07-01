package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/models"
)

func (h *Handler) ListReconciliation(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	modelFilter := r.URL.Query().Get("model")
	statusFilter := r.URL.Query().Get("status")

	var conditions []string
	var args []interface{}
	argIdx := 1

	if modelFilter != "" {
		conditions = append(conditions, fmt.Sprintf("model = $%d", argIdx))
		args = append(args, modelFilter)
		argIdx++
	}

	// Move statusFilter into SQL WHERE clause for correct pagination
	// "error" = statusCode >= 400, "reconciled" = statusCode < 400
	if statusFilter == "error" {
		conditions = append(conditions, "status_code >= 400")
	} else if statusFilter == "reconciled" {
		conditions = append(conditions, "status_code < 400")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM call_logs %s`, where), args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(
		`SELECT id, request_id, model, total_tokens, prompt_tokens, completion_tokens, status_code, created_at
		 FROM call_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	type ReconRecord struct {
		ID           int64  `json:"id"`
		RequestID    string `json:"request_id"`
		Model        string `json:"model"`
		EstTokens    int    `json:"estimated_tokens"`
		ActualTokens int    `json:"actual_tokens"`
		Delta        int    `json:"delta"`
		Status       string `json:"status"`
		CreatedAt    string `json:"created_at"`
	}

	records := make([]ReconRecord, 0)
	for rows.Next() {
		var id int64
		var requestID, model string
		var totalTokens, promptTokens, completionTokens, statusCode int
		var createdAt time.Time
		if err := rows.Scan(&id, &requestID, &model, &totalTokens, &promptTokens, &completionTokens, &statusCode, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}

		estTokens := promptTokens + completionTokens
		status := "reconciled"
		if statusCode >= 400 {
			status = "error"
		}

		records = append(records, ReconRecord{
			ID:           id,
			RequestID:    requestID,
			Model:        model,
			EstTokens:    estTokens,
			ActualTokens: totalTokens,
			Delta:        totalTokens - estTokens,
			Status:       status,
			CreatedAt:    createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": records, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) ListModelPrices(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)

	var total int
	err := h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM model_prices`).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	offset := (page - 1) * pageSize
	rows, err := h.db.Query(r.Context(),
		`SELECT id, model_name, input_price_cents, output_price_cents FROM model_prices ORDER BY model_name LIMIT $1 OFFSET $2`,
		pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	prices := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var modelName string
		var inputPrice, outputPrice int
		rows.Scan(&id, &modelName, &inputPrice, &outputPrice)
		prices = append(prices, map[string]interface{}{
			"id": id, "model_name": modelName, "input_price_cents": inputPrice,
			"output_price_cents": outputPrice, "currency": "CNY",
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": prices, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelName        string `json:"model_name"`
		InputPriceCents  int    `json:"input_price_cents"`
		OutputPriceCents int    `json:"output_price_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model_name required"})
		return
	}
	var newID int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents) VALUES ($1, $2, $3)
		 ON CONFLICT (model_name) DO UPDATE SET input_price_cents = $2, output_price_cents = $3, updated_at = NOW()
		 RETURNING id`,
		req.ModelName, req.InputPriceCents, req.OutputPriceCents).Scan(&newID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Insert failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": newID, "model_name": req.ModelName, "input_price_cents": req.InputPriceCents, "output_price_cents": req.OutputPriceCents})
}

func (h *Handler) UpdateModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		InputPriceCents  *int    `json:"input_price_cents"`
		OutputPriceCents *int    `json:"output_price_cents"`
		ModelName        *string `json:"model_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	var sets []string
	var args []interface{}
	argIdx := 1
	if req.ModelName != nil {
		sets = append(sets, fmt.Sprintf("model_name = $%d", argIdx))
		args = append(args, *req.ModelName)
		argIdx++
	}
	if req.InputPriceCents != nil {
		sets = append(sets, fmt.Sprintf("input_price_cents = $%d", argIdx))
		args = append(args, *req.InputPriceCents)
		argIdx++
	}
	if req.OutputPriceCents != nil {
		sets = append(sets, fmt.Sprintf("output_price_cents = $%d", argIdx))
		args = append(args, *req.OutputPriceCents)
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE model_prices SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Model price not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) ListOrgModelPrices(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.URL.Query().Get("org_id")
	if orgIDStr == "" {
		orgIDStr = "1"
	}
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
		return
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Count total
	var total int
	err = h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_model_prices WHERE org_id = $1`, orgID).Scan(&total)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT id, org_id, model_name, multiplier, created_at, updated_at
		 FROM org_model_prices WHERE org_id = $1 ORDER BY model_name
		 LIMIT $2 OFFSET $3`, orgID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()
	var items []models.OrgModelPrice
	for rows.Next() {
		var p models.OrgModelPrice
		if err := rows.Scan(&p.ID, &p.OrgID, &p.ModelName, &p.Multiplier, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		items = append(items, p)
	}
	if items == nil {
		items = []models.OrgModelPrice{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total})
}

func (h *Handler) CreateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      int64   `json:"org_id"`
		ModelName  string  `json:"model_name"`
		Multiplier float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 || req.ModelName == "" || req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id, model_name, multiplier > 0 required"})
		return
	}
	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, model_name) DO UPDATE SET multiplier = $3, updated_at = NOW()
		 RETURNING id`,
		req.OrgID, req.ModelName, req.Multiplier).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Create failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *Handler) UpdateOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	var req struct {
		Multiplier *float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.Multiplier == nil || *req.Multiplier <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "multiplier > 0 required"})
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE org_model_prices SET multiplier = $1, updated_at = NOW() WHERE id = $2`,
		*req.Multiplier, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) DeleteOrgModelPrice(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid ID"})
		return
	}
	tag, err := h.db.Exec(r.Context(), `DELETE FROM org_model_prices WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Delete failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
