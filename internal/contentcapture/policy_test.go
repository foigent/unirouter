package contentcapture

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func policyNumber(n int64) *int64 { return &n }
func testMetadataPolicy() PolicyConfig {
	return PolicyConfig{Mode: ModeMetadataOnly, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request", "provider_response"}, AllowedPurposes: []string{}}
}
func testFullPolicy() PolicyConfig {
	c := testMetadataPolicy()
	c.Mode = ModeFullContent
	c.RetentionSeconds = policyNumber(60)
	c.QuotaBytes = policyNumber(1024)
	c.MaxSavedBytes = policyNumber(64)
	c.AllowedPurposes = []string{"human_review"}
	c.OnLimit = "record_capture_gap"
	return c
}
func testPolicyView() PolicyView {
	c := testMetadataPolicy()
	return PolicyView{SchemaVersion: PolicyViewSchema, SourceInstance: "local", OrgID: "1", ProjectID: "2", PolicyRef: "project-2", Desired: PolicyVersion{PolicyVersion: "1", Config: c}, Effective: PolicyVersion{PolicyVersion: "1", Config: clonePolicyConfig(c)}, EffectiveAt: "2026-09-15T00:00:00.000000Z", ApplicationStatus: "applied", ApplicationReason: "none", StorageStatus: "ready", Limits: PolicyLimits{MaxSavedBytes: 256, MaxReadBytes: 512, MaxBufferBytes: 32, MaxManifestBytes: 4096, MaxManifestItems: 10}}
}

func TestPolicyStrictDecode(t *testing.T) {
	valid := `{"schema_version":"unirouter.payload-policy.update.v1","expected_policy_version":"1","desired":{"mode":"metadata_only","supported_protocols":["openai.chat"],"observation_points":["client_request"],"allowed_purposes":[]}}`
	if _, err := DecodePolicyPut([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"duplicate root":     strings.Replace(valid, `"expected_policy_version":"1"`, `"expected_policy_version":"1","expected_policy_version":"1"`, 1),
		"duplicate nested":   strings.Replace(valid, `"mode":"metadata_only"`, `"mode":"metadata_only","mode":"disabled"`, 1),
		"case alias":         strings.Replace(valid, `"mode"`, `"Mode"`, 1),
		"unknown":            strings.Replace(valid, `"mode"`, `"extra":false,"mode"`, 1),
		"null optional":      strings.Replace(valid, `"mode"`, `"quota_bytes":null,"mode"`, 1),
		"null required":      strings.Replace(valid, `"allowed_purposes":[]`, `"allowed_purposes":null`, 1),
		"missing required":   strings.Replace(valid, `,"allowed_purposes":[]`, "", 1),
		"empty on limit":     strings.Replace(valid, `"mode"`, `"on_limit":"","mode"`, 1),
		"noncontent storage": strings.Replace(valid, `"mode"`, `"quota_bytes":1,"mode"`, 1),
		"numeric ID":         strings.Replace(valid, `"expected_policy_version":"1"`, `"expected_policy_version":1`, 1),
		"leading zero ID":    strings.Replace(valid, `"expected_policy_version":"1"`, `"expected_policy_version":"01"`, 1),
		"trailing":           valid + `{}`,
		"null root":          `null`,
		"array root":         `[]`,
		"oversize":           valid + strings.Repeat(" ", MaxPolicyJSONBytes),
		"redacted":           strings.Replace(valid, "metadata_only", "redacted", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePolicyPut([]byte(input)); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("accepted invalid input: %v", err)
			}
		})
	}
}

func TestPolicyFullConfiguration(t *testing.T) {
	c := testFullPolicy()
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePolicyConfig(raw)
	if err != nil || !reflect.DeepEqual(c, got) {
		t.Fatalf("round trip: %#v %v", got, err)
	}
	for name, mutate := range map[string]func(*PolicyConfig){
		"missing quota":       func(c *PolicyConfig) { c.QuotaBytes = nil },
		"zero retention":      func(c *PolicyConfig) { c.RetentionSeconds = policyNumber(0) },
		"unbounded quota":     func(c *PolicyConfig) { c.QuotaBytes = policyNumber(maxPolicyInteger + 1) },
		"item above quota":    func(c *PolicyConfig) { c.MaxSavedBytes = policyNumber(1025) },
		"missing purpose":     func(c *PolicyConfig) { c.AllowedPurposes = []string{} },
		"other purpose":       func(c *PolicyConfig) { c.AllowedPurposes = []string{"training"} },
		"duplicate purpose":   func(c *PolicyConfig) { c.AllowedPurposes = []string{"human_review", "human_review"} },
		"duplicate protocols": func(c *PolicyConfig) { c.SupportedProtocols = []string{"a", "a"} },
		"invalid protocol":    func(c *PolicyConfig) { c.SupportedProtocols = []string{"a/b"} },
		"unknown point":       func(c *PolicyConfig) { c.ObservationPoints = []string{"guessed_tool"} },
		"redaction":           func(c *PolicyConfig) { c.RedactionRef = &RedactionRef{ID: "x", Version: "1"} },
		"silent limit":        func(c *PolicyConfig) { c.OnLimit = "drop" },
	} {
		t.Run(name, func(t *testing.T) {
			c := testFullPolicy()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	l := testPolicyView().Limits
	if err := c.ValidateSupport([]string{"openai.chat"}, l); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateSupport([]string{"other"}, l); err == nil {
		t.Fatal("accepted unsupported protocol")
	}
	l.MaxSavedBytes = 32
	if err := c.ValidateSupport([]string{"openai.chat"}, l); err == nil {
		t.Fatal("accepted deployment overlimit")
	}
	l.MaxSavedBytes = 1024
	l.MaxReadBytes = 512
	if err := l.Validate(); err == nil {
		t.Fatal("read limit cannot cover saved representations")
	}
	for _, in := range []string{strings.Replace(string(raw), `"quota_bytes":1024`, `"quota_bytes":1.5`, 1), strings.Replace(string(raw), `"quota_bytes":1024`, `"quota_bytes":9007199254740992`, 1)} {
		if _, err := DecodePolicyConfig([]byte(in)); err == nil {
			t.Fatal("accepted invalid JSON number")
		}
	}
}

func TestDesiredPolicyCASAndIsolation(t *testing.T) {
	current := testPolicyView()
	now := time.Date(2026, 9, 15, 1, 2, 3, 456789000, time.UTC)
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	next, err := TransitionPolicy(current, put, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Desired.PolicyVersion != "2" || next.Effective.PolicyVersion != "1" || next.EffectiveAt != current.EffectiveAt || next.ApplicationStatus != "pending" {
		t.Fatalf("bad pending: %#v", next)
	}
	if !reflect.DeepEqual(next.Effective, current.Effective) {
		t.Fatal("changed historical effective policy")
	}
	if _, err := TransitionPolicy(next, put, true, now); !errors.Is(err, ErrPolicyVersionConflict) {
		t.Fatalf("CAS compared wrong version: %v", err)
	}
	put.ExpectedPolicyVersion = "2"
	put.Desired = testMetadataPolicy()
	put.Desired.Mode = ModeDisabled
	disabled, err := TransitionPolicy(next, put, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.ApplicationStatus != "applied" || disabled.Effective.PolicyVersion != "3" || disabled.Effective.Config.Mode != ModeDisabled || disabled.EffectiveAt != "2026-09-15T01:02:03.456789Z" {
		t.Fatalf("bad disable: %#v", disabled)
	}
	// Results must not alias either caller-owned input or each other.
	next.Desired.Config.SupportedProtocols[0] = "changed"
	*next.Desired.Config.QuotaBytes = 2
	if current.Desired.Config.SupportedProtocols[0] != "openai.chat" || next.Effective.Config.SupportedProtocols[0] != "openai.chat" {
		t.Fatal("result aliases input or effective")
	}
	put = PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	applied, err := TransitionPolicy(current, put, true, now)
	if err != nil {
		t.Fatal(err)
	}
	*applied.Desired.Config.QuotaBytes = 7
	if *put.Desired.QuotaBytes != 1024 || *applied.Effective.Config.QuotaBytes != 1024 {
		t.Fatal("config pointers alias")
	}
	current.StorageStatus = "unavailable"
	if _, err := TransitionPolicy(current, put, true, now); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("applied without storage")
	}
	next, err = TransitionPolicy(current, put, false, now)
	if err != nil || next.ApplicationReason != "storage_unavailable" {
		t.Fatalf("unavailable state: %v", err)
	}
}

func TestPolicyViewAndVersionValidation(t *testing.T) {
	v := testPolicyView()
	data, _ := json.Marshal(v)
	if _, err := DecodePolicyView(data); err != nil {
		t.Fatal(err)
	}
	version, _ := json.Marshal(v.Desired)
	if _, err := DecodePolicyVersion(version); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*PolicyView){
		"bad time":              func(v *PolicyView) { v.EffectiveAt = "2026-09-15T00:00:00Z" },
		"impossible date":       func(v *PolicyView) { v.EffectiveAt = "2026-02-31T00:00:00.000000Z" },
		"wrong applied version": func(v *PolicyView) { v.Desired.PolicyVersion = "2" },
		"wrong applied config":  func(v *PolicyView) { v.Desired.Config.Mode = ModeDisabled },
		"false reason":          func(v *PolicyView) { v.ApplicationReason = "storage_unavailable" },
		"unknown state":         func(v *PolicyView) { v.StorageStatus = "maybe" },
		"zero ID":               func(v *PolicyView) { v.ProjectID = "0" },
		"control text":          func(v *PolicyView) { v.PolicyRef = "a\nb" },
	} {
		t.Run(name, func(t *testing.T) {
			v := testPolicyView()
			change(&v)
			if err := v.Validate(); err == nil {
				t.Fatal("accepted invalid view")
			}
		})
	}
	v.Desired.PolicyVersion = "9999999999999999999"
	v.Effective.PolicyVersion = v.Desired.PolicyVersion
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: v.Desired.PolicyVersion, Desired: testMetadataPolicy()}
	if _, err := TransitionPolicy(v, put, true, time.Now()); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("overflow accepted")
	}
}
