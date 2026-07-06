package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListNotificationEvents(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	conditions, args, argIdx, ok := h.notificationEventConditions(w, r)
	if !ok {
		return
	}
	addTextFilter := func(param, column string) {
		if value := strings.TrimSpace(r.URL.Query().Get(param)); value != "" {
			conditions = append(conditions, fmt.Sprintf("%s = $%d", column, argIdx))
			args = append(args, value)
			argIdx++
		}
	}
	addTextFilter("event_type", "event_type")
	addTextFilter("severity", "severity")
	addTextFilter("status", "status")
	addTextFilter("delivery_state", "delivery_state")

	whereClause := " WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM notification_events"+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, org_id, COALESCE(project_id, 0), event_type, severity, title, message,
		        resource, resource_id, payload, status, dedupe_key, delivery_state,
		        last_delivery_at, created_at, updated_at
		 FROM notification_events
		 %s
		 ORDER BY created_at DESC, id DESC
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

	events := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID int64
		var eventType, severity, title, message, resource, resourceID, status, dedupeKey, deliveryState string
		var payload json.RawMessage
		var lastDeliveryAt sql.NullTime
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &eventType, &severity, &title, &message, &resource, &resourceID, &payload, &status, &dedupeKey, &deliveryState, &lastDeliveryAt, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		events = append(events, map[string]interface{}{
			"id":               id,
			"org_id":           orgID,
			"project_id":       nullableInt64(projectID),
			"event_type":       eventType,
			"severity":         severity,
			"title":            title,
			"message":          message,
			"resource":         resource,
			"resource_id":      resourceID,
			"payload":          payload,
			"status":           status,
			"dedupe_key":       dedupeKey,
			"delivery_state":   deliveryState,
			"last_delivery_at": nullableTime(lastDeliveryAt),
			"created_at":       createdAt,
			"updated_at":       updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": events, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) ListNotificationDeliveries(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	conditions, args, argIdx, ok := h.notificationDeliveryConditions(w, r)
	if !ok {
		return
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		conditions = append(conditions, fmt.Sprintf("d.status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	if eventIDParam := strings.TrimSpace(r.URL.Query().Get("event_id")); eventIDParam != "" {
		eventID, err := strconv.ParseInt(eventIDParam, 10, 64)
		if err != nil || eventID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid event_id"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("d.event_id = $%d", argIdx))
		args = append(args, eventID)
		argIdx++
	}

	whereClause := " WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := h.db.QueryRow(r.Context(),
		"SELECT COUNT(*) FROM notification_deliveries d JOIN notification_events e ON e.id = d.event_id"+whereClause,
		args...,
	).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT d.id, d.event_id, COALESCE(d.webhook_id, 0), d.status, d.attempts,
		        d.response_status, d.error, d.next_retry_at, d.delivered_at, d.created_at, d.updated_at,
		        e.event_type, e.title, COALESCE(w.name, '')
		 FROM notification_deliveries d
		 JOIN notification_events e ON e.id = d.event_id
		 LEFT JOIN notification_webhooks w ON w.id = d.webhook_id
		 %s
		 ORDER BY d.updated_at DESC, d.id DESC
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

	deliveries := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, eventID, webhookID int64
		var status, errMessage, eventType, eventTitle, webhookName string
		var attempts int
		var responseStatus sql.NullInt64
		var nextRetryAt, deliveredAt sql.NullTime
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &eventID, &webhookID, &status, &attempts, &responseStatus, &errMessage, &nextRetryAt, &deliveredAt, &createdAt, &updatedAt, &eventType, &eventTitle, &webhookName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		deliveries = append(deliveries, map[string]interface{}{
			"id":              id,
			"event_id":        eventID,
			"webhook_id":      nullableInt64(webhookID),
			"status":          status,
			"attempts":        attempts,
			"response_status": nullableInt(responseStatus),
			"error":           errMessage,
			"next_retry_at":   nullableTime(nextRetryAt),
			"delivered_at":    nullableTime(deliveredAt),
			"created_at":      createdAt,
			"updated_at":      updatedAt,
			"event_type":      eventType,
			"event_title":     eventTitle,
			"webhook_name":    webhookName,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": deliveries, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) ListNotificationWebhooks(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	conditions, args, argIdx, ok := h.notificationWebhookConditions(w, r, false)
	if !ok {
		return
	}
	if enabledParam := strings.TrimSpace(r.URL.Query().Get("enabled")); enabledParam != "" {
		enabled, err := strconv.ParseBool(enabledParam)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid enabled"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, enabled)
		argIdx++
	}

	whereClause := " WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := h.db.QueryRow(r.Context(), "SELECT COUNT(*) FROM notification_webhooks"+whereClause, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, org_id, COALESCE(project_id, 0), name, url, event_types, enabled, created_at, updated_at
		 FROM notification_webhooks
		 %s
		 ORDER BY id ASC
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

	webhooks := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID int64
		var name, webhookURL string
		var eventTypes json.RawMessage
		var enabled bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &name, &webhookURL, &eventTypes, &enabled, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		webhooks = append(webhooks, notificationWebhookResponse(id, orgID, projectID, name, webhookURL, eventTypes, enabled, createdAt, updatedAt))
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": webhooks, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID      int64    `json:"org_id"`
		ProjectID  int64    `json:"project_id"`
		Name       string   `json:"name"`
		URL        string   `json:"url"`
		EventTypes []string `json:"event_types"`
		Enabled    *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}
	if req.OrgID == 0 {
		req.OrgID = middleware.GetOrgIDFromContext(r.Context())
	}
	if req.OrgID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "org_id is required"})
		return
	}
	if !h.canWriteNotificationWebhook(r, req.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	name, webhookURL, eventTypesJSON, ok := normalizeWebhookInput(w, req.Name, req.URL, req.EventTypes)
	if !ok {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var projectArg interface{}
	if req.ProjectID > 0 {
		projectArg = req.ProjectID
	}

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO notification_webhooks (org_id, project_id, name, url, event_types, enabled, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, projectArg, name, webhookURL, eventTypesJSON, enabled,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create webhook"})
		return
	}

	after := map[string]interface{}{"id": id, "org_id": req.OrgID, "project_id": nullableInt64(req.ProjectID), "name": name, "url": webhookURL, "event_types": json.RawMessage(eventTypesJSON), "enabled": enabled}
	h.recordAuditForOrgProjectFromRequest(r, req.OrgID, req.ProjectID, "notification_webhook.create", "notification_webhook", strconv.FormatInt(id, 10), map[string]interface{}{"after": after})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *Handler) UpdateNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid webhook ID"})
		return
	}

	var existingOrgID, existingProjectID int64
	var existingName, existingURL string
	var existingEventTypes json.RawMessage
	var existingEnabled bool
	err = h.db.QueryRow(r.Context(),
		`SELECT org_id, COALESCE(project_id, 0), name, url, event_types, enabled
		 FROM notification_webhooks
		 WHERE id = $1`,
		id,
	).Scan(&existingOrgID, &existingProjectID, &existingName, &existingURL, &existingEventTypes, &existingEnabled)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Webhook not found"})
		return
	}
	if !h.canWriteNotificationWebhook(r, existingProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var req struct {
		Name       *string  `json:"name"`
		URL        *string  `json:"url"`
		EventTypes []string `json:"event_types"`
		Enabled    *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid JSON"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	after := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
			return
		}
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, name)
		after["name"] = name
		argIdx++
	}
	if req.URL != nil {
		webhookURL := strings.TrimSpace(*req.URL)
		if !validWebhookURL(webhookURL) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid url"})
			return
		}
		sets = append(sets, fmt.Sprintf("url = $%d", argIdx))
		args = append(args, webhookURL)
		after["url"] = webhookURL
		argIdx++
	}
	if req.EventTypes != nil {
		eventTypesJSON, ok := normalizeEventTypes(w, req.EventTypes)
		if !ok {
			return
		}
		sets = append(sets, fmt.Sprintf("event_types = $%d", argIdx))
		args = append(args, eventTypesJSON)
		after["event_types"] = json.RawMessage(eventTypesJSON)
		argIdx++
	}
	if req.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *req.Enabled)
		after["enabled"] = *req.Enabled
		argIdx++
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE notification_webhooks SET %s WHERE id = $%d", strings.Join(sets, ", "), argIdx)
	tag, err := h.db.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update webhook"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Webhook not found"})
		return
	}

	before := map[string]interface{}{"id": id, "org_id": existingOrgID, "project_id": nullableInt64(existingProjectID), "name": existingName, "url": existingURL, "event_types": existingEventTypes, "enabled": existingEnabled}
	after["id"] = id
	after["org_id"] = existingOrgID
	after["project_id"] = nullableInt64(existingProjectID)
	h.recordAuditForOrgProjectFromRequest(r, existingOrgID, existingProjectID, "notification_webhook.update", "notification_webhook", strconv.FormatInt(id, 10), map[string]interface{}{"before": before, "after": after})
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Webhook updated"})
}

func (h *Handler) notificationEventConditions(w http.ResponseWriter, r *http.Request) ([]string, []interface{}, int, bool) {
	return h.notificationProjectScopedConditions(w, r, "org_id", "project_id")
}

func (h *Handler) notificationDeliveryConditions(w http.ResponseWriter, r *http.Request) ([]string, []interface{}, int, bool) {
	conditions, args, argIdx, ok := h.notificationProjectScopedConditions(w, r, "e.org_id", "e.project_id")
	if !ok {
		return nil, nil, 0, false
	}
	return conditions, args, argIdx, true
}

func (h *Handler) notificationWebhookConditions(w http.ResponseWriter, r *http.Request, requireWritable bool) ([]string, []interface{}, int, bool) {
	conditions, args, argIdx, ok := h.notificationProjectScopedConditions(w, r, "org_id", "project_id")
	if !ok {
		return nil, nil, 0, false
	}
	if requireWritable && auth.IsProjectScopedRole(actorRole(r.Context())) {
		projectID, valid := projectIDFromQuery(r)
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
			return nil, nil, 0, false
		}
		if projectID <= 0 || !h.canWriteProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return nil, nil, 0, false
		}
	}
	return conditions, args, argIdx, true
}

func (h *Handler) notificationProjectScopedConditions(w http.ResponseWriter, r *http.Request, orgColumn, projectColumn string) ([]string, []interface{}, int, bool) {
	conditions := []string{}
	args := []interface{}{}
	argIdx := 1

	if orgIDParam := strings.TrimSpace(r.URL.Query().Get("org_id")); orgIDParam != "" {
		orgID, err := strconv.ParseInt(orgIDParam, 10, 64)
		if err != nil || orgID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid org_id"})
			return nil, nil, 0, false
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", orgColumn, argIdx))
		args = append(args, orgID)
		argIdx++
	} else if orgID := middleware.GetOrgIDFromContext(r.Context()); orgID > 0 {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", orgColumn, argIdx))
		args = append(args, orgID)
		argIdx++
	}

	projectID, validProjectID := projectIDFromQuery(r)
	if !validProjectID {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		return nil, nil, 0, false
	}
	if auth.IsProjectScopedRole(actorRole(r.Context())) {
		if projectID <= 0 || !h.canReadProjectUsage(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "project_id is required"})
			return nil, nil, 0, false
		}
	}
	if projectID > 0 {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", projectColumn, argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if len(conditions) == 0 {
		conditions = append(conditions, "1 = 1")
	}
	return conditions, args, argIdx, true
}

func (h *Handler) canWriteNotificationWebhook(r *http.Request, projectID int64) bool {
	role := actorRole(r.Context())
	if auth.IsProjectScopedRole(role) {
		return projectID > 0 && h.canWriteProject(r.Context(), projectID)
	}
	return role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin || role == auth.RoleBillingAdmin || isUnauthenticatedUnitContext(r.Context())
}

func projectIDFromQuery(r *http.Request) (int64, bool) {
	projectIDParam := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectIDParam == "" {
		return 0, true
	}
	projectID, err := strconv.ParseInt(projectIDParam, 10, 64)
	if err != nil || projectID <= 0 {
		return 0, false
	}
	return projectID, true
}

func normalizeWebhookInput(w http.ResponseWriter, name, webhookURL string, eventTypes []string) (string, string, []byte, bool) {
	name = strings.TrimSpace(name)
	webhookURL = strings.TrimSpace(webhookURL)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
		return "", "", nil, false
	}
	if !validWebhookURL(webhookURL) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid url"})
		return "", "", nil, false
	}
	eventTypesJSON, ok := normalizeEventTypes(w, eventTypes)
	return name, webhookURL, eventTypesJSON, ok
}

func normalizeEventTypes(w http.ResponseWriter, eventTypes []string) ([]byte, bool) {
	clean := make([]string, 0, len(eventTypes))
	seen := make(map[string]bool)
	for _, eventType := range eventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		if strings.ContainsAny(eventType, " \t\n\r") {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid event_type"})
			return nil, false
		}
		if !seen[eventType] {
			clean = append(clean, eventType)
			seen[eventType] = true
		}
	}
	eventTypesJSON, err := json.Marshal(clean)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid event_types"})
		return nil, false
	}
	return eventTypesJSON, true
}

func validWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func notificationWebhookResponse(id, orgID, projectID int64, name, webhookURL string, eventTypes json.RawMessage, enabled bool, createdAt, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":          id,
		"org_id":      orgID,
		"project_id":  nullableInt64(projectID),
		"name":        name,
		"url":         webhookURL,
		"event_types": eventTypes,
		"enabled":     enabled,
		"created_at":  createdAt,
		"updated_at":  updatedAt,
	}
}

func nullableInt64(value int64) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt(value sql.NullInt64) interface{} {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullableTime(value sql.NullTime) interface{} {
	if !value.Valid {
		return nil
	}
	return value.Time
}
