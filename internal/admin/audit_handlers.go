package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	offset := paginationOffset(page, pageSize)

	var conditions []string
	var args []interface{}
	argIdx := 1
	addIntFilter := func(queryKey, column string) bool {
		raw := strings.TrimSpace(r.URL.Query().Get(queryKey))
		if raw == "" {
			return true
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid " + queryKey})
			return false
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, argIdx))
		args = append(args, value)
		argIdx++
		return true
	}
	if !addIntFilter("org_id", "org_id") || !addIntFilter("project_id", "project_id") || !addIntFilter("actor_id", "user_id") || !addIntFilter("user_id", "user_id") {
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
	addStringFilter("action", "action")
	addStringFilter("resource", "resource")
	addStringFilter("resource_id", "resource_id")
	if rawSource := strings.TrimSpace(r.URL.Query().Get("source")); rawSource != "" {
		source := normalizeAuditSource(rawSource)
		if !isValidAuditSource(source) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid source"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, source)
		argIdx++
	}
	if start := strings.TrimSpace(r.URL.Query().Get("start_time")); start != "" {
		startAt, err := time.Parse(time.RFC3339, start)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid start_time"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, startAt)
		argIdx++
	}
	if end := strings.TrimSpace(r.URL.Query().Get("end_time")); end != "" {
		endAt, err := time.Parse(time.RFC3339, end)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid end_time"})
			return
		}
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, endAt)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := h.db.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM audit_logs %s`, where), args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Count failed"})
		return
	}

	query := fmt.Sprintf(
		`SELECT id, org_id, COALESCE(project_id, 0), user_id, action, resource, resource_id,
		        detail, COALESCE(before_state, '{}'::jsonb), COALESCE(after_state, '{}'::jsonb),
		        ip, user_agent, request_id, COALESCE(source, 'admin_console'), created_at
		 FROM audit_logs %s ORDER BY id DESC LIMIT $%d OFFSET $%d`,
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
		var action, resource, resourceID, ip, userAgent, requestID, source string
		var detail, beforeState, afterState []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &orgID, &projectID, &userID, &action, &resource, &resourceID, &detail, &beforeState, &afterState, &ip, &userAgent, &requestID, &source, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Scan failed"})
			return
		}
		var projectValue interface{}
		if projectID > 0 {
			projectValue = projectID
		}
		logs = append(logs, map[string]interface{}{
			"id": id, "org_id": orgID, "project_id": projectValue, "actor_id": userID, "user_id": userID,
			"action": action, "resource": resource, "resource_id": resourceID,
			"detail": json.RawMessage(detail), "before": json.RawMessage(beforeState), "after": json.RawMessage(afterState),
			"ip": ip, "user_agent": userAgent, "request_id": requestID, "source": normalizeAuditSource(source), "created_at": createdAt,
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
		OrgID:     middleware.GetOrgIDFromContext(ctx),
		ProjectID: projectID,
		IP:        ip,
		RequestID: middleware.GetRequestIDFromContext(ctx),
		Source:    auditSourceFromContext(ctx),
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
		OrgID:     orgID,
		ProjectID: projectID,
		IP:        r.RemoteAddr,
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetRequestIDFromContext(r.Context()),
		Source:    auditSourceFromContext(r.Context()),
	})
}

type auditMeta struct {
	OrgID     int64
	ProjectID int64
	IP        string
	UserAgent string
	RequestID string
	Source    string
}

func (h *Handler) recordAuditEntry(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, meta auditMeta) {
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
	_, err := h.db.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, user_agent, request_id, source, project_id, before_state, after_state, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, NOW())`,
		meta.OrgID, userID, action, resource, resourceID, detailJSON, meta.IP, meta.UserAgent, meta.RequestID, source, projectIDArg, beforeJSON, afterJSON)
	if err != nil {
		log.Printf("audit log failed: %s %s/%s: %v", action, resource, resourceID, err)
	}
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

	// Release stale frozen requests before pruning idempotency rows. This prevents
	// a failed settle from leaving wallet.frozen permanently held after cleanup.
	result, err = h.db.Exec(ctx,
		`WITH stale AS (
		     SELECT org_id, SUM(frozen_amount)::integer AS total_frozen
		     FROM freeze_idempotency
		     WHERE status = 'frozen' AND updated_at < NOW() - INTERVAL '24 hours'
		     GROUP BY org_id
		 ), released AS (
		     UPDATE wallets w
		     SET frozen = GREATEST(w.frozen - LEAST(w.frozen, stale.total_frozen), 0),
		         balance = w.balance + LEAST(w.frozen, stale.total_frozen),
		         updated_at = NOW()
		     FROM stale
		     WHERE w.org_id = stale.org_id
		     RETURNING w.org_id
		 )
		 UPDATE freeze_idempotency
		 SET status = 'cancelled', updated_at = NOW()
		 WHERE status = 'frozen' AND updated_at < NOW() - INTERVAL '24 hours'`,
	)
	if err != nil {
		return fmt.Errorf("release stale frozen requests: %w", err)
	}
	log.Printf("Cleanup: released %d stale frozen freeze_idempotency records (>24h)", result.RowsAffected())

	// Keep active frozen records until they either settle or the stale-release guard above cancels them.
	result, err = h.db.Exec(ctx,
		`DELETE FROM freeze_idempotency WHERE created_at < NOW() - INTERVAL '30 days' AND status <> 'frozen'`,
	)
	if err != nil {
		return fmt.Errorf("cleanup freeze_idempotency: %w", err)
	}
	log.Printf("Cleanup: deleted %d expired non-frozen freeze_idempotency records (>30d)", result.RowsAffected())

	// First: deduct expired grants from wallets.granted_balance
	result, err = h.db.Exec(ctx,
		`UPDATE wallets w
		 SET granted_balance = granted_balance - COALESCE(sub.total, 0),
		     updated_at = NOW()
		 FROM (
		     SELECT org_id, SUM(balance) AS total
		     FROM grants
		     WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW()
		     GROUP BY org_id
		 ) sub
		 WHERE w.org_id = sub.org_id AND sub.total > 0`)
	if err != nil {
		return fmt.Errorf("deduct expired grants from wallets: %w", err)
	}
	if result.RowsAffected() > 0 {
		log.Printf("Cleanup: deducted expired grants from %d wallets", result.RowsAffected())
	}

	// Second: mark grants as expired
	result, err = h.db.Exec(ctx,
		`UPDATE grants SET status = 'expired', updated_at = NOW()
		 WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("expire grants: %w", err)
	}
	if result.RowsAffected() > 0 {
		log.Printf("Cleanup: expired %d grants past their expiry date", result.RowsAffected())
	}

	return nil
}
