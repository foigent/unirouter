package gateway

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type concurrencyDetectingResponseWriter struct {
	header        http.Header
	active        atomic.Int32
	overlap       atomic.Bool
	writes        atomic.Int32
	flushes       atomic.Int32
	deadlineCalls atomic.Int32
}

func (writer *concurrencyDetectingResponseWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}

func (writer *concurrencyDetectingResponseWriter) WriteHeader(int) {}

func (writer *concurrencyDetectingResponseWriter) enter() {
	if writer.active.Add(1) != 1 {
		writer.overlap.Store(true)
	}
	time.Sleep(100 * time.Microsecond)
}

func (writer *concurrencyDetectingResponseWriter) leave() {
	writer.active.Add(-1)
}

func (writer *concurrencyDetectingResponseWriter) Write(payload []byte) (int, error) {
	writer.enter()
	defer writer.leave()
	writer.writes.Add(1)
	return len(payload), nil
}

func (writer *concurrencyDetectingResponseWriter) Flush() {
	writer.enter()
	defer writer.leave()
	writer.flushes.Add(1)
}

func (writer *concurrencyDetectingResponseWriter) SetWriteDeadline(time.Time) error {
	writer.deadlineCalls.Add(1)
	return nil
}

func TestSSEStreamWriterSerializesWriteFlushAndDeadline(t *testing.T) {
	response := &concurrencyDetectingResponseWriter{}
	writer := newSSEStreamWriter(response, 30*time.Second)

	start := make(chan struct{})
	errs := make(chan error, 100)
	var wait sync.WaitGroup
	for index := range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- writer.WriteAndFlush("data: %d\n\n", index)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("WriteAndFlush() error = %v", err)
		}
	}
	if response.overlap.Load() {
		t.Fatal("response writer observed concurrent Write/Flush access")
	}
	if response.writes.Load() != 100 || response.flushes.Load() != 100 || response.deadlineCalls.Load() != 100 {
		t.Fatalf("unexpected calls writes=%d flushes=%d deadlines=%d", response.writes.Load(), response.flushes.Load(), response.deadlineCalls.Load())
	}
}
