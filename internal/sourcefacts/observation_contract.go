package sourcefacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

// ObservationVersion has its own identity domain. A documents, descriptors and
// cursor bytes remain governed exclusively by Version and Profiles.
const (
	ObservationVersion = assets.SchemaVersion
	RuntimeProfile     = "unirouter.runtime-observation.v1"
	AttemptProfile     = "unirouter.attempt-observation.v1"
	CapabilityProfile  = "unirouter.capability-snapshot.v1"
	PolicyProfile      = "unirouter.policy-observation.v1"
	DestinationProfile = "unirouter.destination-observation.v1"
)

func v1ObservationProfiles() []string {
	return []string{RuntimeProfile, AttemptProfile, CapabilityProfile, PolicyProfile, DestinationProfile}
}

func ObservationProfiles() []string {
	return append(v1ObservationProfiles(), GatewayPreDispatchProfile)
}

func validObservationProfile(profile string) bool {
	return slices.Contains(ObservationProfiles(), profile)
}

// Kinds are finite execution boundaries, never token, frame, trace or heartbeat
// samples. Each identity permits one immutable observation and explicit revisions.
var observationKinds = map[string][]string{
	RuntimeProfile:     {"request_started", "admission_rejected", "request_terminal", "delivery_started", "local_delivery", "output_held", "request_context_done", "response_measurement", "first_output_measurement", "stream_measurement", "media_running", "media_terminal", "state_reconciled"},
	AttemptProfile:     {"attempt_registered", "provider_response_received", "provider_rejected", "provider_outcome_unknown", "provider_response_unusable", "financial_result", "delivery_started", "local_delivery", "output_held", "round_trip_measurement", "media_attempt_registered", "media_provider_succeeded", "media_retry_scheduled", "media_result", "state_reconciled"},
	CapabilityProfile:  {"selection"},
	PolicyProfile:      {"input_result", "output_result", "stream_result"},
	DestinationProfile: {"dispatch_registered", "dispatch_result"},
}

type Observation struct {
	SchemaVersion     string                  `json:"schema_version"`
	SourceInstance    string                  `json:"source_instance"`
	OrgID             string                  `json:"org_id"`
	ProjectID         string                  `json:"project_id"`
	RequestID         string                  `json:"request_id"`
	Profile           string                  `json:"profile"`
	SubjectKind       string                  `json:"subject_kind"`
	SubjectID         string                  `json:"subject_id"`
	Kind              string                  `json:"observation_kind"`
	SourceFactID      string                  `json:"source_fact_id"`
	Revision          string                  `json:"revision"`
	EventID           string                  `json:"event_id"`
	PreviousRevision  string                  `json:"previous_revision,omitempty"`
	PreviousHash      string                  `json:"previous_hash,omitempty"`
	CorrectionReason  string                  `json:"correction_reason,omitempty"`
	OccurredAt        string                  `json:"occurred_at"`
	RecordedAt        string                  `json:"recorded_at"`
	Coverage          string                  `json:"coverage"`
	ContextRef        string                  `json:"context_ref,omitempty"`
	RouteEvidenceHash string                  `json:"route_evidence_hash,omitempty"`
	CaptureExpected   []string                `json:"capture_expected"`
	Runtime           *RuntimeObservation     `json:"runtime,omitempty"`
	Attempt           *AttemptObservation     `json:"attempt,omitempty"`
	Capability        *CapabilityObservation  `json:"capability,omitempty"`
	Policy            *PolicyObservation      `json:"policy,omitempty"`
	Destination       *DestinationObservation `json:"destination,omitempty"`
	Measurement       *Measurement            `json:"measurement,omitempty"`
}

type RuntimeObservation struct {
	Operation         string `json:"operation"`
	StreamMode        string `json:"stream_mode"`
	Admission         string `json:"admission"`
	Provider          string `json:"provider"`
	LocalDelivery     string `json:"local_delivery"`
	Financial         string `json:"financial"`
	Capture           string `json:"capture"`
	EvidenceRef       string `json:"evidence_ref,omitempty"`
	ContextDoneReason string `json:"context_done_reason,omitempty"`
}

type AttemptObservation struct {
	AttemptID       string `json:"attempt_id"`
	Operation       string `json:"operation"`
	RouteSequence   string `json:"route_sequence,omitempty"`
	RouteTotal      string `json:"route_total,omitempty"`
	Provider        string `json:"provider"`
	LocalDelivery   string `json:"local_delivery"`
	Financial       string `json:"financial"`
	RetryAuthorized bool   `json:"retry_authorized"`
	HTTPStatus      string `json:"http_status,omitempty"`
	EvidenceRef     string `json:"evidence_ref"`
}

type ProfileReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
}

// Only selected, committed authority belongs here. None of these references is
// read from mutable current configuration when a terminal record is replayed.
type CapabilityObservation struct {
	ModelRef               string                `json:"model_ref"`
	Provider               string                `json:"provider"`
	Operation              string                `json:"operation"`
	Surface                string                `json:"surface"`
	Protocol               ProfileReference      `json:"protocol"`
	ExecutionProfile       ProfileReference      `json:"execution_profile"`
	ReleaseRef             string                `json:"release_ref,omitempty"`
	SlotVersion            string                `json:"slot_version,omitempty"`
	BindingRef             string                `json:"binding_ref,omitempty"`
	ProbeRef               string                `json:"probe_ref,omitempty"`
	ApprovalRef            string                `json:"approval_ref,omitempty"`
	EvidenceState          string                `json:"evidence_state"`
	RequiredCapabilities   []string              `json:"required_capabilities"`
	Capabilities           []CapabilityReference `json:"capabilities"`
	CapabilityEvidenceHash string                `json:"capability_evidence_hash,omitempty"`
}

// EvidenceRefSHA256 hashes the exact authority's reference text. It is not a
// digest of the referenced artifact, and discloses no path, URL or credential.
type CapabilityReference struct {
	CapabilityID      string `json:"capability_id"`
	SupportState      string `json:"support_state"`
	EvidenceState     string `json:"evidence_state"`
	EvidenceRefSHA256 string `json:"evidence_ref_sha256"`
	VerifiedAt        string `json:"verified_at"`
	Revision          string `json:"revision"`
}

type PolicyObservation struct {
	DetectorOutcomes []string `json:"detector_outcomes"`
	StageDecisions   []string `json:"stage_decisions"`
	PolicyRef        string   `json:"policy_ref"`
	PolicyVersion    string   `json:"policy_version"`
	DetectorRef      string   `json:"detector_ref"`
	DetectorVersion  string   `json:"detector_version"`
	Stage            string   `json:"stage"`
	Mode             string   `json:"mode"`
	ConfiguredAction string   `json:"configured_action"`
	Outcome          string   `json:"outcome"`
	FailureMode      string   `json:"failure_mode"`
	Degraded         bool     `json:"degraded"`
	Mandatory        bool     `json:"mandatory"`
	Effects          []string `json:"effects"`
	EvidenceState    string   `json:"evidence_state"`
}

type DestinationObservation struct {
	Provider          string `json:"provider"`
	Origin            string `json:"origin"`
	Region            string `json:"region,omitempty"`
	RegionSource      string `json:"region_source"`
	Dispatch          string `json:"dispatch"`
	PolicyEvidence    string `json:"policy_evidence"`
	InputBindingState string `json:"input_binding_state"`
	InputBindingRef   string `json:"input_binding_ref,omitempty"`
}

type Measurement struct {
	Kind          string  `json:"kind"`
	Version       string  `json:"version"`
	Value         *string `json:"value,omitempty"`
	Unit          string  `json:"unit"`
	State         string  `json:"state"`
	StartBoundary string  `json:"start_boundary"`
	EndBoundary   string  `json:"end_boundary"`
	Resolution    string  `json:"resolution"`
	Source        string  `json:"source"`
	EvidenceRef   string  `json:"evidence_ref,omitempty"`
}

var measurementBoundaries = map[string][2]string{
	"request_response_complete": {"trusted_ingress", "protocol_response_written"},
	"stream_first_valid_output": {"trusted_ingress", "first_meaningful_frame_written_and_flushed"},
	"stream_complete":           {"trusted_ingress", "protocol_terminal_written_and_flushed"},
	"attempt_http_round_trip":   {"http_do_called", "http_do_returned"},
}

func (d Observation) factID() string {
	return sourceID(ObservationVersion, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.SubjectKind, d.SubjectID, d.Kind)
}

func (d Observation) eventID() string {
	return sourceID(ObservationVersion+".revision", d.factID(), d.Revision)
}

func opaque(value string, maximum int) bool {
	_, e := eventconnector.NewOpaqueString("observation", value, 1, maximum, true)
	return e == nil
}

func oneOf(value string, options ...string) bool { return slices.Contains(options, value) }

func observationTime(value string) bool {
	t, e := time.Parse(time.RFC3339Nano, value)
	return e == nil && t.UTC().Format("2006-01-02T15:04:05.000000Z") == value
}

func (d Observation) Validate() error {
	invalid := errors.New("invalid source observation")
	if d.SchemaVersion != ObservationVersion || !slices.Contains(v1ObservationProfiles(), d.Profile) || !positiveID(d.OrgID) || !positiveID(d.ProjectID) || !positiveID(d.Revision) || !opaque(d.SourceInstance, 128) || !opaque(d.RequestID, 64) || !opaque(d.SubjectID, 128) {
		return invalid
	}
	if !oneOf(d.SubjectKind, "request", "provider_attempt", "media_job", "media_attempt", "policy") || !slices.Contains(observationKinds[d.Profile], d.Kind) || !oneOf(d.Coverage, "observed", "partial", "not_observed") || !observationTime(d.OccurredAt) || !observationTime(d.RecordedAt) || d.SourceFactID != d.factID() || d.EventID != d.eventID() {
		return invalid
	}
	if d.Profile == RuntimeProfile && ((!oneOf(d.SubjectKind, "request", "media_job") && !(d.SubjectKind == "media_attempt" && d.Kind == "media_running")) || (d.Kind == "media_running" && d.SubjectKind != "media_attempt")) || d.Profile == PolicyProfile && d.SubjectKind != "policy" || oneOf(d.Profile, AttemptProfile, CapabilityProfile, DestinationProfile) && !oneOf(d.SubjectKind, "provider_attempt", "media_attempt") || d.SubjectKind == "request" && d.SubjectID != d.RequestID {
		return invalid
	}
	if d.Revision == "1" {
		if d.PreviousRevision != "" || d.PreviousHash != "" || d.CorrectionReason != "" {
			return invalid
		}
	} else {
		prior, e := strconv.ParseInt(d.PreviousRevision, 10, 64)
		rev, _ := strconv.ParseInt(d.Revision, 10, 64)
		if e != nil || !positiveID(d.PreviousRevision) || prior != rev-1 || !validHash(d.PreviousHash) || !opaque(d.CorrectionReason, 128) {
			return invalid
		}
	}
	if d.ContextRef != "" && (!strings.HasPrefix(d.ContextRef, "uf1_") || !validHash("sha256:"+strings.TrimPrefix(d.ContextRef, "uf1_"))) || d.RouteEvidenceHash != "" && !validHash(d.RouteEvidenceHash) || d.CaptureExpected == nil || len(d.CaptureExpected) > 10 {
		return invalid
	}
	seen := map[string]bool{}
	for _, kind := range d.CaptureExpected {
		if !slices.Contains(observationKinds[d.Profile], kind) || seen[kind] {
			return invalid
		}
		seen[kind] = true
	}
	payloads := 0
	for _, present := range []bool{d.Runtime != nil, d.Attempt != nil, d.Capability != nil, d.Policy != nil, d.Destination != nil, d.Measurement != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return invalid
	}
	switch d.Profile {
	case RuntimeProfile:
		if kind, ok := map[string]string{"response_measurement": "request_response_complete", "first_output_measurement": "stream_first_valid_output", "stream_measurement": "stream_complete"}[d.Kind]; ok {
			if d.Measurement == nil || d.Measurement.Kind != kind {
				return invalid
			}
			return d.Measurement.Validate()
		}
		v := d.Runtime
		if v == nil || !opaque(v.Operation, 128) || !oneOf(v.StreamMode, "streaming", "non_streaming", "not_observed") || !oneOf(v.Admission, "authenticated", "admitted", "rejected", "unknown") || !providerState(v.Provider) || !deliveryState(v.LocalDelivery) || !financialState(v.Financial) || !oneOf(v.Capture, "observed", "partial", "not_observed") {
			return invalid
		}
		if v.EvidenceRef != "" && !opaque(v.EvidenceRef, 256) {
			return invalid
		}
		if d.Kind == "request_context_done" {
			if !oneOf(v.ContextDoneReason, "context_cancelled", "context_deadline_exceeded") {
				return invalid
			}
		} else if v.ContextDoneReason != "" {
			return invalid
		}
	case AttemptProfile:
		if d.Kind == "round_trip_measurement" {
			if d.Measurement == nil || d.Measurement.Kind != "attempt_http_round_trip" {
				return invalid
			}
			return d.Measurement.Validate()
		}
		v := d.Attempt
		if v == nil || !opaque(v.AttemptID, 128) || !opaque(v.Operation, 128) || !opaque(v.EvidenceRef, 256) || !providerState(v.Provider) || !deliveryState(v.LocalDelivery) || !financialState(v.Financial) {
			return invalid
		}
		if v.AttemptID != d.SubjectID || v.RetryAuthorized && v.Provider != "rejected" && d.SubjectKind != "media_attempt" || (v.RouteSequence == "") != (v.RouteTotal == "") {
			return invalid
		}
		if (v.RouteSequence != "" && !positiveID(v.RouteSequence)) || (v.RouteTotal != "" && !positiveID(v.RouteTotal)) {
			return invalid
		}
		if v.RouteSequence != "" {
			sequence, _ := strconv.ParseInt(v.RouteSequence, 10, 64)
			total, _ := strconv.ParseInt(v.RouteTotal, 10, 64)
			if sequence > total {
				return invalid
			}
		}
		if v.HTTPStatus != "" {
			n, e := strconv.Atoi(v.HTTPStatus)
			if e != nil || n < 100 || n > 599 || strconv.Itoa(n) != v.HTTPStatus {
				return invalid
			}
		}
	case CapabilityProfile:
		v := d.Capability
		if v == nil || !opaque(v.ModelRef, 128) || !opaque(v.Provider, 64) || !opaque(v.Operation, 128) || !opaque(v.Surface, 128) || !oneOf(v.EvidenceState, "available", "partial", "not_available") {
			return invalid
		}
		for _, ref := range []ProfileReference{v.Protocol, v.ExecutionProfile} {
			if !opaque(ref.ID, 128) || !opaque(ref.Version, 64) || (ref.SHA256 != "" && !validHash(ref.SHA256)) {
				return invalid
			}
		}
		for _, ref := range []string{v.ReleaseRef, v.BindingRef, v.ProbeRef, v.ApprovalRef} {
			if ref != "" && !opaque(ref, 256) {
				return invalid
			}
		}
		if v.SlotVersion != "" && !positiveID(v.SlotVersion) {
			return invalid
		}
		// The planner merges up to 64 invocation and 64 profile requirements;
		// the original dispatch evidence admits up to 64 capability entries.
		if v.RequiredCapabilities == nil || len(v.RequiredCapabilities) > 128 || v.Capabilities == nil || len(v.Capabilities) > 64 || v.CapabilityEvidenceHash != "" && !validHash(v.CapabilityEvidenceHash) {
			return invalid
		}
		for i, id := range v.RequiredCapabilities {
			if !evidenceText(id, 80) || i > 0 && id <= v.RequiredCapabilities[i-1] {
				return invalid
			}
		}
		for i, entry := range v.Capabilities {
			if !evidenceText(entry.CapabilityID, 80) || entry.SupportState != "enabled" || !evidenceText(entry.EvidenceState, 32) || !validHash(entry.EvidenceRefSHA256) || !observationTime(entry.VerifiedAt) || !positiveID(entry.Revision) || i > 0 && entry.CapabilityID <= v.Capabilities[i-1].CapabilityID {
				return invalid
			}
		}
	case PolicyProfile:
		v := d.Policy
		if v == nil || !opaque(v.PolicyRef, 160) || !opaque(v.PolicyVersion, 64) || !opaque(v.DetectorRef, 128) || !opaque(v.DetectorVersion, 64) || !oneOf(v.Stage, "input", "output", "stream") || !oneOf(v.Mode, "shadow", "enforce") || !oneOf(v.ConfiguredAction, "allow", "audit", "block", "replace", "mask", "unknown") || !oneOf(v.Outcome, "allow", "match", "block", "replace", "error", "not_covered", "unknown") || !oneOf(v.FailureMode, "open", "closed", "unknown") || !oneOf(v.EvidenceState, "observed", "partial", "not_observed") || v.Effects == nil || len(v.Effects) > 6 {
			return invalid
		}
		if len(v.DetectorOutcomes) == 0 || len(v.DetectorOutcomes) > 6 || len(v.StageDecisions) == 0 || len(v.StageDecisions) > 2 {
			return invalid
		}
		for i, value := range v.DetectorOutcomes {
			if !oneOf(value, "pass", "match", "transform", "indeterminate", "unavailable", "skipped") || i > 0 && value <= v.DetectorOutcomes[i-1] {
				return invalid
			}
		}
		for i, value := range v.StageDecisions {
			if !oneOf(value, "allow", "block") || i > 0 && value <= v.StageDecisions[i-1] {
				return invalid
			}
		}
		seen := map[string]bool{}
		for _, effect := range v.Effects {
			if seen[effect] || !oneOf(effect, "input_blocked", "input_replaced", "output_blocked", "output_replaced", "stream_terminated", "partial_output") {
				return invalid
			}
			seen[effect] = true
		}
		if v.Mode == "shadow" && len(v.Effects) > 0 {
			return invalid
		}
	case DestinationProfile:
		v := d.Destination
		if v == nil || !opaque(v.Provider, 64) || !oneOf(v.RegionSource, "configured", "not_available") || !oneOf(v.Dispatch, "registered", "do_observed", "response_observed", "unknown") || !oneOf(v.PolicyEvidence, "observed", "not_observed", "not_applicable") || !oneOf(v.InputBindingState, "known", "unknown", "not_applicable") {
			return invalid
		}
		origin, e := DisclosureOrigin(v.Origin)
		if e != nil || origin != v.Origin {
			return invalid
		}
		if (v.RegionSource == "configured") != (v.Region != "") || v.Region != "" && !opaque(v.Region, 64) || (v.InputBindingState == "known") != (v.InputBindingRef != "") || v.InputBindingRef != "" && !opaque(v.InputBindingRef, 256) {
			return invalid
		}
	}
	return nil
}

// Match the original route evidence text contract without narrowing admission.
func evidenceText(v string, limit int) bool {
	return v != "" && len(v) <= limit && v == strings.TrimSpace(v) && !strings.ContainsAny(v, "\x00\r\n")
}

func providerState(v string) bool {
	return oneOf(v, "not_started", "registered", "response_received", "rejected", "accepted", "unknown", "not_applicable")
}
func deliveryState(v string) bool {
	return oneOf(v, "not_started", "write_started", "completed", "write_failed", "interrupted", "unknown", "not_applicable")
}
func financialState(v string) bool {
	return oneOf(v, "not_created", "reserved", "prepared", "pending", "settled", "cancelled", "unknown", "not_applicable")
}

func (m Measurement) Validate() error {
	bounds, ok := measurementBoundaries[m.Kind]
	if !ok || m.Version != "1" || m.Unit != "nanoseconds" || m.Resolution != "nanosecond_representation" || m.Source != "process_monotonic_clock" || m.StartBoundary != bounds[0] || m.EndBoundary != bounds[1] || !oneOf(m.State, "observed", "not_applicable", "not_observed", "legacy_unknown", "invalid") {
		return errors.New("invalid measurement definition")
	}
	if m.State == "observed" {
		if m.Value == nil {
			return errors.New("observed measurement requires value")
		}
		n, e := strconv.ParseInt(*m.Value, 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != *m.Value {
			return errors.New("invalid measurement value")
		}
	} else if m.Value != nil {
		return errors.New("unobserved measurement cannot carry a value")
	}
	if m.EvidenceRef != "" && !opaque(m.EvidenceRef, 256) {
		return errors.New("invalid measurement reference")
	}
	return nil
}

// DisclosureOrigin is an allowlist projection. Never serialize the input URL:
// userinfo, escaped paths, query strings and fragments are deliberately absent.
func DisclosureOrigin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || !oneOf(strings.ToLower(u.Scheme), "http", "https") || u.Opaque != "" || u.Hostname() == "" {
		return "", errors.New("invalid destination origin")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if strings.ContainsAny(host, "\r\n\t /\\%") {
		return "", errors.New("invalid destination host")
	}
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return "", errors.New("invalid destination port")
		}
	}
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}

func EncodeObservation(d Observation) ([]byte, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	wire, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if e = decoder.Decode(&value); e != nil {
		return nil, e
	}
	encoded, ce := eventconnector.CanonicalJSON(value)
	if ce != nil {
		return nil, ce
	}
	if len(encoded) > MaxDocumentBytes {
		return nil, errors.New("source observation exceeds byte limit")
	}
	return encoded, nil
}

func DecodeObservation(wire []byte) (Observation, error) {
	var d Observation
	if len(wire) > MaxDocumentBytes {
		return d, errors.New("source observation exceeds byte limit")
	}
	if e := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); e != nil {
		return d, e
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&d); e != nil {
		return d, e
	}
	if e := decoder.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
		return d, errors.New("trailing observation data")
	}
	canonical, e := EncodeObservation(d)
	if e != nil {
		return d, e
	}
	if !bytes.Equal(canonical, wire) {
		return d, fmt.Errorf("source observation is not canonical")
	}
	return d, nil
}
