package sourcefacts

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const contentReadTestTime = "2026-09-15T00:00:00.000000Z"

func syntheticContentCapabilities() ContentCapabilities {
	return ContentCapabilities{SchemaVersion: ContentCapabilitiesVersion, Profile: ContentProfile, SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", Status: "active", CredentialStatus: "feed_active", SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, CaptureModes: []string{"disabled", "metadata_only"}, AllowedPurposes: []string{ContentHumanReview}, ContentScopeRevision: "1", RecoveryEpoch: "1", CoverageFrom: ContentBoundary{contentReadTestTime, "0"}, RetentionFloor: ContentBoundary{contentReadTestTime, "0"}, Limits: ContentLimits{1024, 1024, 256, 4096, 10}, Endpoints: ContentEndpoints{ContentCapabilitiesPath, ContentManifestsPath, ContentEndpointTemplate}}
}
func syntheticContentPage(count int) ContentManifestPage {
	p := ContentManifestPage{SchemaVersion: ContentManifestPageVersion, Profile: ContentProfile, SourceInstance: "synthetic-ur", OrgID: "1", ProjectID: "2", GeneratedAt: contentReadTestTime, ContentScopeRevision: "1", RecoveryEpoch: "1", CoverageFrom: ContentBoundary{contentReadTestTime, "0"}, RetentionFloor: ContentBoundary{contentReadTestTime, "0"}, AfterSequence: "0", LastSequence: strconv.Itoa(count), HighWatermarkSequence: strconv.Itoa(count), NextCursor: "synthetic_cursor", HighWatermark: "synthetic_high", Coverage: "bounded", Gaps: []ContentGap{}, Items: []ContentManifestEntry{}}
	for i := 0; i < count; i++ {
		m := syntheticContentManifest([]byte{0, 255})
		m.RequestID = "synthetic-request-" + strconv.Itoa(i)
		m.SourceFactID = m.FactID()
		m.EventID = m.EventIDForRevision()
		wire, _ := EncodeContentManifest(m)
		p.Items = append(p.Items, ContentManifestEntry{strconv.Itoa(i + 1), m, eventconnector.HashSourceBytes(wire), ContentAvailability{"available", contentReadTestTime, false}})
	}
	return p
}
func TestContentReadContractRoundTrips(t *testing.T) {
	c := syntheticContentCapabilities()
	wire, err := EncodeContentCapabilities(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeContentCapabilities(wire)
	if err != nil || !reflect.DeepEqual(c, got) {
		t.Fatalf("capabilities: %v", err)
	}
	for _, count := range []int{0, 2, 80} {
		p := syntheticContentPage(count)
		wire, err := EncodeContentManifestPage(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeContentManifestPage(wire)
		if err != nil || !reflect.DeepEqual(p, got) {
			t.Fatalf("page: %v", err)
		}
		if count == 80 && len(wire) <= MaxDocumentBytes {
			t.Fatal("did not exercise page larger than document ceiling")
		}
	}
	e := syntheticContentPage(1).Items[0]
	ew, err := EncodeContentManifestEntry(e)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeContentManifestEntry(ew); err != nil || !reflect.DeepEqual(got, e) {
		t.Fatalf("entry: %v", err)
	}
	for _, g := range []ContentGap{{Reason: "capture_health_unknown"}, {Reason: "retention_floor_ahead", RangeKnown: true, AfterSequence: "0", ThroughSequence: "3"}} {
		wire, err := EncodeContentGap(g)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DecodeContentGap(wire); err != nil || got != g {
			t.Fatalf("gap: %v", err)
		}
	}
}
func TestContentReadContractCapabilitiesConstraints(t *testing.T) {
	cases := map[string]func(*ContentCapabilities){
		"wrong version":         func(c *ContentCapabilities) { c.SchemaVersion = "unirouter.source.v2" },
		"wrong profile":         func(c *ContentCapabilities) { c.Profile = ContextProfile },
		"paused":                func(c *ContentCapabilities) { c.Status = "paused" },
		"staged":                func(c *ContentCapabilities) { c.CredentialStatus = "feed_staged" },
		"protocol duplicate":    func(c *ContentCapabilities) { c.SupportedProtocols = []string{"openai.chat", "openai.chat"} },
		"protocol invalid":      func(c *ContentCapabilities) { c.SupportedProtocols = []string{"a/b"} },
		"points empty":          func(c *ContentCapabilities) { c.ObservationPoints = []string{} },
		"points duplicate":      func(c *ContentCapabilities) { c.ObservationPoints = []string{"client_request", "client_request"} },
		"missing baseline mode": func(c *ContentCapabilities) { c.CaptureModes = []string{"disabled", "full_content"} },
		"unknown mode":          func(c *ContentCapabilities) { c.CaptureModes = append(c.CaptureModes, "automatic") },
		"purpose":               func(c *ContentCapabilities) { c.AllowedPurposes = []string{"inference"} },
		"endpoint":              func(c *ContentCapabilities) { c.Endpoints.Capabilities += "/" },
		"limits":                func(c *ContentCapabilities) { c.Limits.MaxManifestItems = 10001 },
		"scope revision":        func(c *ContentCapabilities) { c.ContentScopeRevision = "" },
		"epoch":                 func(c *ContentCapabilities) { c.RecoveryEpoch = "0" },
		"boundary":              func(c *ContentCapabilities) { c.CoverageFrom.AfterSequence = "01" },
		// C0 permits 19 digit strings. UR's existing DB, cursor and ID helpers
		// implement signed int64 only, not every schema-expressible integer.
		"org exceeds int64":   func(c *ContentCapabilities) { c.OrgID = "9223372036854775808" },
		"epoch exceeds int64": func(c *ContentCapabilities) { c.RecoveryEpoch = "9999999999999999999" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := syntheticContentCapabilities()
			mutate(&c)
			if _, err := EncodeContentCapabilities(c); err == nil {
				t.Fatal("accepted invalid capabilities")
			}
		})
	}
	c := syntheticContentCapabilities()
	c.OrgID = "9223372036854775807"
	c.CaptureModes = append(c.CaptureModes, "full_content", "redacted")
	if _, err := EncodeContentCapabilities(c); err != nil {
		t.Fatal(err)
	} // Contract vocabulary acceptance is not runtime support.
}
func TestContentReadContractAvailabilityAndGaps(t *testing.T) {
	for _, state := range []string{"available", "pending", "not_captured", "expired", "deleted", "missing", "corrupt", "source_unavailable"} {
		a := ContentAvailability{state, contentReadTestTime, oneOf(state, "pending", "source_unavailable")}
		wire, err := EncodeContentAvailability(a)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeContentAvailability(wire)
		if err != nil || got != a {
			t.Fatal(err)
		}
		a.Retryable = !a.Retryable
		if a.Validate() == nil {
			t.Fatalf("accepted retryable mismatch: %s", state)
		}
	}
	for _, g := range []ContentGap{{Reason: "invented"}, {Reason: "capture_health_unknown", AfterSequence: "0"}, {Reason: "source_recovery", RangeKnown: true, AfterSequence: "0"}, {Reason: "source_recovery", RangeKnown: true, AfterSequence: "0", ThroughSequence: "9223372036854775808"}} {
		if g.Validate() == nil {
			t.Fatal("accepted invalid gap")
		}
	}
	p := syntheticContentPage(1)
	original := p.Items[0].SourceSHA256
	p.Items[0].Availability = ContentAvailability{"expired", contentReadTestTime, false}
	if p.Validate() != nil || p.Items[0].SourceSHA256 != original {
		t.Fatal("availability altered immutable hash")
	}
	p.Coverage = "unknown"
	p.Gaps = []ContentGap{{Reason: "capture_health_unknown"}}
	if _, err := EncodeContentManifestPage(p); err != nil {
		t.Fatal(err)
	}
}
func TestContentReadContractPageBindingsAndPositions(t *testing.T) {
	cases := map[string]func(*ContentManifestPage){
		"nil items":              func(p *ContentManifestPage) { p.Items = nil },
		"nil gaps":               func(p *ContentManifestPage) { p.Gaps = nil },
		"bounded gap":            func(p *ContentManifestPage) { p.Gaps = []ContentGap{{Reason: "capture_health_unknown"}} },
		"unknown no gap":         func(p *ContentManifestPage) { p.Coverage = "unknown" },
		"hash":                   func(p *ContentManifestPage) { p.Items[0].SourceSHA256 = "sha256:" + strings.Repeat("0", 64) },
		"page scope":             func(p *ContentManifestPage) { p.ProjectID = "3" },
		"zero member":            func(p *ContentManifestPage) { p.Items[0].Sequence = "0" },
		"descending":             func(p *ContentManifestPage) { p.Items[0], p.Items[1] = p.Items[1], p.Items[0] },
		"duplicate sequence":     func(p *ContentManifestPage) { p.Items[1].Sequence = p.Items[0].Sequence },
		"last":                   func(p *ContentManifestPage) { p.LastSequence = "1" },
		"high":                   func(p *ContentManifestPage) { p.HighWatermarkSequence = "1" },
		"has more above high":    func(p *ContentManifestPage) { p.HasMore = true },
		"cursor empty":           func(p *ContentManifestPage) { p.NextCursor = "" },
		"cursor padding":         func(p *ContentManifestPage) { p.NextCursor = "abc=" },
		"cursor size":            func(p *ContentManifestPage) { p.NextCursor = strings.Repeat("a", 513) },
		"sequence exceeds int64": func(p *ContentManifestPage) { p.HighWatermarkSequence = "9223372036854775808" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := syntheticContentPage(2)
			mutate(&p)
			if _, err := EncodeContentManifestPage(p); err == nil {
				t.Fatal("accepted invalid page")
			}
		})
	}
	p := syntheticContentPage(0)
	p.AfterSequence = "3"
	p.LastSequence = "3"
	p.HighWatermarkSequence = "3"
	if p.Validate() != nil {
		t.Fatal("rejected empty polling page")
	}
	p.LastSequence = "0"
	if p.Validate() == nil {
		t.Fatal("empty page lost position")
	}
}

func contentReadTestMutate(t *testing.T, wire []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var object map[string]any
	d := json.NewDecoder(bytes.NewReader(wire))
	d.UseNumber()
	if err := d.Decode(&object); err != nil {
		t.Fatal(err)
	}
	mutate(object)
	value, err := contentCanonicalIntegers(object)
	if err != nil {
		t.Fatal(err)
	}
	result, ce := eventconnector.CanonicalJSON(value)
	if ce != nil {
		t.Fatal(ce)
	}
	return result
}
func TestContentReadContractStrictClosedCanonical(t *testing.T) {
	c, _ := EncodeContentCapabilities(syntheticContentCapabilities())
	p, _ := EncodeContentManifestPage(syntheticContentPage(1))
	a, _ := EncodeContentAvailability(ContentAvailability{"available", contentReadTestTime, false})
	g, _ := EncodeContentGap(ContentGap{Reason: "capture_health_unknown"})
	e, _ := EncodeContentManifestEntry(syntheticContentPage(1).Items[0])
	cases := []struct {
		name   string
		wire   []byte
		decode func([]byte) error
	}{
		{"capabilities", c, func(w []byte) error { _, err := DecodeContentCapabilities(w); return err }},
		{"page", p, func(w []byte) error { _, err := DecodeContentManifestPage(w); return err }},
		{"availability", a, func(w []byte) error { _, err := DecodeContentAvailability(w); return err }},
		{"gap", g, func(w []byte) error { _, err := DecodeContentGap(w); return err }},
		{"entry", e, func(w []byte) error { _, err := DecodeContentManifestEntry(w); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(tc.wire, &fields); err != nil {
				t.Fatal(err)
			}
			// These fixtures omit optional gap bounds; every present field is required,
			// including false booleans and empty arrays.
			for key := range fields {
				bad := contentReadTestMutate(t, tc.wire, func(m map[string]any) { delete(m, key) })
				if tc.decode(bad) == nil {
					t.Fatalf("accepted missing %s", key)
				}
			}
			for name, bad := range map[string][]byte{
				"whitespace": append([]byte(" "), tc.wire...), "trailing": append(append([]byte{}, tc.wire...), []byte("{}")...),
				"unknown": contentReadTestMutate(t, tc.wire, func(m map[string]any) { m["unknown"] = true }),
				"null":    []byte("null"), "duplicate": append([]byte(`{"duplicate":1,"duplicate":2,`), tc.wire[1:]...),
				"unicode": []byte(`{"invalid":"\ud800"}`), "utf8": {123, 34, 255, 34, 58, 49, 125},
			} {
				if tc.decode(bad) == nil {
					t.Fatalf("accepted %s", name)
				}
			}
		})
	}
	bad := contentReadTestMutate(t, p, func(m map[string]any) {
		m["items"].([]any)[0].(map[string]any)["availability"].(map[string]any)["extra"] = true
	})
	if _, err := DecodeContentManifestPage(bad); err == nil {
		t.Fatal("accepted nested unknown field")
	}
	bad = contentReadTestMutate(t, g, func(m map[string]any) { m["after_sequence"] = "" })
	if _, err := DecodeContentGap(bad); err == nil {
		t.Fatal("accepted present empty optional")
	}
	if _, err := DecodeContentCapabilities(bytes.Repeat([]byte(" "), MaxDocumentBytes+1)); err == nil {
		t.Fatal("accepted oversized capabilities")
	}
	if _, err := DecodeContentManifestPage(bytes.Repeat([]byte(" "), MaxPageBytes+1)); err == nil {
		t.Fatal("accepted oversized page")
	}
}
