package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/guardrail"
)

const (
	streamAdvisoryGapOverload = "overload"
	streamAdvisoryGapTimeout  = "timeout"
	streamAdvisoryGapShutdown = "shutdown"

	streamAdvisoryShutdownEnqueueWait = 2 * time.Second
)

type streamAdvisorySchedulerOptions struct {
	GlobalConcurrency int
	GlobalQueueDepth  int
	OrgQueueDepth     int
	OrgConcurrency    int
	MaxJobTextBytes   int64
	GlobalQueueBytes  int64
	OrgQueueBytes     int64
	EvaluationTimeout time.Duration
}

func defaultStreamAdvisorySchedulerOptions() streamAdvisorySchedulerOptions {
	return streamAdvisorySchedulerOptions{
		GlobalConcurrency: 64,
		GlobalQueueDepth:  4096,
		OrgQueueDepth:     64,
		OrgConcurrency:    4,
		MaxJobTextBytes:   int64(guardrail.DefaultOptions().ExternalDetectorMaxBytes),
		GlobalQueueBytes:  32 * 1024 * 1024,
		OrgQueueBytes:     1024 * 1024,
		EvaluationTimeout: 2 * time.Second,
	}
}

func (options streamAdvisorySchedulerOptions) withByteDefaults() streamAdvisorySchedulerOptions {
	defaults := defaultStreamAdvisorySchedulerOptions()
	if options.MaxJobTextBytes == 0 {
		options.MaxJobTextBytes = defaults.MaxJobTextBytes
	}
	if options.GlobalQueueBytes == 0 {
		options.GlobalQueueBytes = defaults.GlobalQueueBytes
	}
	if options.OrgQueueBytes == 0 {
		options.OrgQueueBytes = defaults.OrgQueueBytes
	}
	return options
}

func (options streamAdvisorySchedulerOptions) valid() bool {
	return options.GlobalConcurrency > 0 && options.GlobalQueueDepth > 0 &&
		options.OrgQueueDepth > 0 && options.OrgConcurrency > 0 &&
		options.OrgConcurrency <= options.GlobalConcurrency && options.MaxJobTextBytes > 0 &&
		options.OrgQueueBytes > 0 && options.OrgQueueBytes <= options.GlobalQueueBytes &&
		options.MaxJobTextBytes <= options.GlobalQueueBytes && options.EvaluationTimeout > 0
}

type streamAdvisoryJob struct {
	id        uint64
	textBytes int64

	ctx       context.Context
	relay     *Relay
	pinned    *guardrail.Pinned
	texts     []guardrail.Text
	orgID     int64
	requestID string
	policies  []guardrail.PolicyIdentity

	model             string
	requestedModel    string
	route             *RouteResult
	upstreamRequestID string
}

type streamAdvisoryScheduler struct {
	options   streamAdvisorySchedulerOptions
	execute   func(context.Context, streamAdvisoryJob)
	recordGap func(context.Context, streamAdvisoryJob, string)

	mu             sync.Mutex
	cond           *sync.Cond
	queues         map[int64][]streamAdvisoryJob
	orgOrder       []int64
	orgQueuedBytes map[int64]int64
	inflight       map[int64]int
	active         map[uint64]streamAdvisoryJob
	queued         int
	queuedBytes    int64
	nextID         uint64
	closed         bool
	shutdownCtx    context.Context

	runCtx context.Context
	cancel context.CancelFunc
	worker sync.WaitGroup
	done   chan struct{}
}

func newStreamAdvisoryScheduler(
	options streamAdvisorySchedulerOptions,
	execute func(context.Context, streamAdvisoryJob),
	recordGap func(context.Context, streamAdvisoryJob, string),
) *streamAdvisoryScheduler {
	options = options.withByteDefaults()
	if !options.valid() {
		panic("invalid stream advisory scheduler options")
	}
	if execute == nil || recordGap == nil {
		panic("stream advisory scheduler callbacks are required")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	scheduler := &streamAdvisoryScheduler{
		options: options, execute: execute, recordGap: recordGap,
		queues: make(map[int64][]streamAdvisoryJob), orgQueuedBytes: make(map[int64]int64),
		inflight: make(map[int64]int),
		active:   make(map[uint64]streamAdvisoryJob), runCtx: runCtx, cancel: cancel,
		done: make(chan struct{}),
	}
	scheduler.cond = sync.NewCond(&scheduler.mu)
	scheduler.worker.Add(options.GlobalConcurrency)
	for range options.GlobalConcurrency {
		go scheduler.runWorker()
	}
	go func() {
		scheduler.worker.Wait()
		close(scheduler.done)
	}()
	return scheduler
}

func (scheduler *streamAdvisoryScheduler) Submit(job streamAdvisoryJob) bool {
	if scheduler == nil || job.orgID <= 0 || job.requestID == "" || len(job.policies) == 0 {
		return false
	}
	job.textBytes = streamAdvisoryJobTextBytes(job.texts)

	scheduler.mu.Lock()
	reason := ""
	gapCtx := scheduler.runCtx
	switch {
	case scheduler.closed:
		reason = streamAdvisoryGapShutdown
		if scheduler.shutdownCtx != nil {
			gapCtx = scheduler.shutdownCtx
		}
	case job.textBytes < 0 || job.textBytes > scheduler.options.MaxJobTextBytes:
		reason = streamAdvisoryGapOverload
	case scheduler.queued >= scheduler.options.GlobalQueueDepth:
		reason = streamAdvisoryGapOverload
	case len(scheduler.queues[job.orgID]) >= scheduler.options.OrgQueueDepth:
		reason = streamAdvisoryGapOverload
	case scheduler.queuedBytes+job.textBytes > scheduler.options.GlobalQueueBytes:
		reason = streamAdvisoryGapOverload
	case scheduler.orgQueuedBytes[job.orgID]+job.textBytes > scheduler.options.OrgQueueBytes:
		reason = streamAdvisoryGapOverload
	default:
		job.texts = append([]guardrail.Text(nil), job.texts...)
		job.policies = append([]guardrail.PolicyIdentity(nil), job.policies...)
		scheduler.nextID++
		job.id = scheduler.nextID
		if _, present := scheduler.queues[job.orgID]; !present {
			scheduler.orgOrder = append(scheduler.orgOrder, job.orgID)
		}
		scheduler.queues[job.orgID] = append(scheduler.queues[job.orgID], job)
		scheduler.queued++
		scheduler.queuedBytes += job.textBytes
		scheduler.orgQueuedBytes[job.orgID] += job.textBytes
		scheduler.cond.Signal()
	}
	scheduler.mu.Unlock()
	if reason != "" {
		scheduler.recordGap(gapCtx, job, reason)
		return false
	}
	return true
}

func streamAdvisoryJobTextBytes(texts []guardrail.Text) int64 {
	var total int64
	for _, text := range texts {
		itemBytes := int64(len(text.Path)) + int64(len(text.Content))
		if itemBytes < 0 || total > int64(^uint64(0)>>1)-itemBytes {
			return -1
		}
		total += itemBytes
	}
	return total
}

func (scheduler *streamAdvisoryScheduler) runWorker() {
	defer scheduler.worker.Done()
	for {
		job, ok := scheduler.nextJob()
		if !ok {
			return
		}
		scheduler.runJob(job)
	}
}

func (scheduler *streamAdvisoryScheduler) nextJob() (streamAdvisoryJob, bool) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	for {
		if scheduler.closed {
			return streamAdvisoryJob{}, false
		}
		if job, ok := scheduler.nextRunnableLocked(); ok {
			scheduler.inflight[job.orgID]++
			scheduler.active[job.id] = job
			scheduler.queued--
			scheduler.queuedBytes -= job.textBytes
			if scheduler.orgQueuedBytes[job.orgID] <= job.textBytes {
				delete(scheduler.orgQueuedBytes, job.orgID)
			} else {
				scheduler.orgQueuedBytes[job.orgID] -= job.textBytes
			}
			return job, true
		}
		scheduler.cond.Wait()
	}
}

// nextRunnableLocked rotates every examined organization to the back. A busy
// organization therefore cannot hide another organization's runnable work.
func (scheduler *streamAdvisoryScheduler) nextRunnableLocked() (streamAdvisoryJob, bool) {
	organizations := len(scheduler.orgOrder)
	for range organizations {
		orgID := scheduler.orgOrder[0]
		scheduler.orgOrder = scheduler.orgOrder[1:]
		queue := scheduler.queues[orgID]
		if len(queue) == 0 {
			delete(scheduler.queues, orgID)
			continue
		}
		if scheduler.inflight[orgID] >= scheduler.options.OrgConcurrency {
			scheduler.orgOrder = append(scheduler.orgOrder, orgID)
			continue
		}
		job := queue[0]
		queue = queue[1:]
		if len(queue) == 0 {
			delete(scheduler.queues, orgID)
		} else {
			scheduler.queues[orgID] = queue
			scheduler.orgOrder = append(scheduler.orgOrder, orgID)
		}
		return job, true
	}
	return streamAdvisoryJob{}, false
}

func (scheduler *streamAdvisoryScheduler) runJob(job streamAdvisoryJob) {
	base := context.Background()
	if job.ctx != nil {
		base = context.WithoutCancel(job.ctx)
	}
	jobCtx, cancel := context.WithTimeout(base, scheduler.options.EvaluationTimeout)
	stopShutdownCancel := context.AfterFunc(scheduler.runCtx, cancel)
	scheduler.execute(jobCtx, job)
	stopShutdownCancel()
	jobErr := jobCtx.Err()
	cancel()

	scheduler.mu.Lock()
	shutdown := scheduler.closed
	delete(scheduler.active, job.id)
	if scheduler.inflight[job.orgID] <= 1 {
		delete(scheduler.inflight, job.orgID)
	} else {
		scheduler.inflight[job.orgID]--
	}
	scheduler.cond.Broadcast()
	scheduler.mu.Unlock()

	// Close emits shutdown gaps for every active job while holding the exact
	// active set. Do not emit a second gap when cancellation lets the worker end.
	if !shutdown && errors.Is(jobErr, context.DeadlineExceeded) {
		scheduler.recordGap(scheduler.runCtx, job, streamAdvisoryGapTimeout)
	}
}

func (scheduler *streamAdvisoryScheduler) Close(ctx context.Context) error {
	if scheduler == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, streamAdvisoryShutdownEnqueueWait)
	defer cancelShutdown()
	scheduler.mu.Lock()
	if !scheduler.closed {
		scheduler.closed = true
		scheduler.shutdownCtx = shutdownCtx
		gaps := make([]streamAdvisoryJob, 0, scheduler.queued+len(scheduler.active))
		for _, orgID := range scheduler.orgOrder {
			gaps = append(gaps, scheduler.queues[orgID]...)
		}
		for _, job := range scheduler.active {
			gaps = append(gaps, job)
		}
		scheduler.queues = make(map[int64][]streamAdvisoryJob)
		scheduler.orgQueuedBytes = make(map[int64]int64)
		scheduler.orgOrder = nil
		scheduler.queued = 0
		scheduler.queuedBytes = 0
		scheduler.cancel()
		scheduler.cond.Broadcast()
		scheduler.mu.Unlock()
		for _, job := range gaps {
			scheduler.recordGap(shutdownCtx, job, streamAdvisoryGapShutdown)
		}
	} else {
		scheduler.mu.Unlock()
	}

	select {
	case <-scheduler.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
