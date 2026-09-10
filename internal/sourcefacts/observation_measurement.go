package sourcefacts

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
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
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
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
		if profile == AttemptProfile {
			if err = tx.QueryRow(ctx, `SELECT COALESCE(route_evidence_hash,'') FROM billing_provider_attempts WHERE org_id=$1 AND project_id=$2 AND request_id=$3 AND attempt_id=$4`, org, project, snapshot.Core.RequestID, subjectID).Scan(&d.RouteEvidenceHash); err != nil {
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
			// The initial, immutable projection supplies the selected origin.
			// A current connection URL can never reconstruct past disclosure.
			registered := s.observation(org, project, snapshot.Core.RequestID, DestinationProfile, "provider_attempt", subjectID, "dispatch_registered", at)
			var wire []byte
			var hash string
			err = tx.QueryRow(ctx, `SELECT document,source_hash FROM execution_source_facts WHERE source_fact_id=$1 AND revision=1`, registered.SourceFactID).Scan(&wire, &hash)
			var destination Observation
			if errors.Is(err, pgx.ErrNoRows) {
				var found bool
				destination, found, err = s.pendingDispatchDestination(ctx, tx, org, project, snapshot.Core.RequestID, subjectID)
				if err != nil {
					return err
				}
				if !found {
					continue
				}
			} else {
				if err != nil {
					return err
				}
				if eventconnector.HashSourceBytes(wire) != hash {
					return errors.New("selected destination source hash differs")
				}
				destination, err = DecodeObservation(wire)
				if err != nil {
					return err
				}
			}
			if destination.SourceFactID != registered.SourceFactID || destination.Destination == nil {
				return errors.New("selected destination binding differs")
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
	return tx.Commit(ctx)
}
