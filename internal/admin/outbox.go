package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
)

type auditOutboxPayload struct {
	OrgID           int64           `json:"org_id"`
	ProjectID       *int64          `json:"project_id,omitempty"`
	UserID          int64           `json:"user_id"`
	Action          string          `json:"action"`
	Resource        string          `json:"resource"`
	ResourceID      string          `json:"resource_id"`
	Detail          json.RawMessage `json:"detail"`
	BeforeState     json.RawMessage `json:"before_state"`
	AfterState      json.RawMessage `json:"after_state"`
	IP              string          `json:"ip"`
	UserAgent       string          `json:"user_agent"`
	RequestID       string          `json:"request_id"`
	ClientRequestID string          `json:"client_request_id,omitempty"`
	Source          string          `json:"source"`
	OccurredAt      time.Time       `json:"occurred_at"`
}

func (h *Handler) enqueueAuditEntry(ctx context.Context, action, resource, resourceID string, detail map[string]interface{}, meta auditMeta) error {
	if h == nil || h.outbox == nil {
		return fmt.Errorf("audit outbox is unavailable")
	}
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
	clientRequestID := meta.ClientRequestID
	if clientRequestID == "" {
		clientRequestID = middleware.GetClientRequestIDFromContext(ctx)
	}
	payload := auditOutboxPayload{
		OrgID:           meta.OrgID,
		ProjectID:       positiveAuditID(meta.ProjectID),
		UserID:          middleware.GetUserIDFromContext(ctx),
		Action:          action,
		Resource:        resource,
		ResourceID:      resourceID,
		Detail:          json.RawMessage(detailJSON),
		BeforeState:     json.RawMessage(beforeJSON),
		AfterState:      json.RawMessage(afterJSON),
		IP:              meta.IP,
		UserAgent:       meta.UserAgent,
		RequestID:       meta.RequestID,
		ClientRequestID: clientRequestID,
		Source:          normalizeAuditSource(meta.Source),
		OccurredAt:      time.Now().UTC(),
	}
	return enqueueAuditPayload(ctx, h.outbox, payload)
}

// EnqueueGatewayAudit routes gateway-side governance evidence through the
// same high-priority audit worker without exposing Handler internals.
func EnqueueGatewayAudit(ctx context.Context, service *outbox.Service, orgID, projectID int64, action, resource, resourceID string, detail map[string]interface{}) error {
	if service == nil {
		return fmt.Errorf("audit outbox is unavailable")
	}
	detailJSON := marshalAuditJSON(detail)
	payload := auditOutboxPayload{
		OrgID:           orgID,
		ProjectID:       positiveAuditID(projectID),
		UserID:          middleware.GetUserIDFromContext(ctx),
		Action:          action,
		Resource:        resource,
		ResourceID:      resourceID,
		Detail:          json.RawMessage(detailJSON),
		BeforeState:     json.RawMessage(`{}`),
		AfterState:      json.RawMessage(detailJSON),
		RequestID:       middleware.GetRequestIDFromContext(ctx),
		ClientRequestID: middleware.GetClientRequestIDFromContext(ctx),
		Source:          "gateway_api",
		OccurredAt:      time.Now().UTC(),
	}
	return enqueueAuditPayload(ctx, service, payload)
}

func enqueueAuditPayload(ctx context.Context, service *outbox.Service, payload auditOutboxPayload) error {
	identity := payload.RequestID + "|" + payload.ClientRequestID
	if identity == "|" {
		// Two legitimate background/login audit events may have identical fields.
		// Only request-correlated producers receive stable duplicate suppression.
		identity = payload.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	canonical := fmt.Sprintf(
		"%d|%d|%s|%s|%s|%s|%s|%s|%s|%s",
		payload.OrgID, payload.UserID, payload.Action, payload.Resource,
		payload.ResourceID, identity, payload.ClientRequestID,
		payload.Source, string(payload.Detail), string(payload.AfterState),
	)
	digest := sha256.Sum256([]byte(canonical))
	event, err := outbox.NewEvent(
		outbox.EventAudit,
		"audit:"+hex.EncodeToString(digest[:]),
		outbox.PriorityAudit,
		payload.OrgID,
		payload.RequestID,
		payload,
		payload.OccurredAt,
	)
	if err != nil {
		return err
	}
	_, _, err = service.Enqueue(ctx, event)
	return err
}

func (h *Handler) ProcessAuditOutbox(ctx context.Context, event outbox.Event) error {
	if h == nil || h.db == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	var payload auditOutboxPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode audit outbox payload: %w", err)
	}
	_, err := h.db.Exec(ctx,
		`INSERT INTO audit_logs (
			org_id, user_id, action, resource, resource_id, detail, ip,
			user_agent, request_id, source, project_id, before_state,
			after_state, client_request_id, outbox_event_key, created_at
		 ) VALUES (
			$1, $2, $3, $4, $5, $6::jsonb, $7,
			$8, $9, $10, $11, $12::jsonb,
			$13::jsonb, $14, $15, $16
		 ) ON CONFLICT (outbox_event_key) DO NOTHING`,
		payload.OrgID, payload.UserID, payload.Action, payload.Resource,
		payload.ResourceID, string(payload.Detail), payload.IP,
		payload.UserAgent, payload.RequestID, payload.Source, payload.ProjectID,
		string(payload.BeforeState), string(payload.AfterState),
		nullableString(payload.ClientRequestID), event.Key, payload.OccurredAt,
	)
	return err
}

func positiveAuditID(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	copy := value
	return &copy
}
