package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var conditions []string
	var args []interface{}
	argIdx := 1
	actorOrgID, orgConstrained := actorOrgConstraint(r.Context())
	if orgConstrained && actorOrgID <= 0 {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Organization scope required"})
		return
	}
	addCondition := func(column string, value interface{}) {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, argIdx))
		args = append(args, value)
		argIdx++
	}
	parsePositiveIntFilter := func(queryKey string) (int64, bool, bool) {
		raw := strings.TrimSpace(r.URL.Query().Get(queryKey))
		if raw == "" {
			return 0, false, true
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid " + queryKey})
			return 0, true, false
		}
		return value, true, true
	}

	requestedOrgID, hasOrgFilter, ok := parsePositiveIntFilter("org_id")
	if !ok {
		return
	}
	if orgConstrained {
		if hasOrgFilter && requestedOrgID != actorOrgID {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Cross-organization access denied"})
			return
		}
		addCondition("a.org_id", actorOrgID)
	} else if hasOrgFilter {
		addCondition("a.org_id", requestedOrgID)
	}

	requestedProjectID, hasProjectFilter, ok := parsePositiveIntFilter("project_id")
	if !ok {
		return
	}
	if hasProjectFilter {
		if orgConstrained && !h.projectBelongsToOrg(r.Context(), requestedProjectID, actorOrgID) {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{"error": "Cross-organization project access denied"})
			return
		}
		addCondition("a.project_id", requestedProjectID)
	}

	addIntFilter := func(queryKey, column string) bool {
		value, present, valid := parsePositiveIntFilter(queryKey)
		if !valid {
			return false
		}
		if present {
			addCondition(column, value)
		}
		return true
	}
	if !addIntFilter("actor_id", "a.user_id") || !addIntFilter("user_id", "a.user_id") {
		return
	}
	addStringFilter := func(queryKey, column string) {
		value := strings.TrimSpace(r.URL.Query().Get(queryKey))
		if value == "" {
			return
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, argIdx))
		args = append(args, value)
		argIdx++
	}
	addStringFilter("action", "a.action")
	addStringFilter("resource", "a.resource")
	addStringFilter("resource_id", "a.resource_id")
	addStringFilter("request_id", "a.request_id")
	addStringFilter("client_request_id", "a.client_request_id")
	if rawSource := strings.TrimSpace(r.URL.Query().Get("source")); rawSource != "" {
		source := normalizeAuditSource(rawSource)
		if !isValidAuditSource(source) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid source"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("a.source = $%d", argIdx))
		args = append(args, source)
		argIdx++
	}
	if start := strings.TrimSpace(r.URL.Query().Get("start_time")); start != "" {
		startAt, err := time.Parse(time.RFC3339, start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid start_time"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("a.created_at >= $%d", argIdx))
		args = append(args, startAt)
		argIdx++
	}
	if end := strings.TrimSpace(r.URL.Query().Get("end_time")); end != "" {
		endAt, err := time.Parse(time.RFC3339, end)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid end_time"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("a.created_at <= $%d", argIdx))
		args = append(args, endAt)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM audit_logs a %s`, where), args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT a.id, a.org_id, COALESCE(a.project_id, 0), a.user_id,
		        COALESCE(o.name, ''), COALESCE(p.name, ''), COALESCE(u.username, ''),
		        a.action, a.resource, a.resource_id,
		        a.detail, COALESCE(a.before_state, '{}'::jsonb), COALESCE(a.after_state, '{}'::jsonb),
		        a.ip, a.user_agent, a.request_id, COALESCE(a.source, 'admin_console'), COALESCE(a.client_request_id, ''), a.created_at
		 FROM audit_logs a
		 LEFT JOIN orgs o ON o.id = a.org_id
		 LEFT JOIN projects p ON p.id = a.project_id AND p.org_id = a.org_id
		 LEFT JOIN users u ON u.id = a.user_id AND (u.org_id = a.org_id OR u.role IN ('admin', 'platform_admin'))
		 %s ORDER BY a.id DESC LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, orgID, projectID, userID int64
		var orgName, projectName, actorName, action, resource, resourceID, ip, userAgent, requestID, source, clientRequestID string
		var detail, beforeState, afterState []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &userID, &orgName, &projectName, &actorName, &action, &resource, &resourceID, &detail, &beforeState, &afterState, &ip, &userAgent, &requestID, &source, &clientRequestID, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		var projectValue interface{}
		if projectID > 0 {
			projectValue = projectID
		}
		logs = append(logs, map[string]interface{}{
			"id": id, "org_id": orgID, "org_name": orgName, "project_id": projectValue, "project_name": projectName,
			"actor_id": userID, "actor_name": actorName, "user_id": userID,
			"action": action, "resource": resource, "resource_id": resourceID,
			"detail": json.RawMessage(detail), "before": json.RawMessage(beforeState), "after": json.RawMessage(afterState),
			"ip": ip, "user_agent": userAgent, "request_id": requestID, "client_request_id": clientRequestID,
			"source": normalizeAuditSource(source), "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rows failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total, "page": page, "page_size": pageSize})
}

// --- Cleanup ---

// recordAudit logs an admin action to audit_logs. Non-blocking, errors are logged.
func (h *Handler) recordAudit(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, ip string) {
	h.recordAuditForProject(ctx, action, resource, resourceID, middleware.GetProjectIDFromContext(ctx), detail, ip)
}

func (h *Handler) recordAuditForProject(ctx context.Context, action, resource, resourceID string, projectID int64, detail map[string]interface{}, ip string) {
	h.recordAuditEntry(ctx, action, resource, resourceID, detail, auditMeta{
		OrgID:           middleware.GetOrgIDFromContext(ctx),
		ProjectID:       projectID,
		IP:              ip,
		RequestID:       middleware.GetRequestIDFromContext(ctx),
		ClientRequestID: middleware.GetClientRequestIDFromContext(ctx),
		Source:          auditSourceFromContext(ctx),
	})
}

func (h *Handler) recordAuditFromRequest(r *http.Request, action, resource, resourceID string, detail map[string]interface{}) {
	h.recordAuditForProjectFromRequest(r, action, resource, resourceID, middleware.GetProjectIDFromContext(r.Context()), detail)
}

func (h *Handler) recordAuditForProjectFromRequest(r *http.Request, action, resource, resourceID string, projectID int64, detail map[string]interface{}) {
	h.recordAuditForOrgProjectFromRequest(r, middleware.GetOrgIDFromContext(r.Context()), projectID, action, resource, resourceID, detail)
}

func (h *Handler) recordAuditForOrgProjectFromRequest(r *http.Request, orgID, projectID int64, action, resource, resourceID string, detail map[string]interface{}) {
	h.recordAuditEntry(r.Context(), action, resource, resourceID, detail, auditMeta{
		OrgID:           orgID,
		ProjectID:       projectID,
		IP:              r.RemoteAddr,
		UserAgent:       r.UserAgent(),
		RequestID:       middleware.GetRequestIDFromContext(r.Context()),
		ClientRequestID: middleware.GetClientRequestIDFromContext(r.Context()),
		Source:          auditSourceFromContext(r.Context()),
	})
}

type auditMeta struct {
	OrgID           int64
	ProjectID       int64
	IP              string
	UserAgent       string
	RequestID       string
	ClientRequestID string
	Source          string
}

func (h *Handler) recordAuditEntry(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, meta auditMeta) {
	if h != nil && h.outbox != nil {
		if err := h.enqueueAuditEntry(ctx, action, resource, resourceID, detail, meta); err == nil {
			return
		} else {
			log.Printf("audit outbox failed; falling back to direct insert: %s %s/%s: %v", action, resource, resourceID, err)
		}
	}
	if err := insertAuditEntry(ctx, h.db, action, resource, resourceID, detail, meta); err != nil {
		log.Printf("audit log failed: %s %s/%s: %v", action, resource, resourceID, err)
	}
}

type auditExecer interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
}

func insertAuditEntry(ctx context.Context, execer auditExecer, action, resource, resourceID string, detail map[string]interface{}, meta auditMeta) error {
	detailJSON := marshalAuditJSON(detail)
	beforeJSON := "{}"
	afterJSON := "{}"
	if detail != nil {
		if before, ok := detail["before"]; ok {
			beforeJSON = marshalAuditJSON(before)
		}
		if after, ok := detail["after"]; ok {
			afterJSON = marshalAuditJSON(after)
		}
	}
	userID := middleware.GetUserIDFromContext(ctx)
	var projectIDArg interface{}
	if meta.ProjectID > 0 {
		projectIDArg = meta.ProjectID
	}
	source := normalizeAuditSource(meta.Source)
	clientRequestID := meta.ClientRequestID
	if clientRequestID == "" {
		clientRequestID = middleware.GetClientRequestIDFromContext(ctx)
	}
	_, err := execer.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, user_agent, request_id, source, project_id, before_state, after_state, client_request_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14, NOW())`,
		meta.OrgID, userID, action, resource, resourceID, detailJSON, meta.IP, meta.UserAgent, meta.RequestID, source, projectIDArg, beforeJSON, afterJSON, nullableString(clientRequestID))
	return err
}

func marshalAuditJSON(value interface{}) string {
	if value == nil {
		return "{}"
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func auditSourceFromContext(ctx context.Context) string {
	role := middleware.GetUserRoleFromContext(ctx)
	switch {
	case auth.IsProjectScopedRole(role):
		return "developer_console"
	case role != "" || middleware.GetUserIDFromContext(ctx) > 0:
		return "admin_console"
	default:
		return "system"
	}
}

func normalizeAuditSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" || source == "admin" {
		return "admin_console"
	}
	return source
}

func isValidAuditSource(source string) bool {
	switch normalizeAuditSource(source) {
	case "admin_console", "developer_console", "gateway_api", "system":
		return true
	default:
		return false
	}
}

func (h *Handler) CleanupOldData(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return h.cleanupOldData(ctx, retentionDays, retentionDays)
}

func (h *Handler) CleanupOldDataWithTraceRetention(ctx context.Context, retentionDays, traceRetentionDays int) error {
	return h.cleanupOldData(ctx, retentionDays, traceRetentionDays)
}

func (h *Handler) cleanupOldData(ctx context.Context, retentionDays, traceRetentionDays int) error {
	var result pgconn.CommandTag
	var err error

	if retentionDays > 0 {
		result, err = h.db.Exec(ctx,
			`DELETE FROM call_logs WHERE created_at < NOW() - INTERVAL '1 day' * $1`,
			retentionDays)
		if err != nil {
			return fmt.Errorf("cleanup call_logs: %w", err)
		}
		log.Printf("Cleanup: deleted %d old call_logs (older than %d days)", result.RowsAffected(), retentionDays)

		result, err = h.db.Exec(ctx,
			`DELETE FROM audit_logs WHERE created_at < NOW() - INTERVAL '1 day' * $1`,
			retentionDays*2)
		if err != nil {
			return fmt.Errorf("cleanup audit_logs: %w", err)
		}
		log.Printf("Cleanup: deleted %d old audit_logs (older than %d days)", result.RowsAffected(), retentionDays*2)

		result, err = h.db.Exec(ctx,
			`DELETE FROM transactions WHERE created_at < NOW() - INTERVAL '1 day' * $1 AND type IN ('settle', 'cancel')`,
			retentionDays)
		if err != nil {
			return fmt.Errorf("cleanup transactions: %w", err)
		}
		log.Printf("Cleanup: deleted %d old transactions (older than %d days)", result.RowsAffected(), retentionDays)
	}

	if traceRetentionDays > 0 {
		result, err = h.db.Exec(ctx,
			`DELETE FROM request_trace_events WHERE created_at < NOW() - INTERVAL '1 day' * $1`,
			traceRetentionDays)
		if err != nil {
			return fmt.Errorf("cleanup request_trace_events: %w", err)
		}
		log.Printf("Cleanup: deleted %d old request_trace_events (older than %d days)", result.RowsAffected(), traceRetentionDays)
	}

	// Release stale pre-output reservations through the same source-preserving,
	// single-terminal state machine as the request path. A future
	// settlement_pending row is intentionally excluded and can never be made
	// free by retention cleanup.
	staleRows, err := h.db.Query(ctx,
		`SELECT org_id, request_id
		 FROM freeze_idempotency
		 WHERE status = 'frozen' AND updated_at < NOW() - INTERVAL '24 hours'
		 ORDER BY updated_at ASC
		 LIMIT 1000`)
	if err != nil {
		return fmt.Errorf("query stale frozen requests: %w", err)
	}
	type staleReservation struct {
		orgID     int64
		requestID string
	}
	stale := make([]staleReservation, 0)
	for staleRows.Next() {
		var reservation staleReservation
		if err := staleRows.Scan(&reservation.orgID, &reservation.requestID); err != nil {
			staleRows.Close()
			return fmt.Errorf("scan stale frozen request: %w", err)
		}
		stale = append(stale, reservation)
	}
	if err := staleRows.Err(); err != nil {
		staleRows.Close()
		return fmt.Errorf("iterate stale frozen requests: %w", err)
	}
	staleRows.Close()
	releasedCount := 0
	for _, reservation := range stale {
		var cancelErr error
		if h.outbox != nil {
			cancelErr = billing.CancelReservationWithOutbox(ctx, h.db, h.outbox, reservation.requestID, reservation.orgID)
		} else {
			cancelErr = billing.CancelReservation(ctx, h.db, reservation.requestID, reservation.orgID)
		}
		if cancelErr != nil {
			if errors.Is(cancelErr, billing.ErrSettlementPending) {
				continue
			}
			return fmt.Errorf("release stale frozen request %s: %w", reservation.requestID, cancelErr)
		}
		releasedCount++
	}
	log.Printf("Cleanup: released %d stale frozen freeze_idempotency records (>24h)", releasedCount)

	// Keep active frozen and billable pending records until their state machines
	// reach a durable terminal.
	result, err = h.db.Exec(ctx,
		`DELETE FROM freeze_idempotency
		 WHERE created_at < NOW() - INTERVAL '30 days'
		   AND status NOT IN ('frozen', 'settlement_pending')`,
	)
	if err != nil {
		return fmt.Errorf("cleanup freeze_idempotency: %w", err)
	}
	log.Printf("Cleanup: deleted %d expired non-frozen freeze_idempotency records (>30d)", result.RowsAffected())

	// Scheme A stores no response body. Remove the bounded operation receipt
	// metadata once its replay-protection window has expired.
	result, err = h.db.Exec(ctx,
		`DELETE FROM request_idempotency WHERE expires_at < NOW()`,
	)
	if err != nil {
		return fmt.Errorf("cleanup request_idempotency: %w", err)
	}
	log.Printf("Cleanup: deleted %d expired request_idempotency receipts", result.RowsAffected())

	expiryResult, err := billing.ExpireGrants(ctx, h.db, 1000)
	if err != nil {
		return fmt.Errorf("expire grants: %w", err)
	}
	if expiryResult.Grants > 0 {
		log.Printf("Cleanup: expired %d grants across %d wallets", expiryResult.Grants, expiryResult.Organizations)
	}

	return nil
}
