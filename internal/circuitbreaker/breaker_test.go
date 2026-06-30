package circuitbreaker

import (
	"testing"
	"time"
)

func TestBreaker_InitialState(t *testing.T) {
	b := NewBreaker(3, 1*time.Second, 50)
	if b.state != StateClosed {
		t.Fatalf("initial state should be closed, got %v", b.state)
	}
}

func TestBreaker_ClosedAllowsRequests(t *testing.T) {
	b := NewBreaker(3, 1*time.Second, 50)
	if !b.Allow() {
		t.Fatal("closed breaker should allow requests")
	}
}

func TestBreaker_OpenAfterThreshold(t *testing.T) {
	b := NewBreaker(3, 1*time.Hour, 50)
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if b.state != StateOpen {
		t.Fatalf("should be open after 3 failures, got %v", b.state)
	}
	if b.Allow() {
		t.Fatal("open breaker should reject requests")
	}
}

func TestBreaker_HalfOpenAfterRecovery(t *testing.T) {
	b := NewBreaker(2, 50*time.Millisecond, 100)
	b.RecordFailure()
	b.RecordFailure()
	if b.state != StateOpen {
		t.Fatalf("should be open, got %v", b.state)
	}

	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("should transition to half-open and allow")
	}
	if b.state != StateHalfOpen {
		t.Fatalf("should be half-open, got %v", b.state)
	}
}

func TestBreaker_HalfOpenSuccess(t *testing.T) {
	b := NewBreaker(2, 50*time.Millisecond, 100)
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	b.Allow() // transitions to half-open

	b.RecordSuccess()
	if b.state != StateClosed {
		t.Fatalf("should recover to closed, got %v", b.state)
	}
}

func TestBreaker_HalfOpenFailure(t *testing.T) {
	b := NewBreaker(2, 50*time.Millisecond, 100)
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	b.Allow() // transitions to half-open

	b.RecordFailure()
	if b.state != StateOpen {
		t.Fatalf("should return to open on half-open failure, got %v", b.state)
	}
}

func TestBreaker_ClosedResetsFailCount(t *testing.T) {
	b := NewBreaker(5, 1*time.Hour, 50)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	if b.failCount != 0 {
		t.Fatalf("success should reset fail count, got %d", b.failCount)
	}
}

func TestBreaker_BelowThreshold(t *testing.T) {
	b := NewBreaker(5, 1*time.Hour, 50)
	b.RecordFailure()
	b.RecordFailure()
	if b.state != StateClosed {
		t.Fatalf("should stay closed below threshold, got %v", b.state)
	}
	if !b.Allow() {
		t.Fatal("should still allow below threshold")
	}
}

func TestState_String(t *testing.T) {
	cases := map[State]string{
		StateClosed:   "closed",
		StateOpen:     "open",
		StateHalfOpen: "half_open",
		State(99):     "unknown",
	}
	for state, expected := range cases {
		if state.String() != expected {
			t.Fatalf("State(%d).String() = %q, want %q", state, state.String(), expected)
		}
	}
}
