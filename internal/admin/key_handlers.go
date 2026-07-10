package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	var conditions []string
	var args []interface{}
	argIdx := 1
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("ak.org_id = $%d", argIdx))
		args = append(args, actorOrgID)
		argIdx++
	}
	if projectIDParam := r.URL.Query().Get("project_id"); projectIDParam != "" {
		projectID, err := strconv.ParseInt(projectIDParam, 10, 64)
		if err != nil || projectID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canReadProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("ak.project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if auth.IsProjectScopedRole(actorRole(r.Context())) {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM project_members pm
			JOIN projects scope_project ON scope_project.id = pm.project_id AND scope_project.org_id = ak.org_id
			JOIN users scope_user ON scope_user.id = pm.user_id AND scope_user.org_id = scope_project.org_id
			WHERE pm.project_id = ak.project_id AND pm.user_id = $%d
		)`, argIdx))
		args = append(args, middleware.GetUserIDFromContext(r.Context()))
		argIdx++
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinStrings(conditions, " AND ")
	}

	rows, err := h.db.Query(r.Context(),
		fmt.Sprintf(`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        COALESCE(ak.budget_limit, 0), ak.allowed_ips, ak.allowed_models, ak.created_at,
		        o.name as org_name, COALESCE(p.name, '') as project_name,
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'), COALESCE(u.username, '')
		 FROM api_keys ak
		 JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		 LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		 %s
		 ORDER BY ak.id ASC`, whereClause),
		args...,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	keys := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID, ownerUserID int64
		var name, keyPrefix, status, orgName, projectName, keyType, ownerUsername string
		var rpmLimit, qpsLimit, budgetLimit int
		var allowedIPs, allowedModels string
		var createdAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &name, &keyPrefix, &status, &rpmLimit, &qpsLimit, &budgetLimit, &allowedIPs, &allowedModels, &createdAt, &orgName, &projectName, &ownerUserID, &keyType, &ownerUsername); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		var owner interface{}
		if ownerUserID > 0 {
			owner = ownerUserID
		}
		keys = append(keys, map[string]interface{}{
			"id": id, "org_id": orgID, "project_id": projectID, "name": name, "key_prefix": keyPrefix,
			"status": status, "rpm_limit": rpmLimit, "qps_limit": qpsLimit, "rps_limit": qpsLimit,
			"budget_limit": budgetLimit, "allowed_ips": allowedIPs, "allowed_models": allowedModels,
			"created_at": createdAt, "org_name": orgName, "project_name": projectName,
			"owner_user_id": owner, "owner_username": ownerUsername, "key_type": keyType,
			"quota": apiKeyQuotaPayload(rpmLimit, qpsLimit, budgetLimit),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": keys, "total": len(keys)})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID         int64  `json:"org_id"`
		ProjectID     int64  `json:"project_id"`
		OwnerUserID   *int64 `json:"owner_user_id"`
		KeyType       string `json:"key_type"`
		Name          string `json:"name"`
		RPMLimit      int    `json:"rpm_limit"`
		RPSLimit      *int   `json:"rps_limit"`
		QPSLimit      int    `json:"qps_limit"`
		AllowedIPs    string `json:"allowed_ips"`
		AllowedModels string `json:"allowed_models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	if req.OrgID == 0 {
		if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
			req.OrgID = actorOrgID
		} else {
			req.OrgID = 1
		}
	}
	if !actorCanAccessOrg(r.Context(), req.OrgID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	name, validationErr := normalizeAPIKeyName(req.Name)
	if validationErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
		return
	}
	req.Name = name
	if req.ProjectID < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		return
	}
	if req.RPMLimit == 0 {
		req.RPMLimit = 60
	}
	if req.RPMLimit < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "rpm_limit must be >= 0"})
		return
	}
	rpsLimit, err := createRPSLimit(req.RPSLimit, req.QPSLimit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	req.QPSLimit = rpsLimit

	existingRows, err := h.db.Query(r.Context(),
		`SELECT 1 FROM api_keys WHERE org_id = $1 AND name = $2 LIMIT 1`,
		req.OrgID, req.Name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to check duplicate key name"})
		return
	}
	defer existingRows.Close()
	if existingRows.Next() {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "API key name already exists"})
		return
	}
	if err := existingRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to check duplicate key name"})
		return
	}

	if auth.IsProjectScopedRole(actorRole(r.Context())) && req.ProjectID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "project_id is required"})
		return
	}

	projectID, err := h.resolveAPIKeyProjectID(r.Context(), req.OrgID, req.ProjectID)
	if err != nil {
		if req.ProjectID > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to resolve default project"})
		}
		return
	}
	if !h.canCreateProjectKey(r.Context(), projectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	ownerUserID, keyType, err := h.resolveCreateAPIKeyOwnership(r.Context(), req.OrgID, projectID, req.OwnerUserID, req.KeyType)
	if err != nil {
		writeAPIKeyOwnershipError(w, err)
		return
	}

	rawKey, keyPrefix, err := auth.GenerateAPIKey(req.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to generate key"})
		return
	}

	keyHash := auth.HashAPIKey(rawKey, h.apiKeyHashSecret)

	var id int64
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO api_keys (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, $10, $11, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, projectID, nullableOwnerUserID(ownerUserID), keyType, req.Name, keyHash, keyPrefix, req.RPMLimit, req.QPSLimit, req.AllowedIPs, req.AllowedModels,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create key"})
		return
	}

	h.recordAuditForProjectFromRequest(r, "apikey.create", "apikey", strconv.FormatInt(id, 10), projectID,
		map[string]interface{}{
			"org_id":     req.OrgID,
			"project_id": projectID,
			"key_type":   keyType,
			"name":       req.Name,
			"after": map[string]interface{}{
				"org_id":         req.OrgID,
				"project_id":     projectID,
				"owner_user_id":  nullableOwnerUserID(ownerUserID),
				"key_type":       keyType,
				"name":           req.Name,
				"status":         "active",
				"rpm_limit":      req.RPMLimit,
				"rps_limit":      req.QPSLimit,
				"qps_limit":      req.QPSLimit,
				"allowed_ips":    req.AllowedIPs,
				"allowed_models": req.AllowedModels,
			},
		})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":            id,
		"key":           rawKey,
		"key_prefix":    keyPrefix,
		"project_id":    projectID,
		"owner_user_id": nullableOwnerUserID(ownerUserID),
		"key_type":      keyType,
		"rpm_limit":     req.RPMLimit,
		"rps_limit":     req.QPSLimit,
		"qps_limit":     req.QPSLimit,
		"quota":         apiKeyQuotaPayload(req.RPMLimit, req.QPSLimit, 0),
	})
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
		ProjectID     *int64  `json:"project_id"`
		OwnerUserID   *int64  `json:"owner_user_id"`
		KeyType       *string `json:"key_type"`
		RPMLimit      *int    `json:"rpm_limit"`
		RPSLimit      *int    `json:"rps_limit"`
		QPSLimit      *int    `json:"qps_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var currentOrgID, currentProjectID, currentOwnerUserID int64
	var currentKeyType string
	if !isUnauthenticatedUnitContext(r.Context()) {
		if err := h.db.QueryRow(r.Context(),
			`SELECT org_id, COALESCE(project_id, 0), COALESCE(owner_user_id, 0), COALESCE(NULLIF(key_type, ''), 'shared')
			 FROM api_keys WHERE id = $1`, id,
		).Scan(&currentOrgID, &currentProjectID, &currentOwnerUserID, &currentKeyType); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
			return
		}
		if !h.canWriteAPIKey(r.Context(), currentOrgID, currentProjectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	targetOrgID := currentOrgID
	targetProjectID := currentProjectID

	if req.Name != nil {
		name, validationErr := normalizeAPIKeyName(*req.Name)
		if validationErr != "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": validationErr})
			return
		}
		*req.Name = name
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
	if req.ProjectID != nil {
		if *req.ProjectID < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		projectID, err := h.resolveAPIKeyProjectID(r.Context(), currentOrgID, *req.ProjectID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return
		}
		if !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return
		}
		targetProjectID = projectID
		sets = append(sets, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if req.ProjectID != nil || req.OwnerUserID != nil || req.KeyType != nil {
		ownerUserID := currentOwnerUserID
		if req.OwnerUserID != nil {
			ownerUserID = *req.OwnerUserID
		}
		keyType := currentKeyType
		ownerWasExplicit := req.OwnerUserID != nil
		if req.KeyType != nil {
			keyType = *req.KeyType
		}
		ownerUserID, keyType, err = h.resolveUpdateAPIKeyOwnership(r.Context(), targetOrgID, targetProjectID, ownerUserID, keyType, ownerWasExplicit)
		if err != nil {
			writeAPIKeyOwnershipError(w, err)
			return
		}
		sets = append(sets, fmt.Sprintf("owner_user_id = $%d", argIdx))
		args = append(args, nullableOwnerUserID(ownerUserID))
		argIdx++
		sets = append(sets, fmt.Sprintf("key_type = $%d", argIdx))
		args = append(args, keyType)
		argIdx++
	}
	if req.RPMLimit != nil {
		if *req.RPMLimit <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "rpm_limit must be > 0"})
			return
		}
		sets = append(sets, fmt.Sprintf("rpm_limit = $%d", argIdx))
		args = append(args, *req.RPMLimit)
		argIdx++
	}
	rpsLimit, rpsProvided, err := updateRPSLimit(req.RPSLimit, req.QPSLimit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	if rpsProvided {
		sets = append(sets, fmt.Sprintf("qps_limit = $%d", argIdx))
		args = append(args, rpsLimit)
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

	query := fmt.Sprintf("UPDATE api_keys SET %s WHERE %s", joinStrings(sets, ", "), whereClause)
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
	if req.ProjectID != nil {
		changedFields = append(changedFields, "project_id")
	}
	if req.OwnerUserID != nil {
		changedFields = append(changedFields, "owner_user_id")
	}
	if req.KeyType != nil {
		changedFields = append(changedFields, "key_type")
	}
	if req.RPMLimit != nil {
		changedFields = append(changedFields, "rpm_limit")
	}
	if req.RPSLimit != nil {
		changedFields = append(changedFields, "rps_limit")
	}
	if req.QPSLimit != nil {
		changedFields = append(changedFields, "qps_limit")
	}
	h.recordAudit(r.Context(), "apikey.update", "apikey", idStr,
		map[string]interface{}{"fields": changedFields}, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Key updated"})
}

const apiKeyNameMaxChars = 255

func normalizeAPIKeyName(name string) (string, string) {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		return "", "name is required"
	}
	if utf8.RuneCountInString(normalized) > apiKeyNameMaxChars {
		return "", "name must be <= 255 characters"
	}
	if strings.Contains(normalized, "--") || strings.ContainsAny(normalized, "<>\"'`;\\") {
		return "", "name contains unsupported characters"
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", "name contains unsupported characters"
		}
	}
	return normalized, ""
}

func (h *Handler) DisableAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid key ID"})
		return
	}

	var eventOrgID, eventProjectID int64
	var eventName, eventKeyPrefix, currentStatus string
	if err := h.db.QueryRow(r.Context(),
		`SELECT org_id, COALESCE(project_id, 0), name, key_prefix, status FROM api_keys WHERE id = $1`,
		id,
	).Scan(&eventOrgID, &eventProjectID, &eventName, &eventKeyPrefix, &currentStatus); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	if !h.canWriteAPIKey(r.Context(), eventOrgID, eventProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	query := `UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`
	args := []interface{}{id}
	if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		query += ` AND org_id = $2`
		args = append(args, actorOrgID)
	}
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to disable key"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Key not found"})
		return
	}

	h.recordAudit(r.Context(), "apikey.disable", "apikey", idStr, map[string]interface{}{}, r.RemoteAddr)
	if currentStatus != "disabled" {
		h.emitAPIKeyDisabledEvent(r.Context(), eventOrgID, eventProjectID, id, eventName, eventKeyPrefix)
	}

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
		ID, OrgID, ProjectID, OwnerUserID int64
		Name, KeyPrefix                   string
		KeyType, OwnerUsername            string
		Status                            string
		RPMLimit, QPSLimit, BudgetLimit   int
		AllowedIPs, AllowedModels         string
		OrgName, ProjectName              string
		CreatedAt                         time.Time
	}
	err = h.db.QueryRow(r.Context(),
		`SELECT ak.id, ak.org_id, COALESCE(ak.project_id, 0), ak.name, ak.key_prefix, ak.status, ak.rpm_limit, ak.qps_limit,
		        COALESCE(ak.budget_limit, 0), ak.allowed_ips, ak.allowed_models, ak.created_at, o.name, COALESCE(p.name, ''),
		        COALESCE(ak.owner_user_id, 0), COALESCE(NULLIF(ak.key_type, ''), 'shared'), COALESCE(u.username, '')
		 FROM api_keys ak JOIN orgs o ON o.id = ak.org_id
		 LEFT JOIN projects p ON p.id = ak.project_id AND p.org_id = ak.org_id
		 LEFT JOIN users u ON u.id = ak.owner_user_id AND u.org_id = ak.org_id
		 WHERE ak.id = $1`, id).Scan(
		&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.KeyPrefix, &k.Status, &k.RPMLimit, &k.QPSLimit,
		&k.BudgetLimit, &k.AllowedIPs, &k.AllowedModels, &k.CreatedAt, &k.OrgName, &k.ProjectName, &k.OwnerUserID, &k.KeyType, &k.OwnerUsername)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "API key not found"})
		return
	}
	if !h.canReadAPIKey(r.Context(), k.OrgID, k.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	var owner interface{}
	if k.OwnerUserID > 0 {
		owner = k.OwnerUserID
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": k.ID, "org_id": k.OrgID, "project_id": k.ProjectID, "name": k.Name, "key_prefix": k.KeyPrefix,
		"status": k.Status, "rpm_limit": k.RPMLimit, "qps_limit": k.QPSLimit, "rps_limit": k.QPSLimit,
		"budget_limit": k.BudgetLimit, "allowed_ips": k.AllowedIPs, "allowed_models": k.AllowedModels,
		"created_at": k.CreatedAt, "org_name": k.OrgName, "project_name": k.ProjectName,
		"owner_user_id": owner, "owner_username": k.OwnerUsername, "key_type": k.KeyType,
		"quota": apiKeyQuotaPayload(k.RPMLimit, k.QPSLimit, k.BudgetLimit),
	})
}

func createRPSLimit(preferredRPS *int, legacyQPS int) (int, error) {
	if preferredRPS != nil && legacyQPS > 0 && *preferredRPS != legacyQPS {
		return 0, fmt.Errorf("rps_limit conflicts with qps_limit")
	}
	limit := legacyQPS
	if preferredRPS != nil {
		limit = *preferredRPS
	}
	if limit == 0 {
		return 10, nil
	}
	if limit < 0 {
		return 0, fmt.Errorf("rps_limit must be >= 0")
	}
	return limit, nil
}

func updateRPSLimit(preferredRPS, legacyQPS *int) (int, bool, error) {
	if preferredRPS == nil && legacyQPS == nil {
		return 0, false, nil
	}
	if preferredRPS != nil && legacyQPS != nil && *preferredRPS != *legacyQPS {
		return 0, true, fmt.Errorf("rps_limit conflicts with qps_limit")
	}
	limit := 0
	if preferredRPS != nil {
		limit = *preferredRPS
	} else {
		limit = *legacyQPS
	}
	if limit <= 0 {
		return 0, true, fmt.Errorf("rps_limit must be > 0")
	}
	return limit, true, nil
}

func apiKeyQuotaPayload(rpmLimit, rpsLimit, budgetLimit int) map[string]interface{} {
	var budget interface{}
	if budgetLimit > 0 {
		budget = budgetLimit
	}
	return map[string]interface{}{
		"rpm_limit":          rpmLimit,
		"rps_limit":          rpsLimit,
		"qps_compat_limit":   rpsLimit,
		"tpm_limit":          nil,
		"tokens_per_second":  nil,
		"budget_limit_cents": budget,
		"tpm_source":         "not_configured",
		"compatibility_note": "qps_limit stores the same value as rps_limit for API compatibility",
	}
}

func nullableOwnerUserID(ownerUserID int64) interface{} {
	if ownerUserID > 0 {
		return ownerUserID
	}
	return nil
}

func writeAPIKeyOwnershipError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if err.Error() == "Forbidden" {
		status = http.StatusForbidden
	}
	writeJSON(w, status, map[string]interface{}{"error": err.Error()})
}

func validAPIKeyType(keyType string) bool {
	switch keyType {
	case "shared", "personal", "service":
		return true
	default:
		return false
	}
}

func (h *Handler) resolveCreateAPIKeyOwnership(ctx context.Context, orgID, projectID int64, ownerReq *int64, keyTypeReq string) (int64, string, error) {
	role := actorRole(ctx)
	actorID := middleware.GetUserIDFromContext(ctx)
	ownerUserID := int64(0)
	if ownerReq != nil {
		ownerUserID = *ownerReq
	}
	if ownerUserID < 0 {
		return 0, "", fmt.Errorf("Invalid owner_user_id")
	}

	keyType := strings.TrimSpace(keyTypeReq)
	if keyType == "" {
		switch {
		case auth.IsProjectScopedRole(role) && actorID > 0:
			keyType = "personal"
			if ownerReq == nil || ownerUserID == 0 {
				ownerUserID = actorID
			}
		case ownerUserID > 0:
			keyType = "personal"
		default:
			keyType = "shared"
		}
	}
	return h.resolveAPIKeyOwnership(ctx, orgID, projectID, ownerUserID, keyType, ownerReq != nil)
}

func (h *Handler) resolveUpdateAPIKeyOwnership(ctx context.Context, orgID, projectID, ownerUserID int64, keyType string, ownerWasExplicit bool) (int64, string, error) {
	if ownerUserID < 0 {
		return 0, "", fmt.Errorf("Invalid owner_user_id")
	}
	return h.resolveAPIKeyOwnership(ctx, orgID, projectID, ownerUserID, keyType, ownerWasExplicit)
}

func (h *Handler) resolveAPIKeyOwnership(ctx context.Context, orgID, projectID, ownerUserID int64, keyType string, ownerWasExplicit bool) (int64, string, error) {
	keyType = strings.TrimSpace(keyType)
	if !validAPIKeyType(keyType) {
		return 0, "", fmt.Errorf("Invalid key_type")
	}
	role := actorRole(ctx)
	actorID := middleware.GetUserIDFromContext(ctx)

	switch keyType {
	case "personal":
		if ownerUserID == 0 {
			if actorID > 0 {
				ownerUserID = actorID
			} else {
				return 0, "", fmt.Errorf("owner_user_id is required for personal keys")
			}
		}
	case "shared", "service":
		if ownerWasExplicit && ownerUserID > 0 {
			return 0, "", fmt.Errorf("owner_user_id is only valid for personal keys")
		}
		ownerUserID = 0
	}

	if role == auth.RoleDeveloper && ownerUserID != actorID {
		return 0, "", fmt.Errorf("Forbidden")
	}
	if auth.IsProjectScopedRole(role) && keyType == "service" {
		return 0, "", fmt.Errorf("Forbidden")
	}
	if ownerUserID > 0 {
		if err := h.validateAPIKeyOwnerUser(ctx, orgID, projectID, ownerUserID); err != nil {
			return 0, "", err
		}
	}
	return ownerUserID, keyType, nil
}

func (h *Handler) validateAPIKeyOwnerUser(ctx context.Context, orgID, projectID, ownerUserID int64) error {
	var count int
	if err := h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		ownerUserID, orgID,
	).Scan(&count); err != nil || count == 0 {
		return fmt.Errorf("Invalid owner_user_id")
	}
	if projectID > 0 {
		if err := h.db.QueryRow(ctx,
			`SELECT COUNT(*)
			 FROM project_members pm
			 JOIN projects p ON p.id = pm.project_id AND p.org_id = $3
			 JOIN users u ON u.id = pm.user_id AND u.org_id = p.org_id
			 WHERE pm.project_id = $1 AND pm.user_id = $2`,
			projectID, ownerUserID, orgID,
		).Scan(&count); err != nil || count == 0 {
			return fmt.Errorf("owner_user_id is not a member of the project")
		}
	}
	return nil
}

func (h *Handler) resolveAPIKeyProjectID(ctx context.Context, orgID, requestedProjectID int64) (int64, error) {
	if orgID <= 0 {
		return 0, fmt.Errorf("invalid org_id")
	}
	if requestedProjectID > 0 {
		var projectID int64
		err := h.db.QueryRow(ctx,
			`SELECT id FROM projects WHERE id = $1 AND org_id = $2 AND status = 'active'`,
			requestedProjectID, orgID,
		).Scan(&projectID)
		return projectID, err
	}

	var projectID int64
	err := h.db.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_at, updated_at)
		 VALUES ($1, 'Default Project', 'default', 'Default project created for PRD2 compatibility', 'active', NOW(), NOW())
		 ON CONFLICT (org_id, slug) DO UPDATE SET updated_at = projects.updated_at
		 RETURNING id`,
		orgID,
	).Scan(&projectID)
	return projectID, err
}
