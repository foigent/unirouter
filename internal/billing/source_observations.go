package billing

import (
	"context"
	"log"
	"time"
)

// CommittedObservation identifies an already committed original writer result.
// It carries no request body, Provider credential, upstream ID or monetary copy.
type CommittedObservation struct {
	Kind                          string
	OrgID                         int64
	RequestID, AttemptID, Outcome string
}

type CommittedObservationSink interface {
	CaptureCommitted(context.Context, CommittedObservation) error
}

func (b *Billing) SetCommittedObservationSink(sink CommittedObservationSink) {
	if b != nil {
		b.committedObservationSink = sink
	}
}

// Source failure never changes the original result or authorizes another
// attempt. No worker, retry queue or financial transaction is introduced.
func (b *Billing) observeCommitted(ctx context.Context, observation CommittedObservation) {
	if b == nil || b.committedObservationSink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	if err := b.committedObservationSink.CaptureCommitted(ctx, observation); err != nil {
		log.Printf("[source-observations] optional capture unavailable: kind=%s", observation.Kind)
	}
}
