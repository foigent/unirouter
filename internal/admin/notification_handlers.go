package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	internaldb "github.com/uniroute/unirouter/internal/db"
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

// AcknowledgeNotificationEvent records that an operator has taken ownership of
// an open event. Delivery state is intentionally independent from this
// lifecycle: acknowledging an event never retries or cancels a webhook.
func (h *Handler) AcknowledgeNotificationEvent(w http.ResponseWriter, r *http.Request) {
	h.transitionNotificationEvent(w, r, "acknowledged")
}

// ResolveNotificationEvent closes an open or acknowledged event after the
// operator has handled it. Resolved events cannot be reopened through this API.
func (h *Handler) ResolveNotificationEvent(w http.ResponseWriter, r *http.Request) {
	h.transitionNotificationEvent(w, r, "resolved")
}

type notificationEventLifecycleRecord struct {
	OrgID     int64
	ProjectID int64
	Status    string
}

func (h *Handler) transitionNotificationEvent(w http.ResponseWriter, r *http.Request, targetStatus string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid event ID"})
		return
	}

	conditions, args, argIdx, ok := h.notificationEventConditions(w, r)
	if !ok {
		return
	}
	conditions = append(conditions, fmt.Sprintf("id = $%d", argIdx))
	args = append(args, id)
	var event notificationEventLifecycleRecord
	err = h.db.QueryRow(r.Context(),
		"SELECT org_id, COALESCE(project_id, 0), status FROM notification_events WHERE "+strings.Join(conditions, " AND "),
		args...,
	).Scan(&event.OrgID, &event.ProjectID, &event.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Notification event not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	if !h.canWriteNotificationEvent(r, event.OrgID, event.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	if event.Status == targetStatus {
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "status": targetStatus})
		return
	}
	legal := targetStatus == "acknowledged" && event.Status == "open" ||
		targetStatus == "resolved" && (event.Status == "open" || event.Status == "acknowledged")
	if !legal {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":          "Illegal notification event status transition",
			"current_status": event.Status,
			"target_status":  targetStatus,
		})
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to start notification event update"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, notificationMutationRequirement(
		event.OrgID, event.ProjectID, true,
	)); err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Notification authorization failed"})
		}
		return
	}

	tag, err := tx.Exec(r.Context(),
		`UPDATE notification_events
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND org_id = $3 AND status = $4 AND COALESCE(project_id, 0) = $5`,
		targetStatus, id, event.OrgID, event.Status, event.ProjectID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update notification event"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Notification event changed; reload and try again"})
		return
	}

	action := "notification_event." + targetStatus
	detail := map[string]interface{}{
		"before": map[string]interface{}{"status": event.Status},
		"after":  map[string]interface{}{"status": targetStatus},
	}
	if err := insertAuditEntry(r.Context(), tx, action, "notification_event", strconv.FormatInt(id, 10), detail, auditMeta{
		OrgID:           event.OrgID,
		ProjectID:       event.ProjectID,
		IP:              r.RemoteAddr,
		UserAgent:       r.UserAgent(),
		RequestID:       middleware.GetRequestIDFromContext(r.Context()),
		ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
		Source:          auditSourceFromContext(r.Context()),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit notification event update"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to commit notification event update"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "status": targetStatus})
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
		        d.response_status, d.error, d.next_retry_at, d.retry_scope_quarantined,
		        d.scope_quarantine_previous_next_retry_at, d.delivered_at, d.created_at, d.updated_at,
		        e.event_type, e.title, COALESCE(w.name, '')
		 FROM notification_deliveries d
		 JOIN notification_events e ON e.id = d.event_id
		 LEFT JOIN notification_webhooks w ON w.id = d.webhook_id AND w.org_id = e.org_id
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
		var nextRetryAt, scopeQuarantinePreviousNextRetryAt, deliveredAt sql.NullTime
		var retryScopeQuarantined bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &eventID, &webhookID, &status, &attempts, &responseStatus, &errMessage, &nextRetryAt, &retryScopeQuarantined, &scopeQuarantinePreviousNextRetryAt, &deliveredAt, &createdAt, &updatedAt, &eventType, &eventTitle, &webhookName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		deliveries = append(deliveries, map[string]interface{}{
			"id":                      id,
			"event_id":                eventID,
			"webhook_id":              nullableInt64(webhookID),
			"status":                  status,
			"attempts":                attempts,
			"response_status":         nullableInt(responseStatus),
			"error":                   sanitizeWebhookDeliveryError(errMessage),
			"next_retry_at":           nullableTime(nextRetryAt),
			"retry_scope_quarantined": retryScopeQuarantined,
			"scope_quarantine_previous_next_retry_at": nullableTime(scopeQuarantinePreviousNextRetryAt),
			"delivered_at": nullableTime(deliveredAt),
			"created_at":   createdAt,
			"updated_at":   updatedAt,
			"event_type":   eventType,
			"event_title":  eventTitle,
			"webhook_name": webhookName,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": deliveries, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) ListNotificationWebhooks(w http.ResponseWriter, r *http.Request) {
	if rejectBillingNotificationWebhookAccess(w, r) {
		return
	}
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
		`SELECT id, org_id, COALESCE(project_id, 0), name, url, event_types, enabled, scope_verified,
		        scope_quarantine_previous_enabled, created_at, updated_at
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
		var enabled, scopeVerified bool
		var scopeQuarantinePreviousEnabled sql.NullBool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &name, &webhookURL, &eventTypes, &enabled, &scopeVerified, &scopeQuarantinePreviousEnabled, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		webhooks = append(webhooks, notificationWebhookResponse(id, orgID, projectID, name, webhookURL, eventTypes, enabled, scopeVerified, scopeQuarantinePreviousEnabled, createdAt, updatedAt))
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": webhooks, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	if rejectBillingNotificationWebhookAccess(w, r) {
		return
	}
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
	if req.ProjectID < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid project_id"})
		return
	}
	if !h.canWriteNotificationWebhook(r, req.OrgID, req.ProjectID) {
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

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to start webhook creation"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, notificationMutationRequirement(
		req.OrgID, req.ProjectID, false,
	)); err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) || errors.Is(err, errCommittingActorScopeNotFound) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Webhook authorization failed"})
		}
		return
	}

	var id int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO notification_webhooks (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_write_generation, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE, 1, NOW(), NOW())
		 RETURNING id`,
		req.OrgID, projectArg, name, webhookURL, eventTypesJSON, enabled,
	).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create webhook"})
		return
	}

	after := map[string]interface{}{"id": id, "org_id": req.OrgID, "project_id": nullableInt64(req.ProjectID), "name": name, "url": maskWebhookURL(webhookURL), "event_types": json.RawMessage(eventTypesJSON), "enabled": enabled, "scope_verified": true}
	if err := insertAuditEntry(r.Context(), tx, "notification_webhook.create", "notification_webhook", strconv.FormatInt(id, 10), map[string]interface{}{"after": after}, requestAuditMeta(r, req.OrgID, req.ProjectID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit webhook creation"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to commit webhook creation"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

type notificationWebhookRecord struct {
	OrgID                          int64
	ProjectID                      int64
	Name                           string
	URL                            string
	EventTypes                     json.RawMessage
	Enabled                        bool
	ScopeVerified                  bool
	ScopeQuarantinePreviousEnabled sql.NullBool
	UpdatedAt                      time.Time
}

func (h *Handler) notificationWebhookForActorWithDB(ctx context.Context, querier internaldb.DBQuerier, id int64, lock bool) (notificationWebhookRecord, int) {
	var record notificationWebhookRecord
	query := `SELECT webhook.org_id, COALESCE(webhook.project_id, 0), webhook.name, webhook.url,
		        webhook.event_types, webhook.enabled, webhook.scope_verified,
			        scope_quarantine_previous_enabled, updated_at
			 FROM notification_webhooks webhook
			 WHERE webhook.id = $1`
	args := []interface{}{id}
	if !isUnauthenticatedUnitContext(ctx) && actorOrganizationRole(ctx) != auth.RolePlatformAdmin {
		actorOrgID := middleware.GetOrgIDFromContext(ctx)
		if actorOrgID <= 0 {
			return record, http.StatusNotFound
		}
		query += " AND webhook.org_id = $2"
		args = append(args, actorOrgID)
		if actorOrganizationRole(ctx) != auth.RoleOrgAdmin {
			actorUserID := middleware.GetUserIDFromContext(ctx)
			if actorUserID <= 0 {
				return record, http.StatusNotFound
			}
			query += ` AND webhook.project_id IS NOT NULL
				AND EXISTS (
					SELECT 1 FROM project_members pm
					 WHERE pm.project_id=webhook.project_id AND pm.user_id=$3
					   AND pm.role=ANY($4::text[])
				)`
			args = append(args, actorUserID, projectMembershipStorageValues([]string{projectMemberAdmin}))
		}
	}
	if lock {
		query += " FOR UPDATE OF webhook"
	}
	err := querier.QueryRow(ctx, query, args...).Scan(
		&record.OrgID,
		&record.ProjectID,
		&record.Name,
		&record.URL,
		&record.EventTypes,
		&record.Enabled,
		&record.ScopeVerified,
		&record.ScopeQuarantinePreviousEnabled,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, http.StatusNotFound
	}
	if err != nil {
		return record, http.StatusInternalServerError
	}
	return record, 0
}

func writeNotificationWebhookLookupError(w http.ResponseWriter, status int) {
	message := "Webhook not found"
	if status == http.StatusForbidden {
		message = "Forbidden"
	} else if status == http.StatusInternalServerError {
		message = "Query failed"
	}
	writeJSON(w, status, map[string]interface{}{"error": message})
}

func (h *Handler) UpdateNotificationWebhook(w http.ResponseWriter, r *http.Request) {
	if rejectBillingNotificationWebhookAccess(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid webhook ID"})
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

	preflight, status := h.notificationWebhookForActorWithDB(r.Context(), h.db, id, false)
	if status != 0 {
		writeNotificationWebhookLookupError(w, status)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to start webhook update"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, notificationMutationRequirement(
		preflight.OrgID, preflight.ProjectID, false,
	)); err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		} else if errors.Is(err, errCommittingActorScopeNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Webhook not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Webhook authorization failed"})
		}
		return
	}

	existing, status := h.notificationWebhookForActorWithDB(r.Context(), tx, id, true)
	if status != 0 {
		writeNotificationWebhookLookupError(w, status)
		return
	}
	if !h.canWriteNotificationWebhook(r, existing.OrgID, existing.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}

	var sets []string
	var args []interface{}
	argIdx := 1
	after := map[string]interface{}{}
	hasChanges := false
	desiredEnabled := existing.Enabled
	if existing.ScopeQuarantinePreviousEnabled.Valid {
		desiredEnabled = existing.ScopeQuarantinePreviousEnabled.Bool
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "name is required"})
			return
		}
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, name)
		after["name"] = name
		hasChanges = true
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
		after["url"] = maskWebhookURL(webhookURL)
		hasChanges = true
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
		hasChanges = true
		argIdx++
	}
	if req.Enabled != nil {
		desiredEnabled = *req.Enabled
		hasChanges = true
	}
	if !hasChanges {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No fields to update"})
		return
	}

	sets = append(sets, "scope_verified = FALSE", "enabled = FALSE", "scope_write_generation = scope_write_generation + 1", fmt.Sprintf("scope_quarantine_previous_enabled = $%d", argIdx))
	args = append(args, desiredEnabled)
	argIdx++
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id, existing.OrgID)
	query := fmt.Sprintf("UPDATE notification_webhooks SET %s WHERE id = $%d AND org_id = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := tx.Exec(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to update webhook"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Webhook not found"})
		return
	}

	before := map[string]interface{}{"id": id, "org_id": existing.OrgID, "project_id": nullableInt64(existing.ProjectID), "name": existing.Name, "url": maskWebhookURL(existing.URL), "event_types": existing.EventTypes, "enabled": existing.Enabled, "scope_verified": existing.ScopeVerified, "scope_quarantine_previous_enabled": nullableBool(existing.ScopeQuarantinePreviousEnabled)}
	after["id"] = id
	after["org_id"] = existing.OrgID
	after["project_id"] = nullableInt64(existing.ProjectID)
	after["enabled"] = false
	after["scope_verified"] = false
	after["scope_quarantine_previous_enabled"] = desiredEnabled
	if err := insertAuditEntry(r.Context(), tx, "notification_webhook.update", "notification_webhook", strconv.FormatInt(id, 10), map[string]interface{}{"before": before, "after": after}, requestAuditMeta(r, existing.OrgID, existing.ProjectID)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit webhook update"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to commit webhook update"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Webhook updated and quarantined pending scope verification", "scope_verified": false})
}

func (h *Handler) VerifyNotificationWebhookScope(w http.ResponseWriter, r *http.Request) {
	if rejectBillingNotificationWebhookAccess(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid webhook ID"})
		return
	}

	var req struct {
		ExpectedUpdatedAt string `json:"expected_updated_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ExpectedUpdatedAt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "expected_updated_at is required"})
		return
	}
	expectedUpdatedAt, err := time.Parse(time.RFC3339Nano, req.ExpectedUpdatedAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid expected_updated_at"})
		return
	}

	preflight, status := h.notificationWebhookForActorWithDB(r.Context(), h.db, id, false)
	if status != 0 {
		writeNotificationWebhookLookupError(w, status)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to start webhook verification"})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := h.reauthorizeCommittingActorTx(r.Context(), tx, notificationMutationRequirement(
		preflight.OrgID, preflight.ProjectID, false,
	)); err != nil {
		if errors.Is(err, errCommittingActorAuthorizationChanged) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		} else if errors.Is(err, errCommittingActorScopeNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Webhook not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Webhook authorization failed"})
		}
		return
	}

	existing, status := h.notificationWebhookForActorWithDB(r.Context(), tx, id, true)
	if status != 0 {
		writeNotificationWebhookLookupError(w, status)
		return
	}
	if !h.canWriteNotificationWebhook(r, existing.OrgID, existing.ProjectID) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return
	}
	if !expectedUpdatedAt.Equal(existing.UpdatedAt) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Webhook changed; reload before verification"})
		return
	}
	if existing.ScopeVerified {
		writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Webhook scope already verified", "scope_verified": true})
		return
	}

	restoredEnabled := existing.Enabled
	if existing.ScopeQuarantinePreviousEnabled.Valid {
		restoredEnabled = existing.ScopeQuarantinePreviousEnabled.Bool
	}
	tag, err := tx.Exec(r.Context(),
		`UPDATE notification_webhooks
		 SET scope_verified = TRUE,
		     enabled = COALESCE(scope_quarantine_previous_enabled, enabled),
		     scope_quarantine_previous_enabled = NULL,
		     scope_write_generation = scope_write_generation + 1,
		     updated_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND scope_verified = FALSE AND updated_at = $3`,
		id, existing.OrgID, expectedUpdatedAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to verify webhook scope"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Webhook changed; reload before verification"})
		return
	}

	before := map[string]interface{}{"id": id, "org_id": existing.OrgID, "project_id": nullableInt64(existing.ProjectID), "name": existing.Name, "url": maskWebhookURL(existing.URL), "event_types": existing.EventTypes, "enabled": existing.Enabled, "scope_verified": false, "scope_quarantine_previous_enabled": nullableBool(existing.ScopeQuarantinePreviousEnabled)}
	after := map[string]interface{}{"id": id, "org_id": existing.OrgID, "project_id": nullableInt64(existing.ProjectID), "name": existing.Name, "url": maskWebhookURL(existing.URL), "event_types": existing.EventTypes, "enabled": restoredEnabled, "scope_verified": true, "scope_quarantine_previous_enabled": nil}
	var projectIDArg int64
	if existing.ProjectID > 0 {
		projectIDArg = existing.ProjectID
	}
	if err := insertAuditEntry(r.Context(), tx, "notification_webhook.scope_verify", "notification_webhook", strconv.FormatInt(id, 10), map[string]interface{}{"before": before, "after": after}, auditMeta{
		OrgID:     existing.OrgID,
		ProjectID: projectIDArg,
		IP:        r.RemoteAddr,
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Source:    auditSourceFromContext(r.Context()),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit webhook verification"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to commit webhook verification"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "Webhook scope verified", "scope_verified": true})
}

func notificationMutationRequirement(orgID, projectID int64, allowBilling bool) committingActorRequirement {
	organizationRoles := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}
	if allowBilling {
		organizationRoles = append(organizationRoles, auth.RoleBillingAdmin)
	}
	requirement := committingActorRequirement{
		TargetOrgID:              orgID,
		AllowedOrganizationRoles: organizationRoles,
	}
	if projectID > 0 {
		requirement.Project = &committingActorProjectRequirement{
			ProjectID:    projectID,
			AllowedRoles: []string{projectMemberAdmin},
		}
	}
	return requirement
}

func (h *Handler) notificationEventConditions(w http.ResponseWriter, r *http.Request) ([]string, []interface{}, int, bool) {
	conditions, args, argIdx, ok := h.notificationProjectScopedConditions(w, r, "org_id", "project_id")
	if !ok {
		return nil, nil, 0, false
	}
	return appendBillingNotificationEventScope(r.Context(), "event_type", conditions, args, argIdx)
}

func (h *Handler) notificationDeliveryConditions(w http.ResponseWriter, r *http.Request) ([]string, []interface{}, int, bool) {
	conditions, args, argIdx, ok := h.notificationProjectScopedConditions(w, r, "e.org_id", "e.project_id")
	if !ok {
		return nil, nil, 0, false
	}
	return appendBillingNotificationEventScope(r.Context(), "e.event_type", conditions, args, argIdx)
}

func appendBillingNotificationEventScope(ctx context.Context, eventTypeColumn string, conditions []string, args []interface{}, argIdx int) ([]string, []interface{}, int, bool) {
	if actorOrganizationRole(ctx) != auth.RoleBillingAdmin {
		return conditions, args, argIdx, true
	}
	conditions = append(conditions, fmt.Sprintf("%s = ANY($%d::text[])", eventTypeColumn, argIdx))
	args = append(args, billing.FinancialNotificationEventTypes())
	return conditions, args, argIdx + 1, true
}

func rejectBillingNotificationWebhookAccess(w http.ResponseWriter, r *http.Request) bool {
	if !isUnauthenticatedUnitContext(r.Context()) && actorOrganizationRole(r.Context()) == auth.RoleBillingAdmin {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
		return true
	}
	return false
}

func (h *Handler) notificationWebhookConditions(w http.ResponseWriter, r *http.Request, requireWritable bool) ([]string, []interface{}, int, bool) {
	conditions, args, argIdx, ok := h.notificationProjectScopedConditions(w, r, "org_id", "project_id")
	if !ok {
		return nil, nil, 0, false
	}
	if requireWritable && !auth.RoleIn(actorOrganizationRole(r.Context()), auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
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
		if !actorCanAccessOrg(r.Context(), orgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return nil, nil, 0, false
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", orgColumn, argIdx))
		args = append(args, orgID)
		argIdx++
	} else if actorOrgID, constrained := actorOrgConstraint(r.Context()); constrained {
		if actorOrgID <= 0 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return nil, nil, 0, false
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", orgColumn, argIdx))
		args = append(args, actorOrgID)
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
	organizationRole := actorOrganizationRole(r.Context())
	hasOrganizationRead := auth.RoleIn(organizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)
	if !isUnauthenticatedUnitContext(r.Context()) && !hasOrganizationRead {
		if projectID <= 0 || !h.canReadNotificationProject(r.Context(), projectID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "project_id is required"})
			return nil, nil, 0, false
		}
	}
	if projectID > 0 {
		canRead := h.canReadNotificationProject(r.Context(), projectID)
		if hasOrganizationRead {
			canRead = h.canReadProjectUsage(r.Context(), projectID)
		}
		if !isUnauthenticatedUnitContext(r.Context()) && !canRead {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Forbidden"})
			return nil, nil, 0, false
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", projectColumn, argIdx))
		args = append(args, projectID)
		argIdx++
	}
	if len(conditions) == 0 {
		conditions = append(conditions, "1 = 1")
	}
	return conditions, args, argIdx, true
}

func (h *Handler) canReadNotificationProject(ctx context.Context, projectID int64) bool {
	if isUnauthenticatedUnitContext(ctx) {
		return true
	}
	return h.hasProjectMembership(ctx, projectID,
		projectMemberAdmin, projectMemberUser, projectMemberViewer,
		projectMemberLegacyDeveloper, auth.RoleDeveloper, auth.RoleViewer)
}

func (h *Handler) canWriteNotificationWebhook(r *http.Request, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(r.Context()) {
		return true
	}
	if !actorCanAccessOrg(r.Context(), orgID) {
		return false
	}

	role := actorOrganizationRole(r.Context())
	if projectID > 0 && !h.projectBelongsToOrg(r.Context(), projectID, orgID) {
		return false
	}
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin {
		return true
	}
	return projectID > 0 && h.canWriteProject(r.Context(), projectID)
}

func (h *Handler) canWriteNotificationEvent(r *http.Request, orgID, projectID int64) bool {
	if isUnauthenticatedUnitContext(r.Context()) {
		return true
	}
	if !actorCanAccessOrg(r.Context(), orgID) {
		return false
	}
	role := actorOrganizationRole(r.Context())
	if projectID > 0 && !h.projectBelongsToOrg(r.Context(), projectID, orgID) {
		return false
	}
	if role == auth.RolePlatformAdmin || role == auth.RoleOrgAdmin || role == auth.RoleBillingAdmin {
		return true
	}
	requestedProjectID, valid := projectIDFromQuery(r)
	return valid && projectID > 0 && requestedProjectID == projectID && h.canWriteProject(r.Context(), projectID)
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
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || hostname == "metadata.google.internal" || hostname == "instance-data" {
		return false
	}
	if ip := net.ParseIP(hostname); ip != nil && !billing.IsNotificationWebhookIPAllowed(ip) {
		return false
	}
	return true
}

func maskWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	if u.Path != "" && u.Path != "/" {
		u.Path = "/..."
		u.RawPath = ""
	}
	return u.String()
}

func sanitizeWebhookDeliveryError(message string) string {
	if strings.Contains(message, "http://") || strings.Contains(message, "https://") {
		return "webhook delivery failed"
	}
	return message
}

func notificationWebhookResponse(id, orgID, projectID int64, name, webhookURL string, eventTypes json.RawMessage, enabled, scopeVerified bool, scopeQuarantinePreviousEnabled sql.NullBool, createdAt, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":                                id,
		"org_id":                            orgID,
		"project_id":                        nullableInt64(projectID),
		"name":                              name,
		"url":                               maskWebhookURL(webhookURL),
		"event_types":                       eventTypes,
		"enabled":                           enabled,
		"scope_verified":                    scopeVerified,
		"scope_quarantine_previous_enabled": nullableBool(scopeQuarantinePreviousEnabled),
		"created_at":                        createdAt,
		"updated_at":                        updatedAt,
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

func nullableBool(value sql.NullBool) interface{} {
	if !value.Valid {
		return nil
	}
	return value.Bool
}

func nullableTime(value sql.NullTime) interface{} {
	if !value.Valid {
		return nil
	}
	return value.Time
}
