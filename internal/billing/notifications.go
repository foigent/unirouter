package billing

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

const (
	EventProjectBudgetThreshold          = "project_budget.threshold"
	EventTokenMeteringEstimatedRatioHigh = "token_metering.estimated_ratio.high"
	EventAPIKeyDisabled                  = "api_key.disabled"
	EventModelDeprecated                 = "model.deprecated"
	EventModelRetired                    = "model.retired"
	EventProviderChannelFailure          = "provider.channel.failure"
	EventLicenseWarning                  = "license.warning"

	NotificationSeverityInfo     = "info"
	NotificationSeverityWarning  = "warning"
	NotificationSeverityCritical = "critical"

	DeliveryStateNotConfigured = "not_configured"
	DeliveryStatePending       = "pending"
	DeliveryStateDelivered     = "delivered"
	DeliveryStateFailed        = "failed"
)

// EventInput describes a persisted notification event.
type EventInput struct {
	OrgID      int64
	ProjectID  int64
	EventType  string
	Severity   string
	Title      string
	Message    string
	Resource   string
	ResourceID string
	Payload    map[string]interface{}
	DedupeKey  string
}

// EventResult summarizes event persistence and synchronous delivery state.
type EventResult struct {
	EventID       int64
	Created       bool
	DeliveryState string
}

// EventService persists notification events and delivers matching webhook routes.
type EventService struct {
	db        db.DBQuerier
	client    *http.Client
	now       func() time.Time
	retryWait time.Duration
	outbox    *outbox.Service
}

type dueWebhookDelivery struct {
	DeliveryID int64
	EventID    int64
	WebhookID  int64
	URL        string
	Input      EventInput
	Payload    []byte
}

// NewEventService creates an event service backed by the shared DB interface.
func NewEventService(db db.DBQuerier) *EventService {
	return &EventService{
		db:        db,
		client:    &http.Client{Timeout: 10 * time.Second},
		now:       func() time.Time { return time.Now().UTC() },
		retryWait: 5 * time.Minute,
	}
}

// WithHTTPClient replaces the delivery client, primarily for tests.
func (s *EventService) WithHTTPClient(client *http.Client) *EventService {
	if client != nil {
		s.client = client
	}
	return s
}

func (s *EventService) SetOutbox(service *outbox.Service) {
	if s != nil {
		s.outbox = service
	}
}

// Emit records an event and immediately attempts matching webhook delivery.
func (s *EventService) Emit(ctx context.Context, input EventInput) (*EventResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("event service is not configured")
	}
	input.EventType = trimNonEmpty(input.EventType)
	input.Title = trimNonEmpty(input.Title)
	if input.OrgID <= 0 || input.EventType == "" || input.Title == "" {
		return nil, errors.New("org_id, event_type, and title are required")
	}
	if input.Severity == "" {
		input.Severity = NotificationSeverityInfo
	}
	if !validNotificationSeverity(input.Severity) {
		return nil, fmt.Errorf("invalid notification severity: %s", input.Severity)
	}

	payloadJSON, err := marshalEventPayload(input.Payload)
	if err != nil {
		return nil, err
	}
	if s.outbox != nil {
		return s.emitWithOutbox(ctx, input, payloadJSON)
	}

	var projectArg interface{}
	if input.ProjectID > 0 {
		projectArg = input.ProjectID
	}

	var eventID int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO notification_events
		 (org_id, project_id, event_type, severity, title, message, resource, resource_id, payload, dedupe_key, delivery_state, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending', NOW(), NOW())
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
		input.OrgID, projectArg, input.EventType, input.Severity, input.Title, input.Message,
		input.Resource, input.ResourceID, payloadJSON, input.DedupeKey,
	).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		if input.DedupeKey == "" {
			return nil, err
		}
		if lookupErr := s.db.QueryRow(ctx,
			`SELECT id FROM notification_events WHERE org_id = $1 AND dedupe_key = $2`,
			input.OrgID, input.DedupeKey,
		).Scan(&eventID); lookupErr != nil {
			return nil, lookupErr
		}
		return &EventResult{EventID: eventID, Created: false, DeliveryState: DeliveryStateNotConfigured}, nil
	}
	if err != nil {
		return nil, err
	}

	deliveryState, err := s.deliverWebhooks(ctx, eventID, input, payloadJSON)
	if err != nil {
		return nil, err
	}
	return &EventResult{EventID: eventID, Created: true, DeliveryState: deliveryState}, nil
}

// RetryDueDeliveries retries failed webhook deliveries whose next_retry_at has passed.
func (s *EventService) RetryDueDeliveries(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("event service is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.db.Query(ctx,
		`SELECT d.id, d.event_id, COALESCE(d.webhook_id, 0), w.url,
		        e.org_id, COALESCE(e.project_id, 0), e.event_type, e.severity,
		        e.title, e.message, e.resource, e.resource_id, e.payload
		 FROM notification_deliveries d
		 JOIN notification_events e ON e.id = d.event_id
		 JOIN notification_webhooks w ON w.id = d.webhook_id AND w.org_id = e.org_id
		 WHERE d.status = 'failed'
		   AND d.next_retry_at IS NOT NULL
		   AND d.next_retry_at <= NOW()
		   AND d.retry_scope_quarantined = FALSE
		   AND w.enabled = TRUE
		   AND w.scope_verified = TRUE
		 ORDER BY d.next_retry_at ASC, d.id ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	deliveries := make([]dueWebhookDelivery, 0)
	for rows.Next() {
		var delivery dueWebhookDelivery
		if err := rows.Scan(
			&delivery.DeliveryID,
			&delivery.EventID,
			&delivery.WebhookID,
			&delivery.URL,
			&delivery.Input.OrgID,
			&delivery.Input.ProjectID,
			&delivery.Input.EventType,
			&delivery.Input.Severity,
			&delivery.Input.Title,
			&delivery.Input.Message,
			&delivery.Input.Resource,
			&delivery.Input.ResourceID,
			&delivery.Payload,
		); err != nil {
			return 0, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	retried := 0
	for _, delivery := range deliveries {
		if s.retryOneWebhook(ctx, delivery) {
			retried++
		}
	}
	return retried, nil
}

// CheckProjectBudget emits a deduplicated budget event when project spending reaches the configured budget.
func (s *EventService) CheckProjectBudget(ctx context.Context, orgID, projectID int64) (*EventResult, error) {
	if orgID <= 0 || projectID <= 0 {
		return nil, nil
	}

	var projectName, budgetPeriod string
	var budgetCents sql.NullInt64
	err := s.db.QueryRow(ctx,
		`SELECT name, budget_cents, budget_period
		 FROM projects
		 WHERE id = $1 AND org_id = $2 AND status = 'active'`,
		projectID, orgID,
	).Scan(&projectName, &budgetCents, &budgetPeriod)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !budgetCents.Valid || budgetCents.Int64 <= 0 || budgetPeriod == "none" {
		return nil, nil
	}

	periodStart := budgetPeriodStart(s.now(), budgetPeriod)
	var spentMicros int64
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_micros), 0)
		 FROM call_logs
		 WHERE org_id = $1 AND project_id = $2 AND created_at >= $3`,
		orgID, projectID, periodStart,
	).Scan(&spentMicros); err != nil {
		return nil, err
	}
	budgetMicros, err := money.CentsToMicros(budgetCents.Int64)
	if err != nil {
		return nil, fmt.Errorf("project budget is out of range: %w", err)
	}
	if spentMicros < budgetMicros {
		return nil, nil
	}

	usagePercent := budgetUsagePercent(spentMicros, budgetMicros)
	dedupeKey := fmt.Sprintf("project_budget.threshold:%d:%s:100", projectID, periodStart.Format("20060102T150405Z"))
	return s.Emit(ctx, EventInput{
		OrgID:      orgID,
		ProjectID:  projectID,
		EventType:  EventProjectBudgetThreshold,
		Severity:   NotificationSeverityCritical,
		Title:      "Project budget threshold reached",
		Message:    fmt.Sprintf("Project %s reached %d%% of its %s budget", projectName, usagePercent, budgetPeriod),
		Resource:   "project",
		ResourceID: fmt.Sprintf("%d", projectID),
		DedupeKey:  dedupeKey,
		Payload: map[string]interface{}{
			"org_id":        orgID,
			"project_id":    projectID,
			"project_name":  projectName,
			"budget_cents":  budgetCents.Int64,
			"budget_micros": strconv.FormatInt(budgetMicros, 10),
			"spent_micros":  strconv.FormatInt(spentMicros, 10),
			"currency":      "CNY", "amount_scale": money.MicrosPerCNY,
			"budget_period":     budgetPeriod,
			"period_start":      periodStart.Format(time.RFC3339),
			"threshold_percent": 100,
			"usage_percent":     usagePercent,
		},
	})
}

func budgetUsagePercent(spentMicros, budgetMicros int64) int {
	if spentMicros <= 0 || budgetMicros <= 0 {
		return 0
	}
	percent := new(big.Int).Mul(big.NewInt(spentMicros), big.NewInt(100))
	percent.Quo(percent, big.NewInt(budgetMicros))
	if !percent.IsInt64() || percent.Int64() > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(percent.Int64())
}

type webhookRoute struct {
	ID  int64
	URL string
}

func (s *EventService) deliverWebhooks(ctx context.Context, eventID int64, input EventInput, payloadJSON []byte) (string, error) {
	var projectArg interface{}
	if input.ProjectID > 0 {
		projectArg = input.ProjectID
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, url
		 FROM notification_webhooks
		 WHERE org_id = $1
		   AND enabled = TRUE
		   AND scope_verified = TRUE
		   AND (project_id IS NULL OR project_id = $2)
		   AND (jsonb_array_length(event_types) = 0 OR event_types ? $3)
		 ORDER BY project_id NULLS FIRST, id ASC`,
		input.OrgID, projectArg, input.EventType,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	routes := make([]webhookRoute, 0)
	for rows.Next() {
		var route webhookRoute
		if err := rows.Scan(&route.ID, &route.URL); err != nil {
			return "", err
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(routes) == 0 {
		if err := s.updateEventDeliveryState(ctx, eventID, DeliveryStateNotConfigured); err != nil {
			return "", err
		}
		return DeliveryStateNotConfigured, nil
	}

	state := DeliveryStateDelivered
	for _, route := range routes {
		delivered := s.deliverOneWebhook(ctx, eventID, route, input, payloadJSON)
		if !delivered {
			state = DeliveryStateFailed
		}
	}
	if err := s.updateEventDeliveryState(ctx, eventID, state); err != nil {
		return "", err
	}
	return state, nil
}

func (s *EventService) deliverOneWebhook(ctx context.Context, eventID int64, route webhookRoute, input EventInput, payloadJSON []byte) bool {
	var deliveryID int64
	if err := s.db.QueryRow(ctx,
		`INSERT INTO notification_deliveries (event_id, webhook_id, status, created_at, updated_at)
		 VALUES ($1, $2, 'pending', NOW(), NOW())
		 RETURNING id`,
		eventID, route.ID,
	).Scan(&deliveryID); err != nil {
		return false
	}

	body, err := json.Marshal(map[string]interface{}{
		"event_id":    eventID,
		"event_type":  input.EventType,
		"severity":    input.Severity,
		"title":       input.Title,
		"message":     input.Message,
		"org_id":      input.OrgID,
		"project_id":  nullableProjectID(input.ProjectID),
		"resource":    input.Resource,
		"resource_id": input.ResourceID,
		"payload":     json.RawMessage(payloadJSON),
		"created_at":  s.now().Format(time.RFC3339),
	})
	if err != nil {
		_ = s.markDeliveryFailed(ctx, deliveryID, nil, err.Error())
		return false
	}

	return s.sendWebhookAndUpdateDelivery(ctx, deliveryID, route.URL, body)
}

func (s *EventService) retryOneWebhook(ctx context.Context, delivery dueWebhookDelivery) bool {
	body, err := json.Marshal(map[string]interface{}{
		"event_id":    delivery.EventID,
		"event_type":  delivery.Input.EventType,
		"severity":    delivery.Input.Severity,
		"title":       delivery.Input.Title,
		"message":     delivery.Input.Message,
		"org_id":      delivery.Input.OrgID,
		"project_id":  nullableProjectID(delivery.Input.ProjectID),
		"resource":    delivery.Input.Resource,
		"resource_id": delivery.Input.ResourceID,
		"payload":     json.RawMessage(delivery.Payload),
		"created_at":  s.now().Format(time.RFC3339),
	})
	if err != nil {
		_ = s.markDeliveryFailed(ctx, delivery.DeliveryID, nil, err.Error())
		return false
	}
	delivered := s.sendWebhookAndUpdateDelivery(ctx, delivery.DeliveryID, delivery.URL, body)
	if err := s.refreshEventDeliveryStateFromDeliveries(ctx, delivery.EventID); err != nil {
		if delivered {
			_ = s.updateEventDeliveryState(ctx, delivery.EventID, DeliveryStateDelivered)
		} else {
			_ = s.updateEventDeliveryState(ctx, delivery.EventID, DeliveryStateFailed)
		}
	}
	return delivered
}

func (s *EventService) sendWebhookAndUpdateDelivery(ctx context.Context, deliveryID int64, webhookURL string, body []byte) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		_ = s.markDeliveryFailed(ctx, deliveryID, nil, err.Error())
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("unirouter-notification-delivery-%d", deliveryID))

	resp, err := s.client.Do(req)
	if err != nil {
		_ = s.markDeliveryFailed(ctx, deliveryID, nil, err.Error())
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	statusCode := resp.StatusCode
	if statusCode < 200 || statusCode >= 300 {
		_ = s.markDeliveryFailed(ctx, deliveryID, &statusCode, fmt.Sprintf("webhook returned status %d", statusCode))
		return false
	}

	_, _ = s.db.Exec(ctx,
		`UPDATE notification_deliveries
		 SET status = 'delivered', attempts = attempts + 1, response_status = $1,
		     error = '', next_retry_at = NULL, delivered_at = NOW(), updated_at = NOW()
		 WHERE id = $2`,
		statusCode, deliveryID,
	)
	return true
}

func (s *EventService) markDeliveryFailed(ctx context.Context, deliveryID int64, responseStatus *int, message string) error {
	var responseArg interface{}
	if responseStatus != nil {
		responseArg = *responseStatus
	}
	_, err := s.db.Exec(ctx,
		`UPDATE notification_deliveries
		 SET status = 'failed', attempts = attempts + 1, response_status = $1,
		     error = $2, next_retry_at = $3, updated_at = NOW()
		 WHERE id = $4`,
		responseArg, message, s.now().Add(s.retryWait), deliveryID,
	)
	return err
}

func (s *EventService) updateEventDeliveryState(ctx context.Context, eventID int64, state string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE notification_events
		 SET delivery_state = $1, last_delivery_at = CASE WHEN $2 THEN NOW() ELSE last_delivery_at END,
		     updated_at = NOW()
		 WHERE id = $3`,
		state, state == DeliveryStateDelivered || state == DeliveryStateFailed, eventID,
	)
	return err
}

func (s *EventService) refreshEventDeliveryStateFromDeliveries(ctx context.Context, eventID int64) error {
	var remaining, delivered int64
	if err := s.db.QueryRow(ctx,
		`SELECT
		    COUNT(*) FILTER (WHERE status <> 'delivered'),
		    COUNT(*) FILTER (WHERE status = 'delivered')
		 FROM notification_deliveries
		 WHERE event_id = $1`,
		eventID,
	).Scan(&remaining, &delivered); err != nil {
		return err
	}

	state := DeliveryStateFailed
	if delivered > 0 && remaining == 0 {
		state = DeliveryStateDelivered
	}
	return s.updateEventDeliveryState(ctx, eventID, state)
}

func budgetPeriodStart(now time.Time, period string) time.Time {
	utc := now.UTC()
	switch period {
	case "daily":
		return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	case "monthly":
		return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return utc
	}
}

func marshalEventPayload(payload map[string]interface{}) ([]byte, error) {
	if payload == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(payload)
}

func nullableProjectID(projectID int64) interface{} {
	if projectID <= 0 {
		return nil
	}
	return projectID
}

func trimNonEmpty(value string) string {
	return string(bytes.TrimSpace([]byte(value)))
}

func validNotificationSeverity(severity string) bool {
	return severity == NotificationSeverityInfo ||
		severity == NotificationSeverityWarning ||
		severity == NotificationSeverityCritical
}
