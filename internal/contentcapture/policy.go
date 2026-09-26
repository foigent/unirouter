// Package contentcapture owns capture policy and bounded capture buffering. It
// does not grant content access or participate in provider or financial state.
package contentcapture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PolicyPutSchema          = "unirouter.payload-policy.update.v1"
	PolicyViewSchema         = "unirouter.payload-policy.v1"
	ModeDisabled             = "disabled"
	ModeMetadataOnly         = "metadata_only"
	ModeFullContent          = "full_content"
	MaxPolicyJSONBytes       = 64 * 1024
	maxPolicyInteger   int64 = 9007199254740991
)

var (
	ErrInvalidPolicy         = errors.New("invalid or unsupported content capture policy")
	ErrPolicyVersionConflict = errors.New("policy version conflict")
	policyProtocolPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	policyTimePattern        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`)
)

// Optional numeric fields are pointers so presence is distinct from zero.
type PolicyConfig struct {
	Mode               string        `json:"mode"`
	SupportedProtocols []string      `json:"supported_protocols"`
	ObservationPoints  []string      `json:"observation_points"`
	RetentionSeconds   *int64        `json:"retention_seconds,omitempty"`
	QuotaBytes         *int64        `json:"quota_bytes,omitempty"`
	MaxSavedBytes      *int64        `json:"max_saved_bytes,omitempty"`
	AllowedPurposes    []string      `json:"allowed_purposes"`
	OnLimit            string        `json:"on_limit,omitempty"`
	RedactionRef       *RedactionRef `json:"redaction_ref,omitempty"`
}

type RedactionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type PolicyVersion struct {
	PolicyVersion string       `json:"policy_version"`
	Config        PolicyConfig `json:"config"`
}

type PolicyPut struct {
	SchemaVersion         string       `json:"schema_version"`
	ExpectedPolicyVersion string       `json:"expected_policy_version"`
	Desired               PolicyConfig `json:"desired"`
}

type PolicyLimits struct {
	MaxSavedBytes    int64 `json:"max_saved_bytes"`
	MaxReadBytes     int64 `json:"max_read_bytes"`
	MaxBufferBytes   int64 `json:"max_buffer_bytes"`
	MaxManifestBytes int64 `json:"max_manifest_bytes"`
	MaxManifestItems int64 `json:"max_manifest_items"`
}

type PolicyView struct {
	SchemaVersion     string        `json:"schema_version"`
	SourceInstance    string        `json:"source_instance"`
	OrgID             string        `json:"org_id"`
	ProjectID         string        `json:"project_id"`
	PolicyRef         string        `json:"policy_ref"`
	Desired           PolicyVersion `json:"desired"`
	Effective         PolicyVersion `json:"effective"`
	EffectiveAt       string        `json:"effective_at"`
	ApplicationStatus string        `json:"application_status"`
	ApplicationReason string        `json:"application_reason"`
	StorageStatus     string        `json:"storage_status"`
	Limits            PolicyLimits  `json:"limits"`
}

func invalidPolicy(message string) error { return fmt.Errorf("%w: %s", ErrInvalidPolicy, message) }

func (c PolicyConfig) Validate() error {
	if c.Mode != ModeDisabled && c.Mode != ModeMetadataOnly && c.Mode != ModeFullContent {
		return invalidPolicy("mode is unsupported")
	}
	if len(c.SupportedProtocols) == 0 || !uniqueStrings(c.SupportedProtocols) {
		return invalidPolicy("protocols must be nonempty and unique")
	}
	for _, p := range c.SupportedProtocols {
		if len(p) > 128 || !policyProtocolPattern.MatchString(p) {
			return invalidPolicy("invalid protocol")
		}
	}
	if len(c.ObservationPoints) == 0 || len(c.ObservationPoints) > 4 || !uniqueStrings(c.ObservationPoints) {
		return invalidPolicy("invalid observation points")
	}
	for _, p := range c.ObservationPoints {
		switch p {
		case "client_request", "provider_request", "provider_response", "client_response":
		default:
			return invalidPolicy("unsupported observation point")
		}
	}
	if c.AllowedPurposes == nil {
		return invalidPolicy("allowed_purposes is required")
	}
	if c.RedactionRef != nil {
		return invalidPolicy("redaction is unsupported")
	}
	if c.Mode != ModeFullContent {
		if c.RetentionSeconds != nil || c.QuotaBytes != nil || c.MaxSavedBytes != nil || c.OnLimit != "" || len(c.AllowedPurposes) != 0 {
			return invalidPolicy("non-content mode cannot contain storage configuration or purposes")
		}
		return nil
	}
	for _, n := range []*int64{c.RetentionSeconds, c.QuotaBytes, c.MaxSavedBytes} {
		if n == nil || !positivePolicyInteger(*n) {
			return invalidPolicy("finite positive retention and byte limits are required")
		}
	}
	if *c.MaxSavedBytes > *c.QuotaBytes {
		return invalidPolicy("max_saved_bytes exceeds quota_bytes")
	}
	if c.OnLimit != "record_capture_gap" || len(c.AllowedPurposes) != 1 || c.AllowedPurposes[0] != "human_review" {
		return invalidPolicy("content requires human_review and record_capture_gap")
	}
	return nil
}

// ValidateSupport must be called against actual deployment capabilities before
// accepting desired state. PolicyConfig's protocol list is not that authority.
func (c PolicyConfig) ValidateSupport(protocols []string, limits PolicyLimits) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	for _, p := range c.SupportedProtocols {
		found := false
		for _, supported := range protocols {
			if p == supported {
				found = true
				break
			}
		}
		if !found {
			return invalidPolicy("protocol is not supported by this deployment")
		}
	}
	if c.MaxSavedBytes != nil && *c.MaxSavedBytes > limits.MaxSavedBytes {
		return invalidPolicy("max_saved_bytes exceeds deployment limit")
	}
	return nil
}

func (l PolicyLimits) Validate() error {
	for _, n := range []int64{l.MaxSavedBytes, l.MaxReadBytes, l.MaxBufferBytes, l.MaxManifestBytes} {
		if !positivePolicyInteger(n) {
			return invalidPolicy("invalid deployment byte limits")
		}
	}
	if l.MaxReadBytes < l.MaxSavedBytes || l.MaxManifestItems < 1 || l.MaxManifestItems > 10000 {
		return invalidPolicy("invalid deployment limits")
	}
	return nil
}

func (p PolicyVersion) Validate() error {
	if _, err := policyID(p.PolicyVersion); err != nil {
		return err
	}
	return p.Config.Validate()
}

func (p PolicyPut) Validate() error {
	if p.SchemaVersion != PolicyPutSchema {
		return invalidPolicy("invalid update schema")
	}
	if _, err := policyID(p.ExpectedPolicyVersion); err != nil {
		return err
	}
	return p.Desired.Validate()
}

func (p PolicyView) Validate() error {
	if p.SchemaVersion != PolicyViewSchema || !policyText(p.SourceInstance) || !policyText(p.PolicyRef) {
		return invalidPolicy("invalid policy view identity")
	}
	for _, id := range []string{p.OrgID, p.ProjectID} {
		if _, err := policyID(id); err != nil {
			return err
		}
	}
	if err := p.Desired.Validate(); err != nil {
		return err
	}
	if err := p.Effective.Validate(); err != nil {
		return err
	}
	d, _ := policyID(p.Desired.PolicyVersion)
	e, _ := policyID(p.Effective.PolicyVersion)
	if e > d {
		return invalidPolicy("effective version exceeds desired version")
	}
	if !policyTimePattern.MatchString(p.EffectiveAt) {
		return invalidPolicy("effective_at must be UTC with six fractional digits")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.EffectiveAt); err != nil {
		return invalidPolicy("invalid effective_at")
	}
	switch p.ApplicationStatus {
	case "applied":
		if p.ApplicationReason != "none" || !reflect.DeepEqual(p.Desired, p.Effective) {
			return invalidPolicy("applied requires identical desired and effective state")
		}
	case "pending", "failed":
		if e >= d {
			return invalidPolicy("unapplied desired state must have a newer version")
		}
		switch p.ApplicationReason {
		case "awaiting_activation", "storage_unavailable", "invalid_configuration":
		default:
			return invalidPolicy("invalid application reason")
		}
	default:
		return invalidPolicy("invalid application status")
	}
	switch p.StorageStatus {
	case "ready", "not_configured", "unavailable", "capacity_exhausted":
	default:
		return invalidPolicy("invalid storage status")
	}
	return p.Limits.Validate()
}

// TransitionPolicy models only the desired-version CAS. The caller must hold
// the database lock and persist this result atomically. applyNow means storage
// readiness has actually been checked; no I/O, grants, or history are changed.
func TransitionPolicy(current PolicyView, put PolicyPut, applyNow bool, now time.Time) (PolicyView, error) {
	if err := current.Validate(); err != nil {
		return PolicyView{}, err
	}
	if err := put.Validate(); err != nil {
		return PolicyView{}, err
	}
	if put.Desired.MaxSavedBytes != nil && *put.Desired.MaxSavedBytes > current.Limits.MaxSavedBytes {
		return PolicyView{}, invalidPolicy("max_saved_bytes exceeds deployment limit")
	}
	if put.ExpectedPolicyVersion != current.Desired.PolicyVersion {
		return PolicyView{}, ErrPolicyVersionConflict
	}
	v, _ := policyID(current.Desired.PolicyVersion)
	if v >= 9999999999999999999 {
		return PolicyView{}, invalidPolicy("policy version exhausted")
	}
	next := current
	next.Effective.Config = clonePolicyConfig(current.Effective.Config)
	next.Desired = PolicyVersion{PolicyVersion: strconv.FormatUint(v+1, 10), Config: clonePolicyConfig(put.Desired)}
	if put.Desired.Mode != ModeFullContent || applyNow {
		if put.Desired.Mode == ModeFullContent && current.StorageStatus != "ready" {
			return PolicyView{}, invalidPolicy("storage is not ready for immediate activation")
		}
		next.Effective = PolicyVersion{PolicyVersion: next.Desired.PolicyVersion, Config: clonePolicyConfig(next.Desired.Config)}
		next.EffectiveAt = now.UTC().Format("2006-01-02T15:04:05.000000Z")
		next.ApplicationStatus, next.ApplicationReason = "applied", "none"
	} else {
		next.ApplicationStatus, next.ApplicationReason = "pending", "awaiting_activation"
		if current.StorageStatus != "ready" {
			next.ApplicationReason = "storage_unavailable"
		}
	}
	if err := next.Validate(); err != nil {
		return PolicyView{}, err
	}
	return next, nil
}

func DecodePolicyConfig(data []byte) (PolicyConfig, error) {
	var p PolicyConfig
	err := decodePolicyJSON(data, &p)
	if err == nil {
		err = p.Validate()
	}
	return p, err
}
func DecodePolicyVersion(data []byte) (PolicyVersion, error) {
	var p PolicyVersion
	err := decodePolicyJSON(data, &p)
	if err == nil {
		err = p.Validate()
	}
	return p, err
}
func DecodePolicyPut(data []byte) (PolicyPut, error) {
	var p PolicyPut
	err := decodePolicyJSON(data, &p)
	if err == nil {
		err = p.Validate()
	}
	return p, err
}
func DecodePolicyView(data []byte) (PolicyView, error) {
	var p PolicyView
	err := decodePolicyJSON(data, &p)
	if err == nil {
		err = p.Validate()
	}
	return p, err
}

// Token validation rejects duplicate keys, nulls and case variants before
// encoding/json can silently collapse them. The byte and depth limits also
// bound management-input work independently of HTTP middleware.
func decodePolicyJSON(data []byte, out any) error {
	if len(data) > MaxPolicyJSONBytes || !utf8.Valid(data) {
		return invalidPolicy("invalid policy JSON size or UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := policyJSONValue(d, reflect.TypeOf(out).Elem(), 0); err != nil {
		return invalidPolicy(err.Error())
	}
	if _, err := d.Token(); err != io.EOF {
		return invalidPolicy("trailing JSON value")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return invalidPolicy("invalid JSON field type")
	}
	// on_limit is optional in the DTO but cannot be explicitly empty.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if err := checkPolicyPresence(raw, reflect.TypeOf(out).Elem()); err != nil {
		return err
	}
	return nil
}

func policyJSONValue(d *json.Decoder, typ reflect.Type, depth int) error {
	if depth > 8 {
		return errors.New("policy JSON is too deep")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t == nil {
		return errors.New("null is not permitted")
	}
	if typ.Kind() == reflect.Struct {
		if t != json.Delim('{') {
			return errors.New("object required")
		}
		fields := map[string]reflect.StructField{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			f, ok := fields[name]
			if !ok || seen[name] {
				return errors.New("unknown or duplicate field")
			}
			seen[name] = true
			if err := policyJSONValue(d, f.Type, depth+1); err != nil {
				return err
			}
		}
		if _, err := d.Token(); err != nil {
			return err
		}
		for name, f := range fields {
			if !strings.Contains(f.Tag.Get("json"), ",omitempty") && !seen[name] {
				return errors.New("required field missing")
			}
		}
		return nil
	}
	if typ.Kind() == reflect.Slice {
		if t != json.Delim('[') {
			return errors.New("array required")
		}
		for d.More() {
			if err := policyJSONValue(d, typ.Elem(), depth+1); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	}
	if _, ok := t.(json.Delim); ok {
		return errors.New("scalar required")
	}
	return nil
}

func checkPolicyPresence(raw map[string]json.RawMessage, typ reflect.Type) error {
	if typ == reflect.TypeOf(PolicyConfig{}) {
		if value, ok := raw["on_limit"]; ok {
			var onLimit string
			if err := json.Unmarshal(value, &onLimit); err != nil || onLimit != "record_capture_gap" {
				return invalidPolicy("invalid on_limit")
			}
		}
		var mode string
		_ = json.Unmarshal(raw["mode"], &mode)
		if mode == ModeDisabled || mode == ModeMetadataOnly {
			for _, key := range []string{"retention_seconds", "quota_bytes", "max_saved_bytes", "on_limit", "redaction_ref"} {
				if _, ok := raw[key]; ok {
					return invalidPolicy("non-content mode contains storage configuration")
				}
			}
		}
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Struct {
			continue
		}
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw[strings.Split(f.Tag.Get("json"), ",")[0]], &child); err == nil {
			if err := checkPolicyPresence(child, f.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

func policyID(s string) (uint64, error) {
	if len(s) == 0 || len(s) > 19 || s[0] < '1' || s[0] > '9' {
		return 0, invalidPolicy("invalid decimal ID")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, invalidPolicy("invalid decimal ID")
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, invalidPolicy("invalid decimal ID")
	}
	return n, nil
}
func policyText(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 128 {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func positivePolicyInteger(n int64) bool { return n > 0 && n <= maxPolicyInteger }
func uniqueStrings(s []string) bool {
	seen := map[string]bool{}
	for _, v := range s {
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func clonePolicyConfig(c PolicyConfig) PolicyConfig {
	c.SupportedProtocols = append([]string{}, c.SupportedProtocols...)
	c.ObservationPoints = append([]string{}, c.ObservationPoints...)
	c.AllowedPurposes = append([]string{}, c.AllowedPurposes...)
	if c.RetentionSeconds != nil {
		v := *c.RetentionSeconds
		c.RetentionSeconds = &v
	}
	if c.QuotaBytes != nil {
		v := *c.QuotaBytes
		c.QuotaBytes = &v
	}
	if c.MaxSavedBytes != nil {
		v := *c.MaxSavedBytes
		c.MaxSavedBytes = &v
	}
	if c.RedactionRef != nil {
		v := *c.RedactionRef
		c.RedactionRef = &v
	}
	return c
}
