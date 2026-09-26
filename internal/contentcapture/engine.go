package contentcapture

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrCaptureClosed   = errors.New("content capture is finished")
	ErrCaptureCapacity = errors.New("content capture session capacity exhausted")
	ErrCaptureJob      = errors.New("invalid prepared content capture")
)

type EngineOptions struct {
	Queue                                        QueueLimits
	MaxActive, MaxProjectActive                  int
	PrepareTimeout, FinalizeTimeout, MaxLifetime time.Duration
}

// CaptureJob binds a durable allocation to one in-memory owner. Prepare must
// finish financial transactions before reserving/publishing pending state, honor
// its context, and report ambiguous commits without retrying. Finalize publishes
// the result or records its gap/health; it must also honor its independent context.
type CaptureJob struct {
	Reservation Reservation
	Limits      StoreLimits
	Finalize    func(context.Context, CaptureOutcome) error
}

type CaptureOutcome struct {
	Reservation  Reservation
	Receipt      *StoredReceipt
	Reason       string
	StorageError error
	PrepareError error
}

type CaptureResult struct {
	Outcome       CaptureOutcome
	FinalizeError error
}

// Engine owns one consumer and a finite set of interleaved file writers. It
// grants no persistent quota or read authority, deletes no files, and never
// retries a prepare/finalize callback. HTTP callers must not use capture errors
// to change provider retries or financial outcomes.
type Engine struct {
	mu         sync.Mutex
	files      *FileStore
	options    EngineOptions
	queue      *Queue
	active     map[*Capture]struct{}
	projects   map[int64]int
	closed     bool
	ctx        context.Context
	cancel     context.CancelFunc
	wake, done chan struct{}
}

type Capture struct {
	mu                             sync.Mutex
	engine                         *Engine
	projectID                      int64
	job                            CaptureJob
	prepared, terminal, finalizing bool
	reason                         string
	pending                        int
	accepted                       int64
	prepareErr                     error
	timer                          *time.Timer
	prepareCancel                  context.CancelFunc
	done                           chan struct{}
	result                         CaptureResult
	// Only the worker owns these fields and performs file I/O.
	writer     *ReservationWriter
	storageErr error
}

func NewEngine(files *FileStore, options EngineOptions) (*Engine, error) {
	if files == nil || options.MaxActive <= 0 || options.MaxProjectActive <= 0 || options.MaxProjectActive > options.MaxActive || options.PrepareTimeout <= 0 || options.FinalizeTimeout <= 0 || options.MaxLifetime <= 0 {
		return nil, ErrCaptureJob
	}
	if err := files.verifyBinding(); err != nil {
		return nil, err
	}
	q, err := NewQueue(options.Queue)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{files: files, options: options, queue: q, active: make(map[*Capture]struct{}), projects: make(map[int64]int), ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go e.run()
	return e, nil
}

// Start reserves session capacity before calling prepare. Preparing and
// finalizing sessions remain charged until their callbacks actually return.
// A nonnil Capture returned with an error still owns any late allocation; use
// Wait to observe its one finalization. Rejection before prepare returns nil.
func (e *Engine) Start(ctx context.Context, projectID int64, prepare func(context.Context) (CaptureJob, error)) (*Capture, error) {
	if e == nil || ctx == nil || projectID <= 0 || prepare == nil {
		return nil, ErrCaptureJob
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, ErrCaptureClosed
	}
	if len(e.active) >= e.options.MaxActive || e.projects[projectID] >= e.options.MaxProjectActive {
		e.mu.Unlock()
		return nil, ErrCaptureCapacity
	}
	c := &Capture{engine: e, projectID: projectID, done: make(chan struct{})}
	e.active[c] = struct{}{}
	e.projects[projectID]++
	prepareCtx, cancel := context.WithTimeout(e.ctx, e.options.PrepareTimeout)
	c.prepareCancel = cancel
	stopRequest := context.AfterFunc(ctx, cancel)
	c.timer = time.AfterFunc(e.options.MaxLifetime, func() { c.Finish("stream_interrupted") })
	e.mu.Unlock()
	job, err := prepare(prepareCtx)
	contextErr := prepareCtx.Err()
	stopRequest()
	cancel()
	c.mu.Lock()
	c.job = job
	c.prepared = true
	c.prepareCancel = nil
	if err == nil && (job.Finalize == nil || job.Reservation.ProjectID != projectID || validateStoreReservation(context.Background(), e.files, job.Reservation, job.Limits) != nil) {
		err = ErrCaptureJob
	}
	// Even late successful allocations must reach their supplied finalizer.
	if err != nil {
		c.prepareErr = err
		c.finishLocked("capture_unavailable")
	}
	if contextErr != nil && err == nil {
		err = contextErr
		c.prepareErr = err
		reason := "capture_unavailable"
		if ctx.Err() != nil {
			reason = "request_cancelled"
		}
		c.finishLocked(reason)
	}
	if c.terminal && err == nil {
		err = ErrCaptureClosed
	}
	c.mu.Unlock()
	e.signal()
	return c, err
}

// Offer copies only a bounded contiguous prefix, never waits for the consumer,
// and stops this capture on rejection. Accepted bytes remain owned by Engine
// after the request context is canceled. An exact byte limit is not truncation
// until an additional byte is observed.
func (c *Capture) Offer(ctx context.Context, payload []byte) error {
	if c == nil || ctx == nil {
		return ErrCaptureJob
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.prepared || c.terminal {
		return ErrCaptureClosed
	}
	if err := ctx.Err(); err != nil {
		c.finishLocked("request_cancelled")
		return err
	}
	limit := c.job.Limits.MaxSavedBytes
	chunkLimit := min(storeBufferSize(c.job.Limits), int(c.engine.options.Queue.MaxChunkBytes))
	for len(payload) > 0 {
		remaining := limit - c.accepted
		if remaining == 0 {
			c.finishLocked("limit_exceeded")
			return ErrStoreLimit
		}
		n := min(len(payload), chunkLimit)
		if int64(n) > remaining {
			n = int(remaining)
		}
		// Capture -> Queue is the only nested lock order. The worker releases
		// Queue before taking Capture, so it cannot see an uncharged item.
		if err := c.engine.queue.offer(ctx, c.projectID, payload[:n], c); err != nil {
			reason := "queue_full"
			if ctx.Err() != nil {
				reason = "request_cancelled"
			}
			c.finishLocked(reason)
			return err
		}
		c.pending++
		c.accepted += int64(n)
		payload = payload[n:]
	}
	return nil
}

// Finish has a separate wake path, including zero-byte captures and full queues.
// The first terminal event wins. Storage failures remain independently visible.
func (c *Capture) Finish(reason string) error {
	if c == nil {
		return ErrCaptureJob
	}
	switch reason {
	case "none", "limit_exceeded", "queue_full", "stream_interrupted", "request_cancelled", "output_held":
	default:
		return fmt.Errorf("%w: unsupported terminal reason", ErrCaptureJob)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(reason)
	return nil
}

func (c *Capture) finishLocked(reason string) {
	if c.terminal {
		return
	}
	c.terminal = true
	c.reason = reason
	if c.prepareCancel != nil {
		c.prepareCancel()
	}
	c.engine.signal()
}

func (c *Capture) Wait(ctx context.Context) (CaptureResult, error) {
	if c == nil || ctx == nil {
		return CaptureResult{}, ErrCaptureJob
	}
	select {
	case <-ctx.Done():
		return CaptureResult{}, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		result := c.result
		if result.Outcome.Receipt != nil {
			receipt := *result.Outcome.Receipt
			result.Outcome.Receipt = &receipt
		}
		return result, nil
	}
}

func (e *Engine) Usage(projectID int64) (active, projectActive int, queue QueueUsage) {
	if e == nil {
		return
	}
	e.mu.Lock()
	active = len(e.active)
	projectActive = e.projects[projectID]
	e.mu.Unlock()
	queue = e.queue.Usage(projectID)
	return
}

// Close drains accepted chunks. Deadline expiry cancels work and returns, but
// only the worker may close its FD/release its current chunk. Until synchronous
// I/O and callbacks really exit, Usage continues to include those resources.
func (e *Engine) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		return ErrCaptureJob
	}
	e.mu.Lock()
	e.closed = true
	all := make([]*Capture, 0, len(e.active))
	for c := range e.active {
		all = append(all, c)
	}
	e.mu.Unlock()
	for _, c := range all {
		_ = c.Finish("stream_interrupted")
	}
	e.signal()
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		e.cancel()
		e.signal()
		return ctx.Err()
	}
}

func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) run() {
	defer close(e.done)
	defer e.cancel()
	for {
		progress := false
		if item := e.queue.tryTake(); item != nil {
			e.write(item)
			progress = true
		}
		e.mu.Lock()
		all := make([]*Capture, 0, len(e.active))
		for c := range e.active {
			all = append(all, c)
		}
		exit := e.closed && len(all) == 0
		e.mu.Unlock()
		if exit {
			e.queue.Close()
			return
		}
		for _, c := range all {
			c.mu.Lock()
			ready := c.prepared && c.terminal && c.pending == 0 && !c.finalizing
			if ready {
				c.finalizing = true
			}
			c.mu.Unlock()
			if ready {
				e.finalize(c)
				progress = true
			}
		}
		if progress {
			continue
		}
		// Worker cancellation does not spin while a prepare callback is still
		// running. Start signals again when that callback actually returns.
		select {
		case <-e.queue.notify:
		case <-e.wake:
		}
	}
}

func (e *Engine) write(item *QueueItem) {
	c := item.capture
	c.mu.Lock()
	job := c.job
	c.mu.Unlock()
	if c.storageErr == nil {
		if c.writer == nil {
			c.writer, c.storageErr = e.files.BeginReservation(e.ctx, job.Reservation, job.Limits)
		}
		if c.storageErr == nil {
			c.storageErr = c.writer.WriteChunk(e.ctx, item.Bytes())
		}
	}
	item.Release()
	c.mu.Lock()
	c.pending--
	if c.storageErr != nil {
		c.finishLocked("storage_write_failed")
	}
	c.mu.Unlock()
}

func (e *Engine) finalize(c *Capture) {
	c.mu.Lock()
	job, reason, prepareErr := c.job, c.reason, c.prepareErr
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	result := CaptureResult{Outcome: CaptureOutcome{Reservation: job.Reservation, Reason: reason, PrepareError: prepareErr}}
	if prepareErr == nil && c.storageErr == nil {
		if c.writer == nil && reason == "none" {
			c.writer, c.storageErr = e.files.BeginReservation(e.ctx, job.Reservation, job.Limits)
		}
		if c.writer != nil && c.storageErr == nil && (c.writer.written > 0 || reason == "none") {
			receipt, err := c.writer.Commit(e.ctx)
			c.storageErr = err
			if err == nil {
				result.Outcome.Receipt = &receipt
			}
		}
	}
	if c.writer != nil {
		_ = c.writer.Close()
	}
	result.Outcome.StorageError = c.storageErr
	if c.storageErr != nil {
		result.Outcome.Reason = "storage_write_failed"
	}
	if job.Finalize != nil {
		ctx, cancel := context.WithTimeout(e.ctx, e.options.FinalizeTimeout)
		result.FinalizeError = job.Finalize(ctx, result.Outcome)
		cancel()
	}
	e.mu.Lock()
	delete(e.active, c)
	e.projects[c.projectID]--
	if e.projects[c.projectID] == 0 {
		delete(e.projects, c.projectID)
	}
	e.mu.Unlock()
	c.mu.Lock()
	c.result = result
	close(c.done)
	c.mu.Unlock()
}
