package gateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestStreamAdvisoryGapBatchAggregatesExactCountsWithoutContent(t *testing.T) {
	typeOfEvent := reflect.TypeOf(streamAdvisoryGapEvent{})
	for index := range typeOfEvent.NumField() {
		name := strings.ToLower(typeOfEvent.Field(index).Name)
		for _, forbidden := range []string{"text", "content", "payload", "body", "finding"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("gap event retains forbidden raw field %q", typeOfEvent.Field(index).Name)
			}
		}
	}

	relay := &Relay{db: &testutil.MockDB{}}
	base := time.Date(2026, time.July, 17, 10, 0, 1, 0, time.UTC)
	policies := []guardrail.PolicyIdentity{
		{Ref: "guardrail:one", Version: 3},
		{Ref: "guardrail:two", Version: 7},
	}
	events := []streamAdvisoryGapEvent{
		{relay: relay, orgID: 9, requestID: "req-1", policies: policies, reason: streamAdvisoryGapOverload, occurredAt: base},
		{relay: relay, orgID: 9, requestID: "req-1", policies: policies, reason: streamAdvisoryGapOverload, occurredAt: base.Add(time.Second)},
		{relay: relay, orgID: 9, requestID: "req-1", policies: policies[:1], reason: streamAdvisoryGapOverload, occurredAt: base.Add(2 * time.Second)},
		{relay: relay, orgID: 9, requestID: "req-1", policies: policies[:1], reason: streamAdvisoryGapOverload, occurredAt: base.Add(5 * time.Second)},
	}
	batches := buildStreamAdvisoryGapBatches(events)
	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1", len(batches))
	}
	counts := make(map[string][]int64)
	for _, aggregate := range batches[0].aggregates {
		counts[aggregate.key.policyRef] = append(counts[aggregate.key.policyRef], aggregate.droppedCount)
	}
	if got := counts["guardrail:one"]; !reflect.DeepEqual(got, []int64{3, 1}) {
		t.Fatalf("policy one window counts = %v, want [3 1]", got)
	}
	if got := counts["guardrail:two"]; !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("policy two window counts = %v, want [2]", got)
	}
}

func TestBoundedGapIdentifierHashesOversizedRequestAndPolicyIdentifiers(t *testing.T) {
	value := strings.Repeat("tenant-controlled-identifier", 32)
	requestID := boundedGapIdentifier(value, 64)
	policyRef := boundedGapIdentifier(value, 160)
	if len(requestID) != 64 || !strings.HasPrefix(requestID, "sha256:") {
		t.Fatalf("bounded request ID = %q (%d bytes)", requestID, len(requestID))
	}
	if len(policyRef) > 160 || len(policyRef) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(policyRef, "sha256:") {
		t.Fatalf("bounded policy ref = %q (%d bytes)", policyRef, len(policyRef))
	}
	if requestID != boundedGapIdentifier(value, 64) || policyRef != boundedGapIdentifier(value, 160) {
		t.Fatal("bounded gap identifiers are not deterministic")
	}
}

func TestPersistStreamAdvisoryGapBatchClaimMakesRetryIdempotent(t *testing.T) {
	var begins atomic.Int32
	var receiptWrites atomic.Int32
	var commits atomic.Int32
	database := &testutil.MockDB{}
	database.BeginFn = func(context.Context) (pgx.Tx, error) {
		attempt := begins.Add(1)
		return &testutil.MockTx{
			ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(query, "guardrail_advisory_gap_batches"):
					if attempt == 1 {
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				case strings.Contains(query, "guardrail_advisory_gap_receipts"):
					receiptWrites.Add(1)
					if len(args) != 10 {
						t.Fatalf("receipt argument count = %d, want 10", len(args))
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				default:
					t.Fatalf("unexpected query: %s", query)
					return pgconn.CommandTag{}, nil
				}
			},
			CommitFn: func(context.Context) error {
				commits.Add(1)
				return nil
			},
		}, nil
	}
	relay := &Relay{db: database}
	occurredAt := time.Date(2026, time.July, 17, 10, 0, 1, 0, time.UTC)
	batch := streamAdvisoryGapBatch{
		id: "sag_test_retry", relay: relay,
		aggregates: []streamAdvisoryGapAggregate{{
			key: streamAdvisoryGapKey{
				orgID: 4, requestID: "req-safe", policyRef: "guardrail:pii", policyVersion: 2,
				reason: streamAdvisoryGapTimeout, windowStartedAt: advisoryGapWindowStart(occurredAt),
			},
			droppedCount: 3, firstOccurredAt: occurredAt, lastOccurredAt: occurredAt.Add(time.Second),
		}},
	}
	if err := persistStreamAdvisoryGapBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if err := persistStreamAdvisoryGapBatch(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if got := receiptWrites.Load(); got != 1 {
		t.Fatalf("aggregate writes after retry = %d, want 1", got)
	}
	if got := commits.Load(); got != 2 {
		t.Fatalf("commits = %d, want 2", got)
	}
}

func TestStreamAdvisoryGapWriterReturnsOnlyAfterCommitAndBackpressuresWhenFull(t *testing.T) {
	commitStarted := make(chan struct{})
	releaseFirstCommit := make(chan struct{})
	var commitCount atomic.Int32
	database := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				ExecFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
				CommitFn: func(context.Context) error {
					if commitCount.Add(1) == 1 {
						close(commitStarted)
						<-releaseFirstCommit
					}
					return nil
				},
			}, nil
		},
	}
	options := defaultStreamAdvisoryGapWriterOptions()
	options.BufferDepth = 1
	options.BatchSize = 1
	options.FlushInterval = time.Millisecond
	writer := newStreamAdvisoryGapWriter(options)
	baseJob := streamAdvisoryJob{
		relay: &Relay{db: database}, orgID: 3,
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:ack", Version: 1}},
	}
	record := func(requestID string) <-chan error {
		result := make(chan error, 1)
		job := baseJob
		job.requestID = requestID
		go func() { result <- writer.Record(context.Background(), job, streamAdvisoryGapOverload) }()
		return result
	}
	first := record("req-gap-first")
	select {
	case <-commitStarted:
	case <-time.After(time.Second):
		t.Fatal("first gap did not reach commit")
	}
	select {
	case err := <-first:
		t.Fatalf("Record returned before commit: %v", err)
	default:
	}
	second := record("req-gap-buffered")
	waitForGapBufferLen(t, writer, 1)
	third := record("req-gap-backpressured")
	select {
	case err := <-third:
		t.Fatalf("full-buffer Record did not backpressure: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirstCommit)
	for index, result := range []<-chan error{first, second, third} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("record %d failed after capacity was released: %v", index, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("record %d did not receive durable ack", index)
		}
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}

func TestStreamAdvisoryGapWriterPersistentDBFailureDoesNotDeadlockClose(t *testing.T) {
	beginStarted := make(chan struct{})
	var beginOnce sync.Once
	database := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			beginOnce.Do(func() { close(beginStarted) })
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	relay := &Relay{db: database}
	options := defaultStreamAdvisoryGapWriterOptions()
	options.BufferDepth = 1
	options.BatchSize = 1
	options.FlushInterval = time.Millisecond
	options.PersistTimeout = time.Second
	options.RetryInterval = time.Millisecond
	writer := newStreamAdvisoryGapWriter(options)
	job := streamAdvisoryJob{
		relay: relay, orgID: 3, requestID: "req-gap-stalled",
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:stalled", Version: 1}},
	}
	recordResults := make([]<-chan error, 0, 3)
	record := func(job streamAdvisoryJob) <-chan error {
		result := make(chan error, 1)
		go func() { result <- writer.Record(context.Background(), job, streamAdvisoryGapOverload) }()
		return result
	}
	recordResults = append(recordResults, record(job))
	select {
	case <-beginStarted:
	case <-time.After(time.Second):
		t.Fatal("gap writer did not enter persistence")
	}
	job.requestID = "req-gap-buffered"
	recordResults = append(recordResults, record(job))
	waitForGapBufferLen(t, writer, 1)
	job.requestID = "req-gap-full"
	recordResults = append(recordResults, record(job))
	select {
	case err := <-recordResults[2]:
		t.Fatalf("full-buffer Record returned instead of backpressuring: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := writer.Close(closeCtx)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("Close ignored its context for %v", elapsed)
	}
	var persistenceErr *streamAdvisoryGapPersistenceError
	if !errors.As(err, &persistenceErr) {
		t.Fatalf("Close error = %v, want unpersisted-count error", err)
	}
	if persistenceErr.UnpersistedPolicies != 3 {
		t.Fatalf("unpersisted policy gaps = %d, want 3", persistenceErr.UnpersistedPolicies)
	}
	for index, result := range recordResults {
		select {
		case err := <-result:
			if err == nil {
				t.Fatalf("record %d reported durable success during database failure", index)
			}
		case <-time.After(time.Second):
			t.Fatalf("record %d remained blocked after Close cancellation", index)
		}
	}
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("gap writer goroutine did not stop after cancellation")
	}
}

func TestStreamAdvisoryGapWriterConcurrentRecordAndCloseIsBounded(t *testing.T) {
	database := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
					if strings.Contains(query, "guardrail_advisory_gap_batches") {
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}, nil
		},
	}
	options := defaultStreamAdvisoryGapWriterOptions()
	options.BufferDepth = 8
	options.BatchSize = 4
	options.FlushInterval = time.Millisecond
	writer := newStreamAdvisoryGapWriter(options)
	job := streamAdvisoryJob{
		relay: &Relay{db: database}, orgID: 8,
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:race", Version: 1}},
	}
	start := make(chan struct{})
	var records sync.WaitGroup
	for index := 0; index < 64; index++ {
		records.Add(1)
		go func(index int) {
			defer records.Done()
			<-start
			jobCopy := job
			jobCopy.requestID = fmt.Sprintf("req-race-%d", index)
			_ = writer.Record(context.Background(), jobCopy, streamAdvisoryGapShutdown)
		}(index)
	}
	close(start)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- writer.Close(closeCtx) }()
	records.Wait()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("concurrent Close deadlocked")
	}
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("concurrent writer did not stop")
	}
}

func TestStreamAdvisoryOverloadGapDBFailureHasBoundedWait(t *testing.T) {
	database := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	options := defaultStreamAdvisoryGapWriterOptions()
	options.BufferDepth = 1
	options.BatchSize = 1
	options.FlushInterval = time.Millisecond
	options.PersistTimeout = time.Second
	writer := newStreamAdvisoryGapWriter(options)
	job := streamAdvisoryJob{
		relay: &Relay{db: database}, orgID: 10, requestID: "req-overload-db-down",
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:overload", Version: 1}},
	}
	recordCtx, cancelRecord := context.WithTimeout(context.Background(), 30*time.Millisecond)
	startedAt := time.Now()
	recordStreamAdvisoryGap(recordCtx, writer, job, streamAdvisoryGapOverload)
	cancelRecord()
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("overload gap recording blocked for %v", elapsed)
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelClose()
	err := writer.Close(closeCtx)
	var persistenceErr *streamAdvisoryGapPersistenceError
	if !errors.As(err, &persistenceErr) || persistenceErr.UnpersistedPolicies != 1 {
		t.Fatalf("Close error = %v, want one observable unpersisted gap", err)
	}
}

func TestShutdownStreamAdvisoryRuntimeBatchesHealthyShutdownGaps(t *testing.T) {
	var begins atomic.Int32
	var batchRows atomic.Int32
	var receiptWrites atomic.Int32
	database := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) {
			begins.Add(1)
			return &testutil.MockTx{
				ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
					if strings.Contains(query, "guardrail_advisory_gap_batches") {
						batchRows.Add(1)
					} else if strings.Contains(query, "guardrail_advisory_gap_receipts") {
						receiptWrites.Add(1)
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}, nil
		},
	}
	writerOptions := defaultStreamAdvisoryGapWriterOptions()
	writerOptions.BufferDepth = 64
	writerOptions.BatchSize = 64
	writerOptions.FlushInterval = 100 * time.Millisecond
	writer := newStreamAdvisoryGapWriter(writerOptions)
	started := make(chan struct{}, 1)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 32, OrgQueueDepth: 32, OrgConcurrency: 1,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		started <- struct{}{}
		<-ctx.Done()
	}, func(ctx context.Context, job streamAdvisoryJob, reason string) {
		recordStreamAdvisoryGap(ctx, writer, job, reason)
	})
	baseJob := streamAdvisoryJob{
		relay: &Relay{db: database}, orgID: 12,
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:shutdown", Version: 4}},
	}
	for index := 0; index < 16; index++ {
		job := baseJob
		job.requestID = fmt.Sprintf("req-shutdown-%02d", index)
		if !scheduler.Submit(job) {
			t.Fatalf("submit shutdown job %d", index)
		}
		if index == 0 {
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("active shutdown job did not start")
			}
		}
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdownStreamAdvisoryRuntime(closeCtx, scheduler, writer); err != nil {
		t.Fatal(err)
	}
	if begins.Load() != 1 || batchRows.Load() != 1 || receiptWrites.Load() != 16 {
		t.Fatalf("shutdown tx/batch/aggregates = %d/%d/%d, want 1/1/16",
			begins.Load(), batchRows.Load(), receiptWrites.Load())
	}
}

func TestShutdownStreamAdvisoryRuntimePersistentDBFailureHonorsDeadline(t *testing.T) {
	database := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	writerOptions := defaultStreamAdvisoryGapWriterOptions()
	writerOptions.BufferDepth = 1
	writerOptions.BatchSize = 1
	writerOptions.FlushInterval = time.Millisecond
	writerOptions.PersistTimeout = time.Second
	writerOptions.RetryInterval = time.Millisecond
	writer := newStreamAdvisoryGapWriter(writerOptions)
	started := make(chan struct{}, 1)
	scheduler := newStreamAdvisoryScheduler(streamAdvisorySchedulerOptions{
		GlobalConcurrency: 1, GlobalQueueDepth: 4, OrgQueueDepth: 4, OrgConcurrency: 1,
		EvaluationTimeout: time.Second,
	}, func(ctx context.Context, _ streamAdvisoryJob) {
		started <- struct{}{}
		<-ctx.Done()
	}, func(ctx context.Context, job streamAdvisoryJob, reason string) {
		recordStreamAdvisoryGap(ctx, writer, job, reason)
	})
	baseJob := streamAdvisoryJob{
		relay: &Relay{db: database}, orgID: 13,
		policies: []guardrail.PolicyIdentity{{Ref: "guardrail:shutdown-stalled", Version: 1}},
	}
	for index := 0; index < 3; index++ {
		job := baseJob
		job.requestID = fmt.Sprintf("req-shutdown-stalled-%d", index)
		if !scheduler.Submit(job) {
			t.Fatalf("submit stalled shutdown job %d", index)
		}
		if index == 0 {
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("active stalled job did not start")
			}
		}
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	err := shutdownStreamAdvisoryRuntime(closeCtx, scheduler, writer)
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("runtime shutdown ignored deadline for %v", elapsed)
	}
	var persistenceErr *streamAdvisoryGapPersistenceError
	if !errors.As(err, &persistenceErr) || persistenceErr.UnpersistedPolicies != 3 {
		t.Fatalf("shutdown error = %v, want 3 unpersisted policy gaps", err)
	}
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("stalled shutdown writer did not stop")
	}
}

func waitForGapBufferLen(t *testing.T, writer *streamAdvisoryGapWriter, length int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(writer.input) == length {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("gap writer buffer length = %d, want %d", len(writer.input), length)
}
