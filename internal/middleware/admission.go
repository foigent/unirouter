package middleware

import (
	"net/http"
	"sync"
)

// AdmissionController enforces per-org concurrency limits and global concurrency limits.
type AdmissionController struct {
	mu sync.Mutex

	// Per-org limits
	orgLimits map[int]*semaphore
	orgMax    int

	// Global limits
	globalMax int
	globalSem *semaphore
}

type semaphore struct {
	ch chan struct{}
}

func newSemaphore(max int) *semaphore {
	return &semaphore{ch: make(chan struct{}, max)}
}

func (s *semaphore) acquire() {
	s.ch <- struct{}{}
}

func (s *semaphore) release() {
	<-s.ch
}

func (s *semaphore) available() int {
	return cap(s.ch) - len(s.ch)
}

// NewAdmissionController creates a new admission controller.
func NewAdmissionController(globalMax, perOrgMax int) *AdmissionController {
	return &AdmissionController{
		orgLimits: make(map[int]*semaphore),
		orgMax:    perOrgMax,
		globalMax: globalMax,
		globalSem: newSemaphore(globalMax),
	}
}

// TryAcquire attempts to acquire a slot for the given org.
// Returns true if acquired, false if limits exceeded.
func (ac *AdmissionController) TryAcquire(orgID int) bool {
	// Try global first (non-blocking)
	select {
	case ac.globalSem.ch <- struct{}{}:
		// Global slot acquired
	default:
		// Global limit reached
		return false
	}

	// Try per-org (non-blocking)
	ac.mu.Lock()
	sem, exists := ac.orgLimits[orgID]
	if !exists {
		sem = newSemaphore(ac.orgMax)
		ac.orgLimits[orgID] = sem
	}
	ac.mu.Unlock()

	select {
	case sem.ch <- struct{}{}:
		// Org slot acquired
		return true
	default:
		// Org limit reached, release global slot
		<-ac.globalSem.ch
		return false
	}
}

// Release releases a slot for the given org.
func (ac *AdmissionController) Release(orgID int) {
	ac.mu.Lock()
	sem, exists := ac.orgLimits[orgID]
	ac.mu.Unlock()

	if exists {
		sem.release()
	}
	ac.globalSem.release()
}

// Stats returns current admission controller statistics.
func (ac *AdmissionController) Stats() (globalAvailable int, orgAvailable map[int]int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	orgAvailable = make(map[int]int)
	for orgID, sem := range ac.orgLimits {
		orgAvailable[orgID] = sem.available()
	}

	return ac.globalSem.available(), orgAvailable
}

// AdmissionMiddleware creates HTTP middleware that enforces concurrency limits.
func AdmissionMiddleware(ac *AdmissionController, orgIDExtractor func(*http.Request) int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID := orgIDExtractor(r)

			if !ac.TryAcquire(orgID) {
				http.Error(w, `{"error":{"type":"rate_limit.exceeded","message":"Too many concurrent requests"}}`, http.StatusTooManyRequests)
				RecordBillingFreeze("admission_rejected")
				return
			}
			defer ac.Release(orgID)

			next.ServeHTTP(w, r)
		})
	}
}

// NewDefaultAdmissionController creates an admission controller with sensible defaults.
func NewDefaultAdmissionController() *AdmissionController {
	return NewAdmissionController(100, 10) // 100 global, 10 per org
}

// NewProductionAdmissionController creates an admission controller for production.
func NewProductionAdmissionController() *AdmissionController {
	return NewAdmissionController(500, 50) // 500 global, 50 per org
}
