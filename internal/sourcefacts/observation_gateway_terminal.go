package sourcefacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	contract "github.com/uniroute/unirouter/contracts/source-facts/pre-dispatch-v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

const GatewayPreDispatchProfile = "unirouter.runtime-observation.v2"
const GatewayPreDispatchKind = "gateway_pre_dispatch_terminal"

type GatewayPreDispatchRuntime struct {
	Operation      string `json:"operation"`
	StreamMode     string `json:"stream_mode"`
	Admission      string `json:"admission"`
	Provider       string `json:"provider"`
	LocalDelivery  string `json:"local_delivery"`
	Financial      string `json:"financial"`
	Capture        string `json:"capture"`
	TerminalOrigin string `json:"terminal_origin"`
	FailureCode    string `json:"failure_code"`
	HTTPStatus     string `json:"http_status"`
}

type GatewayPreDispatchTerminal struct {
	SchemaVersion   string                    `json:"schema_version"`
	SourceInstance  string                    `json:"source_instance"`
	OrgID           string                    `json:"org_id"`
	ProjectID       string                    `json:"project_id"`
	RequestID       string                    `json:"request_id"`
	Profile         string                    `json:"profile"`
	SubjectKind     string                    `json:"subject_kind"`
	SubjectID       string                    `json:"subject_id"`
	Kind            string                    `json:"observation_kind"`
	SourceFactID    string                    `json:"source_fact_id"`
	Revision        string                    `json:"revision"`
	EventID         string                    `json:"event_id"`
	OccurredAt      string                    `json:"occurred_at"`
	RecordedAt      string                    `json:"recorded_at"`
	Coverage        string                    `json:"coverage"`
	ContextRef      string                    `json:"context_ref"`
	CaptureExpected []string                  `json:"capture_expected"`
	Runtime         GatewayPreDispatchRuntime `json:"runtime"`
}

func (d GatewayPreDispatchTerminal) factID() string {
	return sourceID(contract.SchemaVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
}
func (d GatewayPreDispatchTerminal) eventID() string {
	return sourceID(contract.SchemaVersion+".revision", d.factID(), d.Revision)
}
func (d GatewayPreDispatchTerminal) Validate() error {
	invalid := errors.New("invalid gateway pre-dispatch terminal")
	r := d.Runtime
	if d.SchemaVersion != contract.SchemaVersion || d.Profile != GatewayPreDispatchProfile || d.Kind != GatewayPreDispatchKind ||
		!positiveID(d.OrgID) || !positiveID(d.ProjectID) || !opaque(d.SourceInstance, 128) || !opaque(d.RequestID, 64) ||
		d.SubjectKind != "request" || d.SubjectID != d.RequestID || d.Revision != "1" || d.Coverage != "observed" ||
		!observationTime(d.OccurredAt) || !observationTime(d.RecordedAt) || d.SourceFactID != d.factID() || d.EventID != d.eventID() ||
		len(d.CaptureExpected) != 1 || d.CaptureExpected[0] != GatewayPreDispatchKind ||
		d.ContextRef != sourceID(Version, d.SourceInstance, d.OrgID, d.ProjectID, ContextProfile, d.RequestID, "authenticated") ||
		r.Operation != string(modeloperation.IDChatCompletions) || r.StreamMode != "not_observed" || r.Admission != "authenticated" ||
		r.Provider != "not_started" || r.LocalDelivery != "not_started" || r.Financial != "not_created" ||
		r.Capture != "observed" || r.TerminalOrigin != "gateway_pre_dispatch" || r.FailureCode != "routing.no_route" || r.HTTPStatus != "503" {
		return invalid
	}
	return nil
}

func EncodeGatewayPreDispatchTerminal(d GatewayPreDispatchTerminal) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&value); err != nil {
		return nil, err
	}
	wire, ce := eventconnector.CanonicalJSON(value)
	if ce != nil {
		return nil, ce
	}
	if len(wire) > MaxDocumentBytes {
		return nil, errors.New("gateway pre-dispatch terminal exceeds byte limit")
	}
	return wire, nil
}

func DecodeGatewayPreDispatchTerminal(wire []byte) (GatewayPreDispatchTerminal, error) {
	var d GatewayPreDispatchTerminal
	if len(wire) > MaxDocumentBytes {
		return d, errors.New("gateway pre-dispatch terminal exceeds byte limit")
	}
	if err := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); err != nil {
		return d, err
	}
	dec := json.NewDecoder(bytes.NewReader(wire))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return d, errors.New("trailing gateway pre-dispatch terminal")
	}
	canonical, err := EncodeGatewayPreDispatchTerminal(d)
	if err != nil {
		return d, err
	}
	if !bytes.Equal(canonical, wire) {
		return d, errors.New("noncanonical gateway pre-dispatch terminal")
	}
	return d, nil
}

// CaptureGatewayNoRoute persists one observation in the existing source-fact
// stream transaction. It is advisory to the HTTP result: an observation failure
// must never replace the original no-route 503 or authorize a provider retry.
func (s *Service) CaptureGatewayNoRoute(ctx context.Context, snapshot executioncontext.Snapshot) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if snapshot.Core.Operation != string(modeloperation.IDChatCompletions) {
		return errors.New("gateway pre-dispatch terminal requires chat completions")
	}
	org, _ := strconv.ParseInt(snapshot.Core.OrgID, 10, 64)
	project, _ := strconv.ParseInt(snapshot.Core.ProjectID, 10, 64)
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
	var admitted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2)
        OR EXISTS(SELECT 1 FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2)
        OR EXISTS(SELECT 1 FROM async_media_jobs WHERE org_id=$1 AND project_id=$3 AND request_id=$2)`, org, snapshot.Core.RequestID, project).Scan(&admitted); err != nil {
		return captureError("admission_lookup", err)
	}
	if admitted {
		return errors.New("gateway pre-dispatch terminal conflicts with admitted request")
	}
	// Serialize repeated capture of the same immutable identity before choosing
	// terminal timestamps. A replay returns the first committed source bytes.
	if _, err = tx.Exec(ctx, `INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile)
		VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, s.options.SourceInstance, org, project, GatewayPreDispatchProfile); err != nil {
		return captureError("stream_insert", err)
	}
	var sequence int64
	if err = tx.QueryRow(ctx, `SELECT high_sequence FROM execution_source_streams
		WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 FOR UPDATE`,
		s.options.SourceInstance, org, project, GatewayPreDispatchProfile).Scan(&sequence); err != nil {
		return captureError("stream_lock", err)
	}
	identity := GatewayPreDispatchTerminal{SchemaVersion: contract.SchemaVersion, SourceInstance: s.options.SourceInstance,
		OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID,
		Profile: GatewayPreDispatchProfile, SubjectKind: "request", SubjectID: snapshot.Core.RequestID,
		Kind: GatewayPreDispatchKind, Revision: "1"}
	identity.SourceFactID, identity.EventID = identity.factID(), identity.eventID()
	var priorWire []byte
	err = tx.QueryRow(ctx, `SELECT document FROM execution_source_facts
		WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 AND event_id=$5`,
		s.options.SourceInstance, org, project, GatewayPreDispatchProfile, identity.EventID).Scan(&priorWire)
	if err == nil {
		prior, decodeErr := DecodeGatewayPreDispatchTerminal(priorWire)
		if decodeErr != nil || prior.Runtime.Operation != snapshot.Core.Operation || prior.ContextRef != sourceID(Version, identity.SourceInstance, identity.OrgID, identity.ProjectID, ContextProfile, identity.RequestID, "authenticated") {
			return errors.New("gateway pre-dispatch terminal replay conflict")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return captureError("replay_lookup", err)
	}
	at := executioncontext.Time(time.Now())
	d := GatewayPreDispatchTerminal{
		SchemaVersion: contract.SchemaVersion, SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID,
		RequestID: snapshot.Core.RequestID, Profile: GatewayPreDispatchProfile, SubjectKind: "request", SubjectID: snapshot.Core.RequestID,
		Kind: GatewayPreDispatchKind, Revision: "1", OccurredAt: at, RecordedAt: at, Coverage: "observed",
		ContextRef:      (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID(),
		CaptureExpected: []string{GatewayPreDispatchKind},
		Runtime:         GatewayPreDispatchRuntime{Operation: snapshot.Core.Operation, StreamMode: "not_observed", Admission: "authenticated", Provider: "not_started", LocalDelivery: "not_started", Financial: "not_created", Capture: "observed", TerminalOrigin: "gateway_pre_dispatch", FailureCode: "routing.no_route", HTTPStatus: "503"},
	}
	d.SourceFactID, d.EventID = d.factID(), d.eventID()
	wire, err := EncodeGatewayPreDispatchTerminal(d)
	if err != nil {
		return err
	}
	if err = s.publishRecordsTx(ctx, tx, []sourcePublication{{SourceInstance: d.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, RequestID: d.RequestID, Profile: d.Profile, Stage: d.Kind, EventID: d.EventID, SourceFactID: d.SourceFactID, Revision: d.Revision, Wire: wire}}); err != nil {
		return err
	}
	return captureError("commit", tx.Commit(ctx))
}
