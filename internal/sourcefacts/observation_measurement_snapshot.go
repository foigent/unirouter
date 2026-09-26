package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/outbox"
)

// The attempt and both sides of dispatch publication/ack are read by one SQL
// statement. RC may advance between statements, never between these sources.
// The pending input is bounded to two rows so duplicate inputs fail closed.
const measurementAttemptSnapshotSQL = `SELECT COALESCE(p.route_evidence_hash,''), f.document, COALESCE(f.source_hash,''),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('Key',e.event_key,'Type',e.event_type,
 'Priority',e.priority,'OrgID',e.org_id,'RequestID',e.request_id,'Payload',e.payload,'OccurredAt',e.occurred_at))
 FROM (SELECT event_key,event_type,priority,org_id,request_id,payload,occurred_at
 FROM internal_outbox_events WHERE event_type=$6 AND org_id=$1 AND request_id=$3
 AND $9 AND payload->>'attempt_id'=$4 AND payload->>'source_instance'=$7 AND payload->>'project_id'=$8 LIMIT 2) e),'[]'::jsonb)
 FROM billing_provider_attempts p LEFT JOIN execution_source_facts f ON f.source_fact_id=$5 AND f.revision=1 AND $9
 WHERE p.org_id=$1 AND p.project_id=$2 AND p.request_id=$3 AND p.attempt_id=$4`

func (s *Service) measurementAttemptSnapshot(ctx context.Context, tx pgx.Tx, org, project int64, request, attempt string, registered Observation, wantDestination bool) (string, Observation, bool, error) {
	var route, hash string
	var wire, pending []byte
	if err := tx.QueryRow(ctx, measurementAttemptSnapshotSQL, org, project, request, attempt, registered.SourceFactID, outbox.EventSourceDispatch, s.SourceInstance(), strconv.FormatInt(project, 10), wantDestination).Scan(&route, &wire, &hash, &pending); err != nil {
		return "", Observation{}, false, captureError("attempt_lookup", err)
	}
	destination, found, err := s.decodeMeasurementDestination(registered, wire, hash, pending)
	return route, destination, found, captureError("destination_lookup", err)
}

func (s *Service) decodeMeasurementDestination(registered Observation, wire []byte, hash string, pending []byte) (Observation, bool, error) {
	var result Observation
	found := false
	check := func(d Observation) error {
		if d.SourceInstance != registered.SourceInstance || d.OrgID != registered.OrgID || d.ProjectID != registered.ProjectID || d.RequestID != registered.RequestID || d.SourceFactID != registered.SourceFactID || d.Profile != DestinationProfile || d.Kind != "dispatch_registered" || d.SubjectKind != "provider_attempt" || d.SubjectID != registered.SubjectID || d.Revision != "1" || d.Destination == nil {
			return errors.New("selected destination binding differs")
		}
		return nil
	}
	if wire != nil {
		if eventconnector.HashSourceBytes(wire) != hash {
			return result, false, errors.New("selected destination source hash differs")
		}
		var err error
		result, err = DecodeObservation(wire)
		if err != nil {
			return result, false, err
		}
		if err = check(result); err != nil {
			return result, false, err
		}
		found = true
	}
	var events []outbox.Event
	if err := json.Unmarshal(pending, &events); err != nil {
		return result, false, err
	}
	if len(events) > 1 {
		return result, false, errors.New("ambiguous frozen dispatch destination")
	}
	for _, event := range events {
		_, records, err := s.decodeDispatch(event)
		if err != nil {
			return result, false, err
		}
		for _, record := range records {
			if record.Profile != DestinationProfile {
				continue
			}
			d, err := DecodeObservation(record.Wire)
			if err != nil {
				return result, false, err
			}
			if err = check(d); err != nil {
				return result, false, err
			}
			if found && eventconnector.HashSourceBytes(record.Wire) != hash {
				return result, false, errors.New("published and frozen dispatch destination differ")
			}
			result, found = d, true
		}
	}
	return result, found, nil
}
