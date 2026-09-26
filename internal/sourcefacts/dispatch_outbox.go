package sourcefacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const dispatchMarkerReserve = 1024
const dispatchPayloadLimit = 262144

var errDispatchLeaseLost = errors.New("source dispatch live event lease is unavailable")

type frozenDispatchRecord struct {
	EventID      string `json:"event_id"`
	SourceFactID string `json:"source_fact_id"`
	SourceHash   string `json:"source_hash"`
	BytesBase64  string `json:"bytes_base64"`
}

type dispatchBody struct {
	SchemaVersion  string                 `json:"schema_version"`
	SourceInstance string                 `json:"source_instance"`
	OrgID          string                 `json:"org_id"`
	ProjectID      string                 `json:"project_id"`
	RequestID      string                 `json:"request_id"`
	AttemptID      string                 `json:"attempt_id"`
	OccurredAt     string                 `json:"occurred_at"`
	Records        []frozenDispatchRecord `json:"records"`
}

type dispatchPayload struct {
	dispatchBody
	Digest       string `json:"digest"`
	Materialized bool   `json:"materialized"`
	PublishedAt  string `json:"published_at"`
}

func dispatchDigest(body dispatchBody) (string, error) {
	wire, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	var value map[string]any
	if err = json.Unmarshal(wire, &value); err != nil {
		return "", err
	}
	canonical, canonicalErr := eventconnector.CanonicalJSON(value)
	if canonicalErr != nil {
		return "", canonicalErr
	}
	return eventconnector.HashSourceBytes(canonical), nil
}

func dispatchKey(digest string) string {
	return "source-dispatch:" + strings.TrimPrefix(digest, "sha256:")
}

func lockDispatchSchema(ctx context.Context, tx pgx.Tx) error {
	var head string
	if err := tx.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE version=$1 FOR KEY SHARE`, schemaauthority.ObservationExpandHead).Scan(&head); err != nil {
		return fmt.Errorf("source dispatch requires M117: %w", err)
	}
	return nil
}

func (s *Service) enqueueDispatchTx(ctx context.Context, tx pgx.Tx, attempt string, documents []Observation) error {
	if len(documents) != 3 || attempt == "" {
		return errors.New("source dispatch requires three exact records")
	}
	if err := lockDispatchSchema(ctx, tx); err != nil {
		return err
	}
	d := documents[0]
	p := dispatchPayload{dispatchBody: dispatchBody{SchemaVersion: assets.DispatchOutboxContract, SourceInstance: s.SourceInstance(), OrgID: d.OrgID, ProjectID: d.ProjectID, RequestID: d.RequestID, AttemptID: attempt, OccurredAt: d.OccurredAt}}
	documents = slices.Clone(documents)
	slices.SortFunc(documents, func(a, b Observation) int { return strings.Compare(a.Profile, b.Profile) })
	for _, document := range documents {
		wire, err := EncodeObservation(document)
		if err != nil {
			return err
		}
		p.Records = append(p.Records, frozenDispatchRecord{document.EventID, document.SourceFactID, eventconnector.HashSourceBytes(wire), base64.StdEncoding.EncodeToString(wire)})
	}
	var err error
	p.Digest, err = dispatchDigest(p.dispatchBody)
	if err != nil {
		return err
	}
	org, err := strconv.ParseInt(p.OrgID, 10, 64)
	if err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339Nano, p.OccurredAt)
	if err != nil {
		return err
	}
	event, err := outbox.NewEvent(outbox.EventSourceDispatch, dispatchKey(p.Digest), outbox.PriorityCallLog, org, p.RequestID, p, at)
	if err != nil {
		return err
	}
	if _, _, err = s.decodeDispatch(event); err != nil {
		return err
	}
	// Count PostgreSQL's real JSONB text, including base64 and JSONB spacing.
	// Reserve room for the one-way publication marker before the attempt commits.
	var size int
	if err = tx.QueryRow(ctx, `SELECT octet_length($1::jsonb::text)`, string(event.Payload)).Scan(&size); err != nil {
		return err
	}
	if size > dispatchPayloadLimit-dispatchMarkerReserve {
		return errors.New("source dispatch batch exceeds durable event size")
	}
	_, created, err := outbox.EnqueueTx(ctx, tx, event)
	if err != nil {
		return err
	}
	if !created {
		// A duplicate is only acceptable with exactly the same frozen input.
		var previous []byte
		if err = tx.QueryRow(ctx, `SELECT payload FROM internal_outbox_events WHERE event_key=$1 AND event_type=$2`, event.Key, event.Type).Scan(&previous); err != nil {
			return err
		}
		event.Payload = previous
		old, _, decodeErr := s.decodeDispatch(event)
		if decodeErr != nil {
			return decodeErr
		}
		if old.Digest != p.Digest {
			return errors.New("source dispatch replay changed frozen records")
		}
	}
	return nil
}

// decodeDispatch is shared by the worker and the read-only measurement fallback.
// It validates frozen source bytes, never JSONB's presentation of those bytes.
func (s *Service) decodeDispatch(event outbox.Event) (dispatchPayload, []sourcePublication, error) {
	var p dispatchPayload
	if event.Type != outbox.EventSourceDispatch || event.Priority != outbox.PriorityCallLog || len(event.Payload) == 0 || len(event.Payload) > dispatchPayloadLimit {
		return p, nil, errors.New("invalid source dispatch event")
	}
	if e := eventconnector.ValidateStrictJSON(event.Payload, eventconnector.StrictJSONOptions{RejectNull: true}); e != nil {
		return p, nil, e
	}
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, nil, err
	}
	if p.SchemaVersion != assets.DispatchOutboxContract || p.SourceInstance == "" || p.SourceInstance != s.SourceInstance() || p.OrgID != strconv.FormatInt(event.OrgID, 10) || p.RequestID != event.RequestID || p.AttemptID == "" || len(p.Records) != 3 {
		return p, nil, errors.New("source dispatch identity differs")
	}
	if p.Materialized != (p.PublishedAt != "") {
		return p, nil, errors.New("source dispatch marker differs")
	}
	if p.Materialized {
		if _, e := time.Parse(time.RFC3339Nano, p.PublishedAt); e != nil {
			return p, nil, e
		}
	}
	at, err := time.Parse(time.RFC3339Nano, p.OccurredAt)
	if err != nil || !at.Equal(event.OccurredAt) {
		return p, nil, errors.New("source dispatch original time differs")
	}
	digest, err := dispatchDigest(p.dispatchBody)
	if err != nil {
		return p, nil, err
	}
	if digest != p.Digest || event.Key != dispatchKey(digest) {
		return p, nil, errors.New("source dispatch body digest differs")
	}
	profiles := map[string]string{AttemptProfile: "attempt_registered", CapabilityProfile: "selection", DestinationProfile: "dispatch_registered"}
	records := make([]sourcePublication, 0, 3)
	for _, record := range p.Records {
		wire, e := base64.StdEncoding.Strict().DecodeString(record.BytesBase64)
		if e != nil || base64.StdEncoding.EncodeToString(wire) != record.BytesBase64 || eventconnector.HashSourceBytes(wire) != record.SourceHash {
			return p, nil, errors.New("source dispatch frozen bytes differ")
		}
		d, e := DecodeObservation(wire)
		if e != nil {
			return p, nil, e
		}
		kind, known := profiles[d.Profile]
		if !known || d.Kind != kind || d.Revision != "1" || d.SourceInstance != p.SourceInstance || d.OrgID != p.OrgID || d.ProjectID != p.ProjectID || d.RequestID != p.RequestID || d.SubjectKind != "provider_attempt" || d.SubjectID != p.AttemptID || d.OccurredAt != p.OccurredAt || d.RecordedAt != p.OccurredAt || d.EventID != record.EventID || d.SourceFactID != record.SourceFactID {
			return p, nil, errors.New("source dispatch record binding differs")
		}
		delete(profiles, d.Profile)
		publication, e := publicationB(d)
		if e != nil {
			return p, nil, e
		}
		records = append(records, publication)
	}
	return p, records, nil
}

// CheckDispatchRecoveryReady runs before any original worker may claim events,
// including when new A/B capture is disabled. It only reads retained state.
func (s *Service) CheckDispatchRecoveryReady(ctx context.Context) error {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT to_regclass('internal_outbox_events') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var pending, mismatch, schema bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM internal_outbox_events WHERE event_type=$1),
 EXISTS(SELECT 1 FROM internal_outbox_events WHERE event_type=$1 AND (payload->>'source_instance' IS DISTINCT FROM $2 OR payload->>'schema_version' IS DISTINCT FROM $3)),
 EXISTS(SELECT 1 FROM schema_migrations WHERE version=$4)`, outbox.EventSourceDispatch, s.SourceInstance(), assets.DispatchOutboxContract, schemaauthority.ObservationExpandHead).Scan(&pending, &mismatch, &schema); err != nil {
		return err
	}
	if pending && (mismatch || !schema || s.SourceInstance() == "") {
		return errors.New("retained source dispatch requires matching instance/schema before worker startup")
	}
	return nil
}

// ProcessDispatchEvent owns no business transition. The frozen input, its live
// marker and three source publications are the only state in this RC transaction.
func (s *Service) ProcessDispatchEvent(ctx context.Context, claimed outbox.Event) error {
	if claimed.ID <= 0 || claimed.ClaimedBy == "" || claimed.Type != outbox.EventSourceDispatch {
		return errDispatchLeaseLost
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	if err = lockDispatchSchema(ctx, tx); err != nil {
		return err
	}
	live := outbox.Event{ID: claimed.ID, ClaimedBy: claimed.ClaimedBy}
	var owner string
	var valid bool
	err = tx.QueryRow(ctx, `SELECT event_type,event_key,priority,org_id,request_id,payload,occurred_at,claimed_by,COALESCE(claimed_until>clock_timestamp(),false)
	 FROM internal_outbox_events WHERE id=$1 FOR UPDATE`, claimed.ID).Scan(&live.Type, &live.Key, &live.Priority, &live.OrgID, &live.RequestID, &live.Payload, &live.OccurredAt, &owner, &valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return errDispatchLeaseLost
	}
	if err != nil {
		return err
	}
	if owner != claimed.ClaimedBy || !valid || live.Type != claimed.Type || live.Key != claimed.Key || live.OrgID != claimed.OrgID || live.RequestID != claimed.RequestID {
		return errDispatchLeaseLost
	}
	p, records, err := s.decodeDispatch(live)
	if err != nil {
		return err
	}
	if p.Materialized {
		if err = tx.QueryRow(ctx, `SELECT claimed_by=$2 AND claimed_until>clock_timestamp() FROM internal_outbox_events WHERE id=$1`, live.ID, owner).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return errDispatchLeaseLost
		}
		return tx.Commit(ctx)
	} // No retained source rows are required for an already-published replay.
	if err = s.publishRecordsTx(ctx, tx, records); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE internal_outbox_events SET payload=jsonb_set(jsonb_set(payload,'{materialized}','true'::jsonb),'{published_at}',to_jsonb(to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')))
 WHERE id=$1 AND event_type=$2 AND event_key=$3 AND claimed_by=$4 AND claimed_until>clock_timestamp() AND payload->>'materialized'='false'`, live.ID, live.Type, live.Key, owner)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errDispatchLeaseLost
	}
	return tx.Commit(ctx)
}
