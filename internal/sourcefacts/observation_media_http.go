package sourcefacts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func (s *Service) mediaRouteID(job mediajob.Job, route mediajob.SourceMediaRoute) string {
	return sourceID("unirouter.media-route.v1", s.mediaAttemptID(job, job.Attempt), strconv.Itoa(route.Sequence), executioncontext.ID(route.ChannelID), executioncontext.ID(route.ChannelKeyID))
}
func (s *Service) mediaRouteObservation(job mediajob.Job, route mediajob.SourceMediaRoute, profile, kind string, at time.Time) Observation {
	d := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, profile, "media_attempt", s.mediaRouteID(job, route), kind, at)
	d.ContextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, RequestID: job.RequestID, Profile: ContextProfile, Stage: "admission"}).factID()
	return d
}
func validateMediaRoute(job mediajob.Job, route mediajob.SourceMediaRoute) error {
	if job.Attempt <= 0 || job.Attempt > job.MaxAttempts || route.Sequence < 1 || route.Sequence > route.Total || route.Total > mediajob.MaxSourceMediaRoutes || route.ChannelID <= 0 || route.ChannelKeyID <= 0 {
		return errors.New("invalid media route observation")
	}
	return nil
}
func (s *Service) BeforeMediaRoute(ctx context.Context, job mediajob.Job, route mediajob.SourceMediaRoute) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if err := validateMediaRoute(job, route); err != nil {
		return err
	}
	origin, err := DisclosureOrigin(route.Origin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='100ms';SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	// This is a source registration dependency, not a second dispatch authority.
	// The original AttachExecutionReservation already owns the actual job fence.
	initial := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, AttemptProfile, "media_attempt", s.mediaAttemptID(job, job.Attempt), "media_attempt_registered", time.Now())
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_facts WHERE source_fact_id=$1 AND revision=1)`, initial.SourceFactID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("media dispatch source registration missing")
	}
	at := time.Now()
	a := s.mediaRouteObservation(job, route, AttemptProfile, "attempt_registered", at)
	a.CaptureExpected = []string{"attempt_registered", "provider_response_received", "provider_rejected", "provider_outcome_unknown", "round_trip_measurement"}
	a.Attempt = &AttemptObservation{AttemptID: a.SubjectID, Operation: job.Operation, RouteSequence: strconv.Itoa(route.Sequence), RouteTotal: strconv.Itoa(route.Total), Provider: "registered", LocalDelivery: "not_applicable", Financial: "not_applicable", EvidenceRef: initial.SourceFactID}
	c := s.mediaRouteObservation(job, route, CapabilityProfile, "selection", at)
	ref := func(id, version string) ProfileReference {
		if id == "" {
			id = "not_available"
		}
		if version == "" {
			version = "not_available"
		}
		return ProfileReference{ID: id, Version: version}
	}
	// The legacy media router has no per-route execution EvidenceV1. Preserve
	// the exact selected endpoint profile; do not manufacture Release/Probe pins.
	c.Coverage = "partial"
	c.Capability = &CapabilityObservation{ModelRef: fmt.Sprintf("model-name-sha256:%x", sha256.Sum256([]byte(route.Model))), Provider: route.Provider, Operation: job.Operation, Surface: route.Surface, Protocol: ref(route.ProtocolProfile, route.ProtocolVersion), ExecutionProfile: ref(route.ExecutionProfile, route.ExecutionVersion), ReleaseRef: route.ReleaseRef, EvidenceState: "partial", RequiredCapabilities: []string{}, Capabilities: []CapabilityReference{}}
	dest := s.mediaRouteObservation(job, route, DestinationProfile, "dispatch_registered", at)
	dest.CaptureExpected = []string{"dispatch_registered", "dispatch_result"}
	dest.Destination = &DestinationObservation{Provider: route.Provider, Origin: origin, RegionSource: "not_available", Dispatch: "registered", PolicyEvidence: "not_applicable", InputBindingState: "unknown"}
	if err = s.publishTogetherTx(ctx, tx, nil, []Observation{a, c, dest}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) CaptureMediaRoutes(ctx context.Context, job mediajob.Job, samples []mediajob.SourceMediaHTTP) error {
	if !s.ObservationsEnabled() || len(samples) == 0 {
		return nil
	}
	if len(samples) > mediajob.MaxSourceMediaRoutes {
		return errors.New("media HTTP observation limit")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;SET LOCAL lock_timeout='100ms';SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	var attempt int
	var state, outcome string
	if err = tx.QueryRow(ctx, `SELECT attempt,execution_state,provider_outcome FROM async_media_jobs WHERE job_id=$1 AND org_id=$2 AND project_id=$3`, job.JobID, job.Scope.OrgID, job.Scope.ProjectID).Scan(&attempt, &state, &outcome); err != nil {
		return err
	}
	if attempt != job.Attempt || state == mediajob.ExecutionRunning && outcome == mediajob.OutcomePending {
		return errors.New("media original result has not committed for this attempt")
	}
	docs := make([]Observation, 0, 3*len(samples))
	seen := map[string]bool{}
	for _, sample := range samples {
		if err = validateMediaRoute(job, sample.Route); err != nil {
			return err
		}
		id := s.mediaRouteID(job, sample.Route)
		if seen[id] || sample.ObservedAt.IsZero() || sample.Status < 0 || sample.Status > 599 {
			return errors.New("invalid repeated media HTTP observation")
		}
		seen[id] = true
		kind, provider := "provider_outcome_unknown", "unknown"
		if sample.Status >= 100 {
			kind, provider = "provider_response_received", "response_received"
			if sample.Status >= 300 {
				kind, provider = "provider_rejected", "rejected"
			}
		}
		a := s.mediaRouteObservation(job, sample.Route, AttemptProfile, kind, sample.ObservedAt)
		a.Attempt = &AttemptObservation{AttemptID: id, Operation: job.Operation, RouteSequence: strconv.Itoa(sample.Route.Sequence), RouteTotal: strconv.Itoa(sample.Route.Total), Provider: provider, LocalDelivery: "not_applicable", Financial: "not_applicable", EvidenceRef: job.JobID}
		if sample.Status >= 100 {
			a.Attempt.HTTPStatus = strconv.Itoa(sample.Status)
		}
		m := s.mediaRouteObservation(job, sample.Route, AttemptProfile, "round_trip_measurement", sample.ObservedAt)
		value := strconv.FormatInt(sample.RoundTripNanoseconds, 10)
		m.Measurement = &Measurement{Kind: "attempt_http_round_trip", Version: "1", Unit: "nanoseconds", State: "observed", Value: &value, StartBoundary: "http_do_called", EndBoundary: "http_do_returned", Resolution: "nanosecond_representation", Source: "process_monotonic_clock", EvidenceRef: id}
		if sample.RoundTripNanoseconds < 0 {
			m.Measurement.State = "invalid"
			m.Measurement.Value = nil
			m.Coverage = "not_observed"
		}
		origin, e := DisclosureOrigin(sample.Route.Origin)
		if e != nil {
			return e
		}
		d := s.mediaRouteObservation(job, sample.Route, DestinationProfile, "dispatch_result", sample.ObservedAt)
		dispatch := "do_observed"
		if sample.Status >= 100 {
			dispatch = "response_observed"
		}
		d.Destination = &DestinationObservation{Provider: sample.Route.Provider, Origin: origin, RegionSource: "not_available", Dispatch: dispatch, PolicyEvidence: "not_applicable", InputBindingState: "unknown"}
		docs = append(docs, a, m, d)
	}
	if err = s.publishTogetherTx(ctx, tx, nil, docs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
