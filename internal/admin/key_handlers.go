package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
)

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT ak.id, ak.org_id, ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        ak.budget_limit, ak.allowed_ips, ak.allowed_models, ak.created_at,
		        o.name as org_name
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 ORDER BY ak.id ASC`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID int64
		var name, keyPrefix, status, orgName string
		var rpmLimit, qpsLimit, budgetLimit int
		var allowedIPs, allowedModels string
		var createdAt time.Time
		rows.Scan(&id, &orgID, &name, &keyPrefix, &status, &rpmLimit, &qpsLimit, &budgetLimit, &allowedIPs, &allowedModels, &createdAt, &orgName)
		keys = append(keys, map[string]interface{}{
			"id": id, "org_id": orgID, "name": name, "key_prefix": keyPrefix,
			"status": status, "rpm_limit": rpmLimit, "qps_limit": qpsLimit,
			"budget_limit": budgetLimit, "allowed_ips": allowedIPs, "allowed_models": allowedModels,
			"created_at": createdAt, "org_name": orgName,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": len(keys)})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID         int64  `json:"org_id"`
		Name          string `json:"name"`
		RPMLimit      int    `json:"rpm_limit"`
		QPSLimit      int    `json:"qps_limit"`
		AllowedIPs    string `json:"allowed_ips"`
		AllowedModels string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.OrgID == 0 {
		req.OrgID = 1
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
		return
	}
	if req.RPMLimit == 0 {
		req.RPMLimit = 60
	}
	if req.QPSLimit == 0 {
		req.QPSLimit = 10
	}

	rawKey, keyPrefix, err := auth.GenerateAPIKey(req.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate key"})
		return
	}

	keyHash := auth.HashAPIKey(rawKey, h.jwtSecret)

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, req.Name, keyHash, keyPrefix, req.RPMLimit, req.QPSLimit, req.AllowedIPs, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create key"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "key": rawKey, "key_prefix": keyPrefix})
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var req struct {
		Name          *string `json:"name"`
		AllowedIPs    *string `json:"allowed_ips"`
		AllowedModels *string `json:"allowed_models"`
		Status        *string `json:"status"`
		RPMLimit      *int    `json:"rpm_limit"`
		QPSLimit      *int    `json:"qps_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.AllowedIPs != nil {
		sets = append(sets, fmt.Sprintf("allowed_ips = $%d", argIdx))
		args = append(args, *req.AllowedIPs)
		argIdx++
	}
	if req.AllowedModels != nil {
		sets = append(sets, fmt.Sprintf("allowed_models = $%d", argIdx))
		args = append(args, *req.AllowedModels)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
	}
	if req.RPMLimit != nil {
		sets = append(sets, fmt.Sprintf("rpm_limit = $%d", argIdx))
		args = append(args, *req.RPMLimit)
		argIdx++
	}
	if req.QPSLimit != nil {
		sets = append(sets, fmt.Sprintf("qps_limit = $%d", argIdx))
		args = append(args, *req.QPSLimit)
		argIdx++
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE id = $%d", joinStrings(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	changedFields := []string{}
	if req.Name != nil {
		changedFields = append(changedFields, "name")
	}
	if req.AllowedIPs != nil {
		changedFields = append(changedFields, "allowed_ips")
	}
	if req.AllowedModels != nil {
		changedFields = append(changedFields, "allowed_models")
	}
	if req.Status != nil {
		changedFields = append(changedFields, "status")
	}
	if req.RPMLimit != nil {
		changedFields = append(changedFields, "rpm_limit")
	}
	if req.QPSLimit != nil {
		changedFields = append(changedFields, "qps_limit")
	}
	h.recordAudit(r.Context(), "apikey.update", "apikey", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key updated"})
}

func (h *Handler) DisableAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	tag, err := h.db.Exec(r.Context(),
		`UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	h.recordAudit(r.Context(), "apikey.disable", "apikey", idStr, map[string]interface{}{}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key disabled"})
}

func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}
	var k struct {
		ID, OrgID                       int64
		Name, KeyPrefix                 string
		Status                          string
		RPMLimit, QPSLimit, BudgetLimit int
		AllowedIPs, AllowedModels       string
		OrgName                         string
		CreatedAt                       time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT ak.id, ak.org_id, ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        ak.budget_limit, ak.allowed_ips, ak.allowed_models, ak.created_at, o.name
		 FROM api_keys ak JOIN orgs o ON o.id = ak.org_id
		 WHERE ak.id = $1`, id).Scan(
		&k.ID, &k.OrgID, &k.Name, &k.KeyPrefix, &k.Status, &k.RPMLimit, &k.QPSLimit,
		&k.BudgetLimit, &k.AllowedIPs, &k.AllowedModels, &k.CreatedAt, &k.OrgName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": k.ID, "org_id": k.OrgID, "name": k.Name, "key_prefix": k.KeyPrefix,
		"status": k.Status, "rpm_limit": k.RPMLimit, "qps_limit": k.QPSLimit,
		"budget_limit": k.BudgetLimit, "allowed_ips": k.AllowedIPs, "allowed_models": k.AllowedModels,
		"created_at": k.CreatedAt, "org_name": k.OrgName,
	})
}
