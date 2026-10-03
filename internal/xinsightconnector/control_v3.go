package xinsightconnector

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	ControlOperationV3Schema    = "xinsight.connector-control-operation.v3"
	ControlResultV3Schema       = "xinsight.connector-control-result.v3"
	ControlKindAdoptExisting    = "adopt_existing_receiver"
	ControlKindPairMachine      = "pair_machine"
	ControlPhaseBindExisting    = "bind_existing_receiver"
	ControlPhaseConfirmExisting = "confirm_existing_receiver"
	ControlPhaseDeliverMachine  = "deliver_machine_candidate"
	PolicyManagePurpose         = "xinsight.policy.manage"
	SourceEnrollPurpose         = "xinsight.source.enroll"
)

type ControlOperationV3 struct {
	SchemaVersion           string   `json:"schema_version"`
	OperationID             string   `json:"operation_id"`
	InstallationID          string   `json:"installation_id"`
	ProjectID               string   `json:"project_id"`
	EventScope              []string `json:"event_scope"`
	DesiredState            string   `json:"desired_state"`
	Kind                    string   `json:"kind"`
	Phase                   string   `json:"phase"`
	PredecessorOperationID  string   `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string   `json:"predecessor_result_sha256,omitempty"`
	Request                 any      `json:"request"`
}
type ControlResultV3 struct {
	SchemaVersion  string `json:"schema_version"`
	OperationID    string `json:"operation_id"`
	InstallationID string `json:"installation_id"`
	Kind           string `json:"kind"`
	Phase          string `json:"phase"`
	Status         string `json:"status"`
	Result         any    `json:"result,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	ObservedAt     string `json:"observed_at"`
}
type ExistingReceiverCandidate struct {
	SourceInstance       string                   `json:"source_instance"`
	SourceBindingSHA256  string                   `json:"source_binding_sha256"`
	FeedKeyID            string                   `json:"feed_key_id"`
	ReceiverRegistration ReceiverRegistrationV1   `json:"receiver_registration"`
	AllowedIP            string                   `json:"allowed_ip"`
	WorkerConfig         PublicWorkerConfig       `json:"worker_config"`
	Checkpoint           PublicCheckpointIdentity `json:"checkpoint"`
}
type PublicWorkerConfig struct {
	WorkerUnit                 string `json:"worker_unit"`
	UROrigin                   string `json:"ur_origin"`
	FeedRoute                  string `json:"feed_route"`
	SourceInstance             string `json:"source_instance"`
	FeedSchema                 string `json:"feed_schema"`
	CursorKey                  string `json:"cursor_key"`
	ReceiverRegistrationSHA256 string `json:"receiver_registration_sha256"`
}
type PublicCheckpointIdentity struct {
	StateID                string `json:"state_id"`
	ConsumerEpoch          int64  `json:"consumer_epoch"`
	EpochOperationID       string `json:"epoch_operation_id"`
	AuthorizationRefSHA256 string `json:"authorization_ref_sha256"`
	CursorSHA256           string `json:"cursor_sha256"`
	HighWatermarkSHA256    string `json:"high_watermark_sha256"`
}
type BindExistingReceiverRequest struct {
	Candidate       ExistingReceiverCandidate `json:"candidate"`
	CandidateSHA256 string                    `json:"candidate_sha256"`
}
type AdoptionBinding struct {
	InstallationID  string `json:"installation_id"`
	OrgID           int64  `json:"org_id"`
	ProjectID       int64  `json:"project_id"`
	OperationID     string `json:"operation_id"`
	FeedKeyID       string `json:"feed_key_id"`
	CandidateSHA256 string `json:"candidate_sha256"`
}
type BindExistingReceiverResult struct {
	SourceInstance        string          `json:"source_instance"`
	CandidateSHA256       string          `json:"candidate_sha256"`
	AdoptionBinding       AdoptionBinding `json:"adoption_binding"`
	AdoptionBindingSHA256 string          `json:"adoption_binding_sha256"`
	SourceState           string          `json:"source_state"`
	ReceiverState         string          `json:"receiver_state"`
	Readiness             string          `json:"readiness"`
}
type XIAdoptionReceipt struct {
	InstallationID               string `json:"installation_id"`
	BindOperationID              string `json:"bind_operation_id"`
	BindResultSHA256             string `json:"bind_result_sha256"`
	CandidateSHA256              string `json:"candidate_sha256"`
	StateID                      string `json:"state_id"`
	ConsumerEpoch                int64  `json:"consumer_epoch"`
	EpochOperationID             string `json:"epoch_operation_id"`
	CommittedCursorSHA256        string `json:"committed_cursor_sha256"`
	CommittedHighWatermarkSHA256 string `json:"committed_high_watermark_sha256"`
	CommittedAt                  string `json:"committed_at"`
	PagesCommittedBefore         int64  `json:"pages_committed_before"`
}
type FeedReadReceipt struct {
	FeedKeyID                  string `json:"feed_key_id"`
	ReceiverRegistrationSHA256 string `json:"receiver_registration_sha256"`
	WorkerConfigSHA256         string `json:"worker_config_sha256"`
	CheckpointStateID          string `json:"checkpoint_state_id"`
	ConsumerEpoch              int64  `json:"consumer_epoch"`
	EpochOperationID           string `json:"epoch_operation_id"`
	StartedAt                  string `json:"started_at"`
	CompletedAt                string `json:"completed_at"`
	PagesCommitted             int64  `json:"pages_committed"`
	CursorSHA256               string `json:"cursor_sha256"`
	HighWatermarkSHA256        string `json:"high_watermark_sha256"`
	ResponseStatus             int    `json:"response_status"`
}
type ConfirmExistingReceiverRequest struct {
	AdoptionBindingSHA256   string            `json:"adoption_binding_sha256"`
	XIAdoptionReceipt       XIAdoptionReceipt `json:"xi_adoption_receipt"`
	XIAdoptionReceiptSHA256 string            `json:"xi_adoption_receipt_sha256"`
	FeedReadReceipt         FeedReadReceipt   `json:"feed_read_receipt"`
	FeedReadReceiptSHA256   string            `json:"feed_read_receipt_sha256"`
}
type ConfirmExistingReceiverResult struct {
	SourceInstance          string `json:"source_instance"`
	AdoptionBindingSHA256   string `json:"adoption_binding_sha256"`
	XIAdoptionReceiptSHA256 string `json:"xi_adoption_receipt_sha256"`
	FeedReadReceiptSHA256   string `json:"feed_read_receipt_sha256"`
	SourceState             string `json:"source_state"`
	ReceiverState           string `json:"receiver_state"`
	Readiness               string `json:"readiness"`
}

// MachineRegistration preserves M123's six-field identity and Marshal order.
type MachineRegistration struct {
	InstallationID string `json:"installation_id"`
	Purpose        string `json:"purpose"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
	AllowedIP      string `json:"allowed_ip"`
}
type MachineCustodyReceipt struct {
	InstallationID     string `json:"installation_id"`
	Purpose            string `json:"purpose"`
	OperationID        string `json:"operation_id"`
	RegistrationSHA256 string `json:"registration_sha256"`
	CustodyState       string `json:"custody_state"`
}
type DeliverMachineRequest struct {
	SourceInstance       string                `json:"source_instance"`
	SourceBindingSHA256  string                `json:"source_binding_sha256"`
	Registration         MachineRegistration   `json:"registration"`
	RegistrationSHA256   string                `json:"registration_sha256"`
	CustodyReceipt       MachineCustodyReceipt `json:"custody_receipt"`
	CustodyReceiptSHA256 string                `json:"custody_receipt_sha256"`
}
type DeliverMachineResult struct {
	SourceInstance       string `json:"source_instance"`
	Purpose              string `json:"purpose"`
	RegistrationSHA256   string `json:"registration_sha256"`
	CustodyReceiptSHA256 string `json:"custody_receipt_sha256"`
	Readiness            string `json:"readiness"`
	Activation           string `json:"activation"`
}

var v3UUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var v3DecimalID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func PublicCanonicalSHA256(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return hashBytes(b)
}
func (o ControlOperationV3) CanonicalJSON() ([]byte, error) {
	if err := ValidateControlOperationV3(o); err != nil {
		return nil, err
	}
	return json.Marshal(o)
}
func (o ControlOperationV3) CanonicalSHA256() (string, error) {
	b, e := o.CanonicalJSON()
	return hashBytes(b), e
}
func (r ControlResultV3) CanonicalJSON() ([]byte, error) {
	if err := ValidateControlResultV3(r); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}
func (r ControlResultV3) CanonicalSHA256() (string, error) {
	b, e := r.CanonicalJSON()
	return hashBytes(b), e
}

// Closed decoding checks exact field names and presence, including meaningful
// zero counters. encoding/json alone accepts missing fields and case aliases.
func decodeClosedV3(raw []byte, dest any) error {
	if len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) || eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{RejectNull: true}) != nil {
		return ErrInvalid
	}
	if err := closedV3Fields(raw, reflect.TypeOf(dest).Elem()); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dest) != nil {
		return ErrInvalid
	}
	return requireControlV2EOF(d)
}
func closedV3Fields(raw []byte, t reflect.Type) error {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return ErrInvalid
	}
	for n := 0; n < t.NumField(); n++ {
		f := t.Field(n)
		tag := strings.Split(f.Tag.Get("json"), ",")
		if tag[0] == "-" {
			continue
		}
		v, exists := fields[tag[0]]
		optional := len(tag) > 1 && tag[1] == "omitempty"
		if !exists {
			if !optional {
				return ErrInvalid
			}
			continue
		}
		if optional && bytes.Equal(v, []byte(`""`)) {
			return ErrInvalid
		}
		if err := closedV3Fields(v, f.Type); err != nil {
			return err
		}
		delete(fields, tag[0])
	}
	if len(fields) != 0 {
		return ErrInvalid
	}
	return nil
}
func DecodeControlOperationV3(raw []byte) (ControlOperationV3, error) {
	// The v3 wire owns its own DTO; no v2 parsing or fallback is used.
	var w struct {
		SchemaVersion           string          `json:"schema_version"`
		OperationID             string          `json:"operation_id"`
		InstallationID          string          `json:"installation_id"`
		ProjectID               string          `json:"project_id"`
		EventScope              []string        `json:"event_scope"`
		DesiredState            string          `json:"desired_state"`
		Kind                    string          `json:"kind"`
		Phase                   string          `json:"phase"`
		PredecessorOperationID  string          `json:"predecessor_operation_id,omitempty"`
		PredecessorResultSHA256 string          `json:"predecessor_result_sha256,omitempty"`
		Request                 json.RawMessage `json:"request"`
	}
	if err := decodeClosedV3(raw, &w); err != nil {
		return ControlOperationV3{}, err
	}
	o := ControlOperationV3{SchemaVersion: w.SchemaVersion, OperationID: w.OperationID, InstallationID: w.InstallationID, ProjectID: w.ProjectID, EventScope: w.EventScope, DesiredState: w.DesiredState, Kind: w.Kind, Phase: w.Phase, PredecessorOperationID: w.PredecessorOperationID, PredecessorResultSHA256: w.PredecessorResultSHA256}
	switch w.Phase {
	case ControlPhaseBindExisting:
		var v BindExistingReceiverRequest
		if e := decodeClosedV3(w.Request, &v); e != nil {
			return o, e
		}
		o.Request = v
	case ControlPhaseConfirmExisting:
		var v ConfirmExistingReceiverRequest
		if e := decodeClosedV3(w.Request, &v); e != nil {
			return o, e
		}
		o.Request = v
	case ControlPhaseDeliverMachine:
		var v DeliverMachineRequest
		if e := decodeClosedV3(w.Request, &v); e != nil {
			return o, e
		}
		o.Request = v
	default:
		return o, ErrInvalid
	}
	return o, ValidateControlOperationV3(o)
}
func DecodeControlResultV3(raw []byte) (ControlResultV3, error) {
	var w struct {
		SchemaVersion  string          `json:"schema_version"`
		OperationID    string          `json:"operation_id"`
		InstallationID string          `json:"installation_id"`
		Kind           string          `json:"kind"`
		Phase          string          `json:"phase"`
		Status         string          `json:"status"`
		Result         json.RawMessage `json:"result,omitempty"`
		ReasonCode     string          `json:"reason_code,omitempty"`
		ObservedAt     string          `json:"observed_at"`
	}
	if e := decodeClosedV3(raw, &w); e != nil {
		return ControlResultV3{}, e
	}
	r := ControlResultV3{SchemaVersion: w.SchemaVersion, OperationID: w.OperationID, InstallationID: w.InstallationID, Kind: w.Kind, Phase: w.Phase, Status: w.Status, ReasonCode: w.ReasonCode, ObservedAt: w.ObservedAt}
	if w.Status == ControlResultCompleted {
		switch w.Phase {
		case ControlPhaseBindExisting:
			var v BindExistingReceiverResult
			if e := decodeClosedV3(w.Result, &v); e != nil {
				return r, e
			}
			r.Result = v
		case ControlPhaseConfirmExisting:
			var v ConfirmExistingReceiverResult
			if e := decodeClosedV3(w.Result, &v); e != nil {
				return r, e
			}
			r.Result = v
		case ControlPhaseDeliverMachine:
			var v DeliverMachineResult
			if e := decodeClosedV3(w.Result, &v); e != nil {
				return r, e
			}
			r.Result = v
		default:
			return r, ErrInvalid
		}
	} else if len(w.Result) != 0 {
		return r, ErrInvalid
	}
	return r, ValidateControlResultV3(r)
}
func validV3Time(s string) bool {
	if !validControlObservedAt(s) {
		return false
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.Nanosecond()%1000 == 0
}
func v3Time(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func validV3Network(s string) bool {
	p, e := netip.ParsePrefix(s)
	return e == nil && p.String() == s && p == p.Masked() && p.Addr().Zone() == "" && !p.Addr().Is4In6() && !p.Addr().IsUnspecified() && !p.Addr().IsMulticast() && ((p.Addr().Is4() && p.Bits() == 32) || (p.Addr().Is6() && p.Bits() == 128))
}
func v3KindPhase(k, p string) bool {
	return (k == ControlKindAdoptExisting && (p == ControlPhaseBindExisting || p == ControlPhaseConfirmExisting)) || (k == ControlKindPairMachine && p == ControlPhaseDeliverMachine)
}
func validMachineRegistrationV3(r MachineRegistration) bool {
	prefix := ""
	switch r.Purpose {
	case PolicyManagePurpose:
		prefix = "ur-xpm1-"
	case SourceEnrollPurpose:
		prefix = "ur-xse1-"
	default:
		return false
	}
	return validExactExternalIdentity(r.InstallationID) && len(r.KeyPrefix) == 20 && strings.HasPrefix(r.KeyPrefix, prefix) && regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(r.KeyPrefix[len(prefix):]) && controlHexDigestPattern.MatchString(r.VerifierSHA256) && controlFeedOperationID.MatchString(r.OperationID) && validV3Network(r.AllowedIP)
}
func ValidateControlOperationV3(o ControlOperationV3) error {
	if o.SchemaVersion != ControlOperationV3Schema || !remoteOperationID.MatchString(o.OperationID) || !validExactExternalIdentity(o.InstallationID) || !validControlText(o.ProjectID, 128) || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`).MatchString(o.ProjectID) || !reflect.DeepEqual(o.EventScope, []string{EventScopeModelCompleted}) || o.DesiredState != DesiredActive || !v3KindPhase(o.Kind, o.Phase) {
		return ErrInvalid
	}
	if o.Phase == ControlPhaseBindExisting {
		if o.PredecessorOperationID != "" || o.PredecessorResultSHA256 != "" {
			return ErrInvalid
		}
	} else if !remoteOperationID.MatchString(o.PredecessorOperationID) || o.PredecessorOperationID == o.OperationID || !hashPattern.MatchString(o.PredecessorResultSHA256) {
		return ErrInvalid
	}
	switch o.Phase {
	case ControlPhaseBindExisting:
		r, ok := o.Request.(BindExistingReceiverRequest)
		if !ok {
			return ErrInvalid
		}
		c := r.Candidate
		w := c.WorkerConfig
		k := c.Checkpoint
		if r.CandidateSHA256 != PublicCanonicalSHA256(c) || !validSourceInstance(c.SourceInstance) || !hashPattern.MatchString(c.SourceBindingSHA256) || !v3DecimalID.MatchString(c.FeedKeyID) || !validReceiverRegistration(c.ReceiverRegistration) || !validV3Network(c.AllowedIP) || w.WorkerUnit != "xinsight-feed.service" || !validFrozenHTTPSOrigin(w.UROrigin) || w.FeedRoute != ControlFixedReceiverRoute || w.SourceInstance != c.SourceInstance || w.FeedSchema != "unirouter.completed-execution.v1" || w.CursorKey != w.FeedSchema || w.ReceiverRegistrationSHA256 != PublicCanonicalSHA256(c.ReceiverRegistration) || !v3UUID.MatchString(k.StateID) || k.ConsumerEpoch < 1 || !controlFeedOperationID.MatchString(k.EpochOperationID) || !hashPattern.MatchString(k.AuthorizationRefSHA256) || !hashPattern.MatchString(k.CursorSHA256) || !hashPattern.MatchString(k.HighWatermarkSHA256) {
			return ErrInvalid
		}
	case ControlPhaseConfirmExisting:
		r, ok := o.Request.(ConfirmExistingReceiverRequest)
		if !ok {
			return ErrInvalid
		}
		x := r.XIAdoptionReceipt
		f := r.FeedReadReceipt
		if !hashPattern.MatchString(r.AdoptionBindingSHA256) || r.XIAdoptionReceiptSHA256 != PublicCanonicalSHA256(x) || r.FeedReadReceiptSHA256 != PublicCanonicalSHA256(f) || x.InstallationID != o.InstallationID || x.BindOperationID != o.PredecessorOperationID || x.BindResultSHA256 != o.PredecessorResultSHA256 || !hashPattern.MatchString(x.CandidateSHA256) || !v3UUID.MatchString(x.StateID) || x.ConsumerEpoch < 1 || !controlFeedOperationID.MatchString(x.EpochOperationID) || !hashPattern.MatchString(x.CommittedCursorSHA256) || !hashPattern.MatchString(x.CommittedHighWatermarkSHA256) || !validV3Time(x.CommittedAt) || x.PagesCommittedBefore < 0 || !v3DecimalID.MatchString(f.FeedKeyID) || !hashPattern.MatchString(f.ReceiverRegistrationSHA256) || !hashPattern.MatchString(f.WorkerConfigSHA256) || f.CheckpointStateID != x.StateID || f.ConsumerEpoch != x.ConsumerEpoch || f.EpochOperationID != x.EpochOperationID || !validV3Time(f.StartedAt) || !validV3Time(f.CompletedAt) || !v3Time(f.StartedAt).After(v3Time(x.CommittedAt)) || v3Time(f.CompletedAt).Before(v3Time(f.StartedAt)) || f.PagesCommitted <= x.PagesCommittedBefore || !hashPattern.MatchString(f.CursorSHA256) || !hashPattern.MatchString(f.HighWatermarkSHA256) || f.ResponseStatus != 200 {
			return ErrInvalid
		}
	case ControlPhaseDeliverMachine:
		r, ok := o.Request.(DeliverMachineRequest)
		if !ok {
			return ErrInvalid
		}
		m := r.Registration
		c := r.CustodyReceipt
		if !validSourceInstance(r.SourceInstance) || !hashPattern.MatchString(r.SourceBindingSHA256) || !validMachineRegistrationV3(m) || m.InstallationID != o.InstallationID || r.RegistrationSHA256 != PublicCanonicalSHA256(m) || r.CustodyReceiptSHA256 != PublicCanonicalSHA256(c) || c.InstallationID != m.InstallationID || c.Purpose != m.Purpose || c.OperationID != m.OperationID || c.RegistrationSHA256 != r.RegistrationSHA256 || c.CustodyState != "prepared" {
			return ErrInvalid
		}
	}
	return nil
}
func ValidateControlResultV3(r ControlResultV3) error {
	if r.SchemaVersion != ControlResultV3Schema || !remoteOperationID.MatchString(r.OperationID) || !validExactExternalIdentity(r.InstallationID) || !v3KindPhase(r.Kind, r.Phase) || !validV3Time(r.ObservedAt) {
		return ErrInvalid
	}
	if r.Status == ControlResultFailed {
		if r.Result != nil {
			return ErrInvalid
		}
		if r.ReasonCode == "installation_authority_changed" {
			return nil
		}
		if r.Kind == ControlKindPairMachine && (r.ReasonCode == "machine_custody_unavailable" || r.ReasonCode == "machine_purpose_conflict") {
			return nil
		}
		if r.Kind == ControlKindAdoptExisting && (r.ReasonCode == "adoption_identity_conflict" || r.ReasonCode == "adoption_binding_conflict" || r.ReasonCode == "receiver_read_unavailable") {
			return nil
		}
		return ErrInvalid
	}
	if r.Status != ControlResultCompleted || r.ReasonCode != "" {
		return ErrInvalid
	}
	switch r.Phase {
	case ControlPhaseBindExisting:
		b, ok := r.Result.(BindExistingReceiverResult)
		if !ok || !validSourceInstance(b.SourceInstance) || !hashPattern.MatchString(b.CandidateSHA256) || b.AdoptionBindingSHA256 != PublicCanonicalSHA256(b.AdoptionBinding) || b.AdoptionBinding.InstallationID != r.InstallationID || b.AdoptionBinding.OperationID != r.OperationID || b.AdoptionBinding.OrgID < 1 || b.AdoptionBinding.ProjectID < 1 || !v3DecimalID.MatchString(b.AdoptionBinding.FeedKeyID) || b.AdoptionBinding.CandidateSHA256 != b.CandidateSHA256 || b.SourceState != "active" || b.ReceiverState != "feed_active" || b.Readiness != "existing_receiver_bound" {
			return ErrInvalid
		}
	case ControlPhaseConfirmExisting:
		b, ok := r.Result.(ConfirmExistingReceiverResult)
		if !ok || !validSourceInstance(b.SourceInstance) || !hashPattern.MatchString(b.AdoptionBindingSHA256) || !hashPattern.MatchString(b.XIAdoptionReceiptSHA256) || !hashPattern.MatchString(b.FeedReadReceiptSHA256) || b.SourceState != "active" || b.ReceiverState != "feed_active" || b.Readiness != "existing_receiver_adopted" {
			return ErrInvalid
		}
	case ControlPhaseDeliverMachine:
		b, ok := r.Result.(DeliverMachineResult)
		if !ok || !validSourceInstance(b.SourceInstance) || (b.Purpose != PolicyManagePurpose && b.Purpose != SourceEnrollPurpose) || !hashPattern.MatchString(b.RegistrationSHA256) || !hashPattern.MatchString(b.CustodyReceiptSHA256) || b.Readiness != "public_candidate_delivered" || b.Activation != "not_asserted" {
			return ErrInvalid
		}
	}
	return nil
}
func ValidateControlResultForOperationV3(o ControlOperationV3, r ControlResultV3) error {
	if ValidateControlOperationV3(o) != nil || ValidateControlResultV3(r) != nil || o.OperationID != r.OperationID || o.InstallationID != r.InstallationID || o.Kind != r.Kind || o.Phase != r.Phase {
		return ErrInvalid
	}
	if r.Status == ControlResultFailed {
		return nil
	}
	switch q := o.Request.(type) {
	case BindExistingReceiverRequest:
		b := r.Result.(BindExistingReceiverResult)
		if b.SourceInstance != q.Candidate.SourceInstance || b.CandidateSHA256 != q.CandidateSHA256 || b.AdoptionBinding.FeedKeyID != q.Candidate.FeedKeyID {
			return ErrConflict
		}
	case ConfirmExistingReceiverRequest:
		b := r.Result.(ConfirmExistingReceiverResult)
		if b.AdoptionBindingSHA256 != q.AdoptionBindingSHA256 || b.XIAdoptionReceiptSHA256 != q.XIAdoptionReceiptSHA256 || b.FeedReadReceiptSHA256 != q.FeedReadReceiptSHA256 {
			return ErrConflict
		}
	case DeliverMachineRequest:
		b := r.Result.(DeliverMachineResult)
		if b.SourceInstance != q.SourceInstance || b.Purpose != q.Registration.Purpose || b.RegistrationSHA256 != q.RegistrationSHA256 || b.CustodyReceiptSHA256 != q.CustodyReceiptSHA256 {
			return ErrConflict
		}
	}
	return nil
}

// ValidateControlPredecessorV3 accepts only completed, exact canonical v2/v3
// predecessors. Journal additionally verifies stored request/result and fence.
func ValidateControlPredecessorV3(o ControlOperationV3, raw []byte) error {
	if o.Phase == ControlPhaseBindExisting {
		if len(raw) != 0 {
			return ErrConflict
		}
		return nil
	}
	if hashBytes(raw) != o.PredecessorResultSHA256 {
		return ErrConflict
	}
	var s struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return ErrInvalid
	}
	if s.SchemaVersion == ControlResultV2Schema && o.Phase == ControlPhaseDeliverMachine {
		p, e := DecodeControlResultV2(raw)
		if e != nil || p.Status != ControlResultCompleted || p.Phase != ControlPhaseResumeSource || p.InstallationID != o.InstallationID || p.OperationID != o.PredecessorOperationID {
			return ErrConflict
		}
		return nil
	}
	p, e := DecodeControlResultV3(raw)
	if e != nil || p.Status != ControlResultCompleted || p.InstallationID != o.InstallationID || p.OperationID != o.PredecessorOperationID {
		return ErrConflict
	}
	if o.Phase == ControlPhaseDeliverMachine {
		if p.Phase != ControlPhaseConfirmExisting {
			return ErrConflict
		}
		return nil
	}
	if p.Phase != ControlPhaseBindExisting {
		return ErrConflict
	}
	b := p.Result.(BindExistingReceiverResult)
	q := o.Request.(ConfirmExistingReceiverRequest)
	if b.AdoptionBindingSHA256 != q.AdoptionBindingSHA256 || b.CandidateSHA256 != q.XIAdoptionReceipt.CandidateSHA256 || b.AdoptionBinding.FeedKeyID != q.FeedReadReceipt.FeedKeyID {
		return ErrConflict
	}
	return nil
}

// ControlSchemaVersion chooses an explicit version before the corresponding
// closed decoder runs. It provides no compatibility fallback.
func ControlSchemaVersion(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 65536 || rejectDuplicateControlV2Fields(raw) != nil {
		return "", ErrInvalid
	}
	var v struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(raw, &v) != nil || v.SchemaVersion == "" {
		return "", ErrInvalid
	}
	return v.SchemaVersion, nil
}
