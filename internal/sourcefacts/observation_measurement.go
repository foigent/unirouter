package sourcefacts

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
)

func (s *Service) CaptureMeasurements(ctx context.Context, snapshot executioncontext.Snapshot, samples []executionmeasurement.Sample) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	org, _ := strconv.ParseInt(snapshot.Core.OrgID, 10, 64)
	project, _ := strconv.ParseInt(snapshot.Core.ProjectID, 10, 64)
	contextErr := ctx.Err()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return captureError("begin", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`); err != nil {
		return captureError("transaction_setup", err)
	}
	documents := make([]Observation, 0, len(samples))
	at := time.Now()
	contextRef := (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
	if contextErr != nil {
		// Context cancellation has no actor identity. In particular it cannot
		// establish a human cancel action or a Provider-side timeout.
		reason := "context_cancelled"
		if errors.Is(contextErr, context.DeadlineExceeded) {
			reason = "context_deadline_exceeded"
		}
		d := s.observation(org, project, snapshot.Core.RequestID, RuntimeProfile, "request", snapshot.Core.RequestID, "request_context_done", at)
		d.ContextRef = contextRef
		d.Runtime = &RuntimeObservation{Operation: snapshot.Core.Operation, StreamMode: "not_observed", Admission: "authenticated", Provider: "unknown", LocalDelivery: "unknown", Financial: "unknown", Capture: "observed", ContextDoneReason: reason}
		documents = append(documents, d)
	}
	for _, sample := range samples {
		profile, subject, subjectID, kind := RuntimeProfile, "request", snapshot.Core.RequestID, ""
		switch sample.Kind {
		case executionmeasurement.ResponseComplete:
			kind = "response_measurement"
		case executionmeasurement.FirstOutput:
			kind = "first_output_measurement"
		case executionmeasurement.StreamComplete:
			kind = "stream_measurement"
		case executionmeasurement.HTTPRoundTrip:
			profile, subject, subjectID, kind = AttemptProfile, "provider_attempt", sample.AttemptID, "round_trip_measurement"
		default:
			return errors.New("unknown monotonic measurement")
		}
		d := s.observation(org, project, snapshot.Core.RequestID, profile, subject, subjectID, kind, at)
		d.ContextRef = contextRef
		if !sample.ObservedAt.IsZero() {
			d.OccurredAt = executioncontext.Time(sample.ObservedAt)
		}
		var destination Observation
		var destinationFound bool
		if profile == AttemptProfile {
			registered := s.observation(org, project, snapshot.Core.RequestID, DestinationProfile, "provider_attempt", subjectID, "dispatch_registered", at)
			d.RouteEvidenceHash, destination, destinationFound, err = s.measurementAttemptSnapshot(ctx, tx, org, project, snapshot.Core.RequestID, subjectID, registered, sample.State == "observed")
			if err != nil {
				return err
			}
		}
		bounds := measurementBoundaries[sample.Kind]
		m := &Measurement{Kind: sample.Kind, Version: "1", Unit: "nanoseconds", State: sample.State, StartBoundary: bounds[0], EndBoundary: bounds[1], Resolution: "nanosecond_representation", Source: "process_monotonic_clock", EvidenceRef: subjectID}
		if sample.Nanoseconds != nil {
			value := strconv.FormatInt(*sample.Nanoseconds, 10)
			m.Value = &value
		}
		d.Measurement = m
		if sample.State != "observed" {
			d.Coverage = "not_observed"
		}
		documents = append(documents, d)
		if profile == AttemptProfile && sample.State == "observed" {
			// Only frozen dispatch bytes from the same SQL snapshot establish
			// the origin; a current route cannot reconstruct past disclosure.
			if !destinationFound {
				continue
			}
			destination.Kind = "dispatch_result"
			destination.SourceFactID = destination.factID()
			destination.EventID = destination.eventID()
			destination.OccurredAt = d.OccurredAt
			destination.RecordedAt = d.RecordedAt
			destination.CaptureExpected = []string{"dispatch_registered", "dispatch_result"}
			destination.Destination.Dispatch = "do_observed"
			if sample.HTTPStatus > 0 {
				destination.Destination.Dispatch = "response_observed"
			}
			documents = append(documents, destination)
		}
	}
	if len(documents) == 0 {
		return nil
	}
	if err = s.publishTogetherTx(ctx, tx, nil, documents); err != nil {
		return err
	}
	return captureError("commit", tx.Commit(ctx))
}
