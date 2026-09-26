package contentcapture

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrQueueFull     = errors.New("content capture queue capacity exhausted")
	ErrQueueClosed   = errors.New("content capture queue closed")
	ErrChunkTooLarge = errors.New("content capture chunk exceeds limit")
)

type QueueLimits struct {
	// MaxItems covers queued and consumer-held items, including empty chunks.
	MaxItems        int
	MaxBytes        int64
	MaxProjectBytes int64
	MaxChunkBytes   int64
}

type QueueUsage struct {
	Items        int
	QueuedItems  int
	Bytes        int64
	ProjectBytes int64
}

// Queue is an in-process, nonblocking producer buffer. It is neither a durable
// reservation nor a retry system. The caller records capture gaps on rejection
// without altering the original request, provider attempt, or financial state.
type Queue struct {
	mu       sync.Mutex
	limits   QueueLimits
	ring     []*QueueItem
	head     int
	count    int
	items    int
	bytes    int64
	projects map[int64]int64
	closed   bool
	notify   chan struct{}
	done     chan struct{}
}

type QueueItem struct {
	queue     *Queue
	projectID int64
	capture   *Capture
	data      []byte
	once      sync.Once
}

func NewQueue(limits QueueLimits) (*Queue, error) {
	if limits.MaxItems <= 0 || limits.MaxBytes <= 0 || limits.MaxProjectBytes <= 0 || limits.MaxChunkBytes <= 0 || limits.MaxChunkBytes > limits.MaxProjectBytes || limits.MaxProjectBytes > limits.MaxBytes {
		return nil, fmt.Errorf("capture queue requires positive limits and chunk <= project <= total bytes")
	}
	return &Queue{limits: limits, ring: make([]*QueueItem, limits.MaxItems), projects: make(map[int64]int64), notify: make(chan struct{}, 1), done: make(chan struct{})}, nil
}

// Offer reserves both item and byte capacity before copying payload. It never
// waits for consumer progress. The context cancels this offer; once accepted,
// bytes belong to the queue until Take/Release or Close, independently of the
// originating HTTP request's lifetime.
func (q *Queue) Offer(ctx context.Context, projectID int64, payload []byte) error {
	return q.offer(ctx, projectID, payload, nil)
}

func (q *Queue) offer(ctx context.Context, projectID int64, payload []byte, capture *Capture) error {
	if ctx == nil || projectID <= 0 {
		return fmt.Errorf("capture offer requires context and project ID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil {
		return ErrQueueClosed
	}
	size := int64(len(payload))
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.closed {
		return ErrQueueClosed
	}
	if size > q.limits.MaxChunkBytes {
		return ErrChunkTooLarge
	}
	if q.items >= q.limits.MaxItems || size > q.limits.MaxBytes-q.bytes || size > q.limits.MaxProjectBytes-q.projects[projectID] {
		return ErrQueueFull
	}
	q.items++
	q.bytes += size
	if size > 0 {
		q.projects[projectID] += size
	}
	item := &QueueItem{queue: q, projectID: projectID, capture: capture, data: make([]byte, len(payload))}
	copy(item.data, payload)
	q.ring[(q.head+q.count)%len(q.ring)] = item
	q.count++
	q.signalLocked()
	return nil
}

// tryTake is used by Engine's single event loop, which also wakes for terminal
// captures with no payload. Like Take, it retains the charge until Release.
func (q *Queue) tryTake() *QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.count == 0 {
		return nil
	}
	return q.popLocked()
}

// Take blocks only while empty and responds to cancellation or Close. Budget
// remains reserved during consumer I/O; every successful Take requires Release.
func (q *Queue) Take(ctx context.Context) (*QueueItem, error) {
	if ctx == nil {
		return nil, fmt.Errorf("capture take requires context")
	}
	if q == nil {
		return nil, ErrQueueClosed
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q.mu.Lock()
		if err := ctx.Err(); err != nil {
			q.mu.Unlock()
			return nil, err
		}
		if q.closed {
			q.mu.Unlock()
			return nil, ErrQueueClosed
		}
		if q.count > 0 {
			item := q.popLocked()
			if q.count > 0 {
				q.signalLocked()
			}
			q.mu.Unlock()
			return item, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.done:
			return nil, ErrQueueClosed
		case <-q.notify:
		}
	}
}

// Close discards pending chunks and wakes waiters. Consumer-held chunks remain
// accounted until their owners release them. It is safe to call repeatedly.
func (q *Queue) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.done)
	for q.count > 0 {
		item := q.popLocked()
		q.releaseLocked(item)
	}
}

func (q *Queue) Usage(projectID int64) QueueUsage {
	if q == nil {
		return QueueUsage{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return QueueUsage{Items: q.items, QueuedItems: q.count, Bytes: q.bytes, ProjectBytes: q.projects[projectID]}
}

// Bytes is the consumer-owned chunk, valid until Release. Do not retain or use
// it concurrently with Release; retaining it would escape queue accounting.
func (i *QueueItem) Bytes() []byte {
	if i == nil {
		return nil
	}
	return i.data
}
func (i *QueueItem) ProjectID() int64 {
	if i == nil {
		return 0
	}
	return i.projectID
}
func (i *QueueItem) Release() {
	if i == nil || i.queue == nil {
		return
	}
	i.once.Do(func() { q := i.queue; q.mu.Lock(); defer q.mu.Unlock(); q.releaseLocked(i) })
}
func (q *Queue) releaseLocked(i *QueueItem) {
	size := int64(len(i.data))
	q.items--
	q.bytes -= size
	if size > 0 {
		q.projects[i.projectID] -= size
		if q.projects[i.projectID] == 0 {
			delete(q.projects, i.projectID)
		}
	}
	i.data = nil
}
func (q *Queue) popLocked() *QueueItem {
	i := q.ring[q.head]
	q.ring[q.head] = nil
	q.head = (q.head + 1) % len(q.ring)
	q.count--
	return i
}
func (q *Queue) signalLocked() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
