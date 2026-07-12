package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/outbox"
)

type notificationDeliveryPayload struct {
	DeliveryID int64 `json:"delivery_id"`
}

func (s *EventService) emitWithOutbox(ctx context.Context, input EventInput, payloadJSON []byte) (*EventResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin notification outbox transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var eventID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO notification_events
		 (org_id, project_id, event_type, severity, title, message, resource, resource_id, payload, dedupe_key, delivery_state, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', NOW(), NOW())
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
		input.OrgID, nullableProjectID(input.ProjectID), input.EventType, input.Severity,
		input.Title, input.Message, input.Resource, input.ResourceID, payloadJSON,
		input.DedupeKey,
	).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		if input.DedupeKey == "" {
			return nil, err
		}
		var deliveryState string
		if err := tx.QueryRow(ctx,
			`SELECT id, delivery_state FROM notification_events
			 WHERE org_id = $1 AND dedupe_key = $2`, input.OrgID, input.DedupeKey,
		).Scan(&eventID, &deliveryState); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &EventResult{EventID: eventID, Created: false, DeliveryState: deliveryState}, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx,
		`SELECT id
		 FROM notification_webhooks
		 WHERE org_id = $1
		   AND enabled = TRUE
		   AND scope_verified = TRUE
		   AND (project_id IS NULL OR project_id = $2)
		   AND (jsonb_array_length(event_types) = 0 OR event_types ? $3)
		 ORDER BY project_id NULLS FIRST, id ASC`,
		input.OrgID, nullableProjectID(input.ProjectID), input.EventType,
	)
	if err != nil {
		return nil, err
	}
	webhookIDs := make([]int64, 0)
	for rows.Next() {
		var webhookID int64
		if err := rows.Scan(&webhookID); err != nil {
			rows.Close()
			return nil, err
		}
		webhookIDs = append(webhookIDs, webhookID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(webhookIDs) == 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE notification_events
			 SET delivery_state = 'not_configured', updated_at = NOW()
			 WHERE id = $1`, eventID,
		); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &EventResult{EventID: eventID, Created: true, DeliveryState: DeliveryStateNotConfigured}, nil
	}

	for _, webhookID := range webhookIDs {
		eventKey := fmt.Sprintf("notification-delivery:%d:%d", eventID, webhookID)
		var deliveryID int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO notification_deliveries (
				event_id, webhook_id, status, outbox_event_key, created_at, updated_at
			 ) VALUES ($1, $2, 'pending', $3, NOW(), NOW())
			 ON CONFLICT (outbox_event_key) DO UPDATE SET outbox_event_key = EXCLUDED.outbox_event_key
			 RETURNING id`, eventID, webhookID, eventKey,
		).Scan(&deliveryID); err != nil {
			return nil, err
		}
		event, err := outbox.NewEvent(
			outbox.EventNotificationDelivery,
			eventKey,
			outbox.PriorityNotification,
			input.OrgID,
			"",
			notificationDeliveryPayload{DeliveryID: deliveryID},
			s.now(),
		)
		if err != nil {
			return nil, err
		}
		if _, _, err := outbox.EnqueueTx(ctx, tx, event); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE notification_events SET delivery_state = 'pending', updated_at = NOW() WHERE id = $1`, eventID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &EventResult{EventID: eventID, Created: true, DeliveryState: DeliveryStatePending}, nil
}

func (s *EventService) ProcessNotificationDelivery(ctx context.Context, event outbox.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("notification service is unavailable")
	}
	var payload notificationDeliveryPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode notification delivery event: %w", err)
	}
	if payload.DeliveryID <= 0 {
		return fmt.Errorf("notification delivery id is required")
	}

	var status, url string
	var delivery dueWebhookDelivery
	var enabled, scopeVerified, quarantined bool
	err := s.db.QueryRow(ctx,
		`SELECT d.status, d.event_id, COALESCE(d.webhook_id, 0), w.url,
		        w.enabled, w.scope_verified, d.retry_scope_quarantined,
		        e.org_id, COALESCE(e.project_id, 0), e.event_type, e.severity,
		        e.title, e.message, e.resource, e.resource_id, e.payload
		 FROM notification_deliveries d
		 JOIN notification_events e ON e.id = d.event_id
		 JOIN notification_webhooks w ON w.id = d.webhook_id AND w.org_id = e.org_id
		 WHERE d.id = $1`, payload.DeliveryID,
	).Scan(
		&status, &delivery.EventID, &delivery.WebhookID, &url,
		&enabled, &scopeVerified, &quarantined,
		&delivery.Input.OrgID, &delivery.Input.ProjectID, &delivery.Input.EventType,
		&delivery.Input.Severity, &delivery.Input.Title, &delivery.Input.Message,
		&delivery.Input.Resource, &delivery.Input.ResourceID, &delivery.Payload,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "delivered" {
		return nil
	}
	if !enabled || !scopeVerified || quarantined {
		return fmt.Errorf("notification webhook is not eligible for delivery")
	}
	delivery.DeliveryID = payload.DeliveryID
	delivery.URL = url
	if !s.retryOneWebhook(ctx, delivery) {
		return fmt.Errorf("notification delivery %d failed", payload.DeliveryID)
	}
	return nil
}

func (s *EventService) ProcessBudgetEvaluation(ctx context.Context, event outbox.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("notification service is unavailable")
	}
	var payload budgetEvaluationPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode budget evaluation event: %w", err)
	}
	if payload.OrgID <= 0 || payload.ProjectID <= 0 || payload.PeriodStart.IsZero() {
		return fmt.Errorf("budget evaluation identity is invalid")
	}

	var projectName, budgetPeriod string
	var budgetCents sql.NullInt64
	var spentCents int64
	err := s.db.QueryRow(ctx,
		`SELECT p.name, p.budget_cents, p.budget_period, COALESCE(u.spent_cents, 0)
		 FROM projects p
		 LEFT JOIN project_budget_usage u
		   ON u.org_id = p.org_id
		  AND u.project_id = p.id
		  AND u.budget_period = $3
		  AND u.period_start = $4
		 WHERE p.id = $1 AND p.org_id = $2 AND p.status = 'active'`,
		payload.ProjectID, payload.OrgID, payload.BudgetPeriod, payload.PeriodStart,
	).Scan(&projectName, &budgetCents, &budgetPeriod, &spentCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !budgetCents.Valid || budgetCents.Int64 <= 0 || budgetPeriod != payload.BudgetPeriod || spentCents < budgetCents.Int64 {
		return nil
	}

	usagePercent := int(spentCents * 100 / budgetCents.Int64)
	dedupeKey := fmt.Sprintf("project_budget.threshold:%d:%s:100", payload.ProjectID, payload.PeriodStart.Format("20060102T150405Z"))
	_, err = s.Emit(ctx, EventInput{
		OrgID:      payload.OrgID,
		ProjectID:  payload.ProjectID,
		EventType:  EventProjectBudgetThreshold,
		Severity:   NotificationSeverityCritical,
		Title:      "Project budget threshold reached",
		Message:    fmt.Sprintf("Project %s reached %d%% of its %s budget", projectName, usagePercent, budgetPeriod),
		Resource:   "project",
		ResourceID: fmt.Sprintf("%d", payload.ProjectID),
		DedupeKey:  dedupeKey,
		Payload: map[string]interface{}{
			"org_id":            payload.OrgID,
			"project_id":        payload.ProjectID,
			"project_name":      projectName,
			"budget_cents":      budgetCents.Int64,
			"spent_cents":       spentCents,
			"budget_period":     budgetPeriod,
			"period_start":      payload.PeriodStart.Format(time.RFC3339),
			"threshold_percent": 100,
			"usage_percent":     usagePercent,
		},
	})
	return err
}
