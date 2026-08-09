// Package accessevidence writes the atomic audit/outbox pair shared by Access
// provisioning and product-owned subject lifecycle transitions.
package accessevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/outbox"
)

func WriteAuditAndOutbox(ctx context.Context, tx pgx.Tx, orgID, projectID, actorUserID int64,
	action, resource, resourceID, requestID, clientRequestID string, detail map[string]any, now time.Time,
) error {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal access audit: %w", err)
	}
	payload := map[string]any{
		"org_id": orgID, "project_id": projectID, "user_id": actorUserID,
		"action": action, "resource": resource, "resource_id": resourceID,
		"detail": json.RawMessage(detailJSON), "before_state": json.RawMessage(`{}`),
		"after_state": json.RawMessage(detailJSON), "ip": "", "user_agent": "",
		"request_id": requestID, "client_request_id": clientRequestID,
		"source": "admin_console", "occurred_at": now,
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{action, resource, resourceID, requestID, clientRequestID, string(detailJSON)}, "|")))
	eventKey := "audit:" + hex.EncodeToString(digest[:])
	event, err := outbox.NewEvent(outbox.EventAudit, eventKey, outbox.PriorityAudit, orgID, requestID, payload, now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (
		  org_id,user_id,action,resource,resource_id,detail,request_id,source,project_id,
		  before_state,after_state,client_request_id,outbox_event_key,created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'admin_console',$8,'{}'::jsonb,$6,$9,$10,$11)
		ON CONFLICT (outbox_event_key) DO NOTHING`, orgID, actorUserID, action, resource,
		resourceID, detailJSON, requestID, projectID, nullableString(clientRequestID), eventKey, now); err != nil {
		return fmt.Errorf("insert access audit: %w", err)
	}
	if _, _, err := outbox.EnqueueTx(ctx, tx, event); err != nil {
		return fmt.Errorf("enqueue access audit: %w", err)
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
