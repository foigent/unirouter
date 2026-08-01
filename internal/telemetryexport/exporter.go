package telemetryexport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultQueueSize     = 256
	defaultBatchSize     = 32
	defaultFlushInterval = 500 * time.Millisecond
	defaultTimeout       = 2 * time.Second
	defaultRetryDelay    = 50 * time.Millisecond
	maxRetryDelay        = 5 * time.Second
)

var (
	ErrDisabled  = errors.New("telemetry export is disabled")
	ErrClosed    = errors.New("telemetry exporter is closed")
	ErrQueueFull = errors.New("telemetry export queue is full")
)

type Config struct {
	Enabled          bool
	Endpoint         string
	QueueSize        int
	BatchSize        int
	FlushInterval    time.Duration
	Timeout          time.Duration
	MaxRetries       int
	ServiceName      string
	ServiceNamespace string
	ServiceVersion   string
	DeploymentEnv    string
}

func DefaultConfig() Config {
	return Config{
		QueueSize:        defaultQueueSize,
		BatchSize:        defaultBatchSize,
		FlushInterval:    defaultFlushInterval,
		Timeout:          defaultTimeout,
		MaxRetries:       2,
		ServiceName:      "unirouter",
		ServiceNamespace: "foigent",
	}
}

func (cfg Config) withDefaults() Config {
	defaults := DefaultConfig()
	if cfg.QueueSize == 0 {
		cfg.QueueSize = defaults.QueueSize
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = defaults.FlushInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaults.ServiceName
	}
	if cfg.ServiceNamespace == "" {
		cfg.ServiceNamespace = defaults.ServiceNamespace
	}
	return cfg
}

func (cfg Config) Validate() error {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return nil
	}
	if cfg.QueueSize < 1 || cfg.QueueSize > 65536 {
		return fmt.Errorf("telemetry export queue size must be between 1 and 65536")
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 128 || cfg.BatchSize > cfg.QueueSize {
		return fmt.Errorf("telemetry export batch size must be between 1 and 128 and no larger than queue size")
	}
	if cfg.FlushInterval < 10*time.Millisecond || cfg.FlushInterval > time.Minute {
		return fmt.Errorf("telemetry export flush interval must be between 10ms and 1m")
	}
	if cfg.Timeout < 50*time.Millisecond || cfg.Timeout > time.Minute {
		return fmt.Errorf("telemetry export timeout must be between 50ms and 1m")
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 5 {
		return fmt.Errorf("telemetry export max retries must be between 0 and 5")
	}
	if _, err := normalizeEndpoint(cfg.Endpoint); err != nil {
		return err
	}
	return nil
}

type ExportResult struct {
	RejectedSpans uint64
}

type Sink interface {
	Export(context.Context, []byte) (ExportResult, error)
}

type retryableError interface {
	error
	Retryable() bool
}

type retryAfterError interface {
	RetryAfter() time.Duration
}

type Stats struct {
	Queued            uint64
	Exported          uint64
	PartialRejected   uint64
	DroppedRestricted uint64
	DroppedInvalid    uint64
	DroppedQueueFull  uint64
	DroppedAfterClose uint64
	ExportFailures    uint64
	Retries           uint64
}

type Exporter struct {
	cfg      Config
	resource resourceConfig
	sink     Sink
	queue    chan spanRecord
	rootCtx  context.Context
	cancel   context.CancelFunc
	done     chan struct{}

	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once

	queued            atomic.Uint64
	exported          atomic.Uint64
	partialRejected   atomic.Uint64
	droppedRestricted atomic.Uint64
	droppedInvalid    atomic.Uint64
	droppedQueueFull  atomic.Uint64
	droppedAfterClose atomic.Uint64
	exportFailures    atomic.Uint64
	retries           atomic.Uint64
}

func New(cfg Config, sink Sink) (*Exporter, error) {
	cfg = cfg.withDefaults()
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if sink == nil {
		var err error
		sink, err = NewHTTPSink(cfg.Endpoint, cfg.Timeout)
		if err != nil {
			return nil, err
		}
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	exporter := &Exporter{
		cfg: cfg,
		resource: resourceConfig{
			ServiceName:      cfg.ServiceName,
			ServiceNamespace: cfg.ServiceNamespace,
			ServiceVersion:   cfg.ServiceVersion,
			DeploymentEnv:    cfg.DeploymentEnv,
		},
		sink:    sink,
		queue:   make(chan spanRecord, cfg.QueueSize),
		rootCtx: rootCtx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go exporter.run()
	return exporter, nil
}

// TryExport performs only bounded validation/projection and a non-blocking
// channel send. It never performs network I/O and its result must not affect
// the originating request.
func (e *Exporter) TryExport(event SourceEvent) error {
	if e == nil {
		return ErrDisabled
	}
	record, err := buildSpan(event)
	if err != nil {
		if errors.Is(err, ErrRestrictedContent) {
			e.droppedRestricted.Add(1)
		} else {
			e.droppedInvalid.Add(1)
		}
		return err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		e.droppedAfterClose.Add(1)
		return ErrClosed
	}
	select {
	case e.queue <- record:
		e.queued.Add(1)
		return nil
	default:
		e.droppedQueueFull.Add(1)
		return ErrQueueFull
	}
}

func (e *Exporter) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		close(e.queue)
		e.mu.Unlock()
	})
	select {
	case <-e.done:
		e.cancel()
		return nil
	case <-ctx.Done():
		e.cancel()
		select {
		case <-e.done:
		default:
		}
		return ctx.Err()
	}
}

func (e *Exporter) Stats() Stats {
	if e == nil {
		return Stats{}
	}
	return Stats{
		Queued:            e.queued.Load(),
		Exported:          e.exported.Load(),
		PartialRejected:   e.partialRejected.Load(),
		DroppedRestricted: e.droppedRestricted.Load(),
		DroppedInvalid:    e.droppedInvalid.Load(),
		DroppedQueueFull:  e.droppedQueueFull.Load(),
		DroppedAfterClose: e.droppedAfterClose.Load(),
		ExportFailures:    e.exportFailures.Load(),
		Retries:           e.retries.Load(),
	}
}

func (e *Exporter) run() {
	defer close(e.done)
	timer := time.NewTimer(e.cfg.FlushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	batch := make([]spanRecord, 0, e.cfg.BatchSize)
	var timerC <-chan time.Time
	for {
		select {
		case record, ok := <-e.queue:
			if !ok {
				e.exportBatch(batch)
				return
			}
			if len(batch) == 0 {
				timer.Reset(e.cfg.FlushInterval)
				timerC = timer.C
			}
			batch = append(batch, record)
			if len(batch) >= e.cfg.BatchSize {
				stopTimer(timer)
				timerC = nil
				e.exportBatch(batch)
				batch = batch[:0]
			}
		case <-timerC:
			timerC = nil
			e.exportBatch(batch)
			batch = batch[:0]
		case <-e.rootCtx.Done():
			return
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (e *Exporter) exportBatch(batch []spanRecord) {
	if len(batch) == 0 {
		return
	}
	payload, err := marshalBatch(batch, e.resource)
	if errors.Is(err, errOTLPPayloadTooLarge) && len(batch) > 1 {
		middle := len(batch) / 2
		e.exportBatch(batch[:middle])
		e.exportBatch(batch[middle:])
		return
	}
	if err != nil {
		e.exportFailures.Add(uint64(len(batch)))
		return
	}

	for attempt := 0; attempt <= e.cfg.MaxRetries; attempt++ {
		requestCtx, cancel := context.WithTimeout(e.rootCtx, e.cfg.Timeout)
		result, exportErr := e.sink.Export(requestCtx, payload)
		cancel()
		if exportErr == nil {
			rejected := result.RejectedSpans
			if rejected > uint64(len(batch)) {
				rejected = uint64(len(batch))
			}
			e.partialRejected.Add(rejected)
			e.exported.Add(uint64(len(batch)) - rejected)
			return
		}
		if attempt >= e.cfg.MaxRetries || !isRetryable(exportErr) {
			e.exportFailures.Add(uint64(len(batch)))
			return
		}
		e.retries.Add(1)
		delay := retryDelay(exportErr, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-e.rootCtx.Done():
			stopTimer(timer)
			e.exportFailures.Add(uint64(len(batch)))
			return
		}
	}
}

func retryDelay(err error, attempt int) time.Duration {
	delay := defaultRetryDelay << attempt
	var suggested retryAfterError
	if errors.As(err, &suggested) && suggested.RetryAfter() > delay {
		delay = suggested.RetryAfter()
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func isRetryable(err error) bool {
	var classified retryableError
	if errors.As(err, &classified) {
		return classified.Retryable()
	}
	return false
}
