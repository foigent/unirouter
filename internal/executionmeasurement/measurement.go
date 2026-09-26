// Package executionmeasurement captures process-local monotonic intervals at
// protocol boundaries. It retains neither payloads nor per-frame observations.
package executionmeasurement

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	ResponseComplete = "request_response_complete"
	FirstOutput      = "stream_first_valid_output"
	StreamComplete   = "stream_complete"
	HTTPRoundTrip    = "attempt_http_round_trip"
)

type Sample struct {
	HTTPStatus             int
	ObservedAt             time.Time
	Kind, AttemptID, State string
	Nanoseconds            *int64
}

type Collector struct {
	mu      sync.Mutex
	start   time.Time
	now     func() time.Time
	mode    string
	samples map[string]Sample
}
type collectorKey struct{}
type attemptKey struct{}

func Start(ctx context.Context) (context.Context, *Collector) {
	c := &Collector{start: time.Now(), now: time.Now, samples: map[string]Sample{}}
	return context.WithValue(ctx, collectorKey{}, c), c
}
func collector(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(collectorKey{}).(*Collector)
	return c
}
func WithAttempt(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, attemptKey{}, id)
}

func SetStreaming(ctx context.Context, streaming bool) {
	if c := collector(ctx); c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if streaming {
			c.mode = "stream"
		} else {
			c.mode = "nonstream"
		}
	}
}
func (c *Collector) record(kind, attempt string, nanos int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := kind + "\x00" + attempt
	if _, exists := c.samples[key]; exists {
		return
	}
	s := Sample{Kind: kind, AttemptID: attempt, State: "observed", Nanoseconds: &nanos, ObservedAt: c.now()}
	if nanos < 0 {
		s.State = "invalid"
		s.Nanoseconds = nil
	}
	c.samples[key] = s
}
func HTTPStart(ctx context.Context, attempt string) func(int) {
	c := collector(ctx)
	if c == nil {
		return func(int) {}
	}
	if attempt == "" {
		attempt, _ = ctx.Value(attemptKey{}).(string)
	}
	if attempt == "" {
		return func(int) {}
	}
	started := c.now()
	var once sync.Once
	return func(status int) {
		once.Do(func() {
			nanos := c.now().Sub(started).Nanoseconds()
			c.mu.Lock()
			defer c.mu.Unlock()
			key := HTTPRoundTrip + "\x00" + attempt
			if _, exists := c.samples[key]; exists {
				return
			}
			sample := Sample{Kind: HTTPRoundTrip, AttemptID: attempt, State: "observed", Nanoseconds: &nanos, HTTPStatus: status, ObservedAt: c.now()}
			if nanos < 0 {
				sample.State = "invalid"
				sample.Nanoseconds = nil
			}
			c.samples[key] = sample
		})
	}
}

func ResponseWritten(ctx context.Context) {
	if c := collector(ctx); c != nil {
		c.record(ResponseComplete, "", c.now().Sub(c.start).Nanoseconds())
	}
}
func FirstOutputFlushed(ctx context.Context) {
	if c := collector(ctx); c != nil {
		c.record(FirstOutput, "", c.now().Sub(c.start).Nanoseconds())
	}
}
func TerminalFlushed(ctx context.Context) {
	if c := collector(ctx); c != nil {
		c.record(StreamComplete, "", c.now().Sub(c.start).Nanoseconds())
	}
}

// Samples returns at most three request records plus one record per original
// dispatched attempt. No body chunk, heartbeat or trace grows this collection.
func (c *Collector) Samples() []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Sample, 0, len(c.samples)+3)
	for _, kind := range []string{ResponseComplete, FirstOutput, StreamComplete} {
		s, ok := c.samples[kind+"\x00"]
		if !ok {
			s = Sample{Kind: kind, State: "not_observed"}
			if c.mode == "stream" && kind == ResponseComplete || c.mode == "nonstream" && kind != ResponseComplete {
				s.State = "not_applicable"
			}
		}
		result = append(result, s)
	}
	for _, s := range c.samples {
		if s.Kind == HTTPRoundTrip {
			result = append(result, s)
		}
	}
	slices.SortFunc(result, func(a, b Sample) int { return strings.Compare(a.Kind+"\x00"+a.AttemptID, b.Kind+"\x00"+b.AttemptID) })
	for i := range result {
		if result[i].Nanoseconds != nil {
			value := *result[i].Nanoseconds
			result[i].Nanoseconds = &value
		}
	}
	return result
}
