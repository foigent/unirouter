package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

// CaptureCommitted projects a durable original result in a separate bounded
// source transaction. Caller errors are diagnostic only, never business retries.
func (s *Service) CaptureCommitted(ctx context.Context, event billing.CommittedObservation) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if !oneOf(event.Kind, "provider_result", "local_delivery", "output_hold") {
		return errors.New("unknown committed source boundary")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	var project int64
	var sequence, total int
	var operation, hash, state string
	var reservationState, pendingReason string
	var retry bool
	var status int
	var at time.Time
	var contextWire []byte
	var receiptMode, receiptOutcome, receiptError string
	var receiptAt *time.Time
	// Original attempt/reservation and the latest receipt share one snapshot.
	// A receipt for an older outcome is not used to reconstruct an advanced event.
	err = tx.QueryRow(ctx, `SELECT p.project_id,p.route_sequence,p.route_total,COALESCE(p.route_model_operation_id,''),COALESCE(p.route_evidence_hash,''),p.state,p.retry_authorized,COALESCE(p.http_status,0),p.updated_at,r.source_context,r.status,COALESCE(r.settlement_pending_reason,''),
	 COALESCE(d.mode,''),COALESCE(d.outcome,''),COALESCE(d.error_code,''),d.occurred_at
	 FROM billing_provider_attempts p JOIN freeze_idempotency r ON r.org_id=p.org_id AND r.request_id=p.request_id AND r.project_id=p.project_id
	 LEFT JOIN LATERAL (SELECT mode,outcome,error_code,occurred_at FROM request_delivery_receipts
	 WHERE org_id=p.org_id AND request_id=p.request_id AND attempt_id=p.attempt_id AND $4<>'provider_result'
	 ORDER BY sequence DESC LIMIT 1) d ON true
	 WHERE p.org_id=$1 AND p.request_id=$2 AND p.attempt_id=$3`, event.OrgID, event.RequestID, event.AttemptID, event.Kind).Scan(&project, &sequence, &total, &operation, &hash, &state, &retry, &status, &at, &contextWire, &reservationState, &pendingReason, &receiptMode, &receiptOutcome, &receiptError, &receiptAt)
	if err != nil {
		return err
	}
	if project <= 0 {
		return nil
	}
	if operation == "" {
		operation = "not_available"
	}
	contextRef := ""
	if len(contextWire) > 0 {
		var snapshot executioncontext.Snapshot
		if err = json.Unmarshal(contextWire, &snapshot); err != nil {
			return err
		}
		if err = snapshot.Validate(); err != nil {
			return err
		}
		if snapshot.Core.OrgID != executioncontext.ID(event.OrgID) || snapshot.Core.ProjectID != executioncontext.ID(project) || snapshot.Core.RequestID != event.RequestID {
			return errors.New("committed observation scope differs")
		}
		contextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: event.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
	}
	kind, provider, financial := "", "unknown", "unknown"
	mode, delivery := "not_observed", "unknown"
	if event.Kind == "provider_result" {
		if state != event.Outcome {
			return errors.New("committed provider boundary advanced before observation")
		}
		switch event.Outcome {
		case billing.ProviderAttemptOutcomeResponseAccepted:
			kind, provider = "provider_response_received", "response_received"
		case billing.ProviderAttemptOutcomeRejected:
			kind, provider = "provider_rejected", "rejected"
		case billing.ProviderAttemptOutcomeUnknown:
			kind, financial = "provider_outcome_unknown", "pending"
		case billing.ProviderAttemptOutcomeResponseUnusable:
			kind, provider, financial = "provider_response_unusable", "response_received", "pending"
		default:
			return errors.New("unknown original provider result")
		}
	} else if event.Kind == "output_hold" {
		if state != event.Outcome || reservationState != "settlement_pending" || !oneOf(state, billing.ProviderAttemptStateOutputWithheld, "delivery_failed") || !oneOf(pendingReason, billing.OutputHoldReasonGuardrailWithheld, billing.OutputHoldReasonResponseTooLarge, billing.OutputHoldReasonProviderResponseUnusable, billing.OutputHoldReasonProviderAttemptUnresolved) {
			return errors.New("original output hold advanced before observation")
		}
		kind, provider, financial, retry = "output_held", "response_received", "pending", false
		if state == billing.ProviderAttemptStateOutputWithheld {
			mode, delivery = "non_streaming", "not_started"
		} else {
			mode = "streaming"
			if receiptAt == nil || receiptMode != billing.DeliveryModeStream || receiptError != billing.ProviderOutputLimitExceeded || !oneOf(receiptOutcome, billing.DeliveryOutcomeInterrupted, billing.DeliveryOutcomeWriteFailed) {
				return errors.New("original stream output hold receipt unavailable")
			}
			delivery, at = receiptOutcome, *receiptAt
		}
	} else {
		if receiptAt == nil || receiptOutcome != event.Outcome {
			return errors.New("committed delivery boundary advanced or unavailable")
		}
		originalMode := receiptMode
		delivery, at = receiptOutcome, *receiptAt
		if originalMode == billing.DeliveryModeStream {
			mode = "streaming"
		} else if originalMode == billing.DeliveryModeNonStream {
			mode = "non_streaming"
		} else {
			return errors.New("unknown original delivery mode")
		}
		if delivery == billing.DeliveryOutcomeWriteStarted {
			kind = "delivery_started"
		} else if oneOf(delivery, billing.DeliveryOutcomeCompleted, billing.DeliveryOutcomeWriteFailed, billing.DeliveryOutcomeInterrupted) {
			kind = "local_delivery"
		} else {
			return nil
		}
		retry = false
	}
	makeObservation := func(profile, subjectKind, subject string) Observation {
		d := s.observation(event.OrgID, project, event.RequestID, profile, subjectKind, subject, kind, at)
		d.ContextRef = contextRef
		d.RouteEvidenceHash = hash
		if contextRef == "" {
			d.Coverage = "partial"
		}
		return d
	}
	d := makeObservation(AttemptProfile, "provider_attempt", event.AttemptID)
	d.Attempt = &AttemptObservation{AttemptID: event.AttemptID, Operation: operation, RouteSequence: strconv.Itoa(sequence), RouteTotal: strconv.Itoa(total), Provider: provider, LocalDelivery: delivery, Financial: financial, RetryAuthorized: retry, EvidenceRef: event.AttemptID}
	if status > 0 {
		d.Attempt.HTTPStatus = strconv.Itoa(status)
	}
	observations := []Observation{d}
	if event.Kind == "local_delivery" || event.Kind == "output_hold" {
		runtime := makeObservation(RuntimeProfile, "request", event.RequestID)
		runtime.Runtime = &RuntimeObservation{Operation: operation, StreamMode: mode, Admission: "admitted", Provider: provider, LocalDelivery: delivery, Financial: financial, Capture: "partial", EvidenceRef: event.AttemptID}
		runtime.Coverage = "partial"
		observations = append(observations, runtime)
	}
	if err = s.publishTogetherTx(ctx, tx, nil, observations); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
