package mediajob

import (
	"context"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/limitpolicy"
)

type mediaLimitRuntimeFixture struct {
	mu             sync.Mutex
	reserveErr     error
	reserveCalls   []limitpolicy.AdmissionRequest
	finalizeCalls  int
	cancelCalls    int
	preCancelCalls int
}

func (r *mediaLimitRuntimeFixture) Reserve(
	_ context.Context,
	request limitpolicy.AdmissionRequest,
) (*limitpolicy.Reservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserveCalls = append(r.reserveCalls, request)
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	return &limitpolicy.Reservation{OperationID: request.OperationID}, nil
}

func (r *mediaLimitRuntimeFixture) Finalize(context.Context, *limitpolicy.Reservation, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalizeCalls++
	return nil
}

func (r *mediaLimitRuntimeFixture) Cancel(context.Context, *limitpolicy.Reservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelCalls++
	return nil
}

func (r *mediaLimitRuntimeFixture) CancelBeforeDispatch(context.Context, *limitpolicy.Reservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preCancelCalls++
	return nil
}

func (*mediaLimitRuntimeFixture) TerminalStateTTL(leaseTTL time.Duration) time.Duration {
	return leaseTTL + 10*time.Minute
}
