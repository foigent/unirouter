package sourcefacts

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

func syntheticContentManifest(body []byte) ContentManifest {
	const stamp = "2026-09-15T00:00:00.000000Z"
	m := ContentManifest{SchemaVersion: ContentManifestVersion, SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", RequestID: "synthetic-request", Profile: ContentProfile, Revision: "1", OccurredAt: stamp, RecordedAt: stamp, Protocol: "openai.chat", ObservationPoint: "client_request", ContentItemID: "body-0", Capture: ContentCapture{Mode: "full_content", PolicyRef: "synthetic-policy", PolicyVersion: "1", PolicyEffectiveAt: stamp, Completeness: "complete", Reason: "none"}, ContentRef: "uc1_" + strings.Repeat("a", 64), Representation: &ContentRepresentation{MediaType: "application/octet-stream", Encoding: "identity", ByteDomain: ContentByteDomain, SavedBytes: int64(len(body)), Digest: eventconnector.HashSourceBytes(body)}, CapturedAt: stamp, ExpiresAt: "2026-09-16T00:00:00.000000Z"}
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	return m
}
func TestContentManifestStoredBytes(t *testing.T) {
	for _, body := range [][]byte{{}, {0, 255, 10, 0}, []byte(`{"tool":"unfinished`)} {
		m := syntheticContentManifest(body)
		wire, err := EncodeContentManifest(m)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeContentManifest(wire)
		if err != nil {
			t.Fatal(err)
		}
		if err = decoded.Representation.ValidateBytes(body); err != nil {
			t.Fatal(err)
		}
		if err = decoded.Representation.ValidateBytes(append(append([]byte{}, body...), 1)); err == nil {
			t.Fatal("accepted different saved bytes")
		}
		meta, err := m.ReadMetadata()
		if err != nil {
			t.Fatal(err)
		}
		if meta.SourceSHA256 != eventconnector.HashSourceBytes(wire) {
			t.Fatal("metadata does not hash complete manifest")
		}
		header, err := EncodeContentReadMetadataHeader(meta)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeContentReadMetadataHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeContentReadMetadataHeader(header + "="); err == nil {
			t.Fatal("accepted padded metadata")
		}
	}
}
func TestContentStrictCanonicalManifest(t *testing.T) {
	wire, err := EncodeContentManifest(syntheticContentManifest(nil))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"whitespace":         append([]byte(" "), wire...),
		"trailing":           append(append([]byte{}, wire...), []byte("{}")...),
		"unknown":            bytes.Replace(wire, []byte(`{"capture":`), []byte(`{"unknown":1,"capture":`), 1),
		"duplicate":          bytes.Replace(wire, []byte(`"saved_bytes":0`), []byte(`"saved_bytes":0,"saved_bytes":0`), 1),
		"missing_zero":       bytes.Replace(wire, []byte(`,"saved_bytes":0`), nil, 1),
		"null":               bytes.Replace(wire, []byte(`"saved_bytes":0`), []byte(`"saved_bytes":null`), 1),
		"unpaired_surrogate": bytes.Replace(wire, []byte(`synthetic-request`), []byte(`\ud800`), 1),
		"invalid_utf8":       bytes.Replace(wire, []byte(`synthetic-request`), []byte{255}, 1),
		"empty_optional":     bytes.Replace(wire, []byte(`{"capture":`), []byte(`{"attempt_id":"","capture":`), 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(bad, wire) {
				t.Fatal("mutation did not apply")
			}
			if _, err := DecodeContentManifest(bad); err == nil {
				t.Fatal("accepted invalid document")
			}
		})
	}
	meta, _ := syntheticContentManifest(nil).ReadMetadata()
	mw, _ := EncodeContentReadMetadata(meta)
	if _, err := DecodeContentReadMetadata(bytes.Replace(mw, []byte(`,"saved_bytes":0`), nil, 1)); err == nil {
		t.Fatal("metadata accepted missing required zero")
	}
}
func TestContentManifestIdentityAndPredecessor(t *testing.T) {
	m := syntheticContentManifest(nil)
	// Fixed independent vector: four-byte lengths, UTF-8 components, then SHA-256.
	if m.FactID() != "uf1_83597048a12e6b6b4bc52d1a3d2a44bf17b36a8a863555ce2d27321149e00320" {
		t.Fatalf("fact vector %s", m.FactID())
	}
	changed := m
	changed.ContentRef = "uc1_" + strings.Repeat("b", 64)
	if changed.FactID() != m.FactID() {
		t.Fatal("content ref changes logical identity")
	}
	changed = m
	changed.AttemptID = "attempt-1"
	if changed.FactID() == m.FactID() {
		t.Fatal("attempt not bound")
	}
	a := m
	a.RequestID = "ab"
	a.ObservationPoint = "c"
	b := m
	b.RequestID = "a"
	b.ObservationPoint = "bc"
	if a.FactID() == b.FactID() {
		t.Fatal("identity components not framed")
	}
	prevWire, _ := EncodeContentManifest(m)
	next := m
	next.Revision = "2"
	next.PreviousRevision = "1"
	next.PreviousHash = eventconnector.HashSourceBytes(prevWire)
	next.CorrectionReason = "capture_finished"
	next.EventID = next.EventIDForRevision()
	if err := ValidateContentPredecessor(next, m); err != nil {
		t.Fatal(err)
	}
	changed = m
	changed.RecordedAt = "2026-09-15T00:00:00.000001Z"
	if err := ValidateContentPredecessor(next, changed); err == nil {
		t.Fatal("accepted wrong predecessor bytes")
	}
	next.PreviousRevision = "2"
	if err := next.Validate(); err == nil {
		t.Fatal("accepted nonconsecutive revision")
	}
}
func TestContentManifestStateBoundaries(t *testing.T) {
	for name, change := range map[string]func(*ContentManifest){
		"provider_attempt": func(m *ContentManifest) {
			m.ObservationPoint = "provider_request"
			m.SourceFactID = m.FactID()
			m.EventID = m.EventIDForRevision()
		},
		"pending_reference": func(m *ContentManifest) { m.Capture.Completeness = "pending"; m.Capture.Reason = "capture_in_progress" },
		"metadata_complete": func(m *ContentManifest) { m.Capture.Mode = "metadata_only" },
		"partial_none":      func(m *ContentManifest) { m.Capture.Completeness = "partial" },
		"redaction_missing": func(m *ContentManifest) { m.Capture.Mode = "redacted" },
		"unsafe_integer":    func(m *ContentManifest) { m.Representation.SavedBytes = contentMaxSafeInteger + 1 },
		"first_predecessor": func(m *ContentManifest) { m.PreviousRevision = "1" },
	} {
		t.Run(name, func(t *testing.T) {
			m := syntheticContentManifest(nil)
			change(&m)
			if m.Validate() == nil {
				t.Fatal("accepted invalid boundary")
			}
		})
	}
	m := syntheticContentManifest(nil)
	m.Capture.Completeness = "pending"
	m.Capture.Reason = "capture_in_progress"
	m.ContentRef = ""
	m.Representation = nil
	m.CapturedAt = ""
	m.ExpiresAt = ""
	if _, err := EncodeContentManifest(m); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadMetadata(); err == nil {
		t.Fatal("pending is readable")
	}
}
func TestContentAccessAndLimits(t *testing.T) {
	a := ContentAccess{RegistrationHash: "sha256:" + strings.Repeat("a", 64), Profile: ContentProfile, ContentScopeRevision: "1", AllowedPurposes: []string{}}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Enabled = true
	if a.Validate() == nil {
		t.Fatal("enabled without grant")
	}
	a.AllowedPurposes = []string{ContentHumanReview}
	a.CoverageFrom = &ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "0"}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Enabled = false
	if a.Validate() == nil {
		t.Fatal("disabled retains purpose")
	}
	a.AllowedPurposes = []string{}
	if a.Validate() != nil {
		t.Fatal("disabled cannot preserve coverage")
	}
	a.AllowedPurposes = nil
	if a.Validate() == nil {
		t.Fatal("nil purposes serializes forbidden null")
	}
	limits := ContentLimits{1, 2, 3, 4, 5}
	if limits.Validate() != nil {
		t.Fatal("valid limits")
	}
	limits.MaxBufferBytes = 0
	if limits.Validate() == nil {
		t.Fatal("unbounded buffer")
	}
	b := ContentBoundary{Time: "2026-09-15T00:00:00.000000Z", AfterSequence: "01"}
	if b.Validate() == nil {
		t.Fatal("noncanonical sequence")
	}
	// Required false and empty array survive the canonical encoder.
	a.AllowedPurposes = []string{}
	wire, err := encodeContentCanonical(a)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(wire, &fields)
	if string(fields["enabled"]) != "false" || string(fields["allowed_purposes"]) != "[]" {
		t.Fatal("required fields lost")
	}
}
