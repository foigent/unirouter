package circuitbreaker

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // Normal operation
	StateOpen                  // Failing, rejecting requests
	StateHalfOpen              // Testing recovery
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// Breaker implements the circuit breaker pattern.
type Breaker struct {
	failThreshold int
	recoverAfter  time.Duration
	halfOpenPct   int
	failCount     int
	state         State
	lastFail      time.Time
}

// NewBreaker creates a new circuit breaker.
func NewBreaker(failThreshold int, recoverAfter time.Duration, halfOpenPct int) *Breaker {
	return &Breaker{
		failThreshold: failThreshold,
		recoverAfter:  recoverAfter,
		halfOpenPct:   halfOpenPct,
		state:         StateClosed,
	}
}

// Allow checks if a request should be allowed.
func (b *Breaker) Allow() bool {
	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.lastFail) > b.recoverAfter {
			b.state = StateHalfOpen
			log.Printf("[circuit] transitioning to half_open after %v", b.recoverAfter)
			return b.allowHalfOpen()
		}
		return false
	case StateHalfOpen:
		return b.allowHalfOpen()
	}
	return false
}

// allowHalfOpen samples requests in half-open state based on halfOpenPct.
// Returns true if this request should be allowed through.
func (b *Breaker) allowHalfOpen() bool {
	if b.halfOpenPct <= 0 || b.halfOpenPct >= 100 {
		return true
	}
	return rand.Intn(100) < b.halfOpenPct
}

// RecordSuccess records a successful request.
func (b *Breaker) RecordSuccess() {
	switch b.state {
	case StateHalfOpen:
		b.state = StateClosed
		b.failCount = 0
		log.Printf("[circuit] recovered, transitioning to closed")
	case StateClosed:
		b.failCount = 0
	}
}

// RecordFailure records a failed request.
func (b *Breaker) RecordFailure() {
	b.failCount++
	b.lastFail = time.Now()

	if b.state == StateHalfOpen {
		b.state = StateOpen
		log.Printf("[circuit] half_open failed, transitioning to open")
		return
	}

	if b.failCount >= b.failThreshold {
		b.state = StateOpen
		log.Printf("[circuit] threshold %d reached, transitioning to open", b.failThreshold)
	}
}

// GetState returns the current state.
func (b *Breaker) GetState() State {
	return b.state
}

// Reset resets the breaker to closed state.
func (b *Breaker) Reset() {
	b.state = StateClosed
	b.failCount = 0
}

// WrapTransport wraps an http.RoundTripper with circuit breaker logic.
func (b *Breaker) WrapTransport(inner http.RoundTripper) http.RoundTripper {
	return &breakerTransport{breaker: b, inner: inner}
}

type breakerTransport struct {
	breaker *Breaker
	inner   http.RoundTripper
}

func (t *breakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.breaker.Allow() {
		return nil, fmt.Errorf("circuit breaker is open")
	}

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		t.breaker.RecordFailure()
		return nil, err
	}

	if resp.StatusCode >= 500 {
		t.breaker.RecordFailure()
	} else {
		t.breaker.RecordSuccess()
	}

	return resp, nil
}
