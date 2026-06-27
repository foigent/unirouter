package middleware

import (
	"sync"
	"testing"
)

func TestAdmissionController_BasicAcquireRelease(t *testing.T) {
	ac := NewAdmissionController(10, 5)

	if !ac.TryAcquire(1) {
		t.Fatal("expected acquire to succeed")
	}
	ac.Release(1)

	if !ac.TryAcquire(1) {
		t.Fatal("expected re-acquire to succeed after release")
	}
	ac.Release(1)
}

func TestAdmissionController_PerOrgLimit(t *testing.T) {
	ac := NewAdmissionController(10, 3)

	for i := 0; i < 3; i++ {
		if !ac.TryAcquire(1) {
			t.Fatalf("expected acquire %d to succeed", i+1)
		}
	}

	if ac.TryAcquire(1) {
		t.Fatal("expected 4th acquire to fail (per-org limit)")
	}

	if !ac.TryAcquire(2) {
		t.Fatal("expected acquire for different org to succeed")
	}

	ac.Release(1)

	if !ac.TryAcquire(1) {
		t.Fatal("expected acquire after release to succeed")
	}
}

func TestAdmissionController_GlobalLimit(t *testing.T) {
	ac := NewAdmissionController(5, 100)

	for i := 0; i < 5; i++ {
		if !ac.TryAcquire(i) {
			t.Fatalf("expected acquire %d to succeed", i+1)
		}
	}

	if ac.TryAcquire(100) {
		t.Fatal("expected 6th acquire to fail (global limit)")
	}
}

func TestAdmissionController_ConcurrentAccess(t *testing.T) {
	ac := NewAdmissionController(100, 10)
	var wg sync.WaitGroup
	var mu sync.Mutex
	acquired := make([]bool, 100)

	// Concurrent acquires - only release what was actually acquired
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ok := ac.TryAcquire(id % 5)
			mu.Lock()
			acquired[id] = ok
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Release only slots that were successfully acquired
	for i := 0; i < 100; i++ {
		if acquired[i] {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ac.Release(id % 5)
			}(i)
		}
	}
	wg.Wait()
}

func TestAdmissionController_Stats(t *testing.T) {
	ac := NewAdmissionController(10, 5)

	ac.TryAcquire(1)
	ac.TryAcquire(1)
	ac.TryAcquire(2)

	globalAvail, orgAvail := ac.Stats()

	if globalAvail != 7 {
		t.Errorf("expected 7 global available, got %d", globalAvail)
	}
	if orgAvail[1] != 3 {
		t.Errorf("expected 3 available for org 1, got %d", orgAvail[1])
	}
	if orgAvail[2] != 4 {
		t.Errorf("expected 4 available for org 2, got %d", orgAvail[2])
	}
}
