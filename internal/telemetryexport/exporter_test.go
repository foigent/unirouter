package telemetryexport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type sinkResponse struct {
	result ExportResult
	err    error
}

type scriptedSink struct {
	mu        sync.Mutex
	responses []sinkResponse
	payloads  [][]byte
	started   chan struct{}
	release   chan struct{}
}

func (s *scriptedSink) Export(ctx context.Context, payload []byte) (ExportResult, error) {
	s.mu.Lock()
	s.payloads = append(s.payloads, append([]byte(nil), payload...))
	index := len(s.payloads) - 1
	var response sinkResponse
	if index < len(s.responses) {
		response = s.responses[index]
	}
	started := s.started
	release := s.release
	s.mu.Unlock()
	if started != nil && index == 0 {
		close(started)
	}
	if release != nil && index == 0 {
		select {
		case <-release:
		case <-ctx.Done():
			return ExportResult{}, &transportError{err: ctx.Err()}
		}
	}
	return response.result, response.err
}

func (s *scriptedSink) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.payloads)
}

func exporterConfig() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Endpoint = "http://collector.internal:4318"
	cfg.QueueSize = 8
	cfg.BatchSize = 1
	cfg.FlushInterval = 10 * time.Millisecond
	cfg.Timeout = 250 * time.Millisecond
	cfg.MaxRetries = 2
	return cfg
}

func TestExporterRetriesOnlyClassifiedFailuresAndIsDeterministic(t *testing.T) {
	sink := &scriptedSink{responses: []sinkResponse{
		{err: &httpStatusError{statusCode: 503}},
		{err: &httpStatusError{statusCode: 429}},
		{},
	}}
	exporter, err := New(exporterConfig(), sink)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	if err := exporter.TryExport(fixedSourceEvent()); err != nil {
		t.Fatalf("queue event: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exporter.Close(closeCtx); err != nil {
		t.Fatalf("close exporter: %v", err)
	}
	stats := exporter.Stats()
	if stats.Queued != 1 || stats.Exported != 1 || stats.Retries != 2 || stats.ExportFailures != 0 {
		t.Fatalf("unexpected retry stats: %+v", stats)
	}
	if sink.callCount() != 3 {
		t.Fatalf("expected three attempts, got %d", sink.callCount())
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for i := 1; i < len(sink.payloads); i++ {
		if string(sink.payloads[i]) != string(sink.payloads[0]) {
			t.Fatal("retry changed OTLP request bytes")
		}
	}
}

func TestExporterTreatsPartialSuccessAsAcknowledgedWithoutRetry(t *testing.T) {
	sink := &scriptedSink{responses: []sinkResponse{{
		result: ExportResult{RejectedSpans: 1},
	}}}
	exporter, err := New(exporterConfig(), sink)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	if err := exporter.TryExport(fixedSourceEvent()); err != nil {
		t.Fatalf("queue event: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exporter.Close(closeCtx); err != nil {
		t.Fatalf("close exporter: %v", err)
	}
	stats := exporter.Stats()
	if stats.PartialRejected != 1 || stats.Exported != 0 || stats.Retries != 0 || sink.callCount() != 1 {
		t.Fatalf("unexpected partial-success result: stats=%+v calls=%d", stats, sink.callCount())
	}
}

func TestExporterQueueIsBoundedAndRequestPathNeverWaitsForSink(t *testing.T) {
	sink := &scriptedSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	cfg := exporterConfig()
	cfg.QueueSize = 1
	exporter, err := New(cfg, sink)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	if err := exporter.TryExport(fixedSourceEvent()); err != nil {
		t.Fatalf("queue first event: %v", err)
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("sink did not start")
	}
	second := fixedSourceEvent()
	second.SpanID = "1111111111111111"
	if err := exporter.TryExport(second); err != nil {
		t.Fatalf("queue second event: %v", err)
	}
	third := fixedSourceEvent()
	third.SpanID = "2222222222222222"
	start := time.Now()
	if err := exporter.TryExport(third); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("expected queue-full rejection, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("queue-full path blocked for %s", elapsed)
	}
	close(sink.release)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exporter.Close(closeCtx); err != nil {
		t.Fatalf("close exporter: %v", err)
	}
	stats := exporter.Stats()
	if stats.Queued != 2 || stats.Exported != 2 || stats.DroppedQueueFull != 1 {
		t.Fatalf("unexpected bounded queue stats: %+v", stats)
	}
}

func TestExporterRejectsRestrictedContentBeforeQueueing(t *testing.T) {
	sink := &scriptedSink{}
	exporter, err := New(exporterConfig(), sink)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	event := fixedSourceEvent()
	event.Metadata["prompt"] = "do not export"
	if err := exporter.TryExport(event); !errors.Is(err, ErrRestrictedContent) {
		t.Fatalf("expected restricted content error, got %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exporter.Close(closeCtx); err != nil {
		t.Fatalf("close exporter: %v", err)
	}
	stats := exporter.Stats()
	if stats.Queued != 0 || stats.DroppedRestricted != 1 || sink.callCount() != 0 {
		t.Fatalf("restricted content reached sink: stats=%+v calls=%d", stats, sink.callCount())
	}
}

func TestExporterDoesNotRetryNonRetryableHTTPStatus(t *testing.T) {
	sink := &scriptedSink{responses: []sinkResponse{{
		err: &httpStatusError{statusCode: 400},
	}}}
	exporter, err := New(exporterConfig(), sink)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	if err := exporter.TryExport(fixedSourceEvent()); err != nil {
		t.Fatalf("queue event: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exporter.Close(closeCtx); err != nil {
		t.Fatalf("close exporter: %v", err)
	}
	stats := exporter.Stats()
	if stats.ExportFailures != 1 || stats.Retries != 0 || sink.callCount() != 1 {
		t.Fatalf("unexpected non-retryable result: stats=%+v calls=%d", stats, sink.callCount())
	}
}
