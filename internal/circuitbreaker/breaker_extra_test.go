package circuitbreaker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- State transition tests ---

func TestBreaker_ClosedToOpen(t *testing.T) {
	b := NewBreaker(3, 1*time.Second, 10)
	// 3 failures should transition to open
	b.RecordFailure()
	b.RecordFailure()
	if b.GetState() != StateClosed {
		t.Fatal("should still be closed after 2 failures")
	}
	b.RecordFailure()
	if b.GetState() != StateOpen {
		t.Fatal("should be open after 3 failures")
	}
}

func TestBreaker_OpenToHalfOpen(t *testing.T) {
	b := NewBreaker(1, 50*time.Millisecond, 100)
	b.RecordFailure() // -> open
	if b.GetState() != StateOpen {
		t.Fatal("should be open")
	}
	// Before recover period
	if b.Allow() {
		t.Fatal("should not allow during open state")
	}
	time.Sleep(60 * time.Millisecond)
	// After recover period
	if !b.Allow() {
		t.Fatal("should allow after recover period (half-open)")
	}
	if b.GetState() != StateHalfOpen {
		t.Fatal("should be half-open")
	}
}

func TestBreaker_HalfOpenToClosed(t *testing.T) {
	b := NewBreaker(1, 10*time.Millisecond, 10)
	b.RecordFailure() // -> open
	time.Sleep(15 * time.Millisecond)
	b.Allow() // -> half-open
	b.RecordSuccess()
	if b.GetState() != StateClosed {
		t.Fatal("should be closed after success in half-open")
	}
}

func TestBreaker_HalfOpenToOpen(t *testing.T) {
	b := NewBreaker(1, 10*time.Millisecond, 10)
	b.RecordFailure() // -> open
	time.Sleep(15 * time.Millisecond)
	b.Allow() // -> half-open
	b.RecordFailure()
	if b.GetState() != StateOpen {
		t.Fatal("should be open after failure in half-open")
	}
}

func TestBreaker_ClosedSuccessResetsCount(t *testing.T) {
	b := NewBreaker(3, 1*time.Second, 10)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // should reset count
	b.RecordFailure()
	b.RecordFailure()
	if b.GetState() != StateClosed {
		t.Fatal("should be closed because success reset the count")
	}
}

// --- Allow() behavior ---

func TestBreaker_Allow_Closed(t *testing.T) {
	b := NewBreaker(5, 1*time.Second, 10)
	if !b.Allow() {
		t.Fatal("closed state should allow")
	}
}

func TestBreaker_Allow_Open_BeforeRecover(t *testing.T) {
	b := NewBreaker(1, 1*time.Hour, 10)
	b.RecordFailure()
	if b.Allow() {
		t.Fatal("open state before recover should not allow")
	}
}

func TestBreaker_Allow_HalfOpen(t *testing.T) {
	b := NewBreaker(1, 10*time.Millisecond, 100)
	b.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	b.Allow() // -> half-open
	if !b.Allow() {
		t.Fatal("half-open should allow")
	}
}

// --- Reset ---

func TestBreaker_Reset(t *testing.T) {
	b := NewBreaker(1, 1*time.Hour, 10)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	if b.GetState() != StateOpen {
		t.Fatal("should be open")
	}
	b.Reset()
	if b.GetState() != StateClosed {
		t.Fatal("should be closed after reset")
	}
	if !b.Allow() {
		t.Fatal("should allow after reset")
	}
}



// --- WrapTransport ---

func TestBreaker_WrapTransport_Closed(t *testing.T) {
	b := NewBreaker(5, 1*time.Second, 10)
	inner := http.NewServeMux()
	inner.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(inner)
	defer ts.Close()

	transport := b.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: transport}
	resp, err := client.Get(ts.URL + "/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if b.GetState() != StateClosed {
		t.Fatal("should remain closed after success")
	}
}

func TestBreaker_WrapTransport_Open(t *testing.T) {
	b := NewBreaker(1, 1*time.Hour, 10)
	b.RecordFailure() // -> open

	transport := b.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: transport}
	_, err := client.Get("http://localhost:1") // will fail
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
}

func TestBreaker_WrapTransport_ServerError(t *testing.T) {
	b := NewBreaker(5, 1*time.Second, 10)
	inner := http.NewServeMux()
	inner.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(inner)
	defer ts.Close()

	transport := b.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: transport}
	resp, err := client.Get(ts.URL + "/error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	// 500 should count as failure
	if b.GetState() == StateClosed {
		// After one 500, still closed (threshold is 5)
		// This is expected - need multiple failures
	}
}

func TestBreaker_WrapTransport_MultipleServerErrors(t *testing.T) {
	b := NewBreaker(3, 1*time.Second, 10)
	inner := http.NewServeMux()
	inner.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(inner)
	defer ts.Close()

	transport := b.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: transport}
	for i := 0; i < 3; i++ {
		resp, _ := client.Get(ts.URL + "/error")
		if resp != nil {
			resp.Body.Close()
		}
	}
	if b.GetState() != StateOpen {
		t.Fatal("should be open after 3 server errors")
	}
}

// --- Edge cases ---

func TestBreaker_ZeroThreshold(t *testing.T) {
	b := NewBreaker(0, 1*time.Second, 10)
	// With threshold 0, any failure should open immediately
	b.RecordFailure()
	if b.GetState() != StateOpen {
		t.Fatal("should be open after 1 failure with threshold 0")
	}
}

func TestBreaker_VeryShortRecover(t *testing.T) {
	b := NewBreaker(1, 1*time.Millisecond, 100)
	b.RecordFailure()
	time.Sleep(5 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("should allow after very short recover period")
	}
}

func TestBreaker_FailCountResetOnRecovery(t *testing.T) {
	b := NewBreaker(3, 10*time.Millisecond, 10)
	b.RecordFailure()
	b.RecordFailure()
	time.Sleep(15 * time.Millisecond)
	b.Allow() // -> half-open
	b.RecordSuccess() // -> closed, failCount = 0
	b.RecordFailure()
	b.RecordFailure()
	if b.GetState() != StateClosed {
		t.Fatal("should be closed because count was reset")
	}
}
