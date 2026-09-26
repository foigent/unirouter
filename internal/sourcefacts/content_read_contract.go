package sourcefacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	ContentCapabilitiesVersion = "unirouter.content-capabilities.v1"
	ContentManifestPageVersion = "unirouter.content-manifests-page.v1"
	ContentCapabilitiesPath    = "/api/v1/integrations/xinsight/source/v2/content-capabilities"
	ContentManifestsPath       = "/api/v1/integrations/xinsight/source/v2/content-manifests"
	ContentEndpointTemplate    = "/api/v1/integrations/xinsight/source/v2/contents/{content_ref}"
)

// ContentCapabilities is a wire contract, not a declaration of deployment support.
// Callers must populate capabilities and limits from their actual implementation.
type ContentCapabilities struct {
	SchemaVersion        string           `json:"schema_version"`
	Profile              string           `json:"profile"`
	SourceInstance       string           `json:"source_instance"`
	OrgID                string           `json:"org_id"`
	ProjectID            string           `json:"project_id"`
	Status               string           `json:"status"`
	CredentialStatus     string           `json:"credential_status"`
	SupportedProtocols   []string         `json:"supported_protocols"`
	ObservationPoints    []string         `json:"observation_points"`
	CaptureModes         []string         `json:"capture_modes"`
	AllowedPurposes      []string         `json:"allowed_purposes"`
	ContentScopeRevision string           `json:"content_scope_revision"`
	RecoveryEpoch        string           `json:"recovery_epoch"`
	CoverageFrom         ContentBoundary  `json:"coverage_from"`
	RetentionFloor       ContentBoundary  `json:"retention_floor"`
	Limits               ContentLimits    `json:"limits"`
	Endpoints            ContentEndpoints `json:"endpoints"`
}
type ContentEndpoints struct {
	Capabilities string `json:"capabilities"`
	Manifests    string `json:"manifests"`
	Content      string `json:"content"`
}
type ContentAvailability struct {
	State       string `json:"state"`
	EvaluatedAt string `json:"evaluated_at"`
	Retryable   bool   `json:"retryable"`
}
type ContentManifestEntry struct {
	Sequence     string              `json:"sequence"`
	Document     ContentManifest     `json:"document"`
	SourceSHA256 string              `json:"source_sha256"`
	Availability ContentAvailability `json:"availability"`
}
type ContentGap struct {
	Reason          string `json:"reason"`
	RangeKnown      bool   `json:"range_known"`
	AfterSequence   string `json:"after_sequence,omitempty"`
	ThroughSequence string `json:"through_sequence,omitempty"`
}
type ContentManifestPage struct {
	SchemaVersion         string                 `json:"schema_version"`
	Profile               string                 `json:"profile"`
	SourceInstance        string                 `json:"source_instance"`
	OrgID                 string                 `json:"org_id"`
	ProjectID             string                 `json:"project_id"`
	GeneratedAt           string                 `json:"generated_at"`
	ContentScopeRevision  string                 `json:"content_scope_revision"`
	RecoveryEpoch         string                 `json:"recovery_epoch"`
	CoverageFrom          ContentBoundary        `json:"coverage_from"`
	RetentionFloor        ContentBoundary        `json:"retention_floor"`
	AfterSequence         string                 `json:"after_sequence"`
	LastSequence          string                 `json:"last_sequence"`
	HighWatermarkSequence string                 `json:"high_watermark_sequence"`
	NextCursor            string                 `json:"next_cursor"`
	HighWatermark         string                 `json:"high_watermark"`
	HasMore               bool                   `json:"has_more"`
	Coverage              string                 `json:"coverage"`
	Gaps                  []ContentGap           `json:"gaps"`
	Items                 []ContentManifestEntry `json:"items"`
}

func (e ContentEndpoints) Validate() error {
	if e.Capabilities != ContentCapabilitiesPath || e.Manifests != ContentManifestsPath || e.Content != ContentEndpointTemplate {
		return errors.New("invalid content endpoints")
	}
	return nil
}
func (c ContentCapabilities) Validate() error {
	if c.SchemaVersion != ContentCapabilitiesVersion || c.Profile != ContentProfile || !contentReadScope(c.SourceInstance, c.OrgID, c.ProjectID, c.ContentScopeRevision, c.RecoveryEpoch) || c.Status != "active" || c.CredentialStatus != "feed_active" {
		return errors.New("invalid content capabilities scope")
	}
	if !contentReadUnique(c.SupportedProtocols, 1, 0) || !contentReadUnique(c.ObservationPoints, 1, 4) || !contentReadUnique(c.CaptureModes, 2, 4) || len(c.AllowedPurposes) != 1 || c.AllowedPurposes[0] != ContentHumanReview {
		return errors.New("invalid content capabilities sets")
	}
	for _, p := range c.SupportedProtocols {
		if len(p) > 128 || !contentProtocolPattern.MatchString(p) {
			return errors.New("invalid content protocol")
		}
	}
	for _, p := range c.ObservationPoints {
		if !oneOf(p, "client_request", "provider_request", "provider_response", "client_response") {
			return errors.New("invalid content observation point")
		}
	}
	disabled, metadata := false, false
	for _, m := range c.CaptureModes {
		if !oneOf(m, "disabled", "metadata_only", "full_content", "redacted") {
			return errors.New("invalid content capture mode")
		}
		disabled = disabled || m == "disabled"
		metadata = metadata || m == "metadata_only"
	}
	if !disabled || !metadata {
		return errors.New("content capabilities require disabled and metadata_only")
	}
	for _, v := range []interface{ Validate() error }{c.CoverageFrom, c.RetentionFloor, c.Limits, c.Endpoints} {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (a ContentAvailability) Validate() error {
	if !oneOf(a.State, "available", "pending", "not_captured", "expired", "deleted", "missing", "corrupt", "source_unavailable") || eventconnector.ValidateCanonicalMicrosecondTime(a.EvaluatedAt) != nil || a.Retryable != oneOf(a.State, "pending", "source_unavailable") {
		return errors.New("invalid content availability")
	}
	return nil
}
func (e ContentManifestEntry) Validate() error {
	if !positiveID(e.Sequence) || !validHash(e.SourceSHA256) {
		return errors.New("invalid content manifest entry")
	}
	wire, err := EncodeContentManifest(e.Document)
	if err != nil {
		return err
	}
	if eventconnector.HashSourceBytes(wire) != e.SourceSHA256 {
		return errors.New("content manifest entry hash mismatch")
	}
	return e.Availability.Validate()
}
func (g ContentGap) Validate() error {
	if !oneOf(g.Reason, "retention_floor_ahead", "manifest_write_unavailable", "missing_predecessor", "source_recovery", "capture_health_unknown") {
		return errors.New("invalid content gap reason")
	}
	if g.RangeKnown {
		if !contentSequence(g.AfterSequence) || !contentSequence(g.ThroughSequence) {
			return errors.New("known content gap requires bounds")
		}
	} else if g.AfterSequence != "" || g.ThroughSequence != "" {
		return errors.New("unknown content gap has bounds")
	}
	return nil
}

var contentReadCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

func (p ContentManifestPage) Validate() error {
	if p.SchemaVersion != ContentManifestPageVersion || p.Profile != ContentProfile || !contentReadScope(p.SourceInstance, p.OrgID, p.ProjectID, p.ContentScopeRevision, p.RecoveryEpoch) || eventconnector.ValidateCanonicalMicrosecondTime(p.GeneratedAt) != nil {
		return errors.New("invalid content manifest page scope")
	}
	if err := p.CoverageFrom.Validate(); err != nil {
		return err
	}
	if err := p.RetentionFloor.Validate(); err != nil {
		return err
	}
	if !contentSequence(p.AfterSequence) || !contentSequence(p.LastSequence) || !contentSequence(p.HighWatermarkSequence) || !contentReadCursorPattern.MatchString(p.NextCursor) || !contentReadCursorPattern.MatchString(p.HighWatermark) {
		return errors.New("invalid content manifest page positions")
	}
	if p.Items == nil || p.Gaps == nil || !oneOf(p.Coverage, "bounded", "partial", "unknown") || (p.Coverage == "bounded") != (len(p.Gaps) == 0) {
		return errors.New("invalid content manifest page coverage")
	}
	for _, g := range p.Gaps {
		if err := g.Validate(); err != nil {
			return err
		}
	}
	after, _ := strconv.ParseInt(p.AfterSequence, 10, 64)
	last, _ := strconv.ParseInt(p.LastSequence, 10, 64)
	high, _ := strconv.ParseInt(p.HighWatermarkSequence, 10, 64)
	previous := after
	for _, item := range p.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		m := item.Document
		if m.SourceInstance != p.SourceInstance || m.OrgID != p.OrgID || m.ProjectID != p.ProjectID || m.Profile != p.Profile {
			return errors.New("content manifest page member scope mismatch")
		}
		sequence, _ := strconv.ParseInt(item.Sequence, 10, 64)
		if sequence <= previous || sequence > high {
			return errors.New("content manifest page sequence order mismatch")
		}
		previous = sequence
	}
	if last != previous || last > high || (p.HasMore && last >= high) {
		return errors.New("content manifest page watermark mismatch")
	}
	return nil
}

// Numeric strings retain the existing UR signed int64 implementation boundary.
// C0 can express larger 19-digit strings; these DTOs do not extend DB/cursor limits.
func contentReadScope(instance, org, project, generation, epoch string) bool {
	return contentText(instance, 128) && positiveID(org) && positiveID(project) && contentText(generation, 128) && positiveID(epoch)
}
func contentReadUnique(values []string, min, max int) bool {
	if len(values) < min || (max > 0 && len(values) > max) {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

func EncodeContentCapabilities(c ContentCapabilities) ([]byte, error) {
	return encodeContentCanonical(c)
}
func DecodeContentCapabilities(wire []byte) (ContentCapabilities, error) {
	var c ContentCapabilities
	err := decodeContentCanonical(wire, &c)
	return c, err
}
func EncodeContentAvailability(a ContentAvailability) ([]byte, error) {
	return encodeContentCanonical(a)
}
func DecodeContentAvailability(wire []byte) (ContentAvailability, error) {
	var a ContentAvailability
	err := decodeContentCanonical(wire, &a)
	return a, err
}
func EncodeContentManifestEntry(e ContentManifestEntry) ([]byte, error) {
	return encodeContentReadPageCanonical(e)
}
func DecodeContentManifestEntry(wire []byte) (ContentManifestEntry, error) {
	var e ContentManifestEntry
	err := decodeContentReadPageCanonical(wire, &e)
	return e, err
}
func EncodeContentGap(g ContentGap) ([]byte, error) { return encodeContentCanonical(g) }
func DecodeContentGap(wire []byte) (ContentGap, error) {
	var g ContentGap
	err := decodeContentCanonical(wire, &g)
	return g, err
}
func EncodeContentManifestPage(p ContentManifestPage) ([]byte, error) {
	return encodeContentReadPageCanonical(p)
}
func DecodeContentManifestPage(wire []byte) (ContentManifestPage, error) {
	var p ContentManifestPage
	err := decodeContentReadPageCanonical(wire, &p)
	return p, err
}

// Pages use the existing page ceiling; each immutable member is independently
// checked with EncodeContentManifest's document ceiling. The HTTP layer must also
// enforce smaller deployment manifest byte/item limits. Cursor validation here
// is lexical only; the HTTP authority must verify signatures and live bindings.
func encodeContentReadPageCanonical(value interface{ Validate() error }) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(wire) > MaxPageBytes {
		return nil, errors.New("content page exceeds byte limit")
	}
	var object any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err = decoder.Decode(&object); err != nil {
		return nil, err
	}
	object, err = contentCanonicalIntegers(object)
	if err != nil {
		return nil, err
	}
	canonical, canonicalErr := eventconnector.CanonicalJSON(object)
	if canonicalErr != nil {
		return nil, canonicalErr
	}
	if len(canonical) > MaxPageBytes {
		return nil, errors.New("content page exceeds byte limit")
	}
	return canonical, nil
}
func decodeContentReadPageCanonical(wire []byte, value interface{ Validate() error }) error {
	if len(wire) > MaxPageBytes {
		return errors.New("content page exceeds byte limit")
	}
	if err := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	canonical, err := encodeContentReadPageCanonical(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(wire, canonical) {
		return errors.New("content page is not canonical")
	}
	return nil
}
