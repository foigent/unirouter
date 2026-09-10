package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	Version      = "internal-outbox.v1"
	MoneyVersion = "micro-cny.v1"

	EventBillingUsagePrepared = "billing.usage.prepared.v2"
	EventBillingSettled       = "billing.settled.v2"
	EventBillingCancelled     = "billing.cancelled.v2"
	EventCallLog              = "evidence.call_log.v2"
	EventTrace                = "evidence.trace.v1"
	EventAudit                = "evidence.audit.v1"
	EventBudgetEvaluate       = "notification.budget.evaluate.v1"
	EventNotificationDelivery = "notification.delivery.v1"
	EventRuntimeTerminal      = "runtime.terminal.v2"
	EventSourceDispatch       = "evidence.source_dispatch.v1"

	EventBillingUsagePreparedV1 = "billing.usage.prepared.v1"
	EventBillingSettledV1       = "billing.settled.v1"
	EventBillingCancelledV1     = "billing.cancelled.v1"
	EventCallLogV1              = "evidence.call_log.v1"
	EventRuntimeTerminalV1      = "runtime.terminal.v1"

	PriorityTraceInfo    = 100
	PriorityTraceWarning = 200
	PriorityNotification = 250
	PriorityCallLog      = 300
	PriorityAudit        = 350
	PriorityBilling      = 400

	highPriorityBoundary = PriorityCallLog
)

var (
	eventsEnqueued = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_outbox_enqueued_total",
		Help: "Durable internal outbox events accepted by type.",
	}, []string{"event_type"})
	eventsProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_outbox_processed_total",
		Help: "Durable internal outbox events successfully materialized by type.",
	}, []string{"event_type"})
	eventsRetried = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_outbox_retried_total",
		Help: "Durable internal outbox handler failures scheduled for retry by type.",
	}, []string{"event_type"})
	eventsDeadLettered = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unirouter_outbox_dead_lettered_total",
		Help: "Durable internal outbox events moved to a dead letter state by type.",
	}, []string{"event_type"})
	eventLag = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "unirouter_outbox_materialization_lag_seconds",
		Help:    "Time from event occurrence to successful materialization.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 300},
	}, []string{"event_type"})
	oldestTraceLag = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "unirouter_outbox_oldest_trace_lag_seconds",
		Help: "Age of the oldest pending trace event, or zero when none are pending.",
	})
	traceLagSLOBreaches = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "unirouter_outbox_trace_lag_slo_breaches_total",
		Help: "Trace events materialized after the configured repository SLO.",
	})
)

func init() {
	prometheus.MustRegister(
		eventsEnqueued,
		eventsProcessed,
		eventsRetried,
		eventsDeadLettered,
		eventLag,
		oldestTraceLag,
		traceLagSLOBreaches,
	)
}

type Options struct {
	WorkerCount   int
	BatchSize     int
	PollInterval  time.Duration
	LeaseDuration time.Duration
	MaxAttempts   int
	TraceLagSLO   time.Duration
}

func DefaultOptions() Options {
	return Options{
		WorkerCount:   2,
		BatchSize:     25,
		PollInterval:  time.Second,
		LeaseDuration: 30 * time.Second,
		MaxAttempts:   12,
		TraceLagSLO:   30 * time.Second,
	}
}

func (o Options) Validate() error {
	if o.WorkerCount < 1 || o.WorkerCount > 16 {
		return fmt.Errorf("outbox worker count must be between 1 and 16")
	}
	if o.BatchSize < 1 || o.BatchSize > 100 {
		return fmt.Errorf("outbox batch size must be between 1 and 100")
	}
	if o.PollInterval < 25*time.Millisecond || o.PollInterval > time.Minute {
		return fmt.Errorf("outbox poll interval must be between 25ms and 1m")
	}
	if o.LeaseDuration < time.Second || o.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("outbox lease duration must be between 1s and 10m")
	}
	if o.MaxAttempts < 1 || o.MaxAttempts > 100 {
		return fmt.Errorf("outbox max attempts must be between 1 and 100")
	}
	if o.TraceLagSLO < time.Second || o.TraceLagSLO > time.Hour {
		return fmt.Errorf("outbox trace lag SLO must be between 1s and 1h")
	}
	return nil
}

type Event struct {
	ID         int64
	Key        string
	Type       string
	Priority   int
	OrgID      int64
	RequestID  string
	Payload    json.RawMessage
	OccurredAt time.Time
	Attempts   int
	ClaimedBy  string
}

func NewEvent(eventType, key string, priority int, orgID int64, requestID string, payload any, occurredAt time.Time) (Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal outbox payload: %w", err)
	}
	event := Event{
		Key:        strings.TrimSpace(key),
		Type:       strings.TrimSpace(eventType),
		Priority:   priority,
		OrgID:      orgID,
		RequestID:  strings.TrimSpace(requestID),
		Payload:    payloadJSON,
		OccurredAt: occurredAt.UTC(),
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (e Event) Validate() error {
	if e.Type == "" || len(e.Type) > 80 {
		return fmt.Errorf("outbox event type is required and must not exceed 80 characters")
	}
	if e.Key == "" || len(e.Key) > 200 {
		return fmt.Errorf("outbox event key is required and must not exceed 200 characters")
	}
	if e.Priority < 0 || e.Priority > 1000 {
		return fmt.Errorf("outbox priority must be between 0 and 1000")
	}
	if e.OrgID < 0 || len(e.RequestID) > 64 {
		return fmt.Errorf("outbox identity is invalid")
	}
	if len(e.Payload) == 0 || len(e.Payload) > 256*1024 || !json.Valid(e.Payload) {
		return fmt.Errorf("outbox payload must be valid JSON no larger than 256 KiB")
	}
	return nil
}

type Handler func(context.Context, Event) error

type Service struct {
	db      db.DBQuerier
	options Options

	mu       sync.RWMutex
	handlers map[string]Handler
	workerID string
}

func New(database db.DBQuerier, options Options) (*Service, error) {
	if database == nil {
		return nil, fmt.Errorf("outbox database is required")
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	return &Service{
		db:       database,
		options:  options,
		handlers: make(map[string]Handler),
		workerID: fmt.Sprintf("%s:%d:%d", hostname, os.Getpid(), time.Now().UnixNano()),
	}, nil
}

func (s *Service) RegisterHandler(eventType string, handler Handler) error {
	if s == nil || handler == nil {
		return fmt.Errorf("outbox service and handler are required")
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return fmt.Errorf("outbox handler event type is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.handlers[eventType]; exists {
		return fmt.Errorf("outbox handler already registered for %s", eventType)
	}
	s.handlers[eventType] = handler
	return nil
}

func (s *Service) Enqueue(ctx context.Context, event Event) (int64, bool, error) {
	if s == nil || s.db == nil {
		return 0, false, fmt.Errorf("outbox service is unavailable")
	}
	return enqueue(ctx, s.db, event)
}

type queryRower interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func EnqueueTx(ctx context.Context, tx pgx.Tx, event Event) (int64, bool, error) {
	if tx == nil {
		return 0, false, fmt.Errorf("outbox transaction is required")
	}
	return enqueue(ctx, tx, event)
}

func enqueue(ctx context.Context, database queryRower, event Event) (int64, bool, error) {
	if err := event.Validate(); err != nil {
		return 0, false, err
	}
	var id int64
	err := database.QueryRow(ctx,
		`INSERT INTO internal_outbox_events (
			event_key, event_type, priority, org_id, request_id, payload,
			occurred_at, available_at, created_at, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), NOW())
		 ON CONFLICT (event_key) DO NOTHING
		 RETURNING id`,
		event.Key, event.Type, event.Priority, event.OrgID, event.RequestID,
		string(event.Payload), event.OccurredAt,
	).Scan(&id)
	if err == nil {
		eventsEnqueued.WithLabelValues(event.Type).Inc()
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("enqueue outbox event: %w", err)
	}
	if err := database.QueryRow(ctx,
		`SELECT id FROM internal_outbox_events WHERE event_key = $1`, event.Key,
	).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("read existing outbox event: %w", err)
	}
	return id, false, nil
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	var wg sync.WaitGroup
	for worker := 0; worker < s.options.WorkerCount; worker++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			s.runWorker(ctx, index)
		}(worker)
	}
	wg.Wait()
}

func (s *Service) runWorker(ctx context.Context, index int) {
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	lastLagRefresh := time.Time{}
	for {
		claimedBy := fmt.Sprintf("%s:worker-%d", s.workerID, index)
		if _, err := s.drainOnce(ctx, claimedBy); err != nil && ctx.Err() == nil {
			log.Printf("[outbox] worker=%d drain error: %v", index, err)
		}
		if index == 0 && (lastLagRefresh.IsZero() || time.Since(lastLagRefresh) >= 5*time.Second) {
			s.refreshTraceLag(ctx)
			lastLagRefresh = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) DrainOnce(ctx context.Context) (int, error) {
	return s.drainOnce(ctx, s.workerID+":manual")
}

func (s *Service) drainOnce(ctx context.Context, claimedBy string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("outbox service is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	batchSize := s.options.BatchSize
	highLimit := batchSize * 3 / 4
	if highLimit == 0 && batchSize > 1 {
		highLimit = 1
	}
	lowLimit := batchSize - highLimit

	events := make([]Event, 0, batchSize)
	if highLimit > 0 {
		claimed, err := s.claimForWorker(ctx, "high", highLimit, claimedBy)
		if err != nil {
			return 0, err
		}
		events = append(events, claimed...)
	}
	if lowLimit > 0 {
		claimed, err := s.claimForWorker(ctx, "low", lowLimit, claimedBy)
		if err != nil {
			return 0, err
		}
		events = append(events, claimed...)
	}
	if len(events) > 0 && len(events) < batchSize {
		claimed, err := s.claimForWorker(ctx, "any", batchSize-len(events), claimedBy)
		if err != nil {
			return 0, err
		}
		events = append(events, claimed...)
	}

	var processingErrors []error
	for _, event := range events {
		if err := s.process(ctx, event); err != nil {
			processingErrors = append(processingErrors, err)
		}
	}
	return len(events), errors.Join(processingErrors...)
}

func (s *Service) claim(ctx context.Context, lane string, limit int) ([]Event, error) {
	return s.claimForWorker(ctx, lane, limit, s.workerID+":test")
}

func (s *Service) claimForWorker(ctx context.Context, lane string, limit int, claimedBy string) ([]Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx,
		`WITH candidates AS (
		    SELECT id
		    FROM internal_outbox_events
		    WHERE dead_lettered_at IS NULL
		      AND available_at <= NOW()
		      AND (claimed_until IS NULL OR claimed_until <= NOW())
		      AND (
		        $4 = 'any'
		        OR ($4 = 'high' AND priority >= $5)
		        OR ($4 = 'low' AND priority < $5)
		      )
		    ORDER BY priority DESC, occurred_at ASC, id ASC
		    FOR UPDATE SKIP LOCKED
		    LIMIT $1
		)
		UPDATE internal_outbox_events e
		SET claimed_by = $2,
		    claimed_until = NOW() + ($3 * INTERVAL '1 millisecond'),
		    attempts = e.attempts + 1,
		    updated_at = NOW()
		FROM candidates c
		WHERE e.id = c.id
		RETURNING e.id, e.event_key, e.event_type, e.priority, e.org_id,
		          e.request_id, e.payload, e.occurred_at, e.attempts, e.claimed_by`,
		limit,
		claimedBy,
		int64(s.options.LeaseDuration/time.Millisecond),
		lane,
		highPriorityBoundary,
	)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.ID,
			&event.Key,
			&event.Type,
			&event.Priority,
			&event.OrgID,
			&event.RequestID,
			&event.Payload,
			&event.OccurredAt,
			&event.Attempts,
			&event.ClaimedBy,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}
	return events, nil
}

func (s *Service) process(ctx context.Context, event Event) error {
	lease, err := s.db.Exec(ctx,
		`UPDATE internal_outbox_events
		 SET claimed_until = NOW() + ($3 * INTERVAL '1 millisecond'), updated_at = NOW()
		 WHERE id = $1 AND claimed_by = $2`,
		event.ID, event.ClaimedBy, int64(s.options.LeaseDuration/time.Millisecond),
	)
	if err != nil {
		return fmt.Errorf("renew outbox lease for event %d: %w", event.ID, err)
	}
	if lease.RowsAffected() != 1 {
		// Another bounded worker reclaimed the expired lease before this event
		// reached the front of the local batch. That worker now owns replay.
		return nil
	}
	s.mu.RLock()
	handler := s.handlers[event.Type]
	s.mu.RUnlock()
	if handler == nil {
		return s.releaseFailure(ctx, event, fmt.Errorf("no handler registered for %s", event.Type))
	}
	if err := handler(ctx, event); err != nil {
		return s.releaseFailure(ctx, event, err)
	}
	result, err := s.db.Exec(ctx,
		`DELETE FROM internal_outbox_events
		 WHERE id = $1 AND claimed_by = $2`, event.ID, event.ClaimedBy)
	if err != nil {
		return fmt.Errorf("delete processed outbox event %d: %w", event.ID, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("processed outbox lease lost for event %d", event.ID)
	}
	lag := time.Since(event.OccurredAt)
	if lag < 0 {
		lag = 0
	}
	eventsProcessed.WithLabelValues(event.Type).Inc()
	eventLag.WithLabelValues(event.Type).Observe(lag.Seconds())
	if event.Type == EventTrace && lag > s.options.TraceLagSLO {
		traceLagSLOBreaches.Inc()
	}
	return nil
}

func (s *Service) releaseFailure(ctx context.Context, event Event, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 512 {
		message = message[:512]
	}
	// Financial/runtime terminal hooks retry until success or their own durable
	// expiry contract resolves them. Ordinary evidence uses a bounded dead
	// letter policy so one poison payload cannot monopolize workers.
	deadLetter := event.Attempts >= s.options.MaxAttempts && event.Priority < PriorityBilling
	delay := retryBackoff(event.Attempts)
	_, err := s.db.Exec(ctx,
		`UPDATE internal_outbox_events
		 SET claimed_by = '',
		     claimed_until = NULL,
		     available_at = CASE WHEN $3 THEN available_at ELSE NOW() + ($4 * INTERVAL '1 millisecond') END,
		     last_error = $5,
		     dead_lettered_at = CASE WHEN $3 THEN NOW() ELSE NULL END,
		     updated_at = NOW()
		 WHERE id = $1 AND claimed_by = $2`,
		event.ID, event.ClaimedBy, deadLetter, int64(delay/time.Millisecond), message,
	)
	if err != nil {
		return fmt.Errorf("release failed outbox event %d after %v: %w", event.ID, cause, err)
	}
	if deadLetter {
		eventsDeadLettered.WithLabelValues(event.Type).Inc()
	} else {
		eventsRetried.WithLabelValues(event.Type).Inc()
	}
	return fmt.Errorf("process outbox event %d type=%s attempt=%d: %w", event.ID, event.Type, event.Attempts, cause)
}

func retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	delay := time.Duration(1<<attempt) * 100 * time.Millisecond
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func (s *Service) refreshTraceLag(ctx context.Context) {
	var seconds float64
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(occurred_at))), 0)::float8
		 FROM internal_outbox_events
		 WHERE event_type = $1 AND dead_lettered_at IS NULL`, EventTrace,
	).Scan(&seconds)
	if err == nil {
		if seconds < 0 {
			seconds = 0
		}
		oldestTraceLag.Set(seconds)
	}
}
