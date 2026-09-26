package sourcefacts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

func measurementFrozenVector(t *testing.T, s *Service) (Observation, []byte, outbox.Event) {
	t.Helper()
	var vectors struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		} `json:"valid"`
	}
	require.NoError(t, json.Unmarshal(assets.Vectors(), &vectors))
	p := dispatchPayload{dispatchBody: dispatchBody{SchemaVersion: assets.DispatchOutboxContract}}
	var destination Observation
	var wire []byte
	for _, v := range vectors.Valid {
		d, err := DecodeObservation([]byte(v.Canonical))
		require.NoError(t, err)
		if !oneOf(d.Kind, "attempt_registered", "selection", "dispatch_registered") {
			continue
		}
		d.SourceInstance = s.SourceInstance()
		d.OrgID, d.ProjectID, d.RequestID = "1", "2", "snapshot-request"
		d.SubjectKind, d.SubjectID = "provider_attempt", "snapshot-attempt"
		if d.Attempt != nil {
			d.Attempt.AttemptID = d.SubjectID
		}
		d.SourceFactID, d.EventID = d.factID(), ""
		d.EventID = d.eventID()
		encoded, err := EncodeObservation(d)
		require.NoError(t, err)
		p.SourceInstance, p.OrgID, p.ProjectID, p.RequestID, p.AttemptID, p.OccurredAt = d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.SubjectID, d.OccurredAt
		p.Records = append(p.Records, frozenDispatchRecord{d.EventID, d.SourceFactID, eventconnector.HashSourceBytes(encoded), base64.StdEncoding.EncodeToString(encoded)})
		if d.Profile == DestinationProfile {
			destination, wire = d, encoded
		}
	}
	require.Len(t, p.Records, 3)
	var err error
	p.Digest, err = dispatchDigest(p.dispatchBody)
	require.NoError(t, err)
	at, err := time.Parse(time.RFC3339Nano, p.OccurredAt)
	require.NoError(t, err)
	event, err := outbox.NewEvent(outbox.EventSourceDispatch, dispatchKey(p.Digest), outbox.PriorityCallLog, 1, p.RequestID, p, at)
	require.NoError(t, err)
	_, _, err = s.decodeDispatch(event)
	require.NoError(t, err)
	return destination, wire, event
}

func TestMeasurementSnapshotFrozenHandoffOffline(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	d, wire, event := measurementFrozenVector(t, s)
	for _, phase := range []string{"pending", "published_before_ack", "acked", "absent"} {
		t.Run(phase, func(t *testing.T) {
			var published []byte
			events := []outbox.Event{}
			if phase == "pending" || phase == "published_before_ack" {
				events = append(events, event)
			}
			if phase == "acked" || phase == "published_before_ack" {
				published = wire
			}
			pending, err := json.Marshal(events)
			require.NoError(t, err)
			got, found, err := s.decodeMeasurementDestination(d, published, eventconnector.HashSourceBytes(wire), pending)
			require.NoError(t, err)
			require.Equal(t, phase != "absent", found)
			if found {
				require.Equal(t, d, got)
			}
		})
	}
	for _, failure := range []string{"hash", "identity", "duplicate", "frozen_tamper", "cross_source", "published_disagrees"} {
		t.Run(failure, func(t *testing.T) {
			want := d
			published := wire
			hash := eventconnector.HashSourceBytes(wire)
			events := []outbox.Event{event}
			switch failure {
			case "hash":
				hash = "sha256:" + strings.Repeat("0", 64)
			case "identity":
				want.SubjectID = "other"
			case "duplicate":
				events = append(events, event)
			case "frozen_tamper":
				events[0].Payload = []byte(`{}`)
			case "cross_source":
				want.SourceInstance = "other"
			case "published_disagrees":
				changed := d
				clone := *d.Destination
				clone.Origin = "https://different.example"
				changed.Destination = &clone
				var err error
				published, err = EncodeObservation(changed)
				require.NoError(t, err)
				hash = eventconnector.HashSourceBytes(published)
			}
			pending, err := json.Marshal(events)
			require.NoError(t, err)
			_, _, err = s.decodeMeasurementDestination(want, published, hash, pending)
			require.Error(t, err)
		})
	}
}

func TestCommittedObservationSnapshotStateAndIdentityOffline(t *testing.T) {
	for _, scenario := range []string{"provider", "delivery", "hold", "provider_advanced", "delivery_advanced", "hold_advanced", "wrong_identity", "missing_receipt"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := testSnapshot()
			snapshot.Core.RequestID = "snapshot-request"
			wire, err := json.Marshal(snapshot)
			require.NoError(t, err)
			now := time.Now().UTC()
			receiptAt := &now
			event := billing.CommittedObservation{Kind: "provider_result", OrgID: 1, RequestID: snapshot.Core.RequestID, AttemptID: "snapshot-attempt", Outcome: billing.ProviderAttemptOutcomeResponseAccepted}
			state, reservation, reason := event.Outcome, "frozen", ""
			mode, outcome, receiptError := billing.DeliveryModeNonStream, billing.DeliveryOutcomeCompleted, ""
			switch scenario {
			case "provider_advanced":
				state = "settled"
			case "delivery", "delivery_advanced", "missing_receipt":
				event.Kind = "local_delivery"
				event.Outcome = billing.DeliveryOutcomeCompleted
				state = "delivery_pending"
				if scenario == "delivery_advanced" {
					outcome = billing.DeliveryOutcomeWriteFailed
				}
				if scenario == "missing_receipt" {
					receiptAt = nil
				}
			case "hold", "hold_advanced":
				event.Kind = "output_hold"
				event.Outcome = "delivery_failed"
				state = event.Outcome
				reservation = "settlement_pending"
				reason = billing.OutputHoldReasonProviderAttemptUnresolved
				mode = billing.DeliveryModeStream
				outcome = billing.DeliveryOutcomeInterrupted
				receiptError = billing.ProviderOutputLimitExceeded
				if scenario == "hold_advanced" {
					state = "settled"
				}
			case "wrong_identity":
				snapshot.Core.RequestID = "different-request"
				wire, err = json.Marshal(snapshot)
				require.NoError(t, err)
			}
			reads, begins, commits := 0, 0, 0
			tx := &captureStageTestTx{MockTx: &testutil.MockTx{}}
			tx.ExecFn = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				require.Equal(t, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`, sql)
				return pgconn.CommandTag{}, nil
			}
			tx.QueryRowFn = func(_ context.Context, sql string, args ...any) pgx.Row {
				reads++
				require.Contains(t, sql, "LEFT JOIN LATERAL")
				require.Equal(t, []any{int64(1), event.RequestID, event.AttemptID, event.Kind}, args)
				values := []any{int64(2), 1, 1, "openai.chat", "", state, false, 200, now, wire, reservation, reason, mode, outcome, receiptError, receiptAt}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					require.Len(t, dest, len(values))
					for i, v := range values {
						reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
					}
					return nil
				}}
			}
			tx.CommitFn = func(context.Context) error { commits++; return nil }
			s, err := New(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { begins++; return tx, nil }}, Options{Enabled: true, ObservationsEnabled: true, SourceInstance: "synthetic-ur", CursorSecret: strings.Repeat("s", 32)})
			require.NoError(t, err)
			err = s.CaptureCommitted(t.Context(), event)
			good := oneOf(scenario, "provider", "delivery", "hold")
			if good {
				require.NoError(t, err)
				require.Equal(t, 1, commits)
			} else {
				require.Error(t, err)
				require.Zero(t, commits)
				require.Zero(t, tx.batches)
			}
			require.Equal(t, 1, reads)
			require.Equal(t, 1, begins)
		})
	}
}

func TestMeasurementSnapshotLookupErrorPreservedOffline(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	cause := errors.New("snapshot unavailable")
	tx := &testutil.MockTx{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return cause }}
	}}
	_, _, _, err := s.measurementAttemptSnapshot(t.Context(), tx, 1, 2, "r", "a", Observation{}, true)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "attempt_lookup", CaptureErrorStage(err))
}
