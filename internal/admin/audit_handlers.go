package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM audit_logs`).Scan(&total)

	rows, err := h.db.Query(r.Context(),
		`SELECT id, user_id, action, resource, resource_id, detail, created_at
		 FROM audit_logs ORDER BY id DESC LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Query failed"})
		return
	}
	defer rows.Close()

	logs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, userId int64
		var action, resource, resourceId string
		var detail []byte
		var createdAt time.Time
		rows.Scan(&id, &userId, &action, &resource, &resourceId, &detail, &createdAt)
		logs = append(logs, map[string]interface{}{
			"id": id, "user_id": userId, "action": action,
			"resource": resource, "resource_id": resourceId,
			"detail": json.RawMessage(detail), "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": logs, "total": total})
}

// --- Cleanup ---

// recordAudit logs an admin action to audit_logs. Non-blocking, errors are logged.
func (h *Handler) recordAudit(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, ip string) {
	detailJSON := "{}"
	if b, err := json.Marshal(detail); err == nil {
		detailJSON = string(b)
	}
	_, err := h.db.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, request_id, created_at)
		 VALUES (0, 0, $1, $2, $3, $4::jsonb, $5, '', NOW())`,
		action, resource, resourceID, detailJSON, ip)
	if err != nil {
		log.Printf("audit log failed: %s %s/%s: %v", action, resource, resourceID, err)
	}
}

func (h *Handler) CleanupOldData(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	result, err := h.db.Exec(ctx,
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

	// Keep freeze_idempotency records for 30 days (needed for idempotency checks)
	result, err = h.db.Exec(ctx,
		`DELETE FROM freeze_idempotency WHERE created_at < NOW() - INTERVAL '30 days'`,
	)
	if err != nil {
		return fmt.Errorf("cleanup freeze_idempotency: %w", err)
	}
	log.Printf("Cleanup: deleted %d expired freeze_idempotency records (>30d)", result.RowsAffected())

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
