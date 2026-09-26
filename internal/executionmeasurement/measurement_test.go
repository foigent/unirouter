package executionmeasurement

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestIndependentBoundariesMissingAndFirstSuccessfulOutput(t *testing.T) {
	ctx, c := Start(context.Background())
	SetStreaming(ctx, true)
	before := c.Samples()
	for _, s := range before {
		if s.Kind == ResponseComplete && s.State != "not_applicable" {
			t.Fatal(s)
		}
		if s.Kind == FirstOutput && (s.State != "not_observed" || s.Nanoseconds != nil) {
			t.Fatal("headers/heartbeat must not become first output")
		}
	}
	end := HTTPStart(ctx, "attempt-1")
	end(200)
	var workers sync.WaitGroup
	for i := 0; i < 50; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); FirstOutputFlushed(ctx) }()
	}
	workers.Wait()
	TerminalFlushed(ctx)
	samples := c.Samples()
	if len(samples) != 4 {
		t.Fatalf("per-frame growth: %d", len(samples))
	}
	var first, terminal int64
	for _, s := range samples {
		if s.Kind != ResponseComplete && (s.State != "observed" || s.Nanoseconds == nil || *s.Nanoseconds < 0) {
			t.Fatal(s)
		}
		if s.Kind == FirstOutput {
			first = *s.Nanoseconds
		}
		if s.Kind == StreamComplete {
			terminal = *s.Nanoseconds
		}
	}
	if terminal < first {
		t.Fatal("terminal precedes first output")
	}
	for i := range samples {
		if samples[i].Nanoseconds != nil {
			*samples[i].Nanoseconds = -100
		}
	}
	for _, s := range c.Samples() {
		if s.Nanoseconds != nil && *s.Nanoseconds < 0 {
			t.Fatal("export mutated collector")
		}
	}
}

func TestAcceptedTimingVectorsUseIndependentLocalBoundaries(t *testing.T) {
	for _, stream := range []bool{false, true} {
		ctx, c := Start(context.Background())
		SetStreaming(ctx, stream)
		base := time.Now()
		at := base
		c.start, c.now = base, func() time.Time { return at }
		advance := func(ms int) { at = base.Add(time.Duration(ms) * time.Millisecond) }
		if stream {
			advance(2000) // Original stream processing starts; not the new request clock.
			advance(2200) // First frame prepared; original TTFT is 2200-2000=200.
			advance(2250)
			FirstOutputFlushed(ctx)
			advance(6500) // The old log boundary cannot complete local delivery.
			for _, s := range c.Samples() {
				if s.Kind == StreamComplete && s.State != "not_observed" {
					t.Fatal(s)
				}
			}
			advance(6800)
			TerminalFlushed(ctx)
		} else {
			advance(3000)
			ResponseWritten(ctx)
			advance(5200) // Later settlement/log work cannot extend the recorded duration.
			ResponseWritten(ctx)
		}
		for _, s := range c.Samples() {
			want := map[string]int64{ResponseComplete: 3000, FirstOutput: 2250, StreamComplete: 6800}[s.Kind]
			applicable := stream && s.Kind != ResponseComplete || !stream && s.Kind == ResponseComplete
			if applicable {
				if s.State != "observed" || s.Nanoseconds == nil || *s.Nanoseconds != want*int64(time.Millisecond) {
					t.Fatalf("stream=%v sample=%+v want_ms=%d", stream, s, want)
				}
			} else if s.State != "not_applicable" || s.Nanoseconds != nil {
				t.Fatal(s)
			}
		}
	}
}

func TestHTTPRoundTripsRemainPerActualAttempt(t *testing.T) {
	ctx, c := Start(context.Background())
	base := time.Now()
	at := base
	c.now = func() time.Time { return at }
	first := HTTPStart(ctx, "attempt-one")
	at = base.Add(20 * time.Millisecond)
	first(503)
	first(200) // A repeated callback cannot change the original response.
	at = base.Add(25 * time.Millisecond)
	second := HTTPStart(ctx, "attempt-two")
	at = base.Add(75 * time.Millisecond)
	second(200)
	count := 0
	for _, s := range c.Samples() {
		if s.Kind != HTTPRoundTrip {
			continue
		}
		count++
		wantDuration, wantStatus := int64(20*time.Millisecond), 503
		if s.AttemptID == "attempt-two" {
			wantDuration, wantStatus = int64(50*time.Millisecond), 200
		}
		if s.Nanoseconds == nil || *s.Nanoseconds != wantDuration || s.HTTPStatus != wantStatus {
			t.Fatal(s)
		}
	}
	if count != 2 {
		t.Fatalf("got %d actual attempts", count)
	}
}

func TestZeroAndUnknownRemainDifferentAndRequestsAreIsolated(t *testing.T) {
	ctx, c := Start(context.Background())
	SetStreaming(ctx, false)
	c.record(ResponseComplete, "", 0)
	otherCtx, other := Start(context.Background())
	SetStreaming(otherCtx, false)
	for _, s := range c.Samples() {
		if s.Kind == ResponseComplete && (s.State != "observed" || s.Nanoseconds == nil || *s.Nanoseconds != 0) {
			t.Fatal(s)
		}
	}
	for _, s := range other.Samples() {
		if s.Kind == ResponseComplete && (s.State != "not_observed" || s.Nanoseconds != nil) {
			t.Fatal("cross-request timing")
		}
	}
	FirstOutputFlushed(nil)
	ResponseWritten(context.Background())
	HTTPStart(context.Background(), "ignored")(0)
}
