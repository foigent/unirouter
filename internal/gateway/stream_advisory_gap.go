package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uniroute/unirouter/internal/guardrail"
)

const (
	streamAdvisoryGapWindow      = 5 * time.Second
	streamAdvisoryGapMaxPolicies = 256
)

type streamAdvisoryGapWriterOptions struct {
	BufferDepth    int
	BatchSize      int
	FlushInterval  time.Duration
	PersistTimeout time.Duration
	RetryInterval  time.Duration
	Now            func() time.Time
}

func defaultStreamAdvisoryGapWriterOptions() streamAdvisoryGapWriterOptions {
	return streamAdvisoryGapWriterOptions{
		BufferDepth: 8192, BatchSize: 256, FlushInterval: 50 * time.Millisecond,
		PersistTimeout: 2 * time.Second, RetryInterval: 100 * time.Millisecond,
		Now: time.Now,
	}
}

func (options streamAdvisoryGapWriterOptions) valid() bool {
	return options.BufferDepth > 0 && options.BufferDepth <= 65536 &&
		options.BatchSize > 0 && options.BatchSize <= 256 &&
		options.FlushInterval > 0 && options.PersistTimeout > 0 &&
		options.RetryInterval > 0 && options.Now != nil
}

// streamAdvisoryGapEvent deliberately contains no frame text, request body, or
// detector findings. The bounded policy identities are the complete durable
// evidence payload.
type streamAdvisoryGapEvent struct {
	relay      *Relay
	orgID      int64
	requestID  string
	policies   []guardrail.PolicyIdentity
	reason     string
	occurredAt time.Time
	ack        chan error
}

type streamAdvisoryGapKey struct {
	orgID           int64
	requestID       string
	policyRef       string
	policyVersion   int64
	reason          string
	windowStartedAt time.Time
}

type streamAdvisoryGapAggregate struct {
	key             streamAdvisoryGapKey
	droppedCount    int64
	firstOccurredAt time.Time
	lastOccurredAt  time.Time
}

type streamAdvisoryGapBatch struct {
	id         string
	relay      *Relay
	aggregates []streamAdvisoryGapAggregate
	events     []streamAdvisoryGapEvent
}

type streamAdvisoryGapWriter struct {
	options streamAdvisoryGapWriterOptions
	input   chan streamAdvisoryGapEvent
	done    chan struct{}
	runCtx  context.Context
	cancel  context.CancelFunc

	mu             sync.Mutex
	closed         bool
	activeEnqueues sync.WaitGroup
	finishOnce     sync.Once
	closeInputOnce sync.Once

	pendingPolicies atomic.Int64
	failedPolicies  atomic.Int64
}

func newStreamAdvisoryGapWriter(options streamAdvisoryGapWriterOptions) *streamAdvisoryGapWriter {
	if !options.valid() {
		panic("invalid stream advisory gap writer options")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	writer := &streamAdvisoryGapWriter{
		options: options,
		input:   make(chan streamAdvisoryGapEvent, options.BufferDepth),
		done:    make(chan struct{}),
		runCtx:  runCtx,
		cancel:  cancel,
	}
	go writer.run()
	return writer
}

func (writer *streamAdvisoryGapWriter) Record(ctx context.Context, job streamAdvisoryJob, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ack, err := writer.Enqueue(ctx, job, reason)
	if err != nil {
		return err
	}
	// A successful return means the idempotent batch transaction committed, not
	// merely that volatile process memory accepted the event.
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-writer.runCtx.Done():
		return writer.runCtx.Err()
	}
}

// Enqueue applies bounded, cancel-aware backpressure and returns a commit ack.
// Shutdown uses this to queue all jobs before one writer flush instead of
// serializing one database batch per job.
func (writer *streamAdvisoryGapWriter) Enqueue(ctx context.Context, job streamAdvisoryJob, reason string) (<-chan error, error) {
	if writer == nil {
		return nil, fmt.Errorf("stream advisory gap writer is unavailable")
	}
	event, err := writer.gapEvent(job, reason)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	policyCount := int64(len(event.policies))
	event.ack = make(chan error, 1)

	// Register under the lifecycle lock, then release it before any blocking
	// operation. Close prevents later registrations and cancellation releases
	// both a full-buffer sender and a commit waiter.
	writer.mu.Lock()
	if writer.closed {
		writer.mu.Unlock()
		writer.failedPolicies.Add(policyCount)
		return nil, fmt.Errorf("stream advisory gap writer is closed")
	}
	writer.activeEnqueues.Add(1)
	writer.mu.Unlock()
	defer writer.activeEnqueues.Done()

	writer.pendingPolicies.Add(policyCount)
	select {
	case writer.input <- event:
	case <-ctx.Done():
		writer.pendingPolicies.Add(-policyCount)
		writer.failedPolicies.Add(policyCount)
		return nil, ctx.Err()
	case <-writer.runCtx.Done():
		writer.pendingPolicies.Add(-policyCount)
		writer.failedPolicies.Add(policyCount)
		return nil, writer.runCtx.Err()
	}
	return event.ack, nil
}

func (writer *streamAdvisoryGapWriter) gapEvent(job streamAdvisoryJob, reason string) (streamAdvisoryGapEvent, error) {
	if job.relay == nil || job.relay.db == nil || job.orgID <= 0 {
		return streamAdvisoryGapEvent{}, fmt.Errorf("stream advisory gap database identity is unavailable")
	}
	if reason != streamAdvisoryGapOverload && reason != streamAdvisoryGapTimeout && reason != streamAdvisoryGapShutdown {
		return streamAdvisoryGapEvent{}, fmt.Errorf("invalid stream advisory gap reason")
	}
	requestID := boundedGapIdentifier(job.requestID, 64)
	if requestID == "" || len(job.policies) == 0 || len(job.policies) > streamAdvisoryGapMaxPolicies {
		return streamAdvisoryGapEvent{}, fmt.Errorf("invalid stream advisory gap policy metadata")
	}
	policies := make([]guardrail.PolicyIdentity, 0, len(job.policies))
	seen := make(map[string]struct{}, len(job.policies))
	for _, policy := range job.policies {
		ref := boundedGapIdentifier(policy.Ref, 160)
		if ref == "" || policy.Version <= 0 {
			return streamAdvisoryGapEvent{}, fmt.Errorf("invalid stream advisory gap policy identity")
		}
		key := fmt.Sprintf("%s\x00%d", ref, policy.Version)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		policies = append(policies, guardrail.PolicyIdentity{Ref: ref, Version: policy.Version})
	}
	if len(policies) == 0 {
		return streamAdvisoryGapEvent{}, fmt.Errorf("stream advisory gap has no policies")
	}
	return streamAdvisoryGapEvent{
		relay: job.relay, orgID: job.orgID, requestID: requestID,
		policies: policies, reason: reason, occurredAt: writer.options.Now().UTC(),
	}, nil
}

func boundedGapIdentifier(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if len(digest) > maxBytes {
		return digest[:maxBytes]
	}
	return digest
}

func (writer *streamAdvisoryGapWriter) run() {
	defer close(writer.done)
	for first := range writer.input {
		if writer.runCtx.Err() != nil {
			return
		}
		events := make([]streamAdvisoryGapEvent, 0, writer.options.BatchSize)
		events = append(events, first)
		timer := time.NewTimer(writer.options.FlushInterval)
	collect:
		for len(events) < writer.options.BatchSize {
			select {
			case event, open := <-writer.input:
				if !open {
					break collect
				}
				events = append(events, event)
			case <-timer.C:
				break collect
			case <-writer.runCtx.Done():
				ackStreamAdvisoryGapEvents(events, writer.runCtx.Err())
				return
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		for _, batch := range buildStreamAdvisoryGapBatches(events) {
			if err := writer.persistWithRetry(batch); err != nil {
				ackStreamAdvisoryGapEvents(batch.events, err)
				return
			}
			writer.pendingPolicies.Add(-streamAdvisoryGapBatchDroppedCount(batch))
			ackStreamAdvisoryGapEvents(batch.events, nil)
		}
	}
}

func ackStreamAdvisoryGapEvents(events []streamAdvisoryGapEvent, err error) {
	for _, event := range events {
		if event.ack == nil {
			continue
		}
		select {
		case event.ack <- err:
		default:
		}
	}
}

func buildStreamAdvisoryGapBatches(events []streamAdvisoryGapEvent) []streamAdvisoryGapBatch {
	byRelay := make(map[*Relay]map[streamAdvisoryGapKey]*streamAdvisoryGapAggregate)
	eventsByRelay := make(map[*Relay][]streamAdvisoryGapEvent)
	for _, event := range events {
		if event.relay == nil || event.orgID <= 0 || event.requestID == "" || len(event.policies) == 0 {
			continue
		}
		aggregates := byRelay[event.relay]
		if aggregates == nil {
			aggregates = make(map[streamAdvisoryGapKey]*streamAdvisoryGapAggregate)
			byRelay[event.relay] = aggregates
		}
		eventsByRelay[event.relay] = append(eventsByRelay[event.relay], event)
		windowStartedAt := advisoryGapWindowStart(event.occurredAt)
		for _, policy := range event.policies {
			key := streamAdvisoryGapKey{
				orgID: event.orgID, requestID: event.requestID, policyRef: policy.Ref,
				policyVersion: policy.Version, reason: event.reason, windowStartedAt: windowStartedAt,
			}
			aggregate := aggregates[key]
			if aggregate == nil {
				aggregate = &streamAdvisoryGapAggregate{
					key: key, firstOccurredAt: event.occurredAt, lastOccurredAt: event.occurredAt,
				}
				aggregates[key] = aggregate
			}
			aggregate.droppedCount++
			if event.occurredAt.Before(aggregate.firstOccurredAt) {
				aggregate.firstOccurredAt = event.occurredAt
			}
			if event.occurredAt.After(aggregate.lastOccurredAt) {
				aggregate.lastOccurredAt = event.occurredAt
			}
		}
	}

	batches := make([]streamAdvisoryGapBatch, 0, len(byRelay))
	for relay, aggregateMap := range byRelay {
		aggregates := make([]streamAdvisoryGapAggregate, 0, len(aggregateMap))
		for _, aggregate := range aggregateMap {
			aggregates = append(aggregates, *aggregate)
		}
		sort.Slice(aggregates, func(i, j int) bool {
			left, right := aggregates[i].key, aggregates[j].key
			switch {
			case left.orgID != right.orgID:
				return left.orgID < right.orgID
			case left.requestID != right.requestID:
				return left.requestID < right.requestID
			case left.policyRef != right.policyRef:
				return left.policyRef < right.policyRef
			case left.policyVersion != right.policyVersion:
				return left.policyVersion < right.policyVersion
			case left.reason != right.reason:
				return left.reason < right.reason
			default:
				return left.windowStartedAt.Before(right.windowStartedAt)
			}
		})
		batches = append(batches, streamAdvisoryGapBatch{
			id: nextStreamAdvisoryGapBatchID(), relay: relay, aggregates: aggregates,
			events: eventsByRelay[relay],
		})
	}
	return batches
}

func advisoryGapWindowStart(occurredAt time.Time) time.Time {
	seconds := int64(streamAdvisoryGapWindow / time.Second)
	return time.Unix((occurredAt.UTC().Unix()/seconds)*seconds, 0).UTC()
}

var streamAdvisoryGapBatchSequence atomic.Uint64

func nextStreamAdvisoryGapBatchID() string {
	processDigest := sha256.Sum256([]byte(processStreamLeaseOwner))
	return fmt.Sprintf("sag_%s_%016x", hex.EncodeToString(processDigest[:6]), streamAdvisoryGapBatchSequence.Add(1))
}

func (writer *streamAdvisoryGapWriter) persistWithRetry(batch streamAdvisoryGapBatch) error {
	for attempt := 1; ; attempt++ {
		if writer.runCtx.Err() != nil {
			return writer.runCtx.Err()
		}
		ctx, cancel := context.WithTimeout(writer.runCtx, writer.options.PersistTimeout)
		err := persistStreamAdvisoryGapBatch(ctx, batch)
		cancel()
		if err == nil {
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("[guardrail] durable advisory gap batch %s retry %d: %v", batch.id, attempt, err)
		}
		timer := time.NewTimer(writer.options.RetryInterval)
		select {
		case <-timer.C:
		case <-writer.runCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return writer.runCtx.Err()
		}
	}
}

func streamAdvisoryGapBatchDroppedCount(batch streamAdvisoryGapBatch) int64 {
	var count int64
	for _, aggregate := range batch.aggregates {
		count += aggregate.droppedCount
	}
	return count
}

func persistStreamAdvisoryGapBatch(ctx context.Context, batch streamAdvisoryGapBatch) error {
	if batch.relay == nil || batch.relay.db == nil || batch.id == "" || len(batch.aggregates) == 0 {
		return fmt.Errorf("invalid stream advisory gap batch")
	}
	tx, err := batch.relay.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stream advisory gap batch: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	var droppedCount int64
	for _, aggregate := range batch.aggregates {
		droppedCount += aggregate.droppedCount
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO guardrail_advisory_gap_batches (batch_id,aggregate_count,dropped_count,created_at)
		VALUES ($1,$2,$3,NOW()) ON CONFLICT (batch_id) DO NOTHING`,
		batch.id, len(batch.aggregates), droppedCount)
	if err != nil {
		return fmt.Errorf("claim stream advisory gap batch: %w", err)
	}
	if result.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("confirm existing stream advisory gap batch: %w", err)
		}
		committed = true
		return nil
	}

	for _, aggregate := range batch.aggregates {
		key := aggregate.key
		_, err := tx.Exec(ctx, `
			INSERT INTO guardrail_advisory_gap_receipts (
			 org_id,request_id,policy_ref,policy_version,reason,window_started_at,window_seconds,
			 dropped_count,first_occurred_at,last_occurred_at,created_at,updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
			ON CONFLICT (org_id,request_id,policy_ref,policy_version,reason,window_started_at)
			DO UPDATE SET
			 dropped_count=guardrail_advisory_gap_receipts.dropped_count+EXCLUDED.dropped_count,
			 first_occurred_at=LEAST(guardrail_advisory_gap_receipts.first_occurred_at,EXCLUDED.first_occurred_at),
			 last_occurred_at=GREATEST(guardrail_advisory_gap_receipts.last_occurred_at,EXCLUDED.last_occurred_at),
			 updated_at=NOW()`,
			key.orgID, key.requestID, key.policyRef, key.policyVersion, key.reason, key.windowStartedAt,
			int(streamAdvisoryGapWindow/time.Second), aggregate.droppedCount,
			aggregate.firstOccurredAt, aggregate.lastOccurredAt)
		if err != nil {
			return fmt.Errorf("persist stream advisory gap aggregate: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit stream advisory gap batch: %w", err)
	}
	committed = true
	return nil
}

func (writer *streamAdvisoryGapWriter) Close(ctx context.Context) error {
	if writer == nil {
		return nil
	}
	writer.mu.Lock()
	if !writer.closed {
		writer.closed = true
	}
	writer.mu.Unlock()
	// No Enqueue can Add after closed becomes true, so this Wait is safe. Closing
	// input only after every registered Enqueue returns avoids send/close races.
	writer.finishOnce.Do(func() {
		go func() {
			writer.activeEnqueues.Wait()
			writer.closeInputOnce.Do(func() { close(writer.input) })
		}()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-writer.done:
		writer.cancel()
		return writer.persistenceError(nil)
	case <-ctx.Done():
		writer.cancel()
		return writer.persistenceError(ctx.Err())
	}
}

type streamAdvisoryGapPersistenceError struct {
	UnpersistedPolicies int64
}

func (err *streamAdvisoryGapPersistenceError) Error() string {
	return fmt.Sprintf("%d stream advisory policy gaps were not durably persisted", err.UnpersistedPolicies)
}

func (writer *streamAdvisoryGapWriter) persistenceError(cause error) error {
	unpersisted := writer.pendingPolicies.Load() + writer.failedPolicies.Load()
	if unpersisted <= 0 {
		return cause
	}
	return errors.Join(cause, &streamAdvisoryGapPersistenceError{UnpersistedPolicies: unpersisted})
}

func joinStreamAdvisoryShutdownErrors(schedulerErr, writerErr error) error {
	return errors.Join(schedulerErr, writerErr)
}
