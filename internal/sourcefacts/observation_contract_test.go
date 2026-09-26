package sourcefacts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestObservationIndependentGoldenVectors(t *testing.T) {
	var fixture struct {
		Valid []struct {
			Name      string
			Document  json.RawMessage
			Canonical string `json:"canonical_utf8"`
			Hash      string `json:"source_sha256"`
		} `json:"valid"`
		Invalid []struct {
			Name      string
			Document  json.RawMessage
			Canonical string `json:"canonical_utf8"`
		} `json:"invalid"`
	}
	if e := json.Unmarshal(assets.Vectors(), &fixture); e != nil {
		t.Fatal(e)
	}
	if len(fixture.Valid) != 10 || len(fixture.Invalid) != 9 {
		t.Fatal("fixture coverage changed")
	}
	for _, v := range fixture.Valid {
		t.Run(v.Name, func(t *testing.T) {
			d, e := DecodeObservation([]byte(v.Canonical))
			if e != nil {
				t.Fatal(e)
			}
			wire, e := EncodeObservation(d)
			if e != nil {
				t.Fatal(e)
			}
			if string(wire) != v.Canonical || eventconnector.HashSourceBytes(wire) != v.Hash {
				t.Fatal("independent source identity/bytes/hash differ")
			}
		})
	}
	for _, v := range fixture.Invalid {
		t.Run(v.Name, func(t *testing.T) {
			if v.Canonical == "" {
				t.Fatal("missing independent invalid wire")
			}
			if _, e := DecodeObservation([]byte(v.Canonical)); e == nil {
				t.Fatal("accepted invalid golden document")
			}
		})
	}
}

func TestObservationFiniteHandlerStorageBounds(t *testing.T) {
	// Relate finite source kinds to the actual route-policy maximum. These are
	// conservative raw-source ceilings, not database/WAL or performance claims.
	count := func(profile string) int {
		n := 0
		for _, kind := range observationKinds[profile] {
			if !strings.HasPrefix(kind, "media_") && kind != "state_reconciled" {
				n++
			}
		}
		return n
	}
	runtime, perAttempt := count(RuntimeProfile), count(AttemptProfile)+count(CapabilityProfile)+count(DestinationProfile)
	if runtime != 10 || perAttempt != 13 || guardrail.SourcePolicyLimit != 771 {
		t.Fatalf("update documented source bounds: runtime=%d attempt=%d policy=%d", runtime, perAttempt, guardrail.SourcePolicyLimit)
	}
	policy := routingpolicy.Default()
	policy.MaxRetryCount, policy.MaxCandidateCount = 10, 128
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	policy.MaxRetryCount++
	if policy.Validate() == nil {
		t.Fatal("retry maximum changed; review per-handler bound")
	}
	policy.MaxRetryCount = 10
	policy.MaxCandidateCount++
	if policy.Validate() == nil {
		t.Fatal("candidate maximum changed; review per-handler bound")
	}
	if mediajob.MaxSourceMediaRoutes != 128 {
		t.Fatal("media route bound changed")
	}
	for name, rows := range map[string]int{"chat_embeddings": runtime + perAttempt*11 + guardrail.SourcePolicyLimit, "responses_messages": runtime + perAttempt*128 + guardrail.SourcePolicyLimit, "media": 2 + 10*(5+6*mediajob.MaxSourceMediaRoutes)} {
		t.Logf("%s: B revision-1 row ceiling=%d raw source bytes ceiling=%d; excludes DB/index/WAL", name, rows, int64(rows)*MaxDocumentBytes)
	}
}

func TestObservationCapabilityOriginalCollectionBoundsFitOneDocument(t *testing.T) {
	d := observationFixture()
	d.Profile = CapabilityProfile
	d.SubjectKind = "provider_attempt"
	d.SubjectID = "attempt-1"
	d.Kind = "selection"
	d.CaptureExpected = []string{"selection"}
	d.Runtime = nil
	d.Capability = &CapabilityObservation{ModelRef: strings.Repeat("m", 128), Provider: strings.Repeat("p", 64), Operation: strings.Repeat("o", 128), Surface: strings.Repeat("s", 128), Protocol: ProfileReference{ID: strings.Repeat("i", 128), Version: strings.Repeat("v", 64)}, ExecutionProfile: ProfileReference{ID: strings.Repeat("e", 128), Version: strings.Repeat("v", 64)}, ReleaseRef: strings.Repeat("r", 256), BindingRef: strings.Repeat("b", 256), ProbeRef: strings.Repeat("p", 256), ApprovalRef: strings.Repeat("a", 256), EvidenceState: "available", RequiredCapabilities: []string{}, Capabilities: []CapabilityReference{}}
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("%s%05d", strings.Repeat("c", 75), i)
		d.Capability.RequiredCapabilities = append(d.Capability.RequiredCapabilities, id)
		if i < 64 {
			d.Capability.Capabilities = append(d.Capability.Capabilities, CapabilityReference{CapabilityID: id, SupportState: "enabled", EvidenceState: strings.Repeat("\x01", 32), EvidenceRefSHA256: "sha256:" + strings.Repeat("a", 64), Revision: "9223372036854775807", VerifiedAt: d.OccurredAt})
		}
	}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	wire, err := EncodeObservation(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > 65536 {
		t.Fatalf("original collection bounds exceed document bound: %d", len(wire))
	}
	t.Logf("128 required capabilities plus 64 evidence entries: %d canonical bytes", len(wire))
	d.Capability.RequiredCapabilities = append(d.Capability.RequiredCapabilities, strings.Repeat("z", 80))
	if d.Validate() == nil {
		t.Fatal("accepted unsupported requirement count")
	}
}

func observationFixture() Observation {
	d := Observation{SchemaVersion: ObservationVersion, SourceInstance: "synthetic-ur", OrgID: "2", ProjectID: "3", RequestID: "request-1", Profile: RuntimeProfile, SubjectKind: "request", SubjectID: "request-1", Kind: "request_started", Revision: "1", OccurredAt: "2026-09-10T00:00:00.000000Z", RecordedAt: "2026-09-10T00:00:00.000000Z", Coverage: "observed", CaptureExpected: []string{"request_started", "request_terminal"}, Runtime: &RuntimeObservation{Operation: "openai.chat_completions.v1", StreamMode: "not_observed", Admission: "authenticated", Provider: "not_started", LocalDelivery: "not_started", Financial: "not_created", Capture: "observed"}}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	return d
}

func TestObservationIdentitySeparatesAttemptsAndMeasurementBoundaries(t *testing.T) {
	d := observationFixture()
	wire, e := EncodeObservation(d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeObservation(wire); e != nil {
		t.Fatal(e)
	}
	if _, e = Decode(wire); e == nil {
		t.Fatal("A decoder accepted a B document")
	}
	a := Document{SourceInstance: d.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, Profile: d.Profile, RequestID: d.RequestID, Stage: d.Kind}
	if a.factID() == d.SourceFactID {
		t.Fatal("A and B identity domains collide")
	}
	d.Profile = AttemptProfile
	d.SubjectKind = "provider_attempt"
	d.SubjectID = "attempt-1"
	d.Kind = "attempt_registered"
	d.Runtime = nil
	d.CaptureExpected = []string{"attempt_registered", "provider_response_received"}
	d.Attempt = &AttemptObservation{AttemptID: d.SubjectID, Operation: "openai.chat_completions.v1", Provider: "registered", LocalDelivery: "not_started", Financial: "reserved", EvidenceRef: "attempt-1"}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	first := d.SourceFactID
	if _, e = EncodeObservation(d); e != nil {
		t.Fatal(e)
	}
	d.SubjectID = "attempt-2"
	d.Attempt.AttemptID = d.SubjectID
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	if _, e = EncodeObservation(d); e != nil {
		t.Fatal(e)
	}
	if d.SourceFactID == first {
		t.Fatal("two real attempts share an identity")
	}
	d = observationFixture()
	d.Kind = "first_output_measurement"
	first = d.factID()
	d.Kind = "stream_measurement"
	if first == d.factID() {
		t.Fatal("first output and completion collide")
	}
}

func TestObservationStrictDecoderRejectsDisclosureAndShapeChanges(t *testing.T) {
	wire, e := EncodeObservation(observationFixture())
	if e != nil {
		t.Fatal(e)
	}
	for name, b := range map[string][]byte{
		"unknown_secret": append([]byte(`{"bearer":"synthetic-only",`), wire[1:]...),
		"duplicate":      bytes.Replace(wire, []byte(`"revision":"1"`), []byte(`"revision":"1","revision":"1"`), 1),
		"null":           bytes.Replace(wire, []byte(`"capture_expected":["request_started","request_terminal"]`), []byte(`"capture_expected":null`), 1),
		"number_id":      bytes.Replace(wire, []byte(`"org_id":"2"`), []byte(`"org_id":2`), 1),
		"wrong_payload":  bytes.Replace(wire, []byte(`"runtime":`), []byte(`"attempt":`), 1),
		"noncanonical":   append([]byte(" "), wire...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodeObservation(b); e == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
}

func TestMeasurementZeroMissingAndBoundaries(t *testing.T) {
	zero := "0"
	m := Measurement{Kind: "stream_first_valid_output", Version: "1", Value: &zero, Unit: "nanoseconds", State: "observed", StartBoundary: "trusted_ingress", EndBoundary: "first_meaningful_frame_written_and_flushed", Resolution: "nanosecond_representation", Source: "process_monotonic_clock"}
	if e := m.Validate(); e != nil {
		t.Fatal(e)
	}
	m.State = "not_observed"
	if e := m.Validate(); e == nil {
		t.Fatal("missing measurement accepted as zero")
	}
	m.Value = nil
	if e := m.Validate(); e != nil {
		t.Fatal(e)
	}
	m.State = "observed"
	if e := m.Validate(); e == nil {
		t.Fatal("observed without value")
	}
	m.Value = &zero
	m.EndBoundary = "headers_sent"
	if e := m.Validate(); e == nil {
		t.Fatal("headers are not a valid first output")
	}
	m.EndBoundary = "first_meaningful_frame_written_and_flushed"
	m.Source = "wall_clock_subtraction"
	if e := m.Validate(); e == nil {
		t.Fatal("wall clock impersonated monotonic measurement")
	}
}

func TestDestinationOriginNeverContainsPathQueryOrUserinfo(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTPS://user:synthetic@EXAMPLE.invalid:443/v1?key=synthetic#fragment": "https://example.invalid",
		"http://127.0.0.1:8123/provider/path":                                  "http://127.0.0.1:8123",
		"http://[::1]:80/private":                                              "http://[::1]",
	} {
		got, e := DisclosureOrigin(raw)
		if e != nil || got != want {
			t.Fatalf("origin %q: %q %v", raw, got, e)
		}
	}
	for _, raw := range []string{"file:///secret", "https://", "https://example.invalid:0", "https://example.invalid:65536", "https://example.invalid:00443", "https://host%0a.invalid"} {
		if _, e := DisclosureOrigin(raw); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	d := observationFixture()
	d.Profile = DestinationProfile
	d.SubjectKind = "provider_attempt"
	d.SubjectID = "attempt-1"
	d.Kind = "dispatch_registered"
	d.Runtime = nil
	d.CaptureExpected = []string{"dispatch_registered"}
	d.Destination = &DestinationObservation{Provider: "synthetic", Origin: "https://example.invalid/path?key=synthetic", RegionSource: "not_available", Dispatch: "registered", PolicyEvidence: "not_observed", InputBindingState: "unknown"}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	if _, e := EncodeObservation(d); e == nil {
		t.Fatal("unprojected URL accepted")
	}
	d.Destination.Origin = "https://example.invalid"
	wire, e := EncodeObservation(d)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(wire), "key=") {
		t.Fatal("secret query leaked")
	}
}
