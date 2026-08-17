//go:build integration

package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F20LeasedReplayPriorityAndDuplicateSafety(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	options := DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 4
	options.MaxAttempts = 3

	first, err := New(pool, options)
	require.NoError(t, err)
	base := time.Now().UTC().Add(-time.Second)
	events := []Event{
		mustEvent(t, EventBillingSettled, "billing:settled:1:a", PriorityBilling, base.Add(3*time.Millisecond)),
		mustEvent(t, EventAudit, "audit:a", PriorityAudit, base.Add(2*time.Millisecond)),
		mustEvent(t, EventCallLog, "call-log:1:a", PriorityCallLog, base.Add(time.Millisecond)),
		mustEvent(t, EventTrace, "trace:a", PriorityTraceInfo, base.Add(5*time.Millisecond)),
		mustEvent(t, EventTrace, "trace:b", PriorityTraceInfo, base.Add(4*time.Millisecond)),
	}
	var duplicateID int64
	for index, event := range events {
		id, created, enqueueErr := first.Enqueue(ctx, event)
		require.NoError(t, enqueueErr)
		require.True(t, created)
		if index == 0 {
			duplicateID = id
		}
	}
	id, created, err := first.Enqueue(ctx, events[0])
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, duplicateID, id)

	// Claim one event and then simulate a process death before its handler runs.
	claimed, err := first.claim(ctx, "any", 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	_, err = pool.Exec(ctx,
		`UPDATE internal_outbox_events SET claimed_until = NOW() - INTERVAL '1 second'
		 WHERE id = $1`, claimed[0].ID)
	require.NoError(t, err)

	second, err := New(pool, options)
	require.NoError(t, err)
	var mu sync.Mutex
	processed := make(map[string]int)
	order := make([]Event, 0, len(events))
	handler := func(_ context.Context, event Event) error {
		mu.Lock()
		defer mu.Unlock()
		processed[event.Key]++
		order = append(order, event)
		return nil
	}
	for _, eventType := range []string{EventBillingSettled, EventAudit, EventCallLog, EventTrace} {
		require.NoError(t, second.RegisterHandler(eventType, handler))
	}

	count, err := second.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, count, "one bounded batch must reserve capacity for low-priority trace work")
	require.GreaterOrEqual(t, order[0].Priority, PriorityCallLog)
	foundLow := false
	for _, event := range order[:count] {
		if event.Priority < PriorityCallLog {
			foundLow = true
		}
	}
	require.True(t, foundLow, "strict priority must not starve the trace-lag SLO")

	count, err = second.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	for _, event := range events {
		require.Equal(t, 1, processed[event.Key], "event %s materialized more than once", event.Key)
	}
	var remaining int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&remaining))
	require.Zero(t, remaining)
}

func TestP6F20PermanentFailureBecomesBoundedDeadLetter(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	options := DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 1
	options.MaxAttempts = 2
	service, err := New(pool, options)
	require.NoError(t, err)
	require.NoError(t, service.RegisterHandler(EventTrace, func(context.Context, Event) error {
		return context.DeadlineExceeded
	}))
	event := mustEvent(t, EventTrace, "trace:dead-letter", PriorityTraceInfo, time.Now().UTC())
	_, _, err = service.Enqueue(ctx, event)
	require.NoError(t, err)

	_, err = service.DrainOnce(ctx)
	require.Error(t, err)
	_, err = pool.Exec(ctx,
		`UPDATE internal_outbox_events SET available_at = NOW() WHERE event_key = $1`, event.Key)
	require.NoError(t, err)
	_, err = service.DrainOnce(ctx)
	require.Error(t, err)

	var attempts int
	var deadLettered bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT attempts, dead_lettered_at IS NOT NULL
		 FROM internal_outbox_events WHERE event_key = $1`, event.Key,
	).Scan(&attempts, &deadLettered))
	require.Equal(t, 2, attempts)
	require.True(t, deadLettered)
}

func TestP6F20OptionsAreBounded(t *testing.T) {
	options := DefaultOptions()
	require.NoError(t, options.Validate())
	options.WorkerCount = 17
	require.Error(t, options.Validate())
	options = DefaultOptions()
	options.BatchSize = 101
	require.Error(t, options.Validate())
}

func mustEvent(t *testing.T, eventType, key string, priority int, occurredAt time.Time) Event {
	t.Helper()
	event, err := NewEvent(eventType, key, priority, 1, "request-a", map[string]interface{}{
		"key": key,
	}, occurredAt)
	require.NoError(t, err)
	return event
}
