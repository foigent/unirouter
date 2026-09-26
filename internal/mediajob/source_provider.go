package mediajob

import (
	"context"
	"log"
	"net/http"
	"time"
)

// 128 is the original routing-policy maximum candidate count. Image Generate
// iterates the selected routes; it does not apply the text relay's retry limit.
const MaxSourceMediaRoutes = 128

type SourceMediaRoute struct {
	Sequence, Total                                int
	ChannelID, ChannelKeyID                        int64
	Provider, Origin, Model, Operation             string
	ProtocolProfile, ProtocolVersion, Surface      string
	ExecutionProfile, ExecutionVersion, ReleaseRef string
}
type SourceMediaHTTP struct {
	Route                SourceMediaRoute
	Status               int
	RoundTripNanoseconds int64
	ObservedAt           time.Time
}
type SourceMediaProviderObserver interface {
	ObservationsEnabled() bool
	BeforeMediaRoute(context.Context, Job, SourceMediaRoute) error
	CaptureMediaRoutes(context.Context, Job, []SourceMediaHTTP) error
}

// Content is independent of optional B observations. Implementations verify the
// committed job identity and only affect capture, never provider authorization.
type MediaContentObserver interface {
	ObserveMediaProvider(context.Context, Job, SourceMediaRoute, *http.Request) (*http.Request, func(*http.Response, error))
}

func (p *GPTImageProvider) SetContentObserver(observer MediaContentObserver) {
	p.contentObserver = observer
}

func (p *GPTImageProvider) SetSourceObserver(observer SourceMediaProviderObserver) {
	p.sourceObserver = observer
}
func (p *GPTImageProvider) SourceObservationsEnabled() bool {
	return p != nil && p.sourceObserver != nil && p.sourceObserver.ObservationsEnabled()
}

type sourceMediaSamplesKey struct{}
type sourceMediaSamples struct{ samples []SourceMediaHTTP }

func startSourceMediaSamples(ctx context.Context) (context.Context, *sourceMediaSamples) {
	b := &sourceMediaSamples{}
	return context.WithValue(ctx, sourceMediaSamplesKey{}, b), b
}
func recordSourceMediaHTTP(ctx context.Context, value SourceMediaHTTP) {
	b, _ := ctx.Value(sourceMediaSamplesKey{}).(*sourceMediaSamples)
	if b != nil && len(b.samples) < MaxSourceMediaRoutes {
		b.samples = append(b.samples, value)
	}
}

// This runs after the original worker branch has attempted its durable result.
// The source reader verifies that original result before publishing observations.
func (s *PostgresStore) CaptureSourceMediaRoutes(ctx context.Context, job Job, values []SourceMediaHTTP) {
	observer, ok := s.sourceMaterializer.(SourceMediaProviderObserver)
	if !ok || !observer.ObservationsEnabled() || len(values) == 0 {
		return
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	if err := observer.CaptureMediaRoutes(observed, job, values); err != nil {
		log.Printf("[source-observations] optional media HTTP observations unavailable")
	}
}
