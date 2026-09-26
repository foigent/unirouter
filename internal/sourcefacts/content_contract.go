package sourcefacts

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	ContentProfile                   = "unirouter.interaction-content.v1"
	ContentManifestVersion           = "unirouter.content-manifest.v1"
	ContentReadMetadataVersion       = "unirouter.content-read-metadata.v1"
	ContentByteDomain                = "stored_representation_before_http_content_encoding"
	ContentHumanReview               = "human_review"
	contentMaxSafeInteger      int64 = 9007199254740991
)

// ContentBoundary uses an exclusive stream position, including zero for bootstrap.
type ContentBoundary struct {
	Time          string `json:"time"`
	AfterSequence string `json:"after_sequence"`
}
type ContentAccess struct {
	RegistrationHash     string           `json:"registration_hash"`
	Profile              string           `json:"profile"`
	Enabled              bool             `json:"enabled"`
	ContentScopeRevision string           `json:"content_scope_revision"`
	AllowedPurposes      []string         `json:"allowed_purposes"`
	CoverageFrom         *ContentBoundary `json:"coverage_from,omitempty"`
}
type ContentLimits struct {
	MaxSavedBytes    int64 `json:"max_saved_bytes"`
	MaxReadBytes     int64 `json:"max_read_bytes"`
	MaxBufferBytes   int64 `json:"max_buffer_bytes"`
	MaxManifestBytes int64 `json:"max_manifest_bytes"`
	MaxManifestItems int   `json:"max_manifest_items"`
}
type ContentCapture struct {
	Mode              string `json:"mode"`
	PolicyRef         string `json:"policy_ref"`
	PolicyVersion     string `json:"policy_version"`
	PolicyEffectiveAt string `json:"policy_effective_at"`
	Completeness      string `json:"completeness"`
	Reason            string `json:"reason"`
}
type ContentRedactionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type ContentRepresentation struct {
	MediaType    string               `json:"media_type"`
	Encoding     string               `json:"encoding"`
	ByteDomain   string               `json:"byte_domain"`
	SavedBytes   int64                `json:"saved_bytes"`
	Digest       string               `json:"digest"`
	RedactionRef *ContentRedactionRef `json:"redaction_ref,omitempty"`
}
type ContentManifest struct {
	SchemaVersion    string                 `json:"schema_version"`
	SourceInstance   string                 `json:"source_instance"`
	OrgID            string                 `json:"org_id"`
	ProjectID        string                 `json:"project_id"`
	RequestID        string                 `json:"request_id"`
	Profile          string                 `json:"profile"`
	SourceFactID     string                 `json:"source_fact_id"`
	Revision         string                 `json:"revision"`
	EventID          string                 `json:"event_id"`
	PreviousRevision string                 `json:"previous_revision,omitempty"`
	PreviousHash     string                 `json:"previous_hash,omitempty"`
	CorrectionReason string                 `json:"correction_reason,omitempty"`
	OccurredAt       string                 `json:"occurred_at"`
	RecordedAt       string                 `json:"recorded_at"`
	Protocol         string                 `json:"protocol"`
	ObservationPoint string                 `json:"observation_point"`
	AttemptID        string                 `json:"attempt_id,omitempty"`
	ContentItemID    string                 `json:"content_item_id"`
	Capture          ContentCapture         `json:"capture"`
	ContentRef       string                 `json:"content_ref,omitempty"`
	Representation   *ContentRepresentation `json:"representation,omitempty"`
	CapturedAt       string                 `json:"captured_at,omitempty"`
	ExpiresAt        string                 `json:"expires_at,omitempty"`
}
type ContentReadMetadata struct {
	SchemaVersion  string                `json:"schema_version"`
	SourceInstance string                `json:"source_instance"`
	OrgID          string                `json:"org_id"`
	ProjectID      string                `json:"project_id"`
	RequestID      string                `json:"request_id"`
	SourceFactID   string                `json:"source_fact_id"`
	Revision       string                `json:"revision"`
	SourceSHA256   string                `json:"source_sha256"`
	ContentRef     string                `json:"content_ref"`
	Representation ContentRepresentation `json:"representation"`
	ExpiresAt      string                `json:"expires_at"`
}

var contentProtocolPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var contentMediaPattern = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+$`)
var contentRefPattern = regexp.MustCompile(`^uc1_[a-f0-9]{64}$`)
var contentFactPattern = regexp.MustCompile(`^uf1_[a-f0-9]{64}$`)

func contentText(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func contentSequence(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n >= 0 && strconv.FormatInt(n, 10) == s
}
func (b ContentBoundary) Validate() error {
	if eventconnector.ValidateCanonicalMicrosecondTime(b.Time) != nil || !contentSequence(b.AfterSequence) {
		return errors.New("invalid content boundary")
	}
	return nil
}
func (a ContentAccess) Validate() error {
	if !validHash(a.RegistrationHash) || a.Profile != ContentProfile || !contentText(a.ContentScopeRevision, 128) || a.AllowedPurposes == nil {
		return errors.New("invalid content access")
	}
	if a.Enabled {
		if len(a.AllowedPurposes) != 1 || a.AllowedPurposes[0] != ContentHumanReview || a.CoverageFrom == nil {
			return errors.New("enabled content access requires purpose and coverage")
		}
	} else if len(a.AllowedPurposes) != 0 {
		return errors.New("disabled content access has purposes")
	}
	if a.CoverageFrom != nil {
		return a.CoverageFrom.Validate()
	}
	return nil
}
func (l ContentLimits) Validate() error {
	for _, v := range []int64{l.MaxSavedBytes, l.MaxReadBytes, l.MaxBufferBytes, l.MaxManifestBytes} {
		if v <= 0 || v > contentMaxSafeInteger {
			return errors.New("invalid content byte limit")
		}
	}
	if l.MaxManifestItems < 1 || l.MaxManifestItems > 10000 {
		return errors.New("invalid content item limit")
	}
	return nil
}
func (c ContentCapture) Validate() error {
	if !oneOf(c.Mode, "disabled", "metadata_only", "full_content", "redacted") || !contentText(c.PolicyRef, 128) || !positiveID(c.PolicyVersion) || eventconnector.ValidateCanonicalMicrosecondTime(c.PolicyEffectiveAt) != nil {
		return errors.New("invalid content capture policy")
	}
	valid := false
	switch c.Completeness {
	case "complete":
		valid = c.Reason == "none"
	case "pending":
		valid = c.Reason == "capture_in_progress"
	case "partial":
		valid = oneOf(c.Reason, "limit_exceeded", "queue_full", "storage_write_failed", "stream_interrupted", "request_cancelled", "output_held")
	case "not_captured":
		valid = oneOf(c.Reason, "policy_metadata_only", "policy_disabled", "scope_excluded", "unsupported_protocol", "limit_exceeded", "queue_full", "storage_write_failed", "not_applicable", "capture_unavailable")
	}
	if !valid || (c.Mode == "disabled" && (c.Completeness != "not_captured" || c.Reason != "policy_disabled")) || (c.Mode == "metadata_only" && (c.Completeness != "not_captured" || c.Reason != "policy_metadata_only")) {
		return errors.New("invalid content capture state")
	}
	return nil
}
func (r ContentRepresentation) Validate() error {
	if len(r.MediaType) > 255 || !contentMediaPattern.MatchString(r.MediaType) || r.Encoding != "identity" || r.ByteDomain != ContentByteDomain || r.SavedBytes < 0 || r.SavedBytes > contentMaxSafeInteger || !validHash(r.Digest) {
		return errors.New("invalid content representation")
	}
	if r.RedactionRef != nil && (!contentText(r.RedactionRef.ID, 128) || !contentText(r.RedactionRef.Version, 128)) {
		return errors.New("invalid content redaction reference")
	}
	return nil
}

// ValidateBytes hashes the saved byte domain without JSON parsing, transcoding, or decompression.
func (r ContentRepresentation) ValidateBytes(body []byte) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if int64(len(body)) != r.SavedBytes || eventconnector.HashSourceBytes(body) != r.Digest {
		return errors.New("content representation mismatch")
	}
	return nil
}
func (m ContentManifest) FactID() string {
	return sourceID(ContentManifestVersion, m.SourceInstance, m.OrgID, m.ProjectID, ContentProfile, m.RequestID, m.ObservationPoint, m.AttemptID, m.ContentItemID)
}
func (m ContentManifest) EventIDForRevision() string {
	return sourceID(ContentManifestVersion+".revision", m.FactID(), m.Revision)
}
func (m ContentManifest) Validate() error {
	if m.SchemaVersion != ContentManifestVersion || m.Profile != ContentProfile || !contentText(m.SourceInstance, 128) || !positiveID(m.OrgID) || !positiveID(m.ProjectID) || !contentText(m.RequestID, 64) || !positiveID(m.Revision) {
		return errors.New("invalid content manifest scope")
	}
	if !contentText(m.ContentItemID, 128) || (m.AttemptID != "" && !contentText(m.AttemptID, 128)) || !oneOf(m.ObservationPoint, "client_request", "provider_request", "provider_response", "client_response") || (oneOf(m.ObservationPoint, "provider_request", "provider_response") && m.AttemptID == "") {
		return errors.New("invalid content observation identity")
	}
	if m.SourceFactID != m.FactID() || m.EventID != m.EventIDForRevision() || len(m.Protocol) > 128 || !contentProtocolPattern.MatchString(m.Protocol) {
		return errors.New("invalid content manifest identity")
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(m.OccurredAt) != nil || eventconnector.ValidateCanonicalMicrosecondTime(m.RecordedAt) != nil {
		return errors.New("invalid content manifest time")
	}
	if m.Revision == "1" {
		if m.PreviousRevision != "" || m.PreviousHash != "" || m.CorrectionReason != "" {
			return errors.New("initial content manifest has predecessor")
		}
	} else {
		prev, _ := strconv.ParseInt(m.PreviousRevision, 10, 64)
		rev, _ := strconv.ParseInt(m.Revision, 10, 64)
		if !positiveID(m.PreviousRevision) || prev != rev-1 || !validHash(m.PreviousHash) || !contentText(m.CorrectionReason, 128) {
			return errors.New("invalid content predecessor")
		}
	}
	if err := m.Capture.Validate(); err != nil {
		return err
	}
	if !oneOf(m.Capture.Completeness, "complete", "partial") {
		if m.ContentRef != "" || m.Representation != nil || m.CapturedAt != "" || m.ExpiresAt != "" {
			return errors.New("uncaptured content has readable reference")
		}
		return nil
	}
	if !contentRefPattern.MatchString(m.ContentRef) || m.Representation == nil || eventconnector.ValidateCanonicalMicrosecondTime(m.CapturedAt) != nil || eventconnector.ValidateCanonicalMicrosecondTime(m.ExpiresAt) != nil {
		return errors.New("captured content requires representation and times")
	}
	if err := m.Representation.Validate(); err != nil {
		return err
	}
	if (m.Capture.Mode == "redacted") != (m.Representation.RedactionRef != nil) {
		return errors.New("content redaction mode mismatch")
	}
	return nil
}

// ValidateContentPredecessor checks the actual complete canonical predecessor, not a supplied hash alone.
func ValidateContentPredecessor(current, previous ContentManifest) error {
	if err := current.Validate(); err != nil {
		return err
	}
	wire, err := EncodeContentManifest(previous)
	if err != nil {
		return err
	}
	if current.SourceFactID != previous.SourceFactID || current.PreviousRevision != previous.Revision || current.PreviousHash != eventconnector.HashSourceBytes(wire) {
		return errors.New("content predecessor binding mismatch")
	}
	return nil
}
func (m ContentReadMetadata) Validate() error {
	if m.SchemaVersion != ContentReadMetadataVersion || !contentText(m.SourceInstance, 128) || !positiveID(m.OrgID) || !positiveID(m.ProjectID) || !contentText(m.RequestID, 64) || !positiveID(m.Revision) || !contentFactPattern.MatchString(m.SourceFactID) || !validHash(m.SourceSHA256) || !contentRefPattern.MatchString(m.ContentRef) || eventconnector.ValidateCanonicalMicrosecondTime(m.ExpiresAt) != nil {
		return errors.New("invalid content read metadata")
	}
	return m.Representation.Validate()
}

// ReadMetadata binds a read to a specific immutable manifest revision. Authorization is checked by the caller.
func (m ContentManifest) ReadMetadata() (ContentReadMetadata, error) {
	wire, err := EncodeContentManifest(m)
	if err != nil {
		return ContentReadMetadata{}, err
	}
	if m.Representation == nil {
		return ContentReadMetadata{}, errors.New("content has no readable representation")
	}
	return ContentReadMetadata{SchemaVersion: ContentReadMetadataVersion, SourceInstance: m.SourceInstance, OrgID: m.OrgID, ProjectID: m.ProjectID, RequestID: m.RequestID, SourceFactID: m.SourceFactID, Revision: m.Revision, SourceSHA256: eventconnector.HashSourceBytes(wire), ContentRef: m.ContentRef, Representation: *m.Representation, ExpiresAt: m.ExpiresAt}, nil
}
func encodeContentCanonical(value interface{ Validate() error }) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var obj any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}
	obj, err = contentCanonicalIntegers(obj)
	if err != nil {
		return nil, err
	}
	canonical, canonicalErr := eventconnector.CanonicalJSON(obj)
	if canonicalErr != nil {
		return nil, canonicalErr
	}
	if len(canonical) > MaxDocumentBytes {
		return nil, errors.New("content metadata exceeds byte limit")
	}
	return canonical, nil
}
func decodeContentCanonical(wire []byte, value interface{ Validate() error }) error {
	if len(wire) > MaxDocumentBytes {
		return errors.New("content metadata exceeds byte limit")
	}
	if err := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	canonical, err := encodeContentCanonical(value)
	if err != nil {
		return err
	}
	// Reencoding also detects absent required zero-valued fields and explicitly empty optional fields.
	if !bytes.Equal(wire, canonical) {
		return fmt.Errorf("content metadata is not canonical")
	}
	return nil
}
func EncodeContentManifest(m ContentManifest) ([]byte, error) { return encodeContentCanonical(m) }
func DecodeContentManifest(wire []byte) (ContentManifest, error) {
	var m ContentManifest
	err := decodeContentCanonical(wire, &m)
	return m, err
}
func EncodeContentReadMetadata(m ContentReadMetadata) ([]byte, error) {
	return encodeContentCanonical(m)
}
func DecodeContentReadMetadata(wire []byte) (ContentReadMetadata, error) {
	var m ContentReadMetadata
	err := decodeContentCanonical(wire, &m)
	return m, err
}
func EncodeContentReadMetadataHeader(m ContentReadMetadata) (string, error) {
	wire, err := EncodeContentReadMetadata(m)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(wire), nil
}
func DecodeContentReadMetadataHeader(raw string) (ContentReadMetadata, error) {
	if len(raw) > base64.RawURLEncoding.EncodedLen(MaxDocumentBytes) {
		return ContentReadMetadata{}, errors.New("content metadata header exceeds byte limit")
	}
	wire, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || base64.RawURLEncoding.EncodeToString(wire) != raw {
		return ContentReadMetadata{}, errors.New("invalid content metadata header")
	}
	return DecodeContentReadMetadata(wire)
}

// CanonicalJSON accepts int64 rather than json.Number. Preserve exact integer
// values when bridging the DTO encoding; never round through float64.
func contentCanonicalIntegers(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil || n < -contentMaxSafeInteger || n > contentMaxSafeInteger {
			return nil, errors.New("invalid content integer")
		}
		return n, nil
	case map[string]any:
		for key, item := range v {
			normalized, err := contentCanonicalIntegers(item)
			if err != nil {
				return nil, err
			}
			v[key] = normalized
		}
	case []any:
		for i, item := range v {
			normalized, err := contentCanonicalIntegers(item)
			if err != nil {
				return nil, err
			}
			v[i] = normalized
		}
	}
	return value, nil
}
